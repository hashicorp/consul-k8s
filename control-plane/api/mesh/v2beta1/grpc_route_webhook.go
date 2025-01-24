// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

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

type GRPCRouteWebhook struct {
	Logger logr.Logger

	// ConsulTenancyConfig contains the injector's namespace and partition configuration.
	ConsulTenancyConfig common.ConsulTenancyConfig

	decoder *admission.Decoder
	client.Client
}

var _ common.ConsulResourceLister = &GRPCRouteWebhook{}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/inject-connect/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v2beta1-grpcroute,mutating=true,failurePolicy=fail,groups=auth.consul.hashicorp.com,resources=grpcroute,versions=v2beta1,name=mutate-grpcroute.auth.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *GRPCRouteWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resource GRPCRoute
	err := v.decoder.Decode(req, &resource)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return common.ValidateConsulResource(ctx, req, v.Logger, v, &resource, v.ConsulTenancyConfig)
}

func (v *GRPCRouteWebhook) List(ctx context.Context) ([]common.ConsulResource, error) {
	var resourceList GRPCRouteList
	if err := v.Client.List(ctx, &resourceList); err != nil {
		return nil, err
	}
	var entries []common.ConsulResource
	for _, item := range resourceList.Items {
		entries = append(entries, common.ConsulResource(item))
	}
	return entries, nil
}

func (v *GRPCRouteWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v2beta1-grpcroute", &admission.Webhook{Handler: v})
}
