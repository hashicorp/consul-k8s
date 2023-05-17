package binding

import (
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func (b *Binder) consulHTTPRouteFor(ref api.ResourceReference) *api.HTTPRouteConfigEntry {
	for _, route := range b.config.ConsulHTTPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

func (b *Binder) consulTCPRouteFor(ref api.ResourceReference) *api.TCPRouteConfigEntry {
	for _, route := range b.config.ConsulTCPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

type routeBinder[T client.Object, U api.ConfigEntry] struct {
	isGatewayDeleted         bool
	gateway                  *gwv1beta1.Gateway
	gatewayRef               api.ResourceReference
	tracker                  referenceTracker
	namespaces               map[string]corev1.Namespace
	translationReferenceFunc func(route T) api.ResourceReference
	lookupFunc               func(api.ResourceReference) U
	getParentsFunc           func(U) []api.ResourceReference
	setParentsFunc           func(U, []api.ResourceReference)
	removeStatusRefsFunc     func(T, []gwv1beta1.ParentReference) bool
	getHostnamesFunc         func(T) []gwv1beta1.Hostname
	getParentRefsFunc        func(T) []gwv1beta1.ParentReference
	translationFunc          func(T, map[types.NamespacedName]api.ResourceReference) U
	setRouteConditionFunc    func(T, gwv1beta1.ParentReference, metav1.Condition) bool
}

func newRouteBinder[T client.Object, U api.ConfigEntry](
	isGatewayDeleted bool,
	gateway *gwv1beta1.Gateway,
	gatewayRef api.ResourceReference,
	namespaces map[string]corev1.Namespace,
	tracker referenceTracker,
	translationReferenceFunc func(route T) api.ResourceReference,
	lookupFunc func(api.ResourceReference) U,
	getParentsFunc func(U) []api.ResourceReference,
	setParentsFunc func(U, []api.ResourceReference),
	removeStatusRefsFunc func(T, []gwv1beta1.ParentReference) bool,
	getHostnamesFunc func(T) []gwv1beta1.Hostname,
	getParentRefsFunc func(T) []gwv1beta1.ParentReference,
	translationFunc func(T, map[types.NamespacedName]api.ResourceReference) U,
	setRouteConditionFunc func(T, gwv1beta1.ParentReference, metav1.Condition) bool,
) *routeBinder[T, U] {
	return &routeBinder[T, U]{
		isGatewayDeleted:         isGatewayDeleted,
		gateway:                  gateway,
		gatewayRef:               gatewayRef,
		namespaces:               namespaces,
		tracker:                  tracker,
		translationReferenceFunc: translationReferenceFunc,
		lookupFunc:               lookupFunc,
		getParentsFunc:           getParentsFunc,
		setParentsFunc:           setParentsFunc,
		removeStatusRefsFunc:     removeStatusRefsFunc,
		getHostnamesFunc:         getHostnamesFunc,
		getParentRefsFunc:        getParentRefsFunc,
		translationFunc:          translationFunc,
		setRouteConditionFunc:    setRouteConditionFunc,
	}
}

func (r *routeBinder[T, U]) bind(route T, seenRoutes map[api.ResourceReference]struct{}, snapshot Snapshot) Snapshot {
	routeRef := r.translationReferenceFunc(route)
	existing := r.lookupFunc(routeRef)
	seenRoutes[routeRef] = struct{}{}

	gatewayRefs := filterParentRefs(objectToMeta(r.gateway), route.GetNamespace(), r.getParentRefsFunc(route))

	if isDeleted(route) {
		snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
		if removeFinalizer(route) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
		}
		// TODO: drop the number of bound routes from the gateway if necessary
		return snapshot
	}

	if r.isGatewayDeleted {
		// first check if this is our only ref for the route
		if r.tracker.isLastReference(route) {
			// if it is, then mark everything for deletion
			snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
			if removeFinalizer(route) {
				snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
			}
			return snapshot
		}

		// otherwise remove the condition since we no longer know if we should
		// control the route and drop any references for the Consul route
		if !isNil(existing) {
			// this drops all the parent refs
			r.setParentsFunc(existing, parentsForRoute(r.gatewayRef, r.getParentsFunc(existing), nil))
			// and then we mark the route as needing updated
			snapshot.Consul.Updates = append(snapshot.Consul.Updates, existing)
			// drop the status conditions
			if r.removeStatusRefsFunc(route, gatewayRefs) {
				snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
			}
		}
		return snapshot
	}

	if ensureFinalizer(route) {
		snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
		return snapshot
	}

	// TODO: add validation statuses for routes for referenced services
	results := make(parentBindResults, 0)
	namespace := r.namespaces[route.GetNamespace()]
	gk := route.GetObjectKind().GroupVersionKind().GroupKind()
	for _, ref := range gatewayRefs {
		result := make(bindResults, 0)
		for _, listener := range listenersFor(r.gateway, ref.SectionName) {
			if !routeKindIsAllowedForListener(supportedKindsForProtocol[listener.Protocol], gk) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errNotAllowedByListenerProtocol,
				})
				continue
			}

			if !routeKindIsAllowedForListenerExplicit(listener.AllowedRoutes, gk) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errNotAllowedByListenerProtocol,
				})
				continue
			}

			if !routeAllowedForListenerNamespaces(r.gateway.Namespace, listener.AllowedRoutes, namespace) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errNotAllowedByListenerNamespace,
				})
				continue
			}

			if !routeAllowedForListenerHostname(listener.Hostname, r.getHostnamesFunc(route)) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errNoMatchingListenerHostname,
				})
				continue
			}

			result = append(result, bindResult{
				section: listener.Name,
			})
		}

		results = append(results, parentBindResult{
			parent:  ref,
			results: result,
		})
	}
	// TODO: increment the number of bound routes if necessary

	updated := false
	for _, result := range results {
		if r.setRouteConditionFunc(route, result.parent, result.results.Condition()) {
			updated = true
		}
	}

	if updated {
		snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
	}

	entry := r.translationFunc(route, nil)
	// make all parent refs explicit based on what actually bound
	if isNil(existing) {
		r.setParentsFunc(entry, parentsForRoute(r.gatewayRef, nil, results))
	} else {
		r.setParentsFunc(entry, parentsForRoute(r.gatewayRef, r.getParentsFunc(existing), results))
	}
	snapshot.Consul.Updates = append(snapshot.Consul.Updates, entry)

	return snapshot
}

