// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	mapset "github.com/deckarep/golang-set"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// bindRoute contains the main logic for binding a route to a given gateway.
func (r *Binder) bindRoute(route client.Object, boundCount map[gwv1beta1.SectionName]int, snapshot Snapshot) (updatedSnapshot Snapshot) {
	// use the non-normalized key since we can't write back enterprise metadata
	// on non-enterprise installations
	routeConsulKey := r.config.Translator.NonNormalizedConfigEntryReference(entryKind(route), client.ObjectKeyFromObject(route))
	filteredParents := filterParentRefs(r.key, route.GetNamespace(), getRouteParents(route))

	// flags to mark that some operation needs to occur
	kubernetesNeedsUpdate := false
	kubernetesNeedsStatusUpdate := false

	// we do this in a closure at the end to make sure we don't accidentally
	// add something multiple times into the list of update/delete operations
	// instead we just set a flag indicating that an update is needed and then
	// append to the snapshot right before returning
	defer func() {
		if kubernetesNeedsUpdate {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
		}
		if kubernetesNeedsStatusUpdate {
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
		}

		updatedSnapshot = snapshot
	}()

	if isDeleted(route) {
		// mark the route as needing to get cleaned up if we detect that it's being deleted
		if removeFinalizer(route) {
			kubernetesNeedsUpdate = true
		}
		return
	}

	if r.isGatewayDeleted() {
		if canGCOnUnbind(routeConsulKey, r.config.Resources) && removeFinalizer(route) {
			kubernetesNeedsUpdate = true
		}

		// remove the condition since we no longer know if we should
		// control the route and drop any references for the Consul route
		dropConsulRouteParent(route, r.nonNormalizedConsulKey, r.config.Resources)
		// drop the status conditions
		if r.statusSetter.removeRouteReferences(route, filteredParents) {
			kubernetesNeedsStatusUpdate = true
		}
		return
	}

	if ensureFinalizer(route) {
		kubernetesNeedsUpdate = true
		return
	}

	// TODO: scrub route refs from statuses that no longer exist

	validation := validateRefs(route.GetNamespace(), getRouteBackends(route), r.config.Resources)
	// the spec is dumb and makes you set a parent for any status, even when the
	// status is not with respect to a parent, as is the case of resolved refs
	// so we need to set the status on all parents
	for _, parent := range filteredParents {
		if r.statusSetter.setRouteCondition(route, &parent, validation.Condition()) {
			kubernetesNeedsStatusUpdate = true
		}
	}

	namespace := r.config.Namespaces[route.GetNamespace()]
	groupKind := route.GetObjectKind().GroupVersionKind().GroupKind()

	var results parentBindResults

	for _, ref := range filteredParents {
		var result bindResults

		for _, listener := range listenersFor(&r.config.Gateway, ref.SectionName) {
			if !routeKindIsAllowedForListener(supportedKindsForProtocol[listener.Protocol], groupKind) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Protocol,
				})
				continue
			}

			if !routeKindIsAllowedForListenerExplicit(listener.AllowedRoutes, groupKind) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Protocol,
				})
				continue
			}

			if !routeAllowedForListenerNamespaces(r.config.Gateway.Namespace, listener.AllowedRoutes, namespace) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNotAllowedByListeners_Namespace,
				})
				continue
			}

			if !routeAllowedForListenerHostname(listener.Hostname, getRouteHostnames(route)) {
				result = append(result, bindResult{
					section: listener.Name,
					err:     errRouteNoMatchingListenerHostname,
				})
				continue
			}

			result = append(result, bindResult{
				section: listener.Name,
			})

			boundCount[listener.Name]++
		}

		results = append(results, parentBindResult{
			parent:  ref,
			results: result,
		})
	}

	updated := false
	for _, result := range results {
		if r.statusSetter.setRouteCondition(route, &result.parent, result.results.Condition()) {
			updated = true
		}
	}

	if updated {
		kubernetesNeedsStatusUpdate = true
	}

	mutateRouteWithBindingResults(route, r.nonNormalizedConsulKey, r.config.Resources, results)

	return
}

