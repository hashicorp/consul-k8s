// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1exp "sigs.k8s.io/gateway-api-exp/apis/v1beta1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

type referenceValidator struct {
	oldgrants map[string]map[types.NamespacedName]gwv1beta1.ReferenceGrant
	expgrants map[string]map[types.NamespacedName]gwv1beta1exp.ReferenceGrant
}

func NewReferenceValidator(grants []any, old bool) common.ReferenceValidator {
	v := &referenceValidator{
		oldgrants: make(map[string]map[types.NamespacedName]gwv1beta1.ReferenceGrant),
		expgrants: make(map[string]map[types.NamespacedName]gwv1beta1exp.ReferenceGrant),
	}

	for _, g := range grants {
		switch rg := g.(type) {
		case *gwv1beta1.ReferenceGrant:
			if !old {
				continue
			}
			ns := rg.Namespace
			key := types.NamespacedName{
				Namespace: rg.Namespace,
				Name:      rg.Name,
			}
			if _, ok := v.oldgrants[ns]; !ok {
				v.oldgrants[ns] = make(map[types.NamespacedName]gwv1beta1.ReferenceGrant)
			}
			v.oldgrants[ns][key] = *rg
		case *gwv1beta1exp.ReferenceGrant:
			if old {
				continue
			}
			ns := rg.Namespace
			key := types.NamespacedName{
				Namespace: rg.Namespace,
				Name:      rg.Name,
			}
			if _, ok := v.expgrants[ns]; !ok {
				v.expgrants[ns] = make(map[types.NamespacedName]gwv1beta1exp.ReferenceGrant)
			}
			v.expgrants[ns][key] = *rg
		}

	}
	return v
}

func (rv *referenceValidator) GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) bool {
	fromNS := gateway.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: gateway.GroupVersionKind().Group,
		Kind:  gateway.GroupVersionKind().Kind,
	}

	// Kind should default to Secret if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#LL59C21-L59C21
	toNS, toGK := createValuesFromRef(secretRef.Namespace, secretRef.Group, secretRef.Kind, "", common.KindSecret)

	return rv.referenceAllowed(fromGK, fromNS, toGK, toNS, string(secretRef.Name))
}

func (rv *referenceValidator) HTTPRouteCanReferenceBackend(httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) bool {
	fromNS := httproute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: httproute.GroupVersionKind().Group,
		Kind:  httproute.GroupVersionKind().Kind,
	}

	// Kind should default to Service if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#L106
	toNS, toGK := createValuesFromRef(backendRef.Namespace, backendRef.Group, backendRef.Kind, "", common.KindService)

	return rv.referenceAllowed(fromGK, fromNS, toGK, toNS, string(backendRef.Name))
}

func (rv *referenceValidator) TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) bool {
	fromNS := tcpRoute.GetNamespace()
	fromGK := metav1.GroupKind{
		Group: tcpRoute.GroupVersionKind().Group,
		Kind:  tcpRoute.GroupVersionKind().Kind,
	}

	// Kind should default to Service if not set
	// https://github.com/kubernetes-sigs/gateway-api/blob/v0.6.2/apis/v1beta1/object_reference_types.go#L106
	toNS, toGK := createValuesFromRef(backendRef.Namespace, backendRef.Group, backendRef.Kind, common.BetaGroup, common.KindService)

	return rv.referenceAllowed(fromGK, fromNS, toGK, toNS, string(backendRef.Name))
}

func createValuesFromRef(ns *gwv1beta1.Namespace, group *gwv1beta1.Group, kind *gwv1beta1.Kind, defaultGroup, defaultKind string) (string, metav1.GroupKind) {
	toNS := ""
	if ns != nil {
		toNS = string(*ns)
	}

	gk := metav1.GroupKind{
		Kind:  defaultKind,
		Group: defaultGroup,
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
func (rv *referenceValidator) referenceAllowed(fromGK metav1.GroupKind, fromNamespace string, toGK metav1.GroupKind, toNamespace, toName string) bool {
	// Reference does not cross namespaces
	if toNamespace == "" || toNamespace == fromNamespace {
		return true
	}

	// Fetch all ReferenceGrants in the referenced namespace
	grants, ok := rv.grants[toNamespace]
	if !ok {
		return false
	}

	for _, grant := range grants {
		// Check for a From that applies
		fromMatch := false
		for _, from := range grant.Spec.From {
			if fromGK.Group == string(from.Group) && fromGK.Kind == string(from.Kind) && fromNamespace == string(from.Namespace) {
				fromMatch = true
				break
			}
		}

		if !fromMatch {
			continue
		}

		// Check for a To that applies
		for _, to := range grant.Spec.To {
			if toGK.Group == string(to.Group) && toGK.Kind == string(to.Kind) {
				if to.Name == nil || *to.Name == "" {
					// No name specified is treated as a wildcard within the namespace
					return true
				}

				if gwv1beta1.ObjectName(toName) == *to.Name {
					// The ReferenceGrant specifically targets this object
					return true
				}
			}
		}
	}

	// No ReferenceGrant was found which allows this cross-namespace reference
	return false
}
