package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/api/common"
	capi "github.com/hashicorp/consul/api"
	"k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type ProxyDefaultsValidator struct {
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
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-proxydefaults,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=proxydefaults,versions=v1alpha1,name=mutate-proxydefaults.consul.hashicorp.com

func (v *ProxyDefaultsValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	var proxyDefaults ProxyDefaults
	var proxyDefaultsList ProxyDefaultsList
	err := v.decoder.Decode(req, &proxyDefaults)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == v1beta1.Create {
		v.Logger.Info("validate create", "name", proxyDefaults.Name())

		if proxyDefaults.Name() != common.Global {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf(`%s resource name must be "%s"`,
					proxyDefaults.KubeKind(), common.Global))
		}

		if err := v.Client.List(ctx, &proxyDefaultsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if len(proxyDefaultsList.Items) > 0 {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf("%s resource already defined - only one global entry is supported",
					proxyDefaults.KubeKind()))
		}
	}

	if err := proxyDefaults.Validate(); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Allowed(fmt.Sprintf("valid %s request", proxyDefaults.KubeKind()))
}

func (v *ProxyDefaultsValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
