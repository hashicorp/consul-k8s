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

type ServiceSplitterWebhook struct {
	ConsulClient *capi.Client
	Logger       logr.Logger

	// EnableConsulNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which supports namespaces.
	EnableConsulNamespaces bool

	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool

	// ConsulDestinationNamespace is the namespace in Consul that the config entry created
	// in k8s will get mapped into. If the Consul namespace does not already exist, it will
	// be created.
	ConsulDestinationNamespace string

	// NSMirroringPrefix works in conjunction with Namespace Mirroring.
	// It is the prefix added to the Consul namespace to map to a specific.
	// k8s namespace. For example, if `mirroringK8SPrefix` is set to "k8s-", a
	// service in the k8s `staging` namespace will be registered into the
	// `k8s-staging` Consul namespace.
	NSMirroringPrefix string

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
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-servicesplitter,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=servicesplitters,versions=v1alpha1,name=mutate-servicesplitter.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ServiceSplitterWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var serviceSplitter ServiceSplitter
	err := v.decoder.Decode(req, &serviceSplitter)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return common.ValidateConfigEntry(ctx,
		req,
		v.Logger,
		v,
		&serviceSplitter,
		v.EnableConsulNamespaces,
		v.EnableNSMirroring,
		v.ConsulDestinationNamespace,
		v.NSMirroringPrefix)
}

func (v *ServiceSplitterWebhook) List(ctx context.Context) ([]common.ConfigEntryResource, error) {
	var serviceSplitterList ServiceSplitterList
	if err := v.Client.List(ctx, &serviceSplitterList); err != nil {
		return nil, err
	}
	var entries []common.ConfigEntryResource
	for _, item := range serviceSplitterList.Items {
		entries = append(entries, common.ConfigEntryResource(&item))
	}
	return entries, nil
}

func (v *ServiceSplitterWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
