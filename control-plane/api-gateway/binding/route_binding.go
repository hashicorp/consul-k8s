// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"fmt"
	"strings"

	mapset "github.com/deckarep/golang-set"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

// bindRoute contains the main logic for binding a route to a given gateway.
func (r *Binder) bindRoute(route client.Object, boundCount map[gwv1beta1.SectionName]int, snapshot *Snapshot) {
	// use the non-normalized key since we can't write back enterprise metadata
	// on non-enterprise installations
	routeConsulKey := r.config.Translator.NonNormalizedConfigEntryReference(entryKind(route), client.ObjectKeyFromObject(route))
	filteredParents := filterParentRefs(r.key, route.GetNamespace(), getRouteParents(route))
	filteredParentStatuses := filterParentRefs(r.key, route.GetNamespace(),
		common.ConvertSliceFunc(getRouteParentsStatus(route), func(parentStatus gwv1beta1.RouteParentStatus) gwv1beta1.ParentReference {
			return parentStatus.ParentRef
		}),
	)

	// flags to mark that some operation needs to occur
	kubernetesNeedsUpdate := false
	kubernetesNeedsStatusUpdate := false

	// we do this in a closure at the end to make sure we don't accidentally
	// add something multiple times into the list of update/delete operations
	// instead we just set a flag indicating that an update is needed and then
	// append to the snapshot right before returning
	defer func() {
		if kubernetesNeedsUpdate {
			snapshot.Kubernetes.Updates.Add(route)
		}
		if kubernetesNeedsStatusUpdate {
			snapshot.Kubernetes.StatusUpdates.Add(route)
		}
	}()

	if isDeleted(route) {
		// mark the route as needing to get cleaned up if we detect that it's being deleted
		if common.RemoveFinalizer(route) {
			kubernetesNeedsUpdate = true
		}
		return
	}

	if r.isGatewayDeleted() {
		if canGCOnUnbind(routeConsulKey, r.config.Resources) && common.RemoveFinalizer(route) {
			kubernetesNeedsUpdate = true
		} else {
			// Remove the condition since we no longer know if we should
			// control the route and drop any references for the Consul route.
			// This only gets run if we can't GC the route at the end of this
			// loop.
			r.dropConsulRouteParent(snapshot, route, r.nonNormalizedConsulKey, r.config.Resources)
		}

		// drop the status conditions
		if r.statusSetter.removeRouteReferences(route, filteredParentStatuses) {
			kubernetesNeedsStatusUpdate = true
		}
		if r.statusSetter.removeRouteReferences(route, filteredParents) {
			kubernetesNeedsStatusUpdate = true
		}
		return
	}

	if common.EnsureFinalizer(route) {
		kubernetesNeedsUpdate = true
		return
	}

	validation := validateRefs(route, getRouteBackends(route), r.config.Resources)
	// the spec is dumb and makes you set a parent for any status, even when the
	// status is not with respect to a parent, as is the case of resolved refs
	// so we need to set the status on all parents
	for _, parent := range filteredParents {
		if r.statusSetter.setRouteCondition(route, &parent, validation.Condition()) {
			kubernetesNeedsStatusUpdate = true
		}
	}
	// if we're orphaned from this gateway we'll
	// always need a status update.
	if len(filteredParents) == 0 {
		// we already checked that these refs existed, so no need to check
		// the return value here.
		_ = r.statusSetter.removeRouteReferences(route, filteredParentStatuses)
		kubernetesNeedsStatusUpdate = true
	}

	namespace := r.config.Namespaces[route.GetNamespace()]
	groupKind := route.GetObjectKind().GroupVersionKind().GroupKind()

	var results parentBindResults

	for _, ref := range filteredParents {
		var result bindResults

		listeners := listenersFor(&r.config.Gateway, ref.SectionName)

		// If there are no matching listeners, then we failed to find the parent
		if len(listeners) == 0 {
			var sectionName gwv1beta1.SectionName
			if ref.SectionName != nil {
				sectionName = *ref.SectionName
			}

			result = append(result, bindResult{
				section: sectionName,
				err:     errRouteNoMatchingParent,
			})
		}

		for _, listener := range listeners {
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

		httproute, ok := route.(*gwv1beta1.HTTPRoute)
		if ok {
			if !externalRefsOnRouteAllExist(httproute, r.config.Resources) {
				results = append(results, parentBindResult{
					parent: ref,
					results: []bindResult{
						{
							err: errExternalRefNotFound,
						},
					},
				})
			}

			if invalidFilterNames := authFilterReferencesMissingJWTProvider(httproute, r.config.Resources); len(invalidFilterNames) > 0 {
				results = append(results, parentBindResult{
					parent: ref,
					results: []bindResult{
						{
							err: fmt.Errorf("%w: %s", errFilterInvalid, strings.Join(invalidFilterNames, ",")),
						},
					},
				})
			}

			if !externalRefsKindAllowedOnRoute(httproute) {
				results = append(results, parentBindResult{
					parent: ref,
					results: []bindResult{
						{
							err: errInvalidExternalRefType,
						},
					},
				})
			}
		}
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

	r.mutateRouteWithBindingResults(snapshot, route, r.nonNormalizedConsulKey, r.config.Resources, results)
}

// filterParentRefs returns the subset of parent references on a route that point to the given gateway.
func filterParentRefs(gateway types.NamespacedName, namespace string, refs []gwv1beta1.ParentReference) []gwv1beta1.ParentReference {
	references := []gwv1beta1.ParentReference{}
	for _, ref := range refs {
		if common.NilOrEqual(ref.Group, common.BetaGroup) &&
			common.NilOrEqual(ref.Kind, common.KindGateway) &&
			gateway.Namespace == common.ValueOr(ref.Namespace, namespace) &&
			gateway.Name == string(ref.Name) {
			references = append(references, ref)
		}
	}

	return references
}

// listenersFor returns the listeners corresponding to the given section name. If the section
// name is actually specified, the returned set will only contain the named listener. If it is
// unspecified, then all gateway listeners will be returned.
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
	gatewayKey = common.NormalizeMeta(gatewayKey)

	if parent.Namespace == "" {
		parent.Namespace = namespace
	}
	if parent.Kind == "" {
		parent.Kind = api.APIGateway
	}

	parent = common.NormalizeMeta(parent)

	return parent.Kind == api.APIGateway &&
		parent.Name == gatewayKey.Name &&
		parent.Namespace == gatewayKey.Namespace &&
		parent.Partition == gatewayKey.Partition
}

func (r *Binder) dropConsulRouteParent(snapshot *Snapshot, object client.Object, gateway api.ResourceReference, resources *common.ResourceMap) {
	switch object.(type) {
	case *gwv1beta1.HTTPRoute:
		resources.MutateHTTPRoute(client.ObjectKeyFromObject(object), r.handleRouteSyncStatus(snapshot, object), func(entry api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry {
			entry.Parents = common.Filter(entry.Parents, func(parent api.ResourceReference) bool {
				return consulParentMatches(entry.Namespace, gateway, parent)
			})
			return entry
		})
	case *gwv1alpha2.TCPRoute:
		resources.MutateTCPRoute(client.ObjectKeyFromObject(object), r.handleRouteSyncStatus(snapshot, object), func(entry api.TCPRouteConfigEntry) api.TCPRouteConfigEntry {
			entry.Parents = common.Filter(entry.Parents, func(parent api.ResourceReference) bool {
				return consulParentMatches(entry.Namespace, gateway, parent)
			})
			return entry
		})
	}
}

func (r *Binder) mutateRouteWithBindingResults(snapshot *Snapshot, object client.Object, gatewayConsulKey api.ResourceReference, resources *common.ResourceMap, results parentBindResults) {
	if results.boundSections().Cardinality() == 0 {
		r.dropConsulRouteParent(snapshot, object, r.nonNormalizedConsulKey, r.config.Resources)
		return
	}

	key := client.ObjectKeyFromObject(object)

	parents := mapset.NewSet()
	// the normalized set keeps us from accidentally adding the same thing
	// twice due to the Consul server normalizing our refs.
	normalized := make(map[api.ResourceReference]api.ResourceReference)
	for section := range results.boundSections().Iter() {
		ref := api.ResourceReference{
			Kind:        api.APIGateway,
			Name:        gatewayConsulKey.Name,
			SectionName: section.(string),
			Namespace:   gatewayConsulKey.Namespace,
			Partition:   gatewayConsulKey.Partition,
		}
		parents.Add(ref)
		normalized[common.NormalizeMeta(ref)] = ref
	}

	switch object.(type) {
	case *gwv1beta1.HTTPRoute:
		resources.TranslateAndMutateHTTPRoute(key, r.handleRouteSyncStatus(snapshot, object), func(old *api.HTTPRouteConfigEntry, new api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry {
			if old != nil {
				for _, parent := range old.Parents {
					// drop any references that already exist
					if parents.Contains(parent) {
						parents.Remove(parent)
					}
					if id, ok := normalized[parent]; ok {
						parents.Remove(id)
					}
				}

				// set the old parent states
				new.Parents = old.Parents
				new.Status = old.Status
			}
			// and now add what is left
			for parent := range parents.Iter() {
				new.Parents = append(new.Parents, parent.(api.ResourceReference))
			}

			return new
		})
	case *gwv1alpha2.TCPRoute:
		resources.TranslateAndMutateTCPRoute(key, r.handleRouteSyncStatus(snapshot, object), func(old *api.TCPRouteConfigEntry, new api.TCPRouteConfigEntry) api.TCPRouteConfigEntry {
			if old != nil {
				for _, parent := range old.Parents {
					// drop any references that already exist
					if parents.Contains(parent) {
						parents.Remove(parent)
					}
				}

				// set the old parent states
				new.Parents = old.Parents
				new.Status = old.Status
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

func canGCOnUnbind(id api.ResourceReference, resources *common.ResourceMap) bool {
	switch id.Kind {
	case api.HTTPRoute:
		return resources.CanGCHTTPRouteOnUnbind(id)
	case api.TCPRoute:
		return resources.CanGCTCPRouteOnUnbind(id)
	}
	return true
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
		return common.Flatten(common.ConvertSliceFunc(v.Spec.Rules, func(rule gwv1beta1.HTTPRouteRule) []gwv1beta1.BackendRef {
			return common.ConvertSliceFunc(rule.BackendRefs, func(rule gwv1beta1.HTTPBackendRef) gwv1beta1.BackendRef {
				return rule.BackendRef
			})
		}))
	case *gwv1alpha2.TCPRoute:
		return common.Flatten(common.ConvertSliceFunc(v.Spec.Rules, func(rule gwv1alpha2.TCPRouteRule) []gwv1beta1.BackendRef {
			return rule.BackendRefs
		}))
	}
	return nil
}

func canReferenceBackend(object client.Object, ref gwv1beta1.BackendRef, resources *common.ResourceMap) bool {
	switch v := object.(type) {
	case *gwv1beta1.HTTPRoute:
		return resources.HTTPRouteCanReferenceBackend(*v, ref)
	case *gwv1alpha2.TCPRoute:
		return resources.TCPRouteCanReferenceBackend(*v, ref)
	}
	return false
}

func (r *Binder) handleRouteSyncStatus(snapshot *Snapshot, object client.Object) func(error, api.ConfigEntryStatus) {
	return func(err error, status api.ConfigEntryStatus) {
		condition := metav1.Condition{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: object.GetGeneration(),
			LastTransitionTime: timeFunc(),
			Reason:             "Synced",
			Message:            "route synced to Consul",
		}
		if err != nil {
			condition = metav1.Condition{
				Type:               "Synced",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: object.GetGeneration(),
				LastTransitionTime: timeFunc(),
				Reason:             "SyncError",
				Message:            err.Error(),
			}
		}
		if r.statusSetter.setRouteConditionOnAllRefs(object, condition) {
			snapshot.Kubernetes.StatusUpdates.Add(object)
		}
		if consulCondition := consulCondition(object.GetGeneration(), status); consulCondition != nil {
			if r.statusSetter.setRouteConditionOnAllRefs(object, *consulCondition) {
				snapshot.Kubernetes.StatusUpdates.Add(object)
			}
		}
	}
}

func (r *Binder) handleGatewaySyncStatus(snapshot *Snapshot, gateway *gwv1beta1.Gateway, status api.ConfigEntryStatus) func(error) {
	return func(err error) {
		condition := metav1.Condition{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: gateway.Generation,
			LastTransitionTime: timeFunc(),
			Reason:             "Synced",
			Message:            "gateway synced to Consul",
		}
		if err != nil {
			condition = metav1.Condition{
				Type:               "Synced",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: gateway.Generation,
				LastTransitionTime: timeFunc(),
				Reason:             "SyncError",
				Message:            err.Error(),
			}
		}

		if conditions, updated := setCondition(gateway.Status.Conditions, condition); updated {
			gateway.Status.Conditions = conditions
			snapshot.Kubernetes.StatusUpdates.Add(gateway)
		}

		if consulCondition := consulCondition(gateway.Generation, status); consulCondition != nil {
			if conditions, updated := setCondition(gateway.Status.Conditions, *consulCondition); updated {
				gateway.Status.Conditions = conditions
				snapshot.Kubernetes.StatusUpdates.Add(gateway)
			}
		}
	}
}

func consulCondition(generation int64, status api.ConfigEntryStatus) *metav1.Condition {
	for _, c := range status.Conditions {
		// we only care about the top-level status that isn't in reference
		// to a resource.
		if c.Type == "Accepted" && (c.Resource == nil || c.Resource.Name == "") {
			return &metav1.Condition{
				Type:               "ConsulAccepted",
				Reason:             c.Reason,
				Status:             metav1.ConditionStatus(c.Status),
				Message:            c.Message,
				ObservedGeneration: generation,
				LastTransitionTime: timeFunc(),
			}
		}
	}
	return nil
}
