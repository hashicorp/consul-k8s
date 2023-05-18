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

// consulHTTPRouteFor returns the Consul HTTPRouteConfigEntry for the given reference.
func (b *Binder) consulHTTPRouteFor(ref api.ResourceReference) *api.HTTPRouteConfigEntry {
	for _, route := range b.config.ConsulHTTPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

// consulTCPRouteFor returns the Consul TCPRouteConfigEntry for the given reference.
func (b *Binder) consulTCPRouteFor(ref api.ResourceReference) *api.TCPRouteConfigEntry {
	for _, route := range b.config.ConsulTCPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

// routeBinder encapsulates the binding logic for binding a route to the given Gateway.
// The logic for route binding is almost identical between different route types, but
// due to the strong typing in the Spec and Go's inability to deal with fields via generics
// we have to pull in a bunch of accessors (which ideally should be in the upstream spec)
// for each route type.
//
// From the generic signature -- T: the type of Kubernetes route, U: the type of Consul config entry
//
// TODO: consider moving the function closures to something like an interface that we can
// implement the accessors on for each route type.
type routeBinder[T client.Object, U api.ConfigEntry] struct {
	// isGatewayDeleted is used to determine whether we should just ignore
	// attempting to bind the route (since we no longer know whether we
	// should manage the route we only want to remove any state we've
	// set on it).
	isGatewayDeleted bool
	// gateway is the gateway that we want to use for binding
	gateway *gwv1beta1.Gateway
	// gatewayRef is a Consul reference used to prune no-longer bound
	// parents from a Consul resource we've created.
	gatewayRef api.ResourceReference
	// tracker is the referenceTracker used to determine when we want to cleanup
	// routes based on a deleted gateway.
	tracker referenceTracker
	// namespaces is the set of namespaces in Consul that use for determining
	// whether a route in a given namespace can bind to a gateway with AllowedRoutes set
	namespaces map[string]corev1.Namespace
	// services is a catalog of all connect-injected services to check a route against
	// for resolving its backend refs
	services map[types.NamespacedName]api.CatalogService

	// translationReferenceFunc is a function used to translate a Kubernetes object into
	// a Consul object reference
	translationReferenceFunc func(route T) api.ResourceReference
	// lookupFunc is a function used for finding an existing Consul object based on
	// its object reference
	lookupFunc func(api.ResourceReference) U
	// getParentsFunc is a function used for getting the parent references of a Consul route object
	getParentsFunc func(U) []api.ResourceReference
	// setParentsFunc is a function used for setting the parent references of a route object
	setParentsFunc func(U, []api.ResourceReference)
	// removeStatusRefsFunc is a function used for removing the statuses for the given parent
	// references from a route
	removeStatusRefsFunc func(T, []gwv1beta1.ParentReference) bool
	// getHostnamesFunc is a function used for getting the hostnames associated with a route
	getHostnamesFunc func(T) []gwv1beta1.Hostname
	// getParentRefsFunc is used for getting the parent references of a Kubernetes route object
	getParentRefsFunc func(T) []gwv1beta1.ParentReference
	// translationFunc is used for translating a Kubernetes route into the corresponding Consul config entry
	translationFunc func(T, map[types.NamespacedName]api.ResourceReference) U
	// setRouteConditionFunc is used for adding or overwriting a condition on a route at the given
	// parent
	setRouteConditionFunc func(T, *gwv1beta1.ParentReference, metav1.Condition) bool
	// getBackendRefsFunc returns a list of all backend references that we need to validate against the
	// list of known connect-injected services
	getBackendRefsFunc func(T) []gwv1beta1.BackendRef
	// removeControllerStatusFunc is used to remove all of the statuses set by our controller when GC'ing
	// a route
	removeControllerStatusFunc func(T) bool
}

// newRouteBinder creates a new route binder for the given Kubernetes and Consul route types
// generally this is lightly wrapped by other constructors that pass in the various closures
// needed for accessing fields on the objects
func newRouteBinder[T client.Object, U api.ConfigEntry](
	isGatewayDeleted bool,
	gateway *gwv1beta1.Gateway,
	gatewayRef api.ResourceReference,
	namespaces map[string]corev1.Namespace,
	services map[types.NamespacedName]api.CatalogService,
	tracker referenceTracker,
	translationReferenceFunc func(route T) api.ResourceReference,
	lookupFunc func(api.ResourceReference) U,
	getParentsFunc func(U) []api.ResourceReference,
	setParentsFunc func(U, []api.ResourceReference),
	removeStatusRefsFunc func(T, []gwv1beta1.ParentReference) bool,
	getHostnamesFunc func(T) []gwv1beta1.Hostname,
	getParentRefsFunc func(T) []gwv1beta1.ParentReference,
	translationFunc func(T, map[types.NamespacedName]api.ResourceReference) U,
	setRouteConditionFunc func(T, *gwv1beta1.ParentReference, metav1.Condition) bool,
	getBackendRefsFunc func(T) []gwv1beta1.BackendRef,
	removeControllerStatusFunc func(T) bool,
) *routeBinder[T, U] {
	return &routeBinder[T, U]{
		isGatewayDeleted:           isGatewayDeleted,
		gateway:                    gateway,
		gatewayRef:                 gatewayRef,
		namespaces:                 namespaces,
		services:                   services,
		tracker:                    tracker,
		translationReferenceFunc:   translationReferenceFunc,
		lookupFunc:                 lookupFunc,
		getParentsFunc:             getParentsFunc,
		setParentsFunc:             setParentsFunc,
		removeStatusRefsFunc:       removeStatusRefsFunc,
		getHostnamesFunc:           getHostnamesFunc,
		getParentRefsFunc:          getParentRefsFunc,
		translationFunc:            translationFunc,
		setRouteConditionFunc:      setRouteConditionFunc,
		getBackendRefsFunc:         getBackendRefsFunc,
		removeControllerStatusFunc: removeControllerStatusFunc,
	}
}

func (r *routeBinder[T, U]) bind(route T, boundCount map[gwv1beta1.SectionName]int, seenRoutes map[api.ResourceReference]struct{}, snapshot Snapshot) (updatedSnapshot Snapshot) {
	routeRef := r.translationReferenceFunc(route)
	existing := r.lookupFunc(routeRef)
	seenRoutes[routeRef] = struct{}{}

	gatewayRefs := filterParentRefs(objectToMeta(r.gateway), route.GetNamespace(), r.getParentRefsFunc(route))

	var consulUpdate U
	consulNeedsDelete := false
	kubernetesNeedsUpdate := false
	kubernetesNeedsStatusUpdate := false

	defer func() {
		if !isNil(consulUpdate) {
			snapshot.Consul.Updates = append(snapshot.Consul.Updates, consulUpdate)
		}
		if consulNeedsDelete {
			snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
		}
		if kubernetesNeedsUpdate {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
		}
		if kubernetesNeedsStatusUpdate {
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
		}

		updatedSnapshot = snapshot
	}()

	if isDeleted(route) {
		consulNeedsDelete = true
		if removeFinalizer(route) {
			kubernetesNeedsUpdate = true
		}
		return
	}

	if r.isGatewayDeleted {
		// first check if this is our only ref for the route
		if r.tracker.isLastReference(route) {
			// if it is, then mark everything for deletion
			consulNeedsDelete = true
			if r.removeControllerStatusFunc(route) {
				kubernetesNeedsStatusUpdate = true
			}
			if removeFinalizer(route) {
				kubernetesNeedsUpdate = true
			}
			return
		}

		// otherwise remove the condition since we no longer know if we should
		// control the route and drop any references for the Consul route
		if !isNil(existing) {
			// this drops all the parent refs
			r.setParentsFunc(existing, parentsForRoute(r.gatewayRef, r.getParentsFunc(existing), nil))
			// and then we mark the route as needing updated
			consulUpdate = existing
			// drop the status conditions
			if r.removeStatusRefsFunc(route, gatewayRefs) {
				kubernetesNeedsStatusUpdate = true
			}
		}
		return
	}

	if ensureFinalizer(route) {
		kubernetesNeedsUpdate = true
		return
	}

	// TODO: scrub route refs from statuses that no longer exist

	validation := r.validateRefs(route.GetNamespace(), r.getBackendRefsFunc(route))
	// the spec is dumb and makes you set a parent for any status, even when the
	// status is not with respect to a parent, as is the case of resolved refs
	// so we need to set the status on all parents
	for _, ref := range gatewayRefs {
		if r.setRouteConditionFunc(route, &ref, validation.Condition()) {
			kubernetesNeedsStatusUpdate = true
		}
	}

	results := make(parentBindResults, 0)
	namespace := r.namespaces[route.GetNamespace()]
	gk := route.GetObjectKind().GroupVersionKind().GroupKind()
	for _, ref := range gatewayRefs {
		result := make(bindResults, 0)
		for _, listener := range listenersFor(r.gateway, ref.SectionName) {
			if !routeKindIsAllowedForListener(supportedKindsForProtocol[listener.Protocol], gk) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Protocol,
				})
				continue
			}

			if !routeKindIsAllowedForListenerExplicit(listener.AllowedRoutes, gk) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Protocol,
				})
				continue
			}

			if !routeAllowedForListenerNamespaces(r.gateway.Namespace, listener.AllowedRoutes, namespace) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Namespace,
				})
				continue
			}

			if !routeAllowedForListenerHostname(listener.Hostname, r.getHostnamesFunc(route)) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNoMatchingListenerHostname,
				})
				continue
			}

			result = append(result, bindResult{
				section: listener.Name,
			})

			boundCount[listener.Name] = boundCount[listener.Name] + 1
		}

		results = append(results, parentBindResult{
			parent:  ref,
			results: result,
		})
	}

	updated := false
	for _, result := range results {
		if r.setRouteConditionFunc(route, &result.parent, result.results.Condition()) {
			updated = true
		}
	}

	if updated {
		kubernetesNeedsStatusUpdate = true
	}

	entry := r.translationFunc(route, nil)
	// make all parent refs explicit based on what actually bound
	if isNil(existing) {
		r.setParentsFunc(entry, parentsForRoute(r.gatewayRef, nil, results))
	} else {
		r.setParentsFunc(entry, parentsForRoute(r.gatewayRef, r.getParentsFunc(existing), results))
	}
	consulUpdate = entry

	return
}

