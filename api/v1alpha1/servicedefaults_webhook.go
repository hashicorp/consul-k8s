package v1alpha1

import (
	"context"
	"fmt"

	"github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var servicedefaultslog = logf.Log.WithName("servicedefaults-resource")

// todo: use our own validating webhook so we can inject this properly
var ConsulClient *api.Client
var KubeClient client.Client

func (r *ServiceDefaults) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-consul-hashicorp-com-v1alpha1-servicedefaults,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicedefaults,verbs=create;update,versions=v1alpha1,name=mservicedefaults.kb.io

var _ webhook.Defaulter = &ServiceDefaults{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *ServiceDefaults) Default() {
	servicedefaultslog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-consul-hashicorp-com-v1alpha1-servicedefaults,mutating=false,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicedefaults,versions=v1alpha1,name=vservicedefaults.kb.io

var _ webhook.Validator = &ServiceDefaults{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ServiceDefaults) ValidateCreate() error {
	servicedefaultslog.Info("validate create", "name", r.Name)
	var svcDefaultsList ServiceDefaultsList
	if err := KubeClient.List(context.Background(), &svcDefaultsList); err != nil {
		return err
	}
	for _, item := range svcDefaultsList.Items {
		if item.Name == r.Name {
			return fmt.Errorf("ServiceDefaults resource with name %q is already defined in namespace %q – all ServiceDefaults resources must have unique names across namespaces",
				r.Name, item.Namespace)
		}
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ServiceDefaults) ValidateUpdate(old runtime.Object) error {
	servicedefaultslog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ServiceDefaults) ValidateDelete() error {
	servicedefaultslog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
