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

const CustomGatewaypolicy_GatewayIndex = "__custom_gatewaypolicy_referencing_gateway"

// +kubebuilder:object:generate=false

type CustomGatewayPolicyWebhook struct {
	Logger logr.Logger

	// ConsulMeta contains metadata specific to the Consul installation.
	ConsulMeta common.ConsulMeta

	decoder admission.Decoder
	client.Client
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-v1alpha1-gatewaypolicy-custom,mutating=false,failurePolicy=fail,groups=consul.hashicorp.com,resources=customgatewaypolicies,versions=v1alpha1,name=validate-customgatewaypolicy.consul.hashicorp.com,sideEffects=None,admissionReviewVersions=v1beta1;v1

func (v *CustomGatewayPolicyWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resource CustomGatewayPolicy
	err := v.decoder.Decode(req, &resource)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	var list CustomGatewayPolicyList

	gwNamespaceName := types.NamespacedName{Name: resource.Spec.TargetRef.Name, Namespace: resource.Namespace}
	err = v.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(CustomGatewaypolicy_GatewayIndex, gwNamespaceName.String()),
	})

	if err != nil {
		v.Logger.Error(err, "error getting list of policies referencing gateway")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// check if the service if the resource is Gateway
	if !v.isTargetRefKindGateway(resource) {
		return admission.Denied(fmt.Sprintf("policy targetRef kind %q is not supported, only Gateway is allowed", resource.Spec.TargetRef.Kind))
	}

	for _, policy := range list.Items {
		if differentCustomPolicySameTarget(resource, policy) {
			return admission.Denied(fmt.Sprintf("policy targets gateway listener %q that is already the target of an existing policy %q", DerefStringOr(resource.Spec.TargetRef.SectionName, ""), policy.Name))
		}
	}

	return admission.Allowed("gateway policy is valid")
}

// func to check the resource targetref.kind is Gateway
func (v *CustomGatewayPolicyWebhook) isTargetRefKindGateway(resource CustomGatewayPolicy) bool {
	return resource.Spec.TargetRef.Kind == "Gateway"
}

func differentCustomPolicySameTarget(resource, policy CustomGatewayPolicy) bool {
	return resource.Name != policy.Name &&
		resource.Spec.TargetRef.Name == policy.Spec.TargetRef.Name &&
		resource.Spec.TargetRef.Group == policy.Spec.TargetRef.Group &&
		resource.Spec.TargetRef.Kind == policy.Spec.TargetRef.Kind &&
		resource.Spec.TargetRef.Namespace == policy.Spec.TargetRef.Namespace &&
		DerefStringOr(resource.Spec.TargetRef.SectionName, "") == DerefStringOr(policy.Spec.TargetRef.SectionName, "")
}

func (v *CustomGatewayPolicyWebhook) SetupWithManager(mgr ctrl.Manager) {
	v.decoder = admission.NewDecoder(mgr.GetScheme())
	mgr.GetWebhookServer().Register("/validate-v1alpha1-gatewaypolicy-custom", &admission.Webhook{Handler: v})
}

// func DerefStringOr[T ~string, U ~string](v *T, val U) string {
// 	if v == nil {
// 		return string(val)
// 	}
// 	return string(*v)
// }
