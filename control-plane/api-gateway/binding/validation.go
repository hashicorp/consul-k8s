package binding

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func validateGateway(gateway gwv1beta1.Gateway) gatewayValidationResult {
	var result gatewayValidationResult

	if len(gateway.Spec.Addresses) > 0 {
		result.acceptedErr = errGatewayUnsupportedAddress
	}

	return result
}

type mergedListener struct {
	index    int
	listener gwv1beta1.Listener
}

type mergedListeners []mergedListener

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
	return nil, nil
}

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

func routeKindIsAllowedForListenerExplicit(allowedRoutes *gwv1alpha2.AllowedRoutes, gk schema.GroupKind) bool {
	if allowedRoutes == nil {
		return true
	}

	if len(allowedRoutes.Kinds) == 0 {
		return true
	}

	for _, kind := range allowedRoutes.Kinds {
		if string(kind.Kind) == gk.Kind && nilOrEqual(kind.Group, gk.Group) {
			return true
		}
	}

	return false
}
