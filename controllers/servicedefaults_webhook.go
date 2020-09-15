package controllers

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	capi "github.com/hashicorp/consul/api"
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

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is
// it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-servicedefaults,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicedefaults,versions=v1alpha1,name=mutate-servicedefaults.consul.hashicorp.com

func (v *serviceDefaultsValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcDefaults v1alpha1.ServiceDefaults
	err := v.decoder.Decode(req, &svcDefaults)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return ValidateConfigEntry(ctx,
		req,
		v.Logger,
		v,
		&svcDefaults,
		"ServiceDefaults")
}

func (v *serviceDefaultsValidator) List(ctx context.Context) ([]ConfigEntryCRD, error) {
	var svcDefaultsList v1alpha1.ServiceDefaultsList
	if err := v.Client.List(ctx, &svcDefaultsList); err != nil {
		return nil, err
	}
	var entries []ConfigEntryCRD
	for _, item := range svcDefaultsList.Items {
		entries = append(entries, ConfigEntryCRD(&item))
	}
	return entries, nil
}

func (v *serviceDefaultsValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
