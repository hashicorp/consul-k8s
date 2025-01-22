// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	FinalizerName                = "finalizers.consul.hashicorp.com"
	ConsulAgentError             = "ConsulAgentError"
	ConsulPatchError             = "ConsulPatchError"
	ExternallyManagedConfigError = "ExternallyManagedConfigError"
	MigrationFailedError         = "MigrationFailedError"
)

// Controller is implemented by CRD-specific configentries. It is used by
// ConfigEntryController to abstract CRD-specific configentries.
type Controller interface {
	// AddFinalizersPatch creates a patch with the original finalizers with new ones appended to the end.
	AddFinalizersPatch(obj client.Object, finalizers ...string) *FinalizerPatch
	// RemoveFinalizersPatch creates a patch to remove a set of finalizers, while preserving the order.
	RemoveFinalizersPatch(obj client.Object, finalizers ...string) *FinalizerPatch
	// Patch patches the object. This should only ever be used for updating the metadata of an object, and not object
	// spec or status. Updating the spec could have unintended consequences such as defaulting zero values.
	Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
	// UpdateStatus updates the state of just the object's status.
	UpdateStatus(context.Context, client.Object, ...client.SubResourceUpdateOption) error
	// Get retrieves an obj for the given object key from the Kubernetes Cluster.
	// obj must be a struct pointer so that obj can be updated with the response
	// returned by the Server.
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	// Logger returns a logger with values added for the specific controller
	// and request name.
	Logger(types.NamespacedName) logr.Logger
}

// ConfigEntryController is a generic controller that is used to reconcile
// all config entry types, e.g. ServiceDefaults, ServiceResolver, etc, since
// they share the same reconcile behaviour.
type ConfigEntryController struct {
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config

	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager

	// DatacenterName indicates the Consul Datacenter name the controller is
	// operating in. Adds this value as metadata on managed resources.
	DatacenterName string

	// EnableConsulNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which supports namespaces.
	EnableConsulNamespaces bool

	// ConsulDestinationNamespace is the name of the Consul namespace to create
	// all config entries in. If EnableNSMirroring is true this is ignored.
	ConsulDestinationNamespace string

	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool

	// NSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	NSMirroringPrefix string

	// ConsulPartition indicates the Consul Admin Partition name the controller is
	// operating in. Adds this value as metadata on managed resources.
	ConsulPartition string

	// CrossNSACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNSACLPolicy string
}

