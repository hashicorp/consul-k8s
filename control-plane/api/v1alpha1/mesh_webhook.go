// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// +kubebuilder:object:generate=false

type MeshWebhook struct {
	client.Client
	Logger logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

	decoder *admission.Decoder
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

	return common.ValidateConfigEntry(ctx, req, v.Logger, v, &mesh, v.ConsulMeta)
}

func (v *MeshWebhook) List(ctx context.Context) ([]common.ConfigEntryResource, error) {
	var meshList MeshList
	if err := v.Client.List(ctx, &meshList); err != nil {
		return nil, err
	}
	var entries []common.ConfigEntryResource
	for _, item := range meshList.Items {
		entries = append(entries, common.ConfigEntryResource(&item))
	}
	return entries, nil
}

func (v *MeshWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-mesh", &admission.Webhook{Handler: v})
}
