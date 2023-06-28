// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/version"
	"github.com/hashicorp/consul/api"
)

var (
	// the list of kinds we can support by listener protocol.
	supportedKindsForProtocol = map[gwv1beta1.ProtocolType][]gwv1beta1.RouteGroupKind{
		gwv1beta1.HTTPProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.HTTPSProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.TCPProtocolType: {{
			Group: (*gwv1alpha2.Group)(&gwv1alpha2.GroupVersion.Group),
			Kind:  "TCPRoute",
		}},
	}
	allSupportedRouteKinds = map[gwv1beta1.Kind]struct{}{
		gwv1beta1.Kind("HTTPRoute"): {},
		gwv1beta1.Kind("TCPRoute"):  {},
	}
)

// validateRefs validates backend references for a route, determining whether or
// not they were found in the list of known connect-injected services.
func validateRefs(route client.Object, refs []gwv1beta1.BackendRef, resources *common.ResourceMap) routeValidationResults {
	namespace := route.GetNamespace()

	var result routeValidationResults
	for _, ref := range refs {
		backendRef := ref.BackendObjectReference

		nsn := types.NamespacedName{
			Name:      string(backendRef.Name),
			Namespace: common.ValueOr(backendRef.Namespace, namespace),
		}

		isServiceRef := common.NilOrEqual(backendRef.Group, "") && common.NilOrEqual(backendRef.Kind, common.KindService)
		isMeshServiceRef := common.DerefEqual(backendRef.Group, v1alpha1.ConsulHashicorpGroup) && common.DerefEqual(backendRef.Kind, v1alpha1.MeshServiceKind)

		if !isServiceRef && !isMeshServiceRef {
			result = append(result, routeValidationResult{
				namespace: nsn.Namespace,
				backend:   ref,
				err:       errRouteInvalidKind,
			})
			continue
		}

		if isServiceRef && !resources.HasService(nsn) {
			result = append(result, routeValidationResult{
				namespace: nsn.Namespace,
				backend:   ref,
				err:       errRouteBackendNotFound,
			})
			continue
		}

		if isMeshServiceRef && !resources.HasMeshService(nsn) {
			result = append(result, routeValidationResult{
				namespace: nsn.Namespace,
				backend:   ref,
				err:       errRouteBackendNotFound,
			})
			continue
		}

		if !canReferenceBackend(route, ref, resources) {
			result = append(result, routeValidationResult{
				namespace: nsn.Namespace,
				backend:   ref,
				err:       errRefNotPermitted,
			})
			continue
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
			protocol = common.PointerTo(l.listener.Protocol)
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
		if common.BothNilOrEqual(listener.Hostname, l.listener.Hostname) {
			return errListenerHostnameConflict
		}
	}
	return nil
}

// validateTLS validates that the TLS configuration for a given listener is valid and that
// the certificates that it references exist.
func validateTLS(gateway gwv1beta1.Gateway, tls *gwv1beta1.GatewayTLSConfig, resources *common.ResourceMap) (error, error) {
	namespace := gateway.Namespace

	if tls == nil {
		return nil, nil
	}

	var err error

	for _, cert := range tls.CertificateRefs {
		// break on the first error
		if !common.NilOrEqual(cert.Group, "") || !common.NilOrEqual(cert.Kind, common.KindSecret) {
			err = errListenerInvalidCertificateRef_NotSupported
			break
		}

		if !resources.GatewayCanReferenceSecret(gateway, cert) {
			err = errRefNotPermitted
			break
		}

		key := common.IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, namespace)
		secret := resources.Certificate(key)

		if secret == nil {
			err = errListenerInvalidCertificateRef_NotFound
			break
		}

		err = validateCertificateData(*secret)
	}

	if tls.Mode != nil && *tls.Mode == gwv1beta1.TLSModePassthrough {
		return errListenerNoTLSPassthrough, err
	}

	// TODO: validate tls options
	return nil, err
}

// Envoy will silently reject any keys that are less than 2048 bytes long
// https://github.com/envoyproxy/envoy/blob/main/source/extensions/transport_sockets/tls/context_impl.cc#L238
const MinKeyLength = 2048

func validateCertificateData(secret corev1.Secret) error {
	_, privateKey, err := common.ParseCertificateData(secret)
	if err != nil {
		return errListenerInvalidCertificateRef_InvalidData
	}

	err = validateKeyLength(privateKey)
	if err != nil {
		return err
	}

	return nil
}

func validateKeyLength(privateKey string) error {
	// we can assume this is non-nil as it would've caused an error from common.ParseCertificate if it was
	privateKeyBlock, _ := pem.Decode([]byte(privateKey))

	if privateKeyBlock.Type != "RSA PRIVATE KEY" {
		return nil
	}

	key, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return err
	}

	keyBitLen := key.N.BitLen()

	if version.IsFIPS() {
		return fipsLenCheck(keyBitLen)
	}

	return nonFipsLenCheck(keyBitLen)
}

func nonFipsLenCheck(keyLen int) error {
	// ensure private key is of the correct length
	if keyLen < MinKeyLength {
		return errors.New("key length must be at least 2048 bits")
	}

	return nil
}

func fipsLenCheck(keyLen int) error {
	if keyLen != 2048 && keyLen != 3072 && keyLen != 4096 {
		return errors.New("key length invalid: only RSA lengths of 2048, 3072, and 4096 are allowed in FIPS mode")
	}
	return nil
}

// validateListeners validates the given listeners both internally and with respect to each
// other for purposes of setting "Conflicted" status conditions.
func validateListeners(gateway gwv1beta1.Gateway, listeners []gwv1beta1.Listener, resources *common.ResourceMap) listenerValidationResults {
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

		err, refErr := validateTLS(gateway, listener.TLS, resources)
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

			result.routeKindErr = validateListenerAllowedRouteKinds(listener.AllowedRoutes)
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

func validateListenerAllowedRouteKinds(allowedRoutes *gwv1beta1.AllowedRoutes) error {
	if allowedRoutes == nil {
		return nil
	}
	for _, kind := range allowedRoutes.Kinds {
		if _, ok := allSupportedRouteKinds[kind.Kind]; !ok {
			return errListenerInvalidRouteKinds
		}
		if !common.NilOrEqual(kind.Group, gwv1beta1.GroupVersion.Group) {
			return errListenerInvalidRouteKinds
		}
	}
	return nil
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
		if string(kind.Kind) == gk.Kind && common.NilOrEqual(kind.Group, gk.Group) {
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
	if labels[common.NamespaceNameLabel] == name {
		// Already set, avoid copies
		return klabels.Set(labels)
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[common.NamespaceNameLabel] = name
	return klabels.Set(ret)
}
