// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

const OCPGatewayPolicy_GatewayIndex = "__ocpgatewaypolicy_referencing_gateway"

// +kubebuilder:object:generate=false

type OCPGatewayPolicyWebhook struct {
	Logger logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

	decoder *admission.Decoder
	client.Client
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-v1alpha1-ocpgatewaypolicy,mutating=false,failurePolicy=fail,groups=consul.hashicorp.com,resources=ocpgatewaypolicies,versions=v1alpha1,name=validate-ocpgatewaypolicy.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *OCPGatewayPolicyWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resource OCPGatewayPolicy
	err := v.decoder.Decode(req, &resource)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	var list OCPGatewayPolicyList

	gwNamespaceName := types.NamespacedName{Name: resource.Spec.TargetRef.Name, Namespace: resource.Namespace}
	err = v.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(OCPGatewayPolicy_GatewayIndex, gwNamespaceName.String()),
	})

	if err != nil {
		v.Logger.Error(err, "error getting list of policies referencing gateway")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	for _, policy := range list.Items {
		if differentOCPPolicySameTarget(resource, policy) {
			return admission.Denied(fmt.Sprintf("policy targets gateway listener %q that is already the target of an existing policy %q", OCPDerefStringOr(resource.Spec.TargetRef.SectionName, ""), policy.Name))
		}
	}

	return admission.Allowed("gateway policy is valid")
}

func differentOCPPolicySameTarget(resource, policy OCPGatewayPolicy) bool {
	return resource.Name != policy.Name &&
		resource.Spec.TargetRef.Name == policy.Spec.TargetRef.Name &&
		resource.Spec.TargetRef.Group == policy.Spec.TargetRef.Group &&
		resource.Spec.TargetRef.Kind == policy.Spec.TargetRef.Kind &&
		resource.Spec.TargetRef.Namespace == policy.Spec.TargetRef.Namespace &&
		OCPDerefStringOr(resource.Spec.TargetRef.SectionName, "") == OCPDerefStringOr(policy.Spec.TargetRef.SectionName, "")
}

func (v *OCPGatewayPolicyWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/validate-v1alpha1-ocpgatewaypolicy", &admission.Webhook{Handler: v})
}

func OCPDerefStringOr[T ~string, U ~string](v *T, val U) string {
	if v == nil {
		return string(val)
	}
	return string(*v)
}
