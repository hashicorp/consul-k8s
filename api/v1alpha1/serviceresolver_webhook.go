package v1alpha1

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/api/common"
	capi "github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:object:generate=false

type ServiceResolverValidator struct {
	ConsulClient *capi.Client
	Logger       logr.Logger

	// EnableConsulNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which supports namespaces.
	EnableConsulNamespaces bool

	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool

	decoder *admission.Decoder
	client.Client
}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is
// it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-serviceresolver,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=serviceresolvers,versions=v1alpha1,name=mutate-serviceresolver.consul.hashicorp.com

func (v *ServiceResolverValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcResolver ServiceResolver
	err := v.decoder.Decode(req, &svcResolver)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return common.ValidateConfigEntry(ctx,
		req,
		v.Logger,
		v,
		&svcResolver,
		v.EnableConsulNamespaces,
		v.EnableNSMirroring)
}

func (v *ServiceResolverValidator) List(ctx context.Context) ([]common.ConfigEntryResource, error) {
	var svcResolverList ServiceResolverList
	if err := v.Client.List(ctx, &svcResolverList); err != nil {
		return nil, err
	}
	var entries []common.ConfigEntryResource
	for _, item := range svcResolverList.Items {
		entries = append(entries, common.ConfigEntryResource(&item))
	}
	return entries, nil
}

func (v *ServiceResolverValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
