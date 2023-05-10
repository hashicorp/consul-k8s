package controllers

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

//
// client. (K8s client)

type ReferenceValidator struct {
	client.Client
}

/*
		bindValidator.CanBind(ctx, gateway, secret) -> true, false, error
		bindValidator.CanBind(ctx, httproute, gatewayref)
		bindValidator.CanBind(ctx, tcproute, gatewayref)
	    bindValidator.CanBind(ctx, httproute, backendref)
		bindValidator.CanBind(ctx, tcproute, backendref)
*/

func NewReferenceValidator(client client.Client) *ReferenceValidator {
	return &ReferenceValidator{
		client,
	}
}

func (rv ReferenceValidator) HTTPRouteCanReferenceGateway(ctx context.Context, httproute gwv1beta1.HTTPRoute, gatewayRef gwv1beta1.ParentReference) (bool, error) {
	fromNS := httproute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: httproute.GroupVersionKind().Group,
		Kind:  httproute.GroupVersionKind().Kind,
	}

	toName := string(gatewayRef.Name)
	toNS := ""
	if gatewayRef.Namespace != nil {
		toNS = string(*gatewayRef.Namespace)
	}

	// Kind should default to Gateway if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/shared_types.go#L48
	toGK := metav1.GroupKind{Kind: "Gateway"}
	if gatewayRef.Group != nil {
		toGK.Group = string(*gatewayRef.Group)
	}
	if gatewayRef.Kind != nil {
		toGK.Kind = string(*gatewayRef.Kind)
	}

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, toName, rv.Client)

}

// referenceAllowed checks to see if a reference between resources is allowed.
// In particular, references from one namespace to a resource in a different namespace
// require an applicable ReferenceGrant be found in the namespace containing the resource
// being referred to.
//
// For example, a Gateway in namespace "foo" may only reference a Secret in namespace "bar"
// if a ReferenceGrant in namespace "bar" allows references from namespace "foo".
func referenceAllowed(ctx context.Context, fromGK metav1.GroupKind, fromNamespace string, toGK metav1.GroupKind, toNamespace, toName string, c client.Client) (bool, error) {
	// Reference does not cross namespaces
	if toNamespace == "" || toNamespace == fromNamespace {
		return true, nil
	}

	// Fetch all ReferenceGrants in the referenced namespace
	refGrants, err := GetReferenceGrantsInNamespace(ctx, toNamespace, c)
	if err != nil || len(refGrants) == 0 {
		return false, err
	}

	for _, refGrant := range refGrants {
		// Check for a From that applies
		fromMatch := false
		for _, from := range refGrant.Spec.From {
			if fromGK.Group == string(from.Group) && fromGK.Kind == string(from.Kind) && fromNamespace == string(from.Namespace) {
				fromMatch = true
				break
			}
		}

		if !fromMatch {
			continue
		}

		// Check for a To that applies
		for _, to := range refGrant.Spec.To {
			if toGK.Group == string(to.Group) && toGK.Kind == string(to.Kind) {
				if to.Name == nil || *to.Name == "" {
					// No name specified is treated as a wildcard within the namespace
					return true, nil
				}

				if gwv1beta1.ObjectName(toName) == *to.Name {
					// The ReferenceGrant specifically targets this object
					return true, nil
				}
			}
		}
	}

	// No ReferenceGrant was found which allows this cross-namespace reference
	return false, nil
}

func GetReferenceGrantsInNamespace(ctx context.Context, namespace string, c client.Client) ([]gwv1beta1.ReferenceGrant, error) {
	refGrantList := &gwv1beta1.ReferenceGrantList{}
	if err := c.List(ctx, refGrantList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	refGrants := refGrantList.Items

	return refGrants, nil
}
