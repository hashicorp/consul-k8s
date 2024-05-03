// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// +kubebuilder:object:generate=false

type ServiceSplitterWebhook struct {
	Logger logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

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

	return common.ValidateConfigEntry(ctx, req, v.Logger, v, &serviceSplitter, v.ConsulMeta)
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

func (v *ServiceSplitterWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-servicesplitter", &admission.Webhook{Handler: v})
}