// parentsForRoute constructs a list of Consul route parent references based on what parents actually bound
// on a given route. This is necessary due to the fact that some additional validation in Kubernetes might
// require a route not to actually be accepted by a gateway, whereas we may have laxer logic inside of Consul
// itself. In these cases we want to just drop the parent reference in the Consul config entry we are going
// to write in order for it not to succeed in binding where Kubernetes failed to bind.

// filterParentRefs returns the subset of parent references on a route that point to the given gateway.
func filterParentRefs(gateway types.NamespacedName, namespace string, refs []gwv1beta1.ParentReference) []gwv1beta1.ParentReference {
	references := []gwv1beta1.ParentReference{}
	for _, ref := range refs {
		if nilOrEqual(ref.Group, betaGroup) &&
			nilOrEqual(ref.Kind, kindGateway) &&
			gateway.Namespace == valueOr(ref.Namespace, namespace) &&
			gateway.Name == string(ref.Name) {
			references = append(references, ref)
		}
	}

	return references
}

// listenersFor returns the listeners corresponding the given section name. If the section
// name is actually specified, the returned set should just have one listener, if it is
// unspecified, the all gatweway listeners should be returned.
func listenersFor(gateway *gwv1beta1.Gateway, name *gwv1beta1.SectionName) []gwv1beta1.Listener {
	listeners := []gwv1beta1.Listener{}
	for _, listener := range gateway.Spec.Listeners {
		if name == nil {
			listeners = append(listeners, listener)
			continue
		}
		if listener.Name == *name {
			listeners = append(listeners, listener)
		}
	}
	return listeners
}

func consulParentMatches(namespace string, gatewayKey api.ResourceReference, parent api.ResourceReference) bool {
	gatewayKey = apigateway.NormalizeMeta(gatewayKey)

	if parent.Namespace == "" {
		parent.Namespace = namespace
	}
	if parent.Kind == "" {
		parent.Kind = api.APIGateway
	}

	parent = apigateway.NormalizeMeta(parent)

	return parent.Kind == api.APIGateway &&
		parent.Name == gatewayKey.Name &&
		parent.Namespace == gatewayKey.Namespace &&
		parent.Partition == gatewayKey.Partition
}

