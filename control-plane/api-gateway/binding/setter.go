// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// setter wraps the status setting logic for routes.
type setter struct {
	controllerName string
}

// newSetter constructs a status setter with the given controller name.
func newSetter(controllerName string) *setter {
	return &setter{controllerName: controllerName}
}

// setHTTPRouteCondition sets an HTTPRoute condition on its status with the given parent.
func (s *setter) setHTTPRouteCondition(route *gwv1beta1.HTTPRoute, parent *gwv1beta1.ParentReference, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.Generation

	status := s.getParentStatus(route.Status.Parents, parent)
	conditions, modified := setCondition(status.Conditions, condition)
	if modified {
		status.Conditions = conditions
		route.Status.Parents = s.setParentStatus(route.Status.Parents, status)
	}
	return modified
}

// removeHTTPRouteReferences removes the given parent reference sections from an HTTPRoute's status.
func (s *setter) removeHTTPRouteReferences(route *gwv1beta1.HTTPRoute, refs []gwv1beta1.ParentReference) bool {
	modified := false
	for _, parent := range refs {
		parents, removed := s.removeParentStatus(route.Status.Parents, parent)
		route.Status.Parents = parents
		if removed {
			modified = true
		}
	}
	return modified
}

// setTCPRouteCondition sets a TCPRoute condition on its status with the given parent.
func (s *setter) setTCPRouteCondition(route *gwv1alpha2.TCPRoute, parent *gwv1beta1.ParentReference, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.Generation

	status := s.getParentStatus(route.Status.Parents, parent)
	conditions, modified := setCondition(status.Conditions, condition)
	if modified {
		status.Conditions = conditions
		route.Status.Parents = s.setParentStatus(route.Status.Parents, status)
	}
	return modified
}

// removeTCPRouteReferences removes the given parent reference sections from a TCPRoute's status.
func (s *setter) removeTCPRouteReferences(route *gwv1alpha2.TCPRoute, refs []gwv1beta1.ParentReference) bool {
	modified := false
	for _, parent := range refs {
		parents, removed := s.removeParentStatus(route.Status.Parents, parent)
		route.Status.Parents = parents
		if removed {
			modified = true
		}
	}
	return modified
}

// removeHTTPStatuses removes all statuses set by the given controller from an HTTPRoute's status.
func (s *setter) removeHTTPStatuses(route *gwv1beta1.HTTPRoute) bool {
	modified := false
	filtered := []gwv1beta1.RouteParentStatus{}
	for _, status := range route.Status.Parents {
		if string(status.ControllerName) == s.controllerName {
			modified = true
			continue
		}
		filtered = append(filtered, status)
	}

	if modified {
		route.Status.Parents = filtered
	}
	return modified
}

// removeTCPStatuses removes all statuses set by the given controller from a TCPRoute's status.
func (s *setter) removeTCPStatuses(route *gwv1alpha2.TCPRoute) bool {
	modified := false
	filtered := []gwv1beta1.RouteParentStatus{}
	for _, status := range route.Status.Parents {
		if string(status.ControllerName) == s.controllerName {
			modified = true
			continue
		}
		filtered = append(filtered, status)
	}

	if modified {
		route.Status.Parents = filtered
	}
	return modified
}

// getParentStatus returns the section of a status referenced by the given parent reference.
func (s *setter) getParentStatus(statuses []gwv1beta1.RouteParentStatus, parent *gwv1beta1.ParentReference) gwv1beta1.RouteParentStatus {
	var parentRef gwv1beta1.ParentReference
	if parent != nil {
		parentRef = *parent
	}

	for _, status := range statuses {
		if parentsEqual(status.ParentRef, parentRef) && string(status.ControllerName) == s.controllerName {
			return status
		}
	}
	return gwv1beta1.RouteParentStatus{
		ParentRef:      parentRef,
		ControllerName: gwv1beta1.GatewayController(s.controllerName),
	}
}

// removeParentStatus removes the section of a status referenced by the given parent reference.
func (s *setter) removeParentStatus(statuses []gwv1beta1.RouteParentStatus, parent gwv1beta1.ParentReference) ([]gwv1beta1.RouteParentStatus, bool) {
	found := false
	filtered := []gwv1beta1.RouteParentStatus{}
	for _, status := range statuses {
		if parentsEqual(status.ParentRef, parent) && string(status.ControllerName) == s.controllerName {
			found = true
			continue
		}
		filtered = append(filtered, status)
	}
	return filtered, found
}

// setCondition overrides or appends a condition to the list of conditions, returning if a modification
// to the condition set was made or not. Modifications only occur if a field other than the observation
// timestamp is modified.
func setCondition(conditions []metav1.Condition, condition metav1.Condition) ([]metav1.Condition, bool) {
	for i, existing := range conditions {
		if existing.Type == condition.Type {
			// no-op if we have the exact same thing
			if condition.Reason == existing.Reason && condition.Message == existing.Message && condition.ObservedGeneration == existing.ObservedGeneration {
				return conditions, false
			}

			conditions[i] = condition
			return conditions, true
		}
	}
	return append(conditions, condition), true
}

// setParentStatus updates or inserts the set of parent statuses with the newly modified parent.
func (s *setter) setParentStatus(statuses []gwv1beta1.RouteParentStatus, parent gwv1beta1.RouteParentStatus) []gwv1beta1.RouteParentStatus {
	for i, status := range statuses {
		if parentsEqual(status.ParentRef, parent.ParentRef) && status.ControllerName == parent.ControllerName {
			statuses[i] = parent
			return statuses
		}
	}
	return append(statuses, parent)
}

// parentsEqual checks for equality between two parent references.
func parentsEqual(one, two gwv1beta1.ParentReference) bool {
	return bothNilOrEqual(one.Group, two.Group) &&
		bothNilOrEqual(one.Kind, two.Kind) &&
		bothNilOrEqual(one.SectionName, two.SectionName) &&
		bothNilOrEqual(one.Port, two.Port) &&
		one.Name == two.Name
}
