package binding

import (
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type referenceTracker struct {
	httpRouteReferencesGateways      map[types.NamespacedName]int
	tcpRouteReferencesGateways       map[types.NamespacedName]int
	certificatesReferencedByGateways map[types.NamespacedName]int
}

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

func (r referenceTracker) canGCSecret(key types.NamespacedName) bool {
	return r.certificatesReferencedByGateways[key] == 1
}

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
