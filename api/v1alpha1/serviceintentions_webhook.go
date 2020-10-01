package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	"k8s.io/api/admission/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:object:generate=false

type ServiceIntentionsWebhook struct {
	client.Client
	ConsulClient           *capi.Client
	Logger                 logr.Logger
	decoder                *admission.Decoder
	EnableConsulNamespaces bool
	EnableNSMirroring      bool
}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is
// it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-serviceintentions,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=serviceintentions,versions=v1alpha1,name=mutate-serviceintentions.consul.hashicorp.com,webhookVersions=v1beta1,sideEffects=None

func (v *ServiceIntentionsWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var svcIntentions ServiceIntentions
	var svcIntentionsList ServiceIntentionsList
	err := v.decoder.Decode(req, &svcIntentions)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	singleConsulDestNS := !(v.EnableConsulNamespaces && v.EnableNSMirroring)
	if req.Operation == v1beta1.Create {
		v.Logger.Info("validate create", "name", svcIntentions.KubernetesName())

		if err := v.Client.List(ctx, &svcIntentionsList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		for _, item := range svcIntentionsList.Items {
			if singleConsulDestNS {
				// Only verify if the name is the same here as the destination namespace in consul will be identical.
				if item.Spec.Name == svcIntentions.Spec.Name {
					return admission.Errored(http.StatusBadRequest,
						fmt.Errorf("%s resource to manage intentions for service %q is already defined – all %s resources must manage unique services",
							svcIntentions.KubeKind(),
							svcIntentions.Spec.Name,
							svcIntentions.KubeKind()))
				}
				// Verify both name and namespace are the same here as the destination namespace in consul will match the namespace mentioned.
			} else if item.Spec.Name == svcIntentions.Spec.Name && item.Spec.Namespace == svcIntentions.Spec.Namespace {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("%s resource to manage intentions for service %q in namespace %q is already defined – all %s resources must manage unique services across namespaces",
						svcIntentions.KubeKind(),
						svcIntentions.Spec.Name,
						svcIntentions.Spec.Namespace,
						svcIntentions.KubeKind()))
			}
		}
	}
	if req.Operation == v1beta1.Update {
		v.Logger.Info("validate update", "name", svcIntentions.KubernetesName())
		prevIntention := req.OldObject.Object.(*ServiceIntentions)
		newIntention := req.Object.Object.(*ServiceIntentions)

		// validate that name and namespace of a resource cannot be updated so ensure no dangling intentions in Consul
		if prevIntention.Spec.Name != newIntention.Spec.Name || prevIntention.Spec.Namespace != newIntention.Spec.Namespace {
			return admission.Errored(http.StatusBadRequest,
				fmt.Errorf("spec.name and spec.namespace are immutable field for %s",
					svcIntentions.KubeKind()))
		}
	}

	if err := svcIntentions.Validate(); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	return admission.Allowed(fmt.Sprintf("valid %s request", svcIntentions.KubeKind()))
}

func (v *ServiceIntentionsWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
