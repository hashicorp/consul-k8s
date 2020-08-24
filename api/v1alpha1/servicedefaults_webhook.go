package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	capi "github.com/hashicorp/consul/api"
	"k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var servicedefaultslog = logf.Log.WithName("servicedefaults-resource")

type ServiceDefaultsValidator struct {
	client.Client
	ConsulClient *capi.Client
	decoder      *admission.Decoder
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-consul-hashicorp-com-v1alpha1-servicedefaults,mutating=false,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicedefaults,versions=v1alpha1,name=vservicedefaults.kb.io

func (v *ServiceDefaultsValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	svcDefaults := &ServiceDefaults{}
	err := v.decoder.Decode(req, svcDefaults)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == v1beta1.Create {
		servicedefaultslog.Info("validate create", "name", svcDefaults.Name)
		var svcDefaultsList ServiceDefaultsList
		if err := v.Client.List(context.Background(), &svcDefaultsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range svcDefaultsList.Items {
			if item.Name == svcDefaults.Name {
				return admission.Errored(http.StatusBadRequest, fmt.Errorf("ServiceDefaults resource with name %q is already defined in namespace %q – all ServiceDefaults resources must have unique names across namespaces",
					svcDefaults.Name, item.Namespace))
			}
		}
	}
	return admission.Allowed("Valid Service Defaults Request")
}

func (v *ServiceDefaultsValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