// ReconcileEntry reconciles an update to a resource. CRD-specific controller's
// call this function because it handles reconciliation of config entries
// generically.
// CRD-specific controller should pass themselves in as updater since we
// need to call back into their own update methods to ensure they update their
// internal state.
func (r *ConfigEntryController) ReconcileEntry(ctx context.Context, crdCtrl Controller, req ctrl.Request, configEntry common.ConfigEntryResource) (ctrl.Result, error) {
	logger := crdCtrl.Logger(req.NamespacedName)
	err := crdCtrl.Get(ctx, req.NamespacedName, configEntry)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Create Consul client for this reconcile.
	serverState, err := r.ConsulServerConnMgr.State()
	if err != nil {
		logger.Error(err, "failed to get Consul server state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	consulClient, err := consul.NewClientFromConnMgrState(r.ConsulClientConfig, serverState)
	if err != nil {
		logger.Error(err, "failed to create Consul API client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	consulEntry := configEntry.ToConsul(r.DatacenterName)

	if configEntry.GetDeletionTimestamp().IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then let's add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(configEntry.GetFinalizers(), FinalizerName) {
			addPatch := crdCtrl.AddFinalizersPatch(configEntry, FinalizerName)
			err := crdCtrl.Patch(ctx, configEntry, addPatch)
			if err != nil {
				return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulPatchError,
					fmt.Errorf("adding finalizer: %w", err))
			}

			if err := r.syncUnknown(ctx, crdCtrl, configEntry); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(configEntry.GetFinalizers(), FinalizerName) {
			logger.Info("deletion event")
			// Check to see if consul has config entry with the same name
			entry, _, err := consulClient.ConfigEntries().Get(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.QueryOptions{
				Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
			})

			// Ignore the error where the config entry isn't found in Consul.
			// It is indicative of desired state.
			if err != nil && !isNotFoundErr(err) {
				return ctrl.Result{}, fmt.Errorf("getting config entry from consul: %w", err)
			} else if err == nil {
				// Only delete the resource from Consul if it is owned by our datacenter.
				if entry.GetMeta()[common.DatacenterKey] == r.DatacenterName {
					_, err := consulClient.ConfigEntries().Delete(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.WriteOptions{
						Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
					})
					if err != nil {
						return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
							fmt.Errorf("deleting config entry from consul: %w", err))
					}
					logger.Info("deletion from Consul successful")
				} else {
					logger.Info("config entry in Consul was created in another datacenter - skipping delete from Consul", "external-datacenter", entry.GetMeta()[common.DatacenterKey])
				}
			}
			// remove our finalizer from the list and update it.
			removePatch := crdCtrl.RemoveFinalizersPatch(configEntry, FinalizerName)
			if err := crdCtrl.Patch(ctx, configEntry, removePatch); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("finalizer removed")
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Check to see if consul has config entry with the same name
	entryFromConsul, _, err := consulClient.ConfigEntries().Get(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.QueryOptions{
		Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
	})
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("config entry not found in consul")

		// If Consul namespaces are enabled we may need to create the
		// destination consul namespace first.
		if r.EnableConsulNamespaces {
			consulNS := r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource())
			created, err := namespaces.EnsureExists(consulClient, consulNS, r.CrossNSACLPolicy)
			if err != nil {
				return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
					fmt.Errorf("creating consul namespace %q: %w", consulNS, err))
			}
			if created {
				logger.Info("consul namespace created", "ns", consulNS)
			}
		}

		// Create the config entry
		_, writeMeta, err := consulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
			Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
		})
		if err != nil {
			return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("writing config entry to consul: %w", err))
		}

		logger.Info("config entry created", "request-time", writeMeta.RequestTime)
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	}

	// If there is an error when trying to get the config entry from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError, err)
	}

	sourceDatacenter := entryFromConsul.GetMeta()[common.DatacenterKey]
	managedByThisDC := sourceDatacenter == r.DatacenterName
	// Check if the config entry is managed by our datacenter.
	// Do not process resource if the entry was not created within our datacenter
	// as it was created in a different cluster which will be managing that config entry.
	matchesConsul := configEntry.MatchesConsul(entryFromConsul)
	// Note that there is a special case where we will migrate a config entry
	// that wasn't created by the controller if it has the migrate-entry annotation set to true.
	// This functionality exists to help folks who are upgrading from older helm
	// chart versions where they had previously created config entries themselves but
	// now want to manage them through custom resources.
	hasMigrationKey := configEntry.GetObjectMeta().Annotations[common.MigrateEntryKey] == common.MigrateEntryTrue

	switch {
	case !matchesConsul && !managedByThisDC && !hasMigrationKey:
		return r.syncFailed(ctx, logger, crdCtrl, configEntry, ExternallyManagedConfigError,
			sourceDatacenterMismatchErr(sourceDatacenter))
	case !matchesConsul && hasMigrationKey:
		// If we're migrating this config entry but the custom resource
		// doesn't match what's in Consul currently we error out so that
		// it doesn't overwrite something accidentally.
		return r.syncFailed(ctx, logger, crdCtrl, configEntry, MigrationFailedError,
			r.nonMatchingMigrationError(configEntry, entryFromConsul))
	case !matchesConsul:
		logger.Info("config entry does not match consul", "modify-index", entryFromConsul.GetModifyIndex())
		_, writeMeta, err := consulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
			Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		logger.Info("config entry updated", "request-time", writeMeta.RequestTime)
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	case hasMigrationKey && !managedByThisDC:
		// If we get here then we're doing a migration and the entry in Consul
		// matches the entry in Kubernetes. We just need to update the metadata
		// of the entry in Consul to say that it's now managed by Kubernetes.
		logger.Info("migrating config entry to be managed by Kubernetes")
		_, writeMeta, err := consulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
			Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		logger.Info("config entry migrated", "request-time", writeMeta.RequestTime)
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	case configEntry.SyncedConditionStatus() != corev1.ConditionTrue:
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	}

	// For resolvers and splitters, we need to set the ClusterIP of the matching service to Consul so that transparent
	// proxy works correctly. Do not fail the reconcile if assigning the virtual IP returns an error.
	if needsVirtualIPAssignment(r.DatacenterName, configEntry) {
		err = assignServiceVirtualIP(ctx, logger, consulClient, crdCtrl, req.NamespacedName, configEntry, r.DatacenterName)
		if err != nil {
			logger.Error(err, "failed assigning service virtual ip")
		}
	}

	return ctrl.Result{}, nil
}