func (b *Binder) newTCPRouteBinder(tracker referenceTracker) *routeBinder[*gwv1alpha2.TCPRoute, *api.TCPRouteConfigEntry] {
	return newRouteBinder(
		b.isGatewayDeleted(),
		&b.config.Gateway,
		b.gatewayRef(),
		b.config.Namespaces,
		tracker,
		b.config.Translator.ReferenceForTCPRoute,
		b.consulTCPRouteFor,
		func(t *api.TCPRouteConfigEntry) []api.ResourceReference { return t.Parents },
		func(t *api.TCPRouteConfigEntry, parents []api.ResourceReference) { t.Parents = parents },
		b.config.Setter.RemoveTCPRouteReferences,
		func(t *gwv1alpha2.TCPRoute) []gwv1beta1.Hostname { return nil },
		func(t *gwv1alpha2.TCPRoute) []gwv1beta1.ParentReference { return t.Spec.ParentRefs },
		b.config.Translator.TCPRouteToTCPRoute,
		b.config.Setter.SetTCPRouteCondition,
	)
}

func (b *Binder) newHTTPRouteBinder(tracker referenceTracker) *routeBinder[*gwv1beta1.HTTPRoute, *api.HTTPRouteConfigEntry] {
	return newRouteBinder(
		b.isGatewayDeleted(),
		&b.config.Gateway,
		b.gatewayRef(),
		b.config.Namespaces,
		tracker,
		b.config.Translator.ReferenceForHTTPRoute,
		b.consulHTTPRouteFor,
		func(t *api.HTTPRouteConfigEntry) []api.ResourceReference { return t.Parents },
		func(t *api.HTTPRouteConfigEntry, parents []api.ResourceReference) { t.Parents = parents },
		b.config.Setter.RemoveHTTPRouteReferences,
		func(t *gwv1beta1.HTTPRoute) []gwv1beta1.Hostname { return t.Spec.Hostnames },
		func(t *gwv1beta1.HTTPRoute) []gwv1beta1.ParentReference { return t.Spec.ParentRefs },
		b.config.Translator.HTTPRouteToHTTPRoute,
		b.config.Setter.SetHTTPRouteCondition,
	)
}

func cleanRoute[T api.ConfigEntry](
	route T,
	seenRoutes map[api.ResourceReference]struct{},
	snapshot Snapshot,
	gatewayRef api.ResourceReference,
	getParentsFunc func(T) []api.ResourceReference,
	setParentsFunc func(T, []api.ResourceReference),
) Snapshot {
	routeRef := translation.EntryToReference(route)
	if _, ok := seenRoutes[routeRef]; !ok {
		existingParents := getParentsFunc(route)
		parents := parentsForRoute(gatewayRef, existingParents, nil)
		if len(parents) == 0 {
			// we can GC this now since we've dropped all refs from it
			snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
		} else if len(existingParents) != len(parents) {
			// we've mutated the length, which means this route needs an update
			setParentsFunc(route, parents)
			snapshot.Consul.Updates = append(snapshot.Consul.Updates, route)
		}
	}
	return snapshot
}

func (b *Binder) cleanHTTPRoute(route *api.HTTPRouteConfigEntry, seenRoutes map[api.ResourceReference]struct{}, snapshot Snapshot) Snapshot {
	return cleanRoute(route, seenRoutes, snapshot, b.gatewayRef(),
		func(route *api.HTTPRouteConfigEntry) []api.ResourceReference { return route.Parents },
		func(route *api.HTTPRouteConfigEntry, parents []api.ResourceReference) { route.Parents = parents },
	)
}

func (b *Binder) cleanTCPRoute(route *api.TCPRouteConfigEntry, seenRoutes map[api.ResourceReference]struct{}, snapshot Snapshot) Snapshot {
	return cleanRoute(route, seenRoutes, snapshot, b.gatewayRef(),
		func(route *api.TCPRouteConfigEntry) []api.ResourceReference { return route.Parents },
		func(route *api.TCPRouteConfigEntry, parents []api.ResourceReference) { route.Parents = parents },
	)
}

func parentsForRoute(ref api.ResourceReference, existing []api.ResourceReference, results parentBindResults) []api.ResourceReference {
	// store all section names that bound
	parentSet := map[string]struct{}{}
	for _, result := range results {
		for _, r := range result.results {
			if r.err != nil {
				parentSet[string(r.section)] = struct{}{}
			}
		}
	}

	// first, filter out all of the parent refs that don't correspond to this gateway
	parents := []api.ResourceReference{}
	for _, parent := range existing {
		if parent.Kind == api.APIGateway &&
			parent.Name == ref.Name &&
			parent.Namespace == ref.Namespace {
			continue
		}
		parents = append(parents, parent)
	}

	// now construct the bound set
	for parent := range parentSet {
		parents = append(parents, api.ResourceReference{
			Kind:        api.APIGateway,
			Name:        ref.Name,
			Namespace:   ref.Namespace,
			SectionName: parent,
		})
	}
	return parents
}
