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

type ControlPlaneRequestLimitWebhook struct {
	client.Client
	Logger     logr.Logger
	decoder    *admission.Decoder
	ConsulMeta common.ConsulMeta
}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is
// it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-controlplanerequestlimits,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=controlplanerequestlimits,versions=v1alpha1,name=mutate-controlplanerequestlimits.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ControlPlaneRequestLimitWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var limit ControlPlaneRequestLimit
	var limitList ControlPlaneRequestLimitList
	err := v.decoder.Decode(req, &limit)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		v.Logger.Info("validate create", "name", limit.KubernetesName())

		if limit.KubernetesName() != common.ControlPlaneRequestLimit {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf(`%s resource name must be "%s"`,
					limit.KubeKind(), common.ControlPlaneRequestLimit))
		}

		if err := v.Client.List(ctx, &limitList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if len(limitList.Items) > 0 {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf("%s resource already defined - only one control plane request limit entry is supported",
					limit.KubeKind()))
		}
	}

	return common.ValidateConfigEntry(ctx, req, v.Logger, v, &limit, v.ConsulMeta)
}

func (v *ControlPlaneRequestLimitWebhook) List(ctx context.Context) ([]common.ConfigEntryResource, error) {
	var limitList ControlPlaneRequestLimitList
	if err := v.Client.List(ctx, &limitList); err != nil {
		return nil, err
	}
	var entries []common.ConfigEntryResource
	for _, item := range limitList.Items {
		entries = append(entries, common.ConfigEntryResource(&item))
	}
	return entries, nil
}

func (v *ControlPlaneRequestLimitWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-controlplanerequestlimits", &admission.Webhook{Handler: v})
}
