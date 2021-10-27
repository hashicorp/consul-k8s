package v1alpha1

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:object:generate=false

type ServiceRouterWebhook struct {
	ConsulClient *capi.Client
	Logger       logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

	decoder *admission.Decoder
	client.Client
}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-servicerouter,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicerouters,versions=v1alpha1,name=mutate-servicerouter.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ServiceRouterWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcRouter ServiceRouter
	err := v.decoder.Decode(req, &svcRouter)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return common.ValidateConfigEntry(ctx, req, v.Logger, v, &svcRouter, v.ConsulMeta)
}

func (v *ServiceRouterWebhook) List(ctx context.Context) ([]common.ConfigEntryResource, error) {
	var svcRouterList ServiceRouterList
	if err := v.Client.List(ctx, &svcRouterList); err != nil {
		return nil, err
	}
	var entries []common.ConfigEntryResource
	for _, item := range svcRouterList.Items {
		entries = append(entries, common.ConfigEntryResource(&item))
	}
	return entries, nil
}

func (v *ServiceRouterWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
