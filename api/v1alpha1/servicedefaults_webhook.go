package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	"k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func NewServiceDefaultsValidator(client client.Client, consulClient *capi.Client, logger logr.Logger) *serviceDefaultsValidator {
	return &serviceDefaultsValidator{
		Client:       client,
		ConsulClient: consulClient,
		Logger:       logger,
	}
}

type serviceDefaultsValidator struct {
	client.Client
	ConsulClient *capi.Client
	Logger       logr.Logger
	decoder      *admission.Decoder
}

// Note: The path value in the below line is the path to the webhook. If it is updates, run code-gen, update subcommand/controller/command.go and the consul-helm value for the path to the webhook.
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-servicedefaults,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicedefaults,versions=v1alpha1,name=mutate-servicedefaults.consul.hashicorp.com

func (v *serviceDefaultsValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcDefaults ServiceDefaults
	err := v.decoder.Decode(req, &svcDefaults)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == v1beta1.Create {
		v.Logger.Info("validate create", "name", svcDefaults.Name)
		var svcDefaultsList ServiceDefaultsList
		if err := v.Client.List(context.Background(), &svcDefaultsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		for _, item := range svcDefaultsList.Items {
			if item.Name == svcDefaults.Name {
				return admission.Errored(http.StatusBadRequest, fmt.Errorf("ServiceDefaults resource with name %q is already defined – all ServiceDefaults resources must have unique names across namespaces",
					svcDefaults.Name))
			}
		}
	}
	svcDefaults.Default()
	if err := svcDefaults.Validate(); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Allowed("Valid Service Defaults Request")
}

func (v *serviceDefaultsValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
