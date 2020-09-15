package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/namespaces"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FinalizerName    = "finalizers.consul.hashicorp.com"
	ConsulAgentError = "ConsulAgentError"
)

// StateUpdater is implemented by CRD-specific reconcilers. It is used by
// ConfigEntryReconciler to abstract CRD-specific reconcilers.
type StateUpdater interface {
	// Update updates the state of the whole object.
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	// UpdateStatus updates the state of just the object's status.
	UpdateStatus(context.Context, runtime.Object, ...client.UpdateOption) error
}

// ConfigEntryCRD is a generic config entry custom resource. It is implemented
// by each config entry type so that they can be acted upon generically.
type ConfigEntryCRD interface {
	// GetObjectMeta returns object meta.
	GetObjectMeta() metav1.ObjectMeta
	// AddFinalizer adds a finalizer to the list of finalizers.
	AddFinalizer(string)
	// RemoveFinalizer removes this finalizer from the list.
	RemoveFinalizer(string)
	// Finalizers returns the list of finalizers for this object.
	Finalizers() []string
	// Kind returns the Consul config entry kind, i.e. service-defaults, not
	// ServiceDefaults.
	Kind() string
	// Name returns the name of the config entry.
	Name() string
	// SetConditions updates conditions.
	SetConditions(consulv1alpha1.Conditions)
	// GetCondition returns condition of this type.
	GetCondition(consulv1alpha1.ConditionType) *consulv1alpha1.Condition
	// ToConsul converts the CRD to the corresponding Consul API definition.
	// Its return type is the generic ConfigEntry but a specific config entry
	// type should be constructed e.g. ServiceConfigEntry.
	ToConsul() capi.ConfigEntry
	// MatchesConsul returns true if the CRD has the same fields as the Consul
	// config entry.
	MatchesConsul(capi.ConfigEntry) bool
	// GetObjectKind should be implemented by the generated code.
	GetObjectKind() schema.ObjectKind
	// DeepCopyObject should be implemented by the generated code.
	DeepCopyObject() runtime.Object
	// Validate returns an error if the CRD is invalid.
	Validate() error
}

// ConfigEntryReconciler is a generic reconciler that is used to reconcile
// all config entry types, e.g. ServiceDefaults, ServiceResolver, etc, since
// they share the same reconcile behaviour.
type ConfigEntryReconciler struct {
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

// Reconcile reconciles Kubernetes' state with Consul's. CRD-specific reconciler's
// call this function because it handles reconciliation of config entries
// generically.
// CRD-specific reconcilers should pass themselves in as updater since we
// need to call back into their own update methods to ensure they update their
// internal state.
func (r *ConfigEntryReconciler) Reconcile(
	ctx context.Context,
	logger logr.Logger,
	updater StateUpdater,
	req ctrl.Request,
	configEntry ConfigEntryCRD) (ctrl.Result, error) {

	if configEntry.GetObjectMeta().DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(configEntry.GetObjectMeta().Finalizers, FinalizerName) {
			configEntry.AddFinalizer(FinalizerName)
			if err := r.syncUnknown(ctx, updater, configEntry); err != nil {
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
			if err := updater.Update(ctx, configEntry); err != nil {
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
				return r.syncFailed(ctx, logger, updater, configEntry, ConsulAgentError,
					fmt.Errorf("creating consul namespace %q: %w", r.consulNamespace(req.Namespace), err))
			}
		}

		// Create the config entry
		_, _, err := r.ConsulClient.ConfigEntries().Set(configEntry.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncFailed(ctx, logger, updater, configEntry, ConsulAgentError,
				fmt.Errorf("writing config entry to consul: %w", err))
		}
		return r.syncSuccessful(ctx, updater, configEntry)
	}

	// If there is an error when trying to get the config entry from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(ctx, logger, updater, configEntry, ConsulAgentError, err)
	}

	if !configEntry.MatchesConsul(entry) {
		_, _, err := r.ConsulClient.ConfigEntries().Set(configEntry.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, updater, configEntry, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		return r.syncSuccessful(ctx, updater, configEntry)
	} else if !configEntry.GetCondition(consulv1alpha1.ConditionSynced).IsTrue() {
		return r.syncSuccessful(ctx, updater, configEntry)
	}

	return ctrl.Result{}, nil
}

func (r *ConfigEntryReconciler) consulNamespace(kubeNS string) string {
	return namespaces.ConsulNamespace(kubeNS, r.EnableConsulNamespaces, r.ConsulDestinationNamespace, r.EnableNSMirroring, r.NSMirroringPrefix)
}

func (r *ConfigEntryReconciler) syncFailed(ctx context.Context, logger logr.Logger, updater StateUpdater, configEntry ConfigEntryCRD, errType string, err error) (ctrl.Result, error) {
	configEntry.SetConditions(consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             errType,
			Message:            err.Error(),
		},
	})
	if updateErr := updater.UpdateStatus(ctx, configEntry); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise the original error would be lost.
		logger.Error(err, "sync failed")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

func (r *ConfigEntryReconciler) syncSuccessful(ctx context.Context, updater StateUpdater, configEntry ConfigEntryCRD) (ctrl.Result, error) {
	configEntry.SetConditions(consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	})
	return ctrl.Result{}, updater.UpdateStatus(ctx, configEntry)
}

func (r *ConfigEntryReconciler) syncUnknown(ctx context.Context, updater StateUpdater, configEntry ConfigEntryCRD) error {
	configEntry.SetConditions(consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
		},
	})
	return updater.Update(ctx, configEntry)
}

func (r *ConfigEntryReconciler) syncUnknownWithError(ctx context.Context,
	logger logr.Logger,
	updater StateUpdater,
	configEntry ConfigEntryCRD,
	errType string,
	err error) (ctrl.Result, error) {

	configEntry.SetConditions(consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             errType,
			Message:            err.Error(),
		},
	})
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
