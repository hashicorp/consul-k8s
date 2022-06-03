package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
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
)

const (
	FinalizerName                = "finalizers.consul.hashicorp.com"
	ConsulAgentError             = "ConsulAgentError"
	ExternallyManagedConfigError = "ExternallyManagedConfigError"
	MigrationFailedError         = "MigrationFailedError"
)

// Controller is implemented by CRD-specific controllers. It is used by
// ConfigEntryController to abstract CRD-specific controllers.
type Controller interface {
	// Update updates the state of the whole object.
	Update(context.Context, client.Object, ...client.UpdateOption) error
	// UpdateStatus updates the state of just the object's status.
	UpdateStatus(context.Context, client.Object, ...client.UpdateOption) error
	// Get retrieves an obj for the given object key from the Kubernetes Cluster.
	// obj must be a struct pointer so that obj can be updated with the response
	// returned by the Server.
	Get(ctx context.Context, key client.ObjectKey, obj client.Object) error
	// Logger returns a logger with values added for the specific controller
	// and request name.
	Logger(types.NamespacedName) logr.Logger
}

// ConfigEntryController is a generic controller that is used to reconcile
// all config entry types, e.g. ServiceDefaults, ServiceResolver, etc, since
// they share the same reconcile behaviour.
type ConfigEntryController struct {
	ConsulClient *capi.Client

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

	consulEntry := configEntry.ToConsul(r.DatacenterName)

	if configEntry.GetDeletionTimestamp().IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then let's add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(configEntry.GetFinalizers(), FinalizerName) {
			configEntry.AddFinalizer(FinalizerName)
			if err := r.syncUnknown(ctx, crdCtrl, configEntry); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(configEntry.GetFinalizers(), FinalizerName) {
			logger.Info("deletion event")
			// Check to see if consul has config entry with the same name
			entry, _, err := r.ConsulClient.ConfigEntries().Get(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.QueryOptions{
				Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
			})

			// Ignore the error where the config entry isn't found in Consul.
			// It is indicative of desired state.
			if err != nil && !isNotFoundErr(err) {
				return ctrl.Result{}, fmt.Errorf("getting config entry from consul: %w", err)
			} else if err == nil {
				// Only delete the resource from Consul if it is owned by our datacenter.
				if entry.GetMeta()[common.DatacenterKey] == r.DatacenterName {
					_, err := r.ConsulClient.ConfigEntries().Delete(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.WriteOptions{
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
			configEntry.RemoveFinalizer(FinalizerName)
			if err := crdCtrl.Update(ctx, configEntry); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("finalizer removed")
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Check to see if consul has config entry with the same name
	entry, _, err := r.ConsulClient.ConfigEntries().Get(configEntry.ConsulKind(), configEntry.ConsulName(), &capi.QueryOptions{
		Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
	})
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("config entry not found in consul")

		// If Consul namespaces are enabled we may need to create the
		// destination consul namespace first.
		if r.EnableConsulNamespaces {
			consulNS := r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource())
			created, err := namespaces.EnsureExists(r.ConsulClient, consulNS, r.CrossNSACLPolicy)
			if err != nil {
				return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
					fmt.Errorf("creating consul namespace %q: %w", consulNS, err))
			}
			if created {
				logger.Info("consul namespace created", "ns", consulNS)
			}
		}

		// Create the config entry
		_, writeMeta, err := r.ConsulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
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

	requiresMigration := false
	sourceDatacenter := entry.GetMeta()[common.DatacenterKey]

	// Check if the config entry is managed by our datacenter.
	// Do not process resource if the entry was not created within our datacenter
	// as it was created in a different cluster which will be managing that config entry.
	if sourceDatacenter != r.DatacenterName {

		// Note that there is a special case where we will migrate a config entry
		// that wasn't created by the controller if it has the migrate-entry annotation set to true.
		// This functionality exists to help folks who are upgrading from older helm
		// chart versions where they had previously created config entries themselves but
		// now want to manage them through custom resources.
		if configEntry.GetObjectMeta().Annotations[common.MigrateEntryKey] != common.MigrateEntryTrue {
			return r.syncFailed(ctx, logger, crdCtrl, configEntry, ExternallyManagedConfigError,
				sourceDatacenterMismatchErr(sourceDatacenter))
		}

		requiresMigration = true
	}

	if !configEntry.MatchesConsul(entry) {
		if requiresMigration {
			// If we're migrating this config entry but the custom resource
			// doesn't match what's in Consul currently we error out so that
			// it doesn't overwrite something accidentally.
			return r.syncFailed(ctx, logger, crdCtrl, configEntry, MigrationFailedError,
				r.nonMatchingMigrationError(configEntry, entry))
		}

		logger.Info("config entry does not match consul", "modify-index", entry.GetModifyIndex())
		_, writeMeta, err := r.ConsulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
			Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		logger.Info("config entry updated", "request-time", writeMeta.RequestTime)
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	} else if requiresMigration && entry.GetMeta()[common.DatacenterKey] != r.DatacenterName {
		// If we get here then we're doing a migration and the entry in Consul
		// matches the entry in Kubernetes. We just need to update the metadata
		// of the entry in Consul to say that it's now managed by Kubernetes.
		logger.Info("migrating config entry to be managed by Kubernetes")
		_, writeMeta, err := r.ConsulClient.ConfigEntries().Set(consulEntry, &capi.WriteOptions{
			Namespace: r.consulNamespace(consulEntry, configEntry.ConsulMirroringNS(), configEntry.ConsulGlobalResource()),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		logger.Info("config entry migrated", "request-time", writeMeta.RequestTime)
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	} else if configEntry.SyncedConditionStatus() != corev1.ConditionTrue {
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
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
	return updater.Update(ctx, configEntry)
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
