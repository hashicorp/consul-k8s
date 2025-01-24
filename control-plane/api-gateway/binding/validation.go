// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/version"
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

	allSupportedTLSVersions = map[string]struct{}{
		"TLS_AUTO": {},
		"TLSv1_0":  {},
		"TLSv1_1":  {},
		"TLSv1_2":  {},
		"TLSv1_3":  {},
	}

	allTLSVersionsWithConfigurableCipherSuites = map[string]struct{}{
		// Remove "" and "TLS_AUTO" if Envoy ever sets TLS 1.3 as default minimum
		"":         {},
		"TLS_AUTO": {},
		"TLSv1_0":  {},
		"TLSv1_1":  {},
		"TLSv1_2":  {},
	}

	allSupportedTLSCipherSuites = map[string]struct{}{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       {},
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": {},
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         {},
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   {},
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       {},
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         {},

		// NOTE: the following cipher suites are currently supported by Envoy
		// but have been identified as insecure and are pending removal
		// https://github.com/envoyproxy/envoy/issues/5399
		"TLS_RSA_WITH_AES_128_GCM_SHA256": {},
		"TLS_RSA_WITH_AES_128_CBC_SHA":    {},
		"TLS_RSA_WITH_AES_256_GCM_SHA384": {},
		"TLS_RSA_WITH_AES_256_CBC_SHA":    {},
		// https://github.com/envoyproxy/envoy/issues/5400
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA": {},
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":   {},
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA": {},
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":   {},
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

func stringOrEmtpy(s *gwv1beta1.SectionName) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

func validateGatewayPolicies(gateway gwv1beta1.Gateway, policies []v1alpha1.GatewayPolicy, resources *common.ResourceMap) gatewayPolicyValidationResults {
	results := make(gatewayPolicyValidationResults, 0, len(policies))

	for _, policy := range policies {
		result := gatewayPolicyValidationResult{
			resolvedRefsErrs: []error{},
		}

		exists := listenerExistsForPolicy(gateway, policy)
		if !exists {
			result.resolvedRefsErrs = append(result.resolvedRefsErrs, errorForMissingListener(policy.Spec.TargetRef.Name, stringOrEmtpy(policy.Spec.TargetRef.SectionName)))
		}

		missingJWTProviders := make(map[string]struct{})
		if policy.Spec.Override != nil && policy.Spec.Override.JWT != nil {
			for _, policyJWTProvider := range policy.Spec.Override.JWT.Providers {
				_, jwtExists := resources.GetJWTProviderForGatewayJWTProvider(policyJWTProvider)
				if !jwtExists {
					missingJWTProviders[policyJWTProvider.Name] = struct{}{}
				}
			}
		}

		if policy.Spec.Default != nil && policy.Spec.Default.JWT != nil {
			for _, policyJWTProvider := range policy.Spec.Default.JWT.Providers {
				_, jwtExists := resources.GetJWTProviderForGatewayJWTProvider(policyJWTProvider)
				if !jwtExists {
					missingJWTProviders[policyJWTProvider.Name] = struct{}{}
				}
			}
		}

		if len(missingJWTProviders) > 0 {
			result.resolvedRefsErrs = append(result.resolvedRefsErrs, errorForMissingJWTProviders(missingJWTProviders))
		}

		if len(result.resolvedRefsErrs) > 0 {
			result.acceptedErr = errNotAcceptedDueToInvalidRefs
		}
		results = append(results, result)

	}
	return results
}

func listenerExistsForPolicy(gateway gwv1beta1.Gateway, policy v1alpha1.GatewayPolicy) bool {
	if policy.Spec.TargetRef.SectionName == nil {
		return false
	}

	return gateway.Name == policy.Spec.TargetRef.Name &&
		slices.ContainsFunc(gateway.Spec.Listeners, func(l gwv1beta1.Listener) bool { return l.Name == *policy.Spec.TargetRef.SectionName })
}

func errorForMissingListener(name, listenerName string) error {
	return fmt.Errorf("%w: gatewayName - %q, listenerName - %q", errPolicyListenerReferenceDoesNotExist, name, listenerName)
}

func errorForMissingJWTProviders(names map[string]struct{}) error {
	namesList := make([]string, 0, len(names))
	for name := range names {
		namesList = append(namesList, name)
	}
	slices.Sort(namesList)
	mergedNames := strings.Join(namesList, ",")
	return fmt.Errorf("%w: missingProviderNames: %s", errPolicyJWTProvidersReferenceDoesNotExist, mergedNames)
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
	// If there's no TLS, there's nothing to validate
	if tls == nil {
		return nil, nil
	}

	// Validate the certificate references and then return any error
	// alongside any TLS configuration error that we find below.
	refsErr := validateCertificateRefs(gateway, tls.CertificateRefs, resources)

	if tls.Mode != nil && *tls.Mode == gwv1beta1.TLSModePassthrough {
		return errListenerNoTLSPassthrough, refsErr
	}

	if err := validateTLSOptions(tls.Options); err != nil {
		return err, refsErr
	}

	return nil, refsErr
}

func validateJWT(gateway gwv1beta1.Gateway, listener gwv1beta1.Listener, resources *common.ResourceMap) error {
	policy, _ := resources.GetPolicyForGatewayListener(gateway, listener)
	if policy == nil {
		return nil
	}

	if policy.Spec.Override != nil && policy.Spec.Override.JWT != nil {
		for _, provider := range policy.Spec.Override.JWT.Providers {
			_, ok := resources.GetJWTProviderForGatewayJWTProvider(provider)
			if !ok {
				return errListenerJWTProviderNotFound
			}
		}
	}

	if policy.Spec.Default != nil && policy.Spec.Default.JWT != nil {
		for _, provider := range policy.Spec.Default.JWT.Providers {
			_, ok := resources.GetJWTProviderForGatewayJWTProvider(provider)
			if !ok {
				return errListenerJWTProviderNotFound
			}
		}
	}
	return nil
}

func validateCertificateRefs(gateway gwv1beta1.Gateway, refs []gwv1beta1.SecretObjectReference, resources *common.ResourceMap) error {
	for _, cert := range refs {
		// Verify that the reference has a group and kind that we support
		if !common.NilOrEqual(cert.Group, "") || !common.NilOrEqual(cert.Kind, common.KindSecret) {
			return errListenerInvalidCertificateRef_NotSupported
		}

		// Verify that the reference is within the namespace or,
		// if cross-namespace, that it's allowed by a ReferenceGrant
		if !resources.GatewayCanReferenceSecret(gateway, cert) {
			return errRefNotPermitted
		}

		// Verify that the referenced resource actually exists
		key := common.IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace)
		secret := resources.Certificate(key)
		if secret == nil {
			return errListenerInvalidCertificateRef_NotFound
		}

		// Verify that the referenced resource contains the data shape that we expect
		if err := validateCertificateData(*secret); err != nil {
			return err
		}
	}

	return nil
}

