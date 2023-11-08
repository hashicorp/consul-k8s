// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// +kubebuilder:object:generate=false

type ProxyConfigurationWebhook struct {
	Logger logr.Logger

	// ConsulTenancyConfig contains the injector's namespace and partition configuration.
	ConsulTenancyConfig common.ConsulTenancyConfig

	decoder *admission.Decoder
	client.Client
}

var _ common.MeshConfigLister = &ProxyConfigurationWebhook{}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/inject-connect/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v2beta1-proxyconfiguration,mutating=true,failurePolicy=fail,groups=auth.consul.hashicorp.com,resources=proxyconfiguration,versions=v2beta1,name=mutate-proxyconfiguration.auth.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ProxyConfigurationWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resource ProxyConfiguration
	err := v.decoder.Decode(req, &resource)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return common.ValidateMeshConfig(ctx, req, v.Logger, v, &resource, v.ConsulTenancyConfig)
}

func (v *ProxyConfigurationWebhook) List(ctx context.Context) ([]common.MeshConfig, error) {
	var resourceList ProxyConfigurationList
	if err := v.Client.List(ctx, &resourceList); err != nil {
		return nil, err
	}
	var entries []common.MeshConfig
	for _, item := range resourceList.Items {
		entries = append(entries, common.MeshConfig(item))
	}
	return entries, nil
}

func (v *ProxyConfigurationWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
