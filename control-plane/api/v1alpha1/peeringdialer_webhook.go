package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:object:generate=false

type PeeringDialerWebhook struct {
	client.Client
	Logger  logr.Logger
	decoder *admission.Decoder
}

// NOTE: The path value in the below line is the path to the webhook.
// If it is updated, run code-gen, update subcommand/controller/command.go
// and the consul-helm value for the path to the webhook.
//
// NOTE: The below line cannot be combined with any other comment. If it is
// it will break the code generation.
//
// +kubebuilder:webhook:verbs=create;update,path=/mutate-v1alpha1-peeringdialers,mutating=true,failurePolicy=fail,groups=consul.hashicorp.com,resources=peeringdialers,versions=v1alpha1,name=mutate-peeringdialers.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *PeeringDialerWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var dialer PeeringDialer
	var dialerList PeeringDialerList
	err := v.decoder.Decode(req, &dialer)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Call validate first to ensure all the fields are validated before checking for secret name duplicates.
	if err := dialer.Validate(); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		v.Logger.Info("validate create", "name", dialer.KubernetesName())

		if err := v.Client.List(ctx, &dialerList); err != nil {
			return admission.Errored(http.StatusInternalServerError, err)
		}

		for _, item := range dialerList.Items {
			if item.Namespace == dialer.Namespace && item.Secret().Name == dialer.Secret().Name {
				return admission.Errored(http.StatusBadRequest,
					fmt.Errorf("an existing PeeringDialer resource has the same secret name `name: %s, namespace: %s`", dialer.Secret().Name, dialer.Namespace))
			}
		}
	}

	return admission.Allowed(fmt.Sprintf("valid %s request", dialer.KubeKind()))
}

func (v *PeeringDialerWebhook) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}
