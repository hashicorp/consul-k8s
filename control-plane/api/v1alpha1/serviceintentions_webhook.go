// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"errors"
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

type ServiceIntentionsWebhook struct {
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
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-serviceintentions,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=serviceintentions,versions=v1alpha1,name=mutate-serviceintentions.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *ServiceIntentionsWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcIntentions ServiceIntentions
	var svcIntentionsList ServiceIntentionsList
	err := v.decoder.Decode(req, &svcIntentions)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	defaultingPatches, err := common.DefaultingPatches(&svcIntentions, v.ConsulMeta)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	singleConsulDestNS := !(v.ConsulMeta.NamespacesEnabled && v.ConsulMeta.Mirroring)
	if req.Operation == admissionv1.Create {
		v.Logger.Info("validate create", "name", svcIntentions.KubernetesName())

		if err := v.Client.List(ctx, &svcIntentionsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		for _, item := range svcIntentionsList.Items {
			if singleConsulDestNS {
				// If all config entries will be registered in the same Consul namespace, then spec.name
				// must be unique for all entries so two custom resources don't configure the same Consul resource.
				if item.Spec.Destination.Name == svcIntentions.Spec.Destination.Name {
					return admission.Errored(http.StatusBadRequest,
						fmt.Errorf("an existing ServiceIntentions resource has `spec.destination.name: %s`", svcIntentions.Spec.Destination.Name))
				}
				// If namespace mirroring is enabled, each config entry will be registered in the Consul namespace
				// set in spec.namespace. Thus we must check that there isn't already a config entry that sets the same spec.name and spec.namespace.
			} else if item.Spec.Destination.Name == svcIntentions.Spec.Destination.Name && item.Spec.Destination.Namespace == svcIntentions.Spec.Destination.Namespace {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("an existing ServiceIntentions resource has `spec.destination.name: %s` and `spec.destination.namespace: %s`", svcIntentions.Spec.Destination.Name, svcIntentions.Spec.Destination.Namespace))
			}
		}
	} else if req.Operation == admissionv1.Update {
		v.Logger.Info("validate update", "name", svcIntentions.KubernetesName())
		var prevIntention, newIntention ServiceIntentions
		if err := v.decoder.DecodeRaw(*req.OldObject.DeepCopy(), &prevIntention); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}
		if err := v.decoder.DecodeRaw(*req.Object.DeepCopy(), &newIntention); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		// validate that name and namespace of a resource cannot be updated so ensure no dangling intentions in Consul
		if prevIntention.Spec.Destination.Name != newIntention.Spec.Destination.Name || prevIntention.Spec.Destination.Namespace != newIntention.Spec.Destination.Namespace {
			return admission.Errored(http.StatusBadRequest, errors.New("spec.destination.name and spec.destination.namespace are immutable fields for ServiceIntentions"))
		}
	}

	// ServiceIntentions are invalid if destination namespaces or source namespaces are set when Consul Namespaces are not enabled.
	if err := svcIntentions.Validate(v.ConsulMeta); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// We always return an admission.Patched() response, even if there are no patches, since
	// admission.Patched() with no patches is equal to admission.Allowed() under
	// the hood.
	return admission.Patched(fmt.Sprintf("valid %s request", svcIntentions.KubeKind()), defaultingPatches...)
}

func (v *ServiceIntentionsWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/mutate-v1alpha1-serviceintentions", &admission.Webhook{Handler: v})
}
