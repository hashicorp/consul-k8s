// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type ReferenceValidator struct {
	client.Client
}

func NewReferenceValidator(client client.Client) *ReferenceValidator {
	return &ReferenceValidator{
		client,
	}
}

func (rv *ReferenceValidator) GatewayCanReferenceSecret(ctx context.Context, gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) (bool, error) {
	fromNS := gateway.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: gateway.GroupVersionKind().Group,
		Kind:  gateway.GroupVersionKind().Kind,
	}

	// Kind should default to Secret if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#LL59C21-L59C21
	toNS, toGK := createValuesFromRef(secretRef.Namespace, secretRef.Group, secretRef.Kind, "Secret")

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, string(secretRef.Name), rv.Client)
}

func (rv *ReferenceValidator) HTTPRouteCanReferenceGateway(ctx context.Context, httproute gwv1beta1.HTTPRoute, parentRef gwv1beta1.ParentReference) (bool, error) {
	fromNS := httproute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: httproute.GroupVersionKind().Group,
		Kind:  httproute.GroupVersionKind().Kind,
	}

	// Kind should default to Gateway if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/shared_types.go#L48
	toNS, toGK := createValuesFromRef(parentRef.Namespace, parentRef.Group, parentRef.Kind, "Gateway")

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, string(parentRef.Name), rv.Client)
}

func (rv *ReferenceValidator) HTTPRouteCanReferenceBackend(ctx context.Context, httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) (bool, error) {
	fromNS := httproute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: httproute.GroupVersionKind().Group,
		Kind:  httproute.GroupVersionKind().Kind,
	}

	// Kind should default to Service if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#L106
	toNS, toGK := createValuesFromRef(backendRef.Namespace, backendRef.Group, backendRef.Kind, "Service")

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, string(backendRef.Name), rv.Client)

}

func (rv *ReferenceValidator) TCPRouteCanReferenceGateway(ctx context.Context, tcpRoute gwv1alpha2.TCPRoute, parentRef gwv1beta1.ParentReference) (bool, error) {
	fromNS := tcpRoute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: tcpRoute.GroupVersionKind().Group,
		Kind:  tcpRoute.GroupVersionKind().Kind,
	}

	// Kind should default to Gateway if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/shared_types.go#L48
	toNS, toGK := createValuesFromRef(parentRef.Namespace, parentRef.Group, parentRef.Kind, "Gateway")

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, string(parentRef.Name), rv.Client)
}

func (rv *ReferenceValidator) TCPRouteCanReferenceBackend(ctx context.Context, tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) (bool, error) {
	fromNS := tcpRoute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: tcpRoute.GroupVersionKind().Group,
		Kind:  tcpRoute.GroupVersionKind().Kind,
	}

	// Kind should default to Service if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#L106
	toNS, toGK := createValuesFromRef(backendRef.Namespace, backendRef.Group, backendRef.Kind, "Service")

	return referenceAllowed(ctx, fromGK, fromNS, toGK, toNS, string(backendRef.Name), rv.Client)

}

func createValuesFromRef(ns *gwv1beta1.Namespace, group *gwv1beta1.Group, kind *gwv1beta1.Kind, defaultKind string) (string, metav1.GroupKind) {
	toNS := ""
	if ns != nil {
		toNS = string(*ns)
	}

	gk := metav1.GroupKind{
		Kind: defaultKind,
	}
	if group != nil {
		gk.Group = string(*group)
	}
	if kind != nil {
		gk.Kind = string(*kind)
	}

	return toNS, gk
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
	refGrants, err := getReferenceGrantsInNamespace(ctx, toNamespace, c)
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

// This function will get all reference grants in the given namespace.
func getReferenceGrantsInNamespace(ctx context.Context, namespace string, c client.Client) ([]gwv1beta1.ReferenceGrant, error) {
	refGrantList := &gwv1beta1.ReferenceGrantList{}
	if err := c.List(ctx, refGrantList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	refGrants := refGrantList.Items

	return refGrants, nil
}
