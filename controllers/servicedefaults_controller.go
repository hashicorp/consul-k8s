package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/namespaces"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/api/v1alpha1"
)

const (
	FinalizerName    = "finalizers.consul.hashicorp.com"
	ConsulAgentError = "ConsulAgentError"
	CastError        = "CastError"
)

// ServiceDefaultsReconciler reconciles a ServiceDefaults object
type ServiceDefaultsReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
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

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults/status,verbs=get;update;patch

func (r *ServiceDefaultsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("controller", "servicedefaults", "request", req.NamespacedName)
	var svcDefaults consulv1alpha1.ServiceDefaults

	err := r.Get(ctx, req.NamespacedName, &svcDefaults)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	if svcDefaults.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !containsString(svcDefaults.ObjectMeta.Finalizers, FinalizerName) {
			svcDefaults.ObjectMeta.Finalizers = append(svcDefaults.ObjectMeta.Finalizers, FinalizerName)
			svcDefaults.Status.Conditions = syncUnknown()
			if err := r.Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(svcDefaults.ObjectMeta.Finalizers, FinalizerName) {
			logger.Info("deletion event")
			// Our finalizer is present, so we need to delete the config entry
			// from consul.
			_, err = r.ConsulClient.ConfigEntries().Delete(capi.ServiceDefaults, svcDefaults.Name, &capi.WriteOptions{
				Namespace: r.consulNamespace(req.Namespace),
			})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("deleting config entry from consul: %w", err)
			}
			logger.Info("deletion from Consul successful")

			// remove our finalizer from the list and update it.
			svcDefaults.ObjectMeta.Finalizers = removeString(svcDefaults.ObjectMeta.Finalizers, FinalizerName)
			if err := r.Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("finalizer removed")
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Check to see if consul has service defaults with the same name
	entry, _, err := r.ConsulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaults.Name, &capi.QueryOptions{
		Namespace: r.consulNamespace(req.Namespace),
	})
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("config entry not found in consul")

		// If Consul namespaces are enabled we may need to create the
		// destination consul namespace first.
		if r.EnableConsulNamespaces {
			if err := namespaces.EnsureExists(r.ConsulClient, r.consulNamespace(req.Namespace), r.CrossNSACLPolicy); err != nil {
				return r.syncFailed(logger, svcDefaults, ConsulAgentError,
					fmt.Errorf("creating consul namespace %q: %w", r.consulNamespace(req.Namespace), err))
			}
		}

		// Create the config entry
		_, _, err := r.ConsulClient.ConfigEntries().Set(svcDefaults.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncFailed(logger, svcDefaults, ConsulAgentError,
				fmt.Errorf("writing config entry to consul: %w", err))
		}
		return r.syncSuccessful(svcDefaults)
	}

	// If there is an error when trying to get the config entry from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(logger, svcDefaults, ConsulAgentError, err)
	}

	svcDefaultEntry, ok := entry.(*capi.ServiceConfigEntry)
	if !ok {
		return r.syncUnknownWithError(logger, svcDefaults, CastError,
			fmt.Errorf("could not cast entry as ServiceConfigEntry"))
	}
	if !svcDefaults.MatchesConsul(svcDefaultEntry) {
		_, _, err := r.ConsulClient.ConfigEntries().Set(svcDefaults.ToConsul(), &capi.WriteOptions{
			Namespace: r.consulNamespace(req.Namespace),
		})
		if err != nil {
			return r.syncUnknownWithError(logger, svcDefaults, ConsulAgentError,
				fmt.Errorf("updating config entry in consul: %w", err))
		}
		return r.syncSuccessful(svcDefaults)
	} else if !svcDefaults.Status.GetCondition(consulv1alpha1.ConditionSynced).IsTrue() {
		return r.syncSuccessful(svcDefaults)
	}

	return ctrl.Result{}, nil
}

func (r *ServiceDefaultsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.ServiceDefaults{}).
		Complete(r)
}

// consulNamespace returns the namespace that a service should be
// registered in based on the namespace options. It returns an
// empty string if namespaces aren't enabled.
func (r *ServiceDefaultsReconciler) consulNamespace(ns string) string {
	if !r.EnableConsulNamespaces {
		return ""
	}

	// Mirroring takes precedence.
	if r.EnableNSMirroring {
		return fmt.Sprintf("%s%s", r.NSMirroringPrefix, ns)
	}

	return r.ConsulDestinationNamespace
}

func (r *ServiceDefaultsReconciler) syncFailed(logger logr.Logger, svcDefaults consulv1alpha1.ServiceDefaults, errType string, err error) (ctrl.Result, error) {
	svcDefaults.Status.Conditions = consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             errType,
			Message:            err.Error(),
		},
	}
	if updateErr := r.Status().Update(context.Background(), &svcDefaults); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise the original error would be lost.
		logger.Error(err, "sync failed")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

func (r *ServiceDefaultsReconciler) syncSuccessful(svcDefaults consulv1alpha1.ServiceDefaults) (ctrl.Result, error) {
	svcDefaults.Status.Conditions = consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
	return ctrl.Result{}, r.Status().Update(context.Background(), &svcDefaults)
}

func syncUnknown() consulv1alpha1.Conditions {
	return consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
		},
	}
}

func (r *ServiceDefaultsReconciler) syncUnknownWithError(logger logr.Logger, svcDefaults consulv1alpha1.ServiceDefaults, errType string, err error) (ctrl.Result, error) {
	svcDefaults.Status.Conditions = consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             errType,
			Message:            err.Error(),
		},
	}
	if updateErr := r.Status().Update(context.Background(), &svcDefaults); updateErr != nil {
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

// removeString removes s from slice and returns the new slice.
func removeString(slice []string, s string) []string {
	var result []string
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return result
}
