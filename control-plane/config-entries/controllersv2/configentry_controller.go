// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllersv2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/proto-public/pbresource"
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
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	ConsulAgentError             = "ConsulAgentError"
	ExternallyManagedConfigError = "ExternallyManagedConfigError"
	MigrationFailedError         = "MigrationFailedError"
)

// Controller is implemented by CRD-specific config-entries. It is used by
// ConfigEntryController to abstract CRD-specific config-entries.
type Controller interface {
	// Update updates the state of the whole object.
	Update(context.Context, client.Object, ...client.UpdateOption) error
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

	// CrossNSACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNSACLPolicy string

	EnableConsulPartitions bool

	ConsulPartition string
}

// ReconcileEntry reconciles an update to a resource. CRD-specific controller's
// call this function because it handles reconciliation of config entries
// generically.
// CRD-specific controller should pass themselves in as updater since we
// need to call back into their own update methods to ensure they update their
// internal state.
func (r *ConfigEntryController) ReconcileEntry(ctx context.Context, crdCtrl Controller, req ctrl.Request, configEntry common.ConfigEntryV2Resource) (ctrl.Result, error) {
	logger := crdCtrl.Logger(req.NamespacedName)
	err := crdCtrl.Get(ctx, req.NamespacedName, configEntry)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Create Consul resource service client for this reconcile.
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		logger.Error(err, "failed to create Consul resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	if !configEntry.GetDeletionTimestamp().IsZero() {
		// The object is being deleted
		logger.Info("deletion event")
		// Check to see if consul has config entry with the same name
		_, err := resourceClient.Read(ctx, &pbresource.ReadRequest{Id: configEntry.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})

		// Ignore the error where the config entry isn't found in Consul.
		// It is indicative of desired state.
		if err != nil && !isNotFoundErr(err) {
			return ctrl.Result{}, fmt.Errorf("getting config entry from consul: %w", err)
		} else if err == nil {
			// Only delete the resource from Consul if it is owned by our datacenter.
			_, err := resourceClient.Delete(ctx, &pbresource.DeleteRequest{Id: configEntry.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})
			if err != nil {
				return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
					fmt.Errorf("deleting config entry from consul: %w", err))
			}
			logger.Info("deletion from Consul successful")
		}
		if err := crdCtrl.Update(ctx, configEntry); err != nil {
			return ctrl.Result{}, err
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Check to see if consul has config entry with the same name
	entry, err := resourceClient.Read(ctx, &pbresource.ReadRequest{Id: configEntry.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("config entry not found in consul")

		// Create the config entry
		_, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: configEntry.Resource(r.consulNamespace(req.Namespace), r.getConsulPartition())})
		if err != nil {
			return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("writing config entry to consul: %w", err))
		}

		logger.Info("config entry created")
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	}

	// If there is an error when trying to get the config entry from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError, err)
	}

	if !configEntry.MatchesConsul(entry.Resource, r.consulNamespace(req.Namespace), r.getConsulPartition()) {
		logger.Info("config entry does not match consul")
		_, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: configEntry.Resource(r.consulNamespace(req.Namespace), r.getConsulPartition())})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		logger.Info("config entry updated")
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

func (r *ConfigEntryController) consulNamespace(namespace string) string {
	ns := namespaces.ConsulNamespace(
		namespace,
		r.EnableConsulNamespaces,
		r.ConsulDestinationNamespace,
		r.EnableNSMirroring,
		r.NSMirroringPrefix,
	)

	// TODO: remove this if and when the default namespace of resources is no longer required to be set explicitly.
	if ns == "" {
		ns = constants.DefaultConsulNS
	}
	return ns
}

func (r *ConfigEntryController) syncFailed(ctx context.Context, logger logr.Logger, updater Controller, configEntry common.ConfigEntryV2Resource, errType string, err error) (ctrl.Result, error) {
	configEntry.SetSyncedCondition(corev1.ConditionFalse, errType, err.Error())
	if updateErr := updater.UpdateStatus(ctx, configEntry); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise the original error would be lost.
		logger.Error(err, "sync failed")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

func (r *ConfigEntryController) syncSuccessful(ctx context.Context, updater Controller, configEntry common.ConfigEntryV2Resource) (ctrl.Result, error) {
	configEntry.SetSyncedCondition(corev1.ConditionTrue, "", "")
	timeNow := metav1.NewTime(time.Now())
	configEntry.SetLastSyncedTime(&timeNow)
	return ctrl.Result{}, updater.UpdateStatus(ctx, configEntry)
}

func (r *ConfigEntryController) syncUnknown(ctx context.Context, updater Controller, configEntry common.ConfigEntryV2Resource) error {
	configEntry.SetSyncedCondition(corev1.ConditionUnknown, "", "")
	return updater.Update(ctx, configEntry)
}

func (r *ConfigEntryController) syncUnknownWithError(ctx context.Context,
	logger logr.Logger,
	updater Controller,
	configEntry common.ConfigEntryV2Resource,
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

func isNotFoundErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

func (r *ConfigEntryController) getConsulPartition() string {
	if !r.EnableConsulPartitions || r.ConsulPartition == "" {
		return constants.DefaultConsulPartition
	}
	return r.ConsulPartition
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
