// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// referenceTracker acts as a reference counting object for:
//  1. the number of controlled gateways that are referenced by an HTTPRoute
//  2. the number of controlled gateways that are referenced by a TCPRoute
//  3. the number of gateways that reference a certificate Secret
//
// These are used for determining when dissasociating from a gateway
// should cause us to cleanup a route or certificate both in Consul and
// whatever state we have set on the object in Kubernetes.
type referenceTracker struct {
	httpRouteReferencesGateways      map[types.NamespacedName]int
	tcpRouteReferencesGateways       map[types.NamespacedName]int
	certificatesReferencedByGateways map[types.NamespacedName]int
}

// isLastReference checks if the given gateway is the last controlled gateway
// that a route references. If it is and the gateway has been deleted, we
// should clean up all state created for the route.
func (r referenceTracker) isLastReference(object client.Object) bool {
	key := types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}

	switch object.(type) {
	case *gwv1alpha2.TCPRoute:
		return r.tcpRouteReferencesGateways[key] == 1
	case *gwv1beta1.HTTPRoute:
		return r.httpRouteReferencesGateways[key] == 1
	default:
		return false
	}
}

// canGCSecret checks if we can garbage collect a secret that has
// not been upserted.
func (r referenceTracker) canGCSecret(key types.NamespacedName) bool {
	// should this be 1 or 0?
	return r.certificatesReferencedByGateways[key] == 1
}

// references initializes a referenceTracker based on the HTTPRoutes, TCPRoutes,
// and ControlledGateways associated with this Binder.
func (b *Binder) references() referenceTracker {
	tracker := referenceTracker{
		httpRouteReferencesGateways:      make(map[types.NamespacedName]int),
		tcpRouteReferencesGateways:       make(map[types.NamespacedName]int),
		certificatesReferencedByGateways: make(map[types.NamespacedName]int),
	}

	for _, route := range b.config.HTTPRoutes {
		references := map[types.NamespacedName]struct{}{}
		for _, ref := range route.Spec.ParentRefs {
			for _, gateway := range b.config.ControlledGateways {
				parentName := string(ref.Name)
				parentNamespace := valueOr(ref.Namespace, route.Namespace)
				if nilOrEqual(ref.Group, betaGroup) &&
					nilOrEqual(ref.Kind, kindGateway) &&
					gateway.Namespace == parentNamespace &&
					gateway.Name == parentName {
					// the route references a gateway we control, store the ref to this gateway
					references[types.NamespacedName{
						Namespace: parentNamespace,
						Name:      parentName,
					}] = struct{}{}
				}
			}
		}
		tracker.httpRouteReferencesGateways[types.NamespacedName{
			Namespace: route.Namespace,
			Name:      route.Name,
		}] = len(references)
	}

	for _, route := range b.config.TCPRoutes {
		references := map[types.NamespacedName]struct{}{}
		for _, ref := range route.Spec.ParentRefs {
			for _, gateway := range b.config.ControlledGateways {
				parentName := string(ref.Name)
				parentNamespace := valueOr(ref.Namespace, route.Namespace)
				if nilOrEqual(ref.Group, betaGroup) &&
					nilOrEqual(ref.Kind, kindGateway) &&
					gateway.Namespace == parentNamespace &&
					gateway.Name == parentName {
					// the route references a gateway we control, store the ref to this gateway
					references[types.NamespacedName{
						Namespace: parentNamespace,
						Name:      parentName,
					}] = struct{}{}
				}
			}
		}
		tracker.tcpRouteReferencesGateways[types.NamespacedName{
			Namespace: route.Namespace,
			Name:      route.Name,
		}] = len(references)
	}

	for _, gateway := range b.config.ControlledGateways {
		references := map[types.NamespacedName]struct{}{}
		for _, listener := range gateway.Spec.Listeners {
			if listener.TLS == nil {
				continue
			}
			for _, ref := range listener.TLS.CertificateRefs {
				if nilOrEqual(ref.Group, "") &&
					nilOrEqual(ref.Kind, kindSecret) {
					// the gateway references a secret, store it
					references[types.NamespacedName{
						Namespace: valueOr(ref.Namespace, gateway.Namespace),
						Name:      string(ref.Name),
					}] = struct{}{}
				}
			}
		}

		for ref := range references {
			count := tracker.certificatesReferencedByGateways[ref]
			tracker.certificatesReferencedByGateways[ref] = count + 1
		}
	}

	return tracker
}