func validateTLSOptions(options map[gwv1beta1.AnnotationKey]gwv1beta1.AnnotationValue) error {
	if options == nil {
		return nil
	}

	tlsMinVersionValue := string(options[common.TLSMinVersionAnnotationKey])
	if tlsMinVersionValue != "" {
		if _, supported := allSupportedTLSVersions[tlsMinVersionValue]; !supported {
			return errListenerUnsupportedTLSMinVersion
		}
	}

	tlsMaxVersionValue := string(options[common.TLSMaxVersionAnnotationKey])
	if tlsMaxVersionValue != "" {
		if _, supported := allSupportedTLSVersions[tlsMaxVersionValue]; !supported {
			return errListenerUnsupportedTLSMaxVersion
		}
	}

	tlsCipherSuitesValue := string(options[common.TLSCipherSuitesAnnotationKey])
	if tlsCipherSuitesValue != "" {
		// If a minimum TLS version is configured, verify that it supports configuring cipher suites
		if tlsMinVersionValue != "" {
			if _, supported := allTLSVersionsWithConfigurableCipherSuites[tlsMinVersionValue]; !supported {
				return errListenerTLSCipherSuiteNotConfigurable
			}
		}

		for _, tlsCipherSuiteValue := range strings.Split(tlsCipherSuitesValue, ",") {
			tlsCipherSuite := strings.TrimSpace(tlsCipherSuiteValue)
			if _, supported := allSupportedTLSCipherSuites[tlsCipherSuite]; !supported {
				return errListenerUnsupportedTLSCipherSuite
			}
		}
	}

	return nil
}

