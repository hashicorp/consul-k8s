// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// validateRefs validates backend references for a route, determining whether or
// not they were found in the list of known connect-injected services.
func validateRefs(namespace string, refs []gwv1beta1.BackendRef, services map[types.NamespacedName]api.CatalogService, meshServices map[types.NamespacedName]v1alpha1.MeshService) routeValidationResults {
	var result routeValidationResults
	for _, ref := range refs {
		nsn := types.NamespacedName{
			Name:      string(ref.BackendObjectReference.Name),
			Namespace: valueOr(ref.BackendObjectReference.Namespace, namespace),
		}

		// TODO: check reference grants

		backendRef := ref.BackendObjectReference

		isServiceRef := nilOrEqual(backendRef.Group, "") && nilOrEqual(backendRef.Kind, "Service")
		isMeshServiceRef := derefEqual(backendRef.Group, v1alpha1.ConsulHashicorpGroup) && derefEqual(backendRef.Kind, v1alpha1.MeshServiceKind)

		if !isServiceRef && !isMeshServiceRef {
			result = append(result, routeValidationResult{
				namespace: nsn.Namespace,
				backend:   ref,
				err:       errRouteInvalidKind,
			})
			continue
		}

		if isServiceRef {
			if _, found := services[nsn]; !found {
				result = append(result, routeValidationResult{
					namespace: nsn.Namespace,
					backend:   ref,
					err:       errRouteBackendNotFound,
				})
				continue
			}
		}

		if isMeshServiceRef {
			if _, found := meshServices[nsn]; !found {
				result = append(result, routeValidationResult{
					namespace: nsn.Namespace,
					backend:   ref,
					err:       errRouteBackendNotFound,
				})
				continue
			}
		}

		result = append(result, routeValidationResult{
			namespace: nsn.Namespace,
			backend:   ref,
		})
	}
	return result
}

// validateGateway validates that a gateway is semantically valid given
// the set of features that we support.
func validateGateway(gateway gwv1beta1.Gateway, pods []corev1.Pod, consulGateway *api.APIGatewayConfigEntry) gatewayValidationResult {
	var result gatewayValidationResult

	if len(gateway.Spec.Addresses) > 0 {
		result.acceptedErr = errGatewayUnsupportedAddress
	}

	if len(pods) == 0 {
		result.programmedErr = errGatewayPending_Pods
	} else if consulGateway == nil {
		result.programmedErr = errGatewayPending_Consul
	}

	return result
}

// mergedListener associates a listener with its indexed position
// in the gateway spec, it's used to re-associate a status with
// a listener after we merge compatible listeners together and then
// validate their conflicts.
type mergedListener struct {
	index    int
	listener gwv1beta1.Listener
}

// mergedListeners is a set of a listeners that are considered "merged"
// due to referencing the same listener port.
type mergedListeners []mergedListener

// validateProtocol validates that the protocols used across all merged
// listeners are compatible.
func (m mergedListeners) validateProtocol() error {
	var protocol *gwv1beta1.ProtocolType
	for _, l := range m {
		if protocol == nil {
			protocol = pointerTo(l.listener.Protocol)
		}
		if *protocol != l.listener.Protocol {
			return errListenerProtocolConflict
		}
	}
	return nil
}

// validateHostname validates that the merged listeners don't use the same
// hostnames as per the spec.
func (m mergedListeners) validateHostname(index int, listener gwv1beta1.Listener) error {
	for _, l := range m {
		if l.index == index {
			continue
		}
		if bothNilOrEqual(listener.Hostname, l.listener.Hostname) {
			return errListenerHostnameConflict
		}
	}
	return nil
}

// validateTLS validates that the TLS configuration for a given listener is valid and that
// the certificates that it references exist.
func validateTLS(namespace string, tls *gwv1beta1.GatewayTLSConfig, certificates []corev1.Secret) (error, error) {
	if tls == nil {
		return nil, nil
	}

	// TODO: Resource Grants

	var err error
MAIN_LOOP:
	for _, ref := range tls.CertificateRefs {
		// break on the first error
		if !nilOrEqual(ref.Group, "") || !nilOrEqual(ref.Kind, "Secret") {
			err = errListenerInvalidCertificateRef_NotSupported
			break MAIN_LOOP
		}
		ns := valueOr(ref.Namespace, namespace)

		for _, secret := range certificates {
			if secret.Namespace == ns && secret.Name == string(ref.Name) {
				continue MAIN_LOOP
			}
		}

		// not found, set error
		err = errListenerInvalidCertificateRef_NotFound
		break MAIN_LOOP
	}

	if tls.Mode != nil && *tls.Mode == gwv1beta1.TLSModePassthrough {
		return errListenerNoTLSPassthrough, err
	}

	// TODO: validate tls options
	return nil, err
}

