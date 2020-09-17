package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul-k8s/namespaces"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FinalizerName    = "finalizers.consul.hashicorp.com"
	ConsulAgentError = "ConsulAgentError"
)

// Controller is implemented by CRD-specific controllers. It is used by
// ConfigEntryController to abstract CRD-specific controllers.
type Controller interface {
	// Update updates the state of the whole object.
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	// UpdateStatus updates the state of just the object's status.
	UpdateStatus(context.Context, runtime.Object, ...client.UpdateOption) error
	// Get retrieves an obj for the given object key from the Kubernetes Cluster.
	// obj must be a struct pointer so that obj can be updated with the response
	// returned by the Server.
	Get(ctx context.Context, key client.ObjectKey, obj runtime.Object) error
	// Logger returns a logger with values added for the specific controller
	// and request name.
	Logger(types.NamespacedName) logr.Logger
}

// ConfigEntryController is a generic controller that is used to reconcile
// all config entry types, e.g. ServiceDefaults, ServiceResolver, etc, since
// they share the same reconcile behaviour.
type ConfigEntryController struct {
	ConsulClient *capi.Client

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
func (r *ConfigEntryController) ReconcileEntry(
	crdCtrl Controller,
	req ctrl.Request,
	configEntry common.ConfigEntryResource) (ctrl.Result, error) {

	ctx := context.Background()
	logger := crdCtrl.Logger(req.NamespacedName)

	err := crdCtrl.Get(ctx, req.NamespacedName, configEntry)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	if configEntry.GetObjectMeta().DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(configEntry.GetObjectMeta().Finalizers, FinalizerName) {
			configEntry.AddFinalizer(FinalizerName)
			if err := r.syncUnknown(ctx, crdCtrl, configEntry); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(configEntry.GetObjectMeta().Finalizers, FinalizerName) {
			logger.Info("deletion event")
			// Our finalizer is present, so we need to delete the config entry
			// from consul.
			_, err := r.ConsulClient.ConfigEntries().Delete(configEntry.Kind(), configEntry.Name(), &capi.WriteOptions{
				Namespace: r.consulNamespace(req.Namespace),
			})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("deleting config entry from consul: %w", err)
			}
			logger.Info("deletion from Consul successful")

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

	// Check to see if consul has service defaults with the same name
	entry, _, err := r.ConsulClient.ConfigEntries().Get(configEntry.Kind(), configEntry.Name(), &capi.QueryOptions{
		Namespace: r.consulNamespace(req.Namespace),
	})
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("config entry not found in consul")

		// If Consul namespaces are enabled we may need to create the
		// destination consul namespace first.
		if r.EnableConsulNamespaces {
			if err := namespaces.EnsureExists(r.ConsulClient, r.consulNamespace(req.Namespace), r.CrossNSACLPolicy); err != nil {
				return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
					fmt.Errorf("creating consul namespace %q: %w", r.consulNamespace(req.Namespace), err))
			}
		}

		// Create the config entry
		_, _, err := r.ConsulClient.ConfigEntries().Set(configEntry.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("writing config entry to consul: %w", err))
		}
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	}

	// If there is an error when trying to get the config entry from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(ctx, logger, crdCtrl, configEntry, ConsulAgentError, err)
	}

	if !configEntry.MatchesConsul(entry) {
		_, _, err := r.ConsulClient.ConfigEntries().Set(configEntry.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	} else if configEntry.GetSyncedConditionStatus() == corev1.ConditionTrue {
		return r.syncSuccessful(ctx, crdCtrl, configEntry)
	}

	return ctrl.Result{}, nil
}

func (r *ConfigEntryController) consulNamespace(kubeNS string) string {
	return namespaces.ConsulNamespace(kubeNS, r.EnableConsulNamespaces, r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix)
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
