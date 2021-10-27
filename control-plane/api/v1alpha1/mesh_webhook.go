package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:object:generate=false

type MeshWebhook struct {
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
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-mesh,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=mesh,versions=v1alpha1,name=mutate-mesh.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *MeshWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var mesh Mesh
	var meshList MeshList
	err := v.decoder.Decode(req, &mesh)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		v.Logger.Info("validate create", "name", mesh.KubernetesName())

		if mesh.KubernetesName() != common.Mesh {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf(`%s resource name must be "%s"`,
					mesh.KubeKind(), common.Mesh))
		}

		if err := v.Client.List(ctx, &meshList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if len(meshList.Items) > 0 {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf("%s resource already defined - only one mesh entry is supported",
					mesh.KubeKind()))
		}
	}

	return admission.Allowed(fmt.Sprintf("valid %s request", mesh.KubeKind()))
}

func (v *MeshWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
