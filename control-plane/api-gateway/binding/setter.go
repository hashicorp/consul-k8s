// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"fmt"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// removeRouteReferences removes the given parent reference sections from a routes's status.
func (s *setter) removeRouteReferences(route client.Object, refs []gwv1beta1.ParentReference) bool {
	modified := false
	for _, parent := range refs {
		parents, removed := s.removeParentStatus(getRouteParentsStatus(route), parent)
		setRouteParentsStatus(route, parents)
		if removed {
			modified = true
		}
	}
	return modified
}

// setRouteCondition sets an route condition on its status with the given parent.
func (s *setter) setRouteCondition(route client.Object, parent *gwv1beta1.ParentReference, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.GetGeneration()

	parents := getRouteParentsStatus(route)
	status := s.getParentStatus(parents, parent)
	conditions, modified := setCondition(status.Conditions, condition)
	if modified {
		status.Conditions = conditions
		setRouteParentsStatus(route, s.setParentStatus(parents, status))
	}
	return modified
}

// setRouteConditionOnAllRefs sets an route condition and its status on all parents.
func (s *setter) setRouteConditionOnAllRefs(route client.Object, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.GetGeneration()

	parents := getRouteParentsStatus(route)
	statuses := apigateway.Filter(getRouteParentsStatus(route), func(status gwv1beta1.RouteParentStatus) bool {
		return string(status.ControllerName) != s.controllerName
	})

	fmt.Println("SETTING", condition, "ON", statuses, "FROM", getRouteParentsStatus(route))
	updated := false
	for _, status := range statuses {
		conditions, modified := setCondition(status.Conditions, condition)
		if modified {
			updated = true
			status.Conditions = conditions
			setRouteParentsStatus(route, s.setParentStatus(parents, status))
		}
	}
	return updated
}

// removeStatuses removes all statuses set by the given controller from an route's status.
func (s *setter) removeStatuses(route client.Object) bool {
	modified := false
	filtered := []gwv1beta1.RouteParentStatus{}
	for _, status := range getRouteParentsStatus(route) {
		if string(status.ControllerName) == s.controllerName {
			modified = true
			continue
		}
		filtered = append(filtered, status)
	}

	if modified {
		setRouteParentsStatus(route, filtered)
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
