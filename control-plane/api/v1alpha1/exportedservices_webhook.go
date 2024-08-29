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

type ExportedServicesWebhook struct {
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

func (v *ExportedServicesWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var exports ExportedServices
	var exportsList ExportedServicesList
	err := v.decoder.Decode(req, &exports)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		v.Logger.Info("validate create", "name", exports.KubernetesName())

		if err := v.Client.List(ctx, &exportsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		if len(exportsList.Items) > 0 {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf("%s resource already defined - only one exportedservices entry is supported per Kubernetes cluster",
					exports.KubeKind()))
		}
	}

	if err := exports.Validate(v.ConsulMeta); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.Allowed(fmt.Sprintf("valid %s request", exports.KubeKind()))
}

func (v *ExportedServicesWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-exportedservices", &admission.Webhook{Handler: v})
}