// validateListeners validates the given listeners both internally and with respect to each
// other for purposes of setting "Conflicted" status conditions.
func validateListeners(namespace string, listeners []gwv1beta1.Listener, secrets []corev1.Secret) listenerValidationResults {
	var results listenerValidationResults
	merged := make(map[gwv1beta1.PortNumber]mergedListeners)
	for i, listener := range listeners {
		merged[listener.Port] = append(merged[listener.Port], mergedListener{
			index:    i,
			listener: listener,
		})
	}

	for i, listener := range listeners {
		var result listenerValidationResult

		err, refErr := validateTLS(namespace, listener.TLS, secrets)
		result.refErr = refErr
		if err != nil {
			result.acceptedErr = err
		} else {
			_, supported := supportedKindsForProtocol[listener.Protocol]
			if !supported {
				result.acceptedErr = errListenerUnsupportedProtocol
			} else if listener.Port == 20000 { //admin port
				result.acceptedErr = errListenerPortUnavailable
			}
		}

		if err := merged[listener.Port].validateProtocol(); err != nil {
			result.conflictedErr = err
		} else {
			result.conflictedErr = merged[listener.Port].validateHostname(i, listener)
		}

		results = append(results, result)
	}
	return results
}

// routeAllowedForListenerNamespaces determines whether the route is allowed
// to bind to the Gateway based on the AllowedRoutes namespace selectors.
func routeAllowedForListenerNamespaces(gatewayNamespace string, allowedRoutes *gwv1beta1.AllowedRoutes, namespace corev1.Namespace) bool {
	var namespaceSelector *gwv1beta1.RouteNamespaces
	if allowedRoutes != nil {
		// check gateway namespace
		namespaceSelector = allowedRoutes.Namespaces
	}

	// set default if namespace selector is nil
	from := gwv1beta1.NamespacesFromSame
	if namespaceSelector != nil && namespaceSelector.From != nil && *namespaceSelector.From != "" {
		from = *namespaceSelector.From
	}

	switch from {
	case gwv1beta1.NamespacesFromAll:
		return true
	case gwv1beta1.NamespacesFromSame:
		return gatewayNamespace == namespace.Name
	case gwv1beta1.NamespacesFromSelector:
		namespaceSelector, err := metav1.LabelSelectorAsSelector(namespaceSelector.Selector)
		if err != nil {
			// log the error here, the label selector is invalid
			return false
		}

		return namespaceSelector.Matches(toNamespaceSet(namespace.GetName(), namespace.GetLabels()))
	default:
		return false
	}
}

// routeAllowedForListenerHostname checks that a hostname specified on a route and the hostname specified
// on the gateway listener are compatible.
func routeAllowedForListenerHostname(hostname *gwv1beta1.Hostname, hostnames []gwv1beta1.Hostname) bool {
	if hostname == nil || len(hostnames) == 0 {
		return true
	}

	for _, name := range hostnames {
		if hostnamesMatch(name, *hostname) {
			return true
		}
	}
	return false
}

// hostnameMatch checks that an individual hostname matches another hostname for
// compatibility.
func hostnamesMatch(a gwv1alpha2.Hostname, b gwv1beta1.Hostname) bool {
	if a == "" || a == "*" || b == "" || b == "*" {
		// any wildcard always matches
		return true
	}

	if strings.HasPrefix(string(a), "*.") || strings.HasPrefix(string(b), "*.") {
		aLabels, bLabels := strings.Split(string(a), "."), strings.Split(string(b), ".")
		if len(aLabels) != len(bLabels) {
			return false
		}

		for i := 1; i < len(aLabels); i++ {
			if !strings.EqualFold(aLabels[i], bLabels[i]) {
				return false
			}
		}
		return true
	}

	return string(a) == string(b)
}

// routeKindIsAllowedForListener checks that the given route kind is present in the allowed set.
func routeKindIsAllowedForListener(kinds []gwv1beta1.RouteGroupKind, gk schema.GroupKind) bool {
	if kinds == nil {
		return true
	}

	for _, kind := range kinds {
		if string(kind.Kind) == gk.Kind && nilOrEqual(kind.Group, gk.Group) {
			return true
		}
	}

	return false
}

// routeKindIsAllowedForListenerExplicit checks that a route is allowed by the kinds specified explicitly
// on the listener.
func routeKindIsAllowedForListenerExplicit(allowedRoutes *gwv1alpha2.AllowedRoutes, gk schema.GroupKind) bool {
	if allowedRoutes == nil {
		return true
	}

	return routeKindIsAllowedForListener(allowedRoutes.Kinds, gk)
}

// toNamespaceSet constructs a list of labels used to match a Namespace.
func toNamespaceSet(name string, labels map[string]string) klabels.Labels {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[namespaceNameLabel] == name {
		// Already set, avoid copies
		return klabels.Set(labels)
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[namespaceNameLabel] = name
	return klabels.Set(ret)
}

func derefEqual[T ~string](v *T, check string) bool {
	if v == nil {
		return false
	}
	return string(*v) == check
}