func (b *Binder) newTCPRouteBinder(tracker referenceTracker, services map[types.NamespacedName]api.CatalogService) *routeBinder[*gwv1alpha2.TCPRoute, *api.TCPRouteConfigEntry] {
	return newRouteBinder(
		b.isGatewayDeleted(),
		&b.config.Gateway,
		b.gatewayRef(),
		b.config.Namespaces,
		services,
		tracker,
		b.config.Translator.ReferenceForTCPRoute,
		b.consulTCPRouteFor,
		func(t *api.TCPRouteConfigEntry) []api.ResourceReference { return t.Parents },
		func(t *api.TCPRouteConfigEntry, parents []api.ResourceReference) { t.Parents = parents },
		b.statusSetter.RemoveTCPRouteReferences,
		func(t *gwv1alpha2.TCPRoute) []gwv1beta1.Hostname { return nil },
		func(t *gwv1alpha2.TCPRoute) []gwv1beta1.ParentReference { return t.Spec.ParentRefs },
		b.config.Translator.TCPRouteToTCPRoute,
		b.statusSetter.SetTCPRouteCondition,
		func(t *gwv1alpha2.TCPRoute) []gwv1beta1.BackendRef {
			refs := []gwv1beta1.BackendRef{}
			for _, rule := range t.Spec.Rules {
				refs = append(refs, rule.BackendRefs...)
			}
			return refs
		},
		b.statusSetter.RemoveTCPStatuses,
	)
}