func validateCertificateData(secret corev1.Secret) error {
	_, privateKey, err := common.ParseCertificateData(secret)
	if err != nil {
		return errListenerInvalidCertificateRef_InvalidData
	}

	err = common.ValidateKeyLength(privateKey)
	if err != nil {
		if version.IsFIPS() {
			return errListenerInvalidCertificateRef_FIPSRSAKeyLen
		}

		return errListenerInvalidCertificateRef_NonFIPSRSAKeyLen
	}

	return nil
}

// validateListeners validates the given listeners both internally and with respect to each
// other for purposes of setting "Conflicted" status conditions.
func validateListeners(gateway gwv1beta1.Gateway, listeners []gwv1beta1.Listener, resources *common.ResourceMap, gwcc *v1alpha1.GatewayClassConfig) listenerValidationResults {
	var results listenerValidationResults
	merged := make(map[gwv1beta1.PortNumber]mergedListeners)
	for i, listener := range listeners {
		merged[listener.Port] = append(merged[listener.Port], mergedListener{
			index:    i,
			listener: listener,
		})
	}
	// This list keeps track of port conflicts directly on gateways. i.e., two listeners on the same port as
	// defined by the user.
	seenListenerPorts := map[int]struct{}{}
	// This list keeps track of port conflicts caused by privileged port mappings.
	seenContainerPorts := map[int]struct{}{}
	portMapping := int32(0)
	if gwcc != nil {
		portMapping = gwcc.Spec.MapPrivilegedContainerPorts
	}
	for i, listener := range listeners {
		var result listenerValidationResult

		err, refErr := validateTLS(gateway, listener.TLS, resources)
		if refErr != nil {
			result.refErrs = append(result.refErrs, refErr)
		}

		jwtErr := validateJWT(gateway, listener, resources)
		if jwtErr != nil {
			result.refErrs = append(result.refErrs, jwtErr)
		}

		if err != nil {
			result.acceptedErr = err
		} else if jwtErr != nil {
			result.acceptedErr = jwtErr
		} else {
			_, supported := supportedKindsForProtocol[listener.Protocol]
			if !supported {
				result.acceptedErr = errListenerUnsupportedProtocol
			} else if listener.Port == 20000 { // admin port
				result.acceptedErr = errListenerPortUnavailable
			} else if _, ok := seenListenerPorts[int(listener.Port)]; ok {
				result.acceptedErr = errListenerPortUnavailable
			} else if _, ok := seenContainerPorts[common.ToContainerPort(listener.Port, portMapping)]; ok {
				result.acceptedErr = errListenerMappedToPrivilegedPortMapping
			}

			result.routeKindErr = validateListenerAllowedRouteKinds(listener.AllowedRoutes)
		}

		if err := merged[listener.Port].validateProtocol(); err != nil {
			result.conflictedErr = err
		} else {
			result.conflictedErr = merged[listener.Port].validateHostname(i, listener)
		}

		results = append(results, result)

		seenListenerPorts[int(listener.Port)] = struct{}{}
		seenContainerPorts[common.ToContainerPort(listener.Port, portMapping)] = struct{}{}
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

// externalRefsOnRouteAllExist checks to make sure that all external filters referenced by the route exist in the resource map.
func externalRefsOnRouteAllExist(route *gwv1beta1.HTTPRoute, resources *common.ResourceMap) bool {
	for _, rule := range route.Spec.Rules {
		for _, filter := range rule.Filters {
			if filter.Type != gwv1beta1.HTTPRouteFilterExtensionRef {
				continue
			}

			if !resources.ExternalFilterExists(*filter.ExtensionRef, route.Namespace) {
				return false
			}

		}

		for _, backendRef := range rule.BackendRefs {
			for _, filter := range backendRef.Filters {
				if filter.Type != gwv1beta1.HTTPRouteFilterExtensionRef {
					continue
				}

				if !resources.ExternalFilterExists(*filter.ExtensionRef, route.Namespace) {
					return false
				}
			}
		}
	}

	return true
}

func checkIfReferencesMissingJWTProvider(filter gwv1beta1.HTTPRouteFilter, resources *common.ResourceMap, namespace string, invalidFilters map[string]struct{}) {
	if filter.Type != gwv1beta1.HTTPRouteFilterExtensionRef {
		return
	}
	externalFilter, ok := resources.GetExternalFilter(*filter.ExtensionRef, namespace)
	if !ok {
		return
	}
	authFilter, ok := externalFilter.(*v1alpha1.RouteAuthFilter)
	if !ok {
		return
	}

	for _, provider := range authFilter.Spec.JWT.Providers {
		_, ok := resources.GetJWTProviderForGatewayJWTProvider(provider)
		if !ok {
			invalidFilters[fmt.Sprintf("%s/%s", namespace, authFilter.Name)] = struct{}{}
			return
		}
	}
}

func authFilterReferencesMissingJWTProvider(httproute *gwv1beta1.HTTPRoute, resources *common.ResourceMap) []string {
	invalidFilters := make(map[string]struct{})
	for _, rule := range httproute.Spec.Rules {
		for _, filter := range rule.Filters {
			checkIfReferencesMissingJWTProvider(filter, resources, httproute.Namespace, invalidFilters)
		}

		for _, backendRef := range rule.BackendRefs {
			for _, filter := range backendRef.Filters {
				checkIfReferencesMissingJWTProvider(filter, resources, httproute.Namespace, invalidFilters)
			}
		}
	}

	return maps.Keys(invalidFilters)
}

// externalRefsKindAllowedOnRoute makes sure that all externalRefs reference a kind supported by gatewaycontroller.
func externalRefsKindAllowedOnRoute(route *gwv1beta1.HTTPRoute) bool {
	for _, rule := range route.Spec.Rules {
		if !filtersAllAllowedType(rule.Filters) {
			return false
		}

		// same thing but for backendref
		for _, backendRef := range rule.BackendRefs {
			if !filtersAllAllowedType(backendRef.Filters) {
				return false
			}
		}
	}
	return true
}

func filtersAllAllowedType(filters []gwv1beta1.HTTPRouteFilter) bool {
	for _, filter := range filters {
		if filter.ExtensionRef == nil {
			continue
		}

		if !common.FilterIsExternalFilter(filter) {
			return false
		}
	}
	return true
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

func validateAuthFilters(authFilters []*v1alpha1.RouteAuthFilter, resources *common.ResourceMap) authFilterValidationResults {
	results := make(authFilterValidationResults, 0, len(authFilters))

	for _, filter := range authFilters {
		if filter == nil {
			continue
		}
		var result authFilterValidationResult
		missingJWTProviders := make([]string, 0)
		for _, provider := range filter.Spec.JWT.Providers {
			if _, ok := resources.GetJWTProviderForGatewayJWTProvider(provider); !ok {
				missingJWTProviders = append(missingJWTProviders, provider.Name)
			}
		}

		if len(missingJWTProviders) > 0 {
			mergedNames := strings.Join(missingJWTProviders, ",")
			result.resolvedRefErr = fmt.Errorf("%w: missingProviderNames: %s", errRouteFilterJWTProvidersReferenceDoesNotExist, mergedNames)
		}

		if result.resolvedRefErr != nil {
			result.acceptedErr = errRouteFilterNotAcceptedDueToInvalidRefs
		}

		results = append(results, result)
	}
	return results
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