// setupWithManager sets up the controller manager for the given resource
// with our default options.
func setupWithManager(mgr ctrl.Manager, resource client.Object, reconciler reconcile.Reconciler) error {
	options := controller.Options{
		// Taken from https://github.com/kubernetes/client-go/blob/master/util/workqueue/default_rate_limiters.go#L39
		// and modified from a starting backoff of 5ms and max of 1000s to a
		// starting backoff of 200ms and a max of 5s to better fit our most
		// common error cases and performance characteristics.
		//
		// One common error case is that a config entry is applied that requires
		// a protocol like http or grpc. Often the user will apply a new config
		// entry to set the protocol in a minute or two. During this time, the
		// default backoff could then be set up to 5m or more which means the
		// original config entry takes a long time to re-sync.
		//
		// In terms of performance, Consul servers can handle tens of thousands
		// of writes per second, so retrying at max every 5s isn't an issue and
		// provides a better UX.
		RateLimiter: workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(200*time.Millisecond, 5*time.Second),
			// 10 qps, 100 bucket size.  This is only for retry speed and its only the overall factor (not per item)
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(resource).
		WithOptions(options).
		Complete(reconciler)
}

func (r *ConfigEntryController) consulNamespace(configEntry capi.ConfigEntry, namespace string, globalResource bool) string {
	if !r.EnableConsulNamespaces {
		return ""
	}
	// ServiceIntentions have the appropriate Consul Namespace set on them as the value
	// is defaulted by the webhook. These are then set on the ServiceIntentions config entry
	// but not on the others. In case the ConfigEntry has the Consul Namespace set, we just
	// use the namespace assigned instead of attempting to determine it.
	if configEntry.GetNamespace() != "" {
		return configEntry.GetNamespace()
	}

	// Does not attempt to parse the namespace for global resources like ProxyDefaults or
	// wildcard namespace destinations are they will not be prefixed and will remain "default"/"*".
	if !globalResource && namespace != common.WildcardNamespace {
		return namespaces.ConsulNamespace(namespace, r.EnableConsulNamespaces, r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix)
	}
	if r.EnableConsulNamespaces {
		return namespace
	}
	return ""
}

func (r *ConfigEntryController) syncFailed(ctx context.Context, logger logr.Logger, updater Controller, configEntry common.ConfigEntryResource, errType string, err error) (ctrl.Result, error) {
	configEntry.SetSyncedCondition(corev1.ConditionFalse, errType, err.Error())
	if updateErr := updater.UpdateStatus(ctx, configEntry); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise the original error would be lost.
		logger.Error(err, "sync failed")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

func (r *ConfigEntryController) syncSuccessful(ctx context.Context, updater Controller, configEntry common.ConfigEntryResource) (ctrl.Result, error) {
	configEntry.SetSyncedCondition(corev1.ConditionTrue, "", "")
	timeNow := metav1.NewTime(time.Now())
	configEntry.SetLastSyncedTime(&timeNow)
	return ctrl.Result{}, updater.UpdateStatus(ctx, configEntry)
}

func (r *ConfigEntryController) syncUnknown(ctx context.Context, updater Controller, configEntry common.ConfigEntryResource) error {
	configEntry.SetSyncedCondition(corev1.ConditionUnknown, "", "")
	timeNow := metav1.NewTime(time.Now())
	configEntry.SetLastSyncedTime(&timeNow)
	return updater.UpdateStatus(ctx, configEntry)
}

func (r *ConfigEntryController) syncUnknownWithError(ctx context.Context,
	logger logr.Logger,
	updater Controller,
	configEntry common.ConfigEntryResource,
	errType string,
	err error) (ctrl.Result, error) {

	configEntry.SetSyncedCondition(corev1.ConditionUnknown, errType, err.Error())
	if updateErr := updater.UpdateStatus(ctx, configEntry); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise the original error would be lost.
		logger.Error(err, "sync status unknown")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

// nonMatchingMigrationError returns an error that indicates the migration failed
// because the config entries did not match.
func (r *ConfigEntryController) nonMatchingMigrationError(kubeEntry common.ConfigEntryResource, consulEntry capi.ConfigEntry) error {
	// We marshal into JSON to include in the error message so users will know
	// which fields aren't matching.
	kubeJSON, err := json.Marshal(kubeEntry.ToConsul(r.DatacenterName))
	if err != nil {
		return fmt.Errorf("migration failed: unable to marshal Kubernetes resource: %s", err)
	}
	consulJSON, err := json.Marshal(consulEntry)
	if err != nil {
		return fmt.Errorf("migration failed: unable to marshal Consul resource: %s", err)
	}

	return fmt.Errorf("migration failed: Kubernetes resource does not match existing Consul config entry: consul=%s, kube=%s", consulJSON, kubeJSON)
}

// needsVirtualIPAssignment checks to see if a configEntry type needs to be assigned a virtual IP.
func needsVirtualIPAssignment(datacenterName string, configEntry common.ConfigEntryResource) bool {
	switch configEntry.KubeKind() {
	case common.ServiceResolver:
		return true
	case common.ServiceRouter:
		return true
	case common.ServiceSplitter:
		return true
	case common.ServiceDefaults:
		return true
	case common.ServiceIntentions:
		entry := configEntry.ToConsul(datacenterName)
		intention, ok := entry.(*capi.ServiceIntentionsConfigEntry)
		if !ok {
			return false
		}
		// We should not persist virtual ips if the destination is a wildcard
		// in any form, since that would target multiple services.
		return !strings.Contains(intention.Name, "*") &&
			!strings.Contains(intention.Namespace, "*") &&
			!strings.Contains(intention.Partition, "*")
	}
	return false
}

// assignServiceVirtualIPs manually sends the ClusterIP for a matching service for ServiceRouter or ServiceSplitter
// CRDs to Consul so that it can be added to the virtual IP table. The assignment is skipped if the matching service
// does not exist or if an older version of Consul is being used. Endpoints Controller, on service registration, also
// manually sends a ClusterIP when a service is created. This increases the chance of a real IP ending up in the
// discovery chain.
func assignServiceVirtualIP(ctx context.Context, logger logr.Logger, consulClient *capi.Client, crdCtrl Controller, namespacedName types.NamespacedName, configEntry common.ConfigEntryResource, datacenter string) error {
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configEntry.KubernetesName(),
			Namespace: namespacedName.Namespace,
		},
	}
	if err := crdCtrl.Get(ctx, namespacedName, &service); err != nil {
		// It is non-fatal if the service does not exist. The ClusterIP will get added when the service is registered in
		// the endpoints controller
		if k8serr.IsNotFound(err) {
			return nil
		}
		// Something is really wrong with the service
		return err
	}

	consulType := configEntry.ToConsul(datacenter)
	wo := &capi.WriteOptions{
		Namespace: consulType.GetNamespace(),
		Partition: consulType.GetPartition(),
	}

	logger.Info("adding manual ip to virtual ip table in Consul", "name", service.Name)
	_, _, err := consulClient.Internal().AssignServiceVirtualIP(ctx, consulType.GetName(), []string{service.Spec.ClusterIP}, wo)
	if err != nil {
		// Maintain backwards compatibility with older versions of Consul that do not support the manual VIP improvements. With the older version, the mesh
		// will still work.
		if isNotFoundErr(err) {
			logger.Error(err, "failed to add ip to virtual ip table. Please upgrade Consul to version 1.16 or higher", "name", service.Name)
			return nil
		} else {
			return err
		}
	}
	return nil
}

func isNotFoundErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

// containsString returns true if s is in slice.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// sourceDatacenterMismatchErr returns an error for when the source datacenter
// meta key does not match our datacenter. This could be because the config
// entry was created directly in Consul or because it was created by another
// controller in another Consul datacenter.
func sourceDatacenterMismatchErr(sourceDatacenter string) error {
	// If the datacenter is empty, then they likely created it in Consul
	// directly (vs. another controller in another DC creating it).
	if sourceDatacenter == "" {
		return fmt.Errorf("config entry already exists in Consul")
	}
	return fmt.Errorf("config entry managed in different datacenter: %q", sourceDatacenter)
}
