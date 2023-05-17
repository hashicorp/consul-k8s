package statuses

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type Setter struct {
	controllerName string
}

func NewSetter(controllerName string) *Setter {
	return &Setter{controllerName: controllerName}
}

func (s *Setter) SetHTTPRouteCondition(route *gwv1beta1.HTTPRoute, parent gwv1beta1.ParentReference, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.Generation

	status := s.getParentStatus(route.Status.Parents, parent)
	conditions, modified := setCondition(status.Conditions, condition)
	if modified {
		status.Conditions = conditions
		route.Status.Parents = setParentStatus(route.Status.Parents, status)
	}
	return modified
}

func (s *Setter) RemoveHTTPRouteReferences(route *gwv1beta1.HTTPRoute, refs []gwv1beta1.ParentReference) bool {
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

func (s *Setter) SetTCPRouteCondition(route *gwv1alpha2.TCPRoute, parent gwv1beta1.ParentReference, condition metav1.Condition) bool {
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = route.Generation

	status := s.getParentStatus(route.Status.Parents, parent)
	conditions, modified := setCondition(status.Conditions, condition)
	if modified {
		status.Conditions = conditions
		route.Status.Parents = setParentStatus(route.Status.Parents, status)
	}
	return modified
}

func (s *Setter) RemoveTCPRouteReferences(route *gwv1alpha2.TCPRoute, refs []gwv1beta1.ParentReference) bool {
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

func (s *Setter) getParentStatus(statuses []gwv1beta1.RouteParentStatus, parent gwv1beta1.ParentReference) gwv1beta1.RouteParentStatus {
	for _, status := range statuses {
		if status.ParentRef == parent && string(status.ControllerName) == s.controllerName {
			return status
		}
	}
	return gwv1beta1.RouteParentStatus{
		ParentRef:      parent,
		ControllerName: gwv1beta1.GatewayController(s.controllerName),
	}
}

func (s *Setter) removeParentStatus(statuses []gwv1beta1.RouteParentStatus, parent gwv1beta1.ParentReference) ([]gwv1beta1.RouteParentStatus, bool) {
	found := false
	filtered := []gwv1beta1.RouteParentStatus{}
	for _, status := range statuses {
		if status.ParentRef == parent && string(status.ControllerName) == s.controllerName {
			found = true
			continue
		}
		filtered = append(filtered, status)
	}
	return filtered, found
}

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

func setParentStatus(statuses []gwv1beta1.RouteParentStatus, parent gwv1beta1.RouteParentStatus) []gwv1beta1.RouteParentStatus {
	for i, status := range statuses {
		if status.ParentRef == parent.ParentRef && status.ControllerName == parent.ControllerName {
			statuses[i] = parent
			return statuses
		}
	}
	return append(statuses, parent)
}