func (b *Binder) newHTTPRouteBinder(tracker referenceTracker, services map[types.NamespacedName]api.CatalogService) *routeBinder[*gwv1beta1.HTTPRoute, *api.HTTPRouteConfigEntry] {
	return newRouteBinder(
		b.isGatewayDeleted(),
		&b.config.Gateway,
		b.gatewayRef(),
		b.config.Namespaces,
		services,
		tracker,
		b.config.Translator.ReferenceForHTTPRoute,
		b.consulHTTPRouteFor,
		func(t *api.HTTPRouteConfigEntry) []api.ResourceReference { return t.Parents },
		func(t *api.HTTPRouteConfigEntry, parents []api.ResourceReference) { t.Parents = parents },
		b.statusSetter.RemoveHTTPRouteReferences,
		func(t *gwv1beta1.HTTPRoute) []gwv1beta1.Hostname { return t.Spec.Hostnames },
		func(t *gwv1beta1.HTTPRoute) []gwv1beta1.ParentReference { return t.Spec.ParentRefs },
		b.config.Translator.HTTPRouteToHTTPRoute,
		b.statusSetter.SetHTTPRouteCondition,
		func(t *gwv1beta1.HTTPRoute) []gwv1beta1.BackendRef {
			refs := []gwv1beta1.BackendRef{}
			for _, rule := range t.Spec.Rules {
				for _, ref := range rule.BackendRefs {
					refs = append(refs, ref.BackendRef)
				}
			}
			return refs
		},
		b.statusSetter.RemoveHTTPStatuses,
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