func dropConsulRouteParent(object client.Object, gateway api.ResourceReference, resources *apigateway.ResourceMap) {
	switch object.(type) {
	case *gwv1beta1.HTTPRoute:
		resources.MutateHTTPRoute(client.ObjectKeyFromObject(object), func(entry api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry {
			entry.Parents = apigateway.Filter(entry.Parents, func(parent api.ResourceReference) bool {
				return consulParentMatches(entry.Namespace, gateway, parent)
			})
			entry.Status.Conditions = apigateway.Filter(entry.Status.Conditions, func(condition api.Condition) bool {
				if condition.Resource == nil {
					return false
				}
				return consulParentMatches(entry.Namespace, gateway, *condition.Resource)
			})
			return entry
		})
	case *gwv1alpha2.TCPRoute:
		resources.MutateTCPRoute(client.ObjectKeyFromObject(object), func(entry api.TCPRouteConfigEntry) api.TCPRouteConfigEntry {
			entry.Parents = apigateway.Filter(entry.Parents, func(parent api.ResourceReference) bool {
				return consulParentMatches(entry.Namespace, gateway, parent)
			})
			entry.Status.Conditions = apigateway.Filter(entry.Status.Conditions, func(condition api.Condition) bool {
				if condition.Resource == nil {
					return false
				}
				return consulParentMatches(entry.Namespace, gateway, *condition.Resource)
			})
			return entry
		})
	}
}

func mutateRouteWithBindingResults(object client.Object, gatewayConsulKey api.ResourceReference, resources *apigateway.ResourceMap, results parentBindResults) {
	key := client.ObjectKeyFromObject(object)

	parents := mapset.NewSet()
	for section := range results.boundSections().Iter() {
		parents.Add(api.ResourceReference{
			Kind:        api.APIGateway,
			Name:        gatewayConsulKey.Name,
			SectionName: section.(string),
			Namespace:   gatewayConsulKey.Namespace,
			Partition:   gatewayConsulKey.Partition,
		})
	}

	switch object.(type) {
	case *gwv1beta1.HTTPRoute:
		resources.TranslateAndMutateHTTPRoute(key, func(old *api.HTTPRouteConfigEntry, new api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry {
			if old != nil {
				for _, parent := range old.Parents {
					// drop any references that already exist
					if parents.Contains(parent) {
						parents.Remove(parent)
					}
				}

				// set the old parent states
				new.Parents = old.Parents
			}
			// and now add what is left
			for parent := range parents.Iter() {
				new.Parents = append(new.Parents, parent.(api.ResourceReference))
			}
			return new
		})
	case *gwv1alpha2.TCPRoute:
		resources.TranslateAndMutateTCPRoute(key, func(old *api.TCPRouteConfigEntry, new api.TCPRouteConfigEntry) api.TCPRouteConfigEntry {
			if old != nil {
				for _, parent := range old.Parents {
					// drop any references that already exist
					if parents.Contains(parent) {
						parents.Remove(parent)
					}
				}

				// set the old parent states
				new.Parents = old.Parents
			}
			// and now add what is left
			for parent := range parents.Iter() {
				new.Parents = append(new.Parents, parent.(api.ResourceReference))
			}
			return new
		})
	}
}

func entryKind(object client.Object) string {
	switch object.(type) {
	case *gwv1beta1.HTTPRoute:
		return api.HTTPRoute
	case *gwv1alpha2.TCPRoute:
		return api.TCPRoute
	}
	return ""
}

func canGCOnUnbind(id api.ResourceReference, resources *apigateway.ResourceMap) bool {
	switch id.Kind {
	case api.HTTPRoute:
		return resources.CanGCHTTPRouteOnUnbind(id)
	case api.TCPRoute:
		return resources.CanGCTCPRouteOnUnbind(id)
	}
	return true
}

func setParents(entry api.ConfigEntry, parents []api.ResourceReference) {
	switch v := entry.(type) {
	case *api.HTTPRouteConfigEntry:
		v.Parents = parents
	case *api.TCPRouteConfigEntry:
		v.Parents = parents
	}
}

func getConsulParents(entry api.ConfigEntry) []api.ResourceReference {
	switch v := entry.(type) {
	case *api.HTTPRouteConfigEntry:
		return v.Parents
	case *api.TCPRouteConfigEntry:
		return v.Parents
	}
	return nil
}

func getRouteHostnames(object client.Object) []gwv1beta1.Hostname {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		return v.Spec.Hostnames
	}
	return nil
}

func getRouteParents(object client.Object) []gwv1beta1.ParentReference {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		return v.Spec.ParentRefs
	case *gwv1alpha2.TCPRoute:
		return v.Spec.ParentRefs
	}
	return nil
}

func getRouteParentsStatus(object client.Object) []gwv1beta1.RouteParentStatus {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		return v.Status.RouteStatus.Parents
	case *gwv1alpha2.TCPRoute:
		return v.Status.RouteStatus.Parents
	}
	return nil
}

func setRouteParentsStatus(object client.Object, parents []gwv1beta1.RouteParentStatus) {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		v.Status.RouteStatus.Parents = parents
	case *gwv1alpha2.TCPRoute:
		v.Status.RouteStatus.Parents = parents
	}
}

func getRouteBackends(object client.Object) []gwv1beta1.BackendRef {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		return apigateway.Flatten(apigateway.ConvertSliceFunc(v.Spec.Rules, func(rule gwv1beta1.HTTPRouteRule) []gwv1beta1.BackendRef {
			return apigateway.ConvertSliceFunc(rule.BackendRefs, func(rule gwv1beta1.HTTPBackendRef) gwv1beta1.BackendRef {
				return rule.BackendRef
			})
		}))
	case *gwv1alpha2.TCPRoute:
		return apigateway.Flatten(apigateway.ConvertSliceFunc(v.Spec.Rules, func(rule gwv1alpha2.TCPRouteRule) []gwv1beta1.BackendRef {
			return rule.BackendRefs
		}))
	}
	return nil
}
