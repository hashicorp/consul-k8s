// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-exportedservices,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=exportedservices,versions=v1alpha1,name=mutate-exportedservices.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ControlPlaneRequestLimitWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	// TODO: no idea what I'm supposed to be doing here. The docs say copy and existing webhook?
	// They're all different.

	return nil
}

func (v *ControlPlaneRequestLimitWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
