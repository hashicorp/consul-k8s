package controllers

import (
	"context"
	"errors"
	"strings"

	"github.com/go-logr/logr"
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
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicedefaults/status,verbs=get;update;patch

func (r *ServiceDefaultsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("servicedefaults", req.NamespacedName)
	var svcDefaults consulv1alpha1.ServiceDefaults

	err := r.Get(ctx, req.NamespacedName, &svcDefaults)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "failed to retrieve resource")
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
			_, err = r.ConsulClient.ConfigEntries().Delete(capi.ServiceDefaults, svcDefaults.Name, nil)
			if err != nil {
				return ctrl.Result{}, err
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
	entry, _, err := r.ConsulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaults.Name, nil)
	// If a config entry with this name does not exist
	if isNotFoundErr(err) {
		// Create the config entry
		_, _, err := r.ConsulClient.ConfigEntries().Set(svcDefaults.ToConsul(), nil)
		if err != nil {
			svcDefaults.Status.Conditions = syncFailed(ConsulAgentError, err.Error())
			if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
		svcDefaults.Status.Conditions = syncSuccessful()
		if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
			return ctrl.Result{}, err
		}
	} else if err != nil {
		svcDefaults.Status.Conditions = syncFailed(ConsulAgentError, err.Error())
		if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	} else {
		svcDefaultEntry, ok := entry.(*capi.ServiceConfigEntry)
		if !ok {
			err := errors.New("could not cast entry as ServiceConfigEntry")
			svcDefaults.Status.Conditions = syncUnknownWithError(CastError, err.Error())
			if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
		if !svcDefaults.MatchesConsul(svcDefaultEntry) {
			_, _, err := r.ConsulClient.ConfigEntries().Set(svcDefaults.ToConsul(), nil)
			if err != nil {
				svcDefaults.Status.Conditions = syncUnknownWithError(ConsulAgentError, err.Error())
				if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, err
			}
			svcDefaults.Status.Conditions = syncSuccessful()
			if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
		} else if !svcDefaults.Status.GetCondition(consulv1alpha1.ConditionSynced).IsTrue() {
			svcDefaults.Status.Conditions = syncSuccessful()
			if err := r.Status().Update(context.Background(), &svcDefaults); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ServiceDefaultsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.ServiceDefaults{}).
		Complete(r)
}

func syncFailed(reason, message string) consulv1alpha1.Conditions {
	return consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionFalse,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

func syncSuccessful() consulv1alpha1.Conditions {
	return consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
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

func syncUnknownWithError(reason, message string) consulv1alpha1.Conditions {
	return consulv1alpha1.Conditions{
		{
			Type:               consulv1alpha1.ConditionSynced,
			Status:             corev1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
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
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
