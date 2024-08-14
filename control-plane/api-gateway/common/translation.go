// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

// ResourceTranslator handles translating K8s resources into Consul config entries.
type ResourceTranslator struct {
	EnableConsulNamespaces bool
	ConsulDestNamespace    string
	EnableK8sMirroring     bool
	MirroringPrefix        string
	ConsulPartition        string
	Datacenter             string
}

func (t ResourceTranslator) NonNormalizedConfigEntryReference(kind string, id types.NamespacedName) api.ResourceReference {
	return api.ResourceReference{
		Kind:      kind,
		Name:      id.Name,
		Namespace: t.Namespace(id.Namespace),
		Partition: t.ConsulPartition,
	}
}

func (t ResourceTranslator) ConfigEntryReference(kind string, id types.NamespacedName) api.ResourceReference {
	return NormalizeMeta(t.NonNormalizedConfigEntryReference(kind, id))
}

func (t ResourceTranslator) NormalizedResourceReference(kind, namespace string, ref api.ResourceReference) api.ResourceReference {
	return NormalizeMeta(api.ResourceReference{
		Kind:        kind,
		Name:        ref.Name,
		SectionName: ref.SectionName,
		Namespace:   t.Namespace(namespace),
		Partition:   t.ConsulPartition,
	})
}

func (t ResourceTranslator) Namespace(namespace string) string {
	return namespaces.ConsulNamespace(namespace, t.EnableConsulNamespaces, t.ConsulDestNamespace, t.EnableK8sMirroring, t.MirroringPrefix)
}

// ToAPIGateway translates a kuberenetes API gateway into a Consul APIGateway Config Entry.
func (t ResourceTranslator) ToAPIGateway(gateway gwv1beta1.Gateway, resources *ResourceMap, gwcc *v1alpha1.GatewayClassConfig) *api.APIGatewayConfigEntry {
	namespace := t.Namespace(gateway.Namespace)

	listeners := ConvertSliceFuncIf(gateway.Spec.Listeners, func(listener gwv1beta1.Listener) (api.APIGatewayListener, bool) {
		return t.toAPIGatewayListener(gateway, listener, resources, gwcc)
	})

	return &api.APIGatewayConfigEntry{
		Kind:      api.APIGateway,
		Name:      gateway.Name,
		Namespace: namespace,
		Partition: t.ConsulPartition,
		Meta: t.addDatacenterToMeta(map[string]string{
			constants.MetaKeyKubeNS:   gateway.Namespace,
			constants.MetaKeyKubeName: gateway.Name,
		}),
		Listeners: listeners,
	}
}

var listenerProtocolMap = map[string]string{
	"https": "http",
	"http":  "http",
	"tcp":   "tcp",
}

func (t ResourceTranslator) toAPIGatewayListener(gateway gwv1beta1.Gateway, listener gwv1beta1.Listener, resources *ResourceMap, gwcc *v1alpha1.GatewayClassConfig) (api.APIGatewayListener, bool) {
	namespace := gateway.Namespace

	var certificates []api.ResourceReference
	var cipherSuites []string
	var maxVersion, minVersion string

	if listener.TLS != nil {
		cipherSuitesVal := string(listener.TLS.Options[TLSCipherSuitesAnnotationKey])
		if cipherSuitesVal != "" {
			cipherSuites = strings.Split(cipherSuitesVal, ",")
		}
		maxVersion = string(listener.TLS.Options[TLSMaxVersionAnnotationKey])
		minVersion = string(listener.TLS.Options[TLSMinVersionAnnotationKey])

		for _, ref := range listener.TLS.CertificateRefs {
			if !resources.GatewayCanReferenceSecret(gateway, ref) {
				return api.APIGatewayListener{}, false
			}

			if !NilOrEqual(ref.Group, "") || !NilOrEqual(ref.Kind, "Secret") {
				// only translate the valid types we support
				continue
			}

			ref := IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, namespace)
			if resources.Certificate(ref) != nil {
				certificates = append(certificates, t.NonNormalizedConfigEntryReference(api.FileSystemCertificate, ref))
			}
		}
	}

	// Grab policy if it exists.
	gatewayPolicyCrd, _ := resources.GetPolicyForGatewayListener(gateway, listener)
	defaultPolicy, overridePolicy := t.translateGatewayPolicy(gatewayPolicyCrd)

	portMapping := int32(0)
	if gwcc != nil {
		portMapping = gwcc.Spec.MapPrivilegedContainerPorts
	}

	return api.APIGatewayListener{
		Name:     string(listener.Name),
		Hostname: DerefStringOr(listener.Hostname, ""),
		Port:     ToContainerPort(listener.Port, portMapping),
		Protocol: listenerProtocolMap[strings.ToLower(string(listener.Protocol))],
		TLS: api.APIGatewayTLSConfiguration{
			Certificates: certificates,
			CipherSuites: cipherSuites,
			MaxVersion:   maxVersion,
			MinVersion:   minVersion,
		},
		Default:  defaultPolicy,
		Override: overridePolicy,
	}, true
}

func ToContainerPort(portNumber gwv1beta1.PortNumber, mapPrivilegedContainerPorts int32) int {
	if portNumber >= 1024 {
		// We don't care about privileged port-mapping, this is a non-privileged port
		return int(portNumber)
	}

	return int(portNumber) + int(mapPrivilegedContainerPorts)
}

func (t ResourceTranslator) translateRouteRetryFilter(routeRetryFilter *v1alpha1.RouteRetryFilter) *api.RetryFilter {
	filter := &api.RetryFilter{
		RetryOn:            routeRetryFilter.Spec.RetryOn,
		RetryOnStatusCodes: routeRetryFilter.Spec.RetryOnStatusCodes,
	}

	if routeRetryFilter.Spec.NumRetries != nil {
		filter.NumRetries = *routeRetryFilter.Spec.NumRetries
	}

	if routeRetryFilter.Spec.RetryOnConnectFailure != nil {
		filter.RetryOnConnectFailure = *routeRetryFilter.Spec.RetryOnConnectFailure
	}

	return filter
}

func (t ResourceTranslator) translateRouteTimeoutFilter(routeTimeoutFilter *v1alpha1.RouteTimeoutFilter) *api.TimeoutFilter {
	return &api.TimeoutFilter{
		RequestTimeout: routeTimeoutFilter.Spec.RequestTimeout.Duration,
		IdleTimeout:    routeTimeoutFilter.Spec.IdleTimeout.Duration,
	}
}

func (t ResourceTranslator) translateRouteJWTFilter(routeJWTFilter *v1alpha1.RouteAuthFilter) *api.JWTFilter {
	if routeJWTFilter.Spec.JWT == nil {
		return nil
	}

	return &api.JWTFilter{
		Providers: ConvertSliceFunc(routeJWTFilter.Spec.JWT.Providers, t.translateJWTProvider),
	}
}

func (t ResourceTranslator) translateGatewayPolicy(policy *v1alpha1.GatewayPolicy) (*api.APIGatewayPolicy, *api.APIGatewayPolicy) {
	if policy == nil {
		return nil, nil
	}

	var defaultPolicy, overridePolicy *api.APIGatewayPolicy

	if policy.Spec.Default != nil {
		defaultPolicy = &api.APIGatewayPolicy{
			JWT: t.translateJWTRequirement(policy.Spec.Default.JWT),
		}
	}

	if policy.Spec.Override != nil {
		overridePolicy = &api.APIGatewayPolicy{
			JWT: t.translateJWTRequirement(policy.Spec.Override.JWT),
		}
	}
	return defaultPolicy, overridePolicy
}

func (t ResourceTranslator) translateJWTRequirement(crdRequirement *v1alpha1.GatewayJWTRequirement) *api.APIGatewayJWTRequirement {
	apiRequirement := api.APIGatewayJWTRequirement{}
	providers := ConvertSliceFunc(crdRequirement.Providers, t.translateJWTProvider)
	apiRequirement.Providers = providers
	return &apiRequirement
}

func (t ResourceTranslator) translateJWTProvider(crdProvider *v1alpha1.GatewayJWTProvider) *api.APIGatewayJWTProvider {
	if crdProvider == nil {
		return nil
	}

	apiProvider := api.APIGatewayJWTProvider{
		Name: crdProvider.Name,
	}
	claims := ConvertSliceFunc(crdProvider.VerifyClaims, t.translateVerifyClaims)
	apiProvider.VerifyClaims = claims

	return &apiProvider
}

func (t ResourceTranslator) translateVerifyClaims(crdClaims *v1alpha1.GatewayJWTClaimVerification) *api.APIGatewayJWTClaimVerification {
	if crdClaims == nil {
		return nil
	}
	verifyClaim := api.APIGatewayJWTClaimVerification{
		Path:  crdClaims.Path,
		Value: crdClaims.Value,
	}
	return &verifyClaim
}

func (t ResourceTranslator) ToHTTPRoute(route gwv1beta1.HTTPRoute, resources *ResourceMap) *api.HTTPRouteConfigEntry {
	namespace := t.Namespace(route.Namespace)

	// We don't translate parent refs.

	hostnames := StringLikeSlice(route.Spec.Hostnames)
	rules := ConvertSliceFuncIf(
		route.Spec.Rules,
		func(rule gwv1beta1.HTTPRouteRule) (api.HTTPRouteRule, bool) {
			return t.translateHTTPRouteRule(route, rule, resources)
		})

	configEntry := api.HTTPRouteConfigEntry{
		Kind:      api.HTTPRoute,
		Name:      route.Name,
		Namespace: namespace,
		Partition: t.ConsulPartition,
		Meta: t.addDatacenterToMeta(map[string]string{
			constants.MetaKeyKubeNS:   route.Namespace,
			constants.MetaKeyKubeName: route.Name,
		}),
		Hostnames: hostnames,
		Rules:     rules,
	}

	return &configEntry
}

func (t ResourceTranslator) translateHTTPRouteRule(route gwv1beta1.HTTPRoute, rule gwv1beta1.HTTPRouteRule, resources *ResourceMap) (api.HTTPRouteRule, bool) {
	services := ConvertSliceFuncIf(
		rule.BackendRefs,
		func(ref gwv1beta1.HTTPBackendRef) (api.HTTPService, bool) {
			return t.translateHTTPBackendRef(route, ref, resources)
		})

	if len(services) == 0 {
		return api.HTTPRouteRule{}, false
	}

	matches := ConvertSliceFunc(rule.Matches, t.translateHTTPMatch)
	filters, responseFilters := t.translateHTTPFilters(rule.Filters, resources, route.Namespace)

	return api.HTTPRouteRule{
		Filters:         filters,
		Matches:         matches,
		ResponseFilters: responseFilters,
		Services:        services,
	}, true
}

func (t ResourceTranslator) translateHTTPBackendRef(route gwv1beta1.HTTPRoute, ref gwv1beta1.HTTPBackendRef, resources *ResourceMap) (api.HTTPService, bool) {
	id := types.NamespacedName{
		Name:      string(ref.Name),
		Namespace: DerefStringOr(ref.Namespace, route.Namespace),
	}

	isServiceRef := NilOrEqual(ref.Group, "") && NilOrEqual(ref.Kind, "Service")

	if isServiceRef && resources.HasService(id) && resources.HTTPRouteCanReferenceBackend(route, ref.BackendRef) {
		filters, responseFilters := t.translateHTTPFilters(ref.Filters, resources, route.Namespace)
		service := resources.Service(id)
		return api.HTTPService{
			Name:            service.Name,
			Namespace:       service.Namespace,
			Partition:       t.ConsulPartition,
			Filters:         filters,
			ResponseFilters: responseFilters,
			Weight:          DerefIntOr(ref.Weight, 1),
		}, true
	}

	isMeshServiceRef := DerefEqual(ref.Group, v1alpha1.ConsulHashicorpGroup) && DerefEqual(ref.Kind, v1alpha1.MeshServiceKind)
	if isMeshServiceRef && resources.HasMeshService(id) && resources.HTTPRouteCanReferenceBackend(route, ref.BackendRef) {
		filters, responseFilters := t.translateHTTPFilters(ref.Filters, resources, route.Namespace)
		service := resources.MeshService(id)

		return api.HTTPService{
			Name:            service.Name,
			Namespace:       service.Namespace,
			Partition:       t.ConsulPartition,
			Filters:         filters,
			ResponseFilters: responseFilters,
			Weight:          DerefIntOr(ref.Weight, 1),
		}, true
	}

	return api.HTTPService{}, false
}

var headerMatchTypeTranslation = map[gwv1beta1.HeaderMatchType]api.HTTPHeaderMatchType{
	gwv1beta1.HeaderMatchExact:             api.HTTPHeaderMatchExact,
	gwv1beta1.HeaderMatchRegularExpression: api.HTTPHeaderMatchRegularExpression,
}

var headerPathMatchTypeTranslation = map[gwv1beta1.PathMatchType]api.HTTPPathMatchType{
	gwv1beta1.PathMatchExact:             api.HTTPPathMatchExact,
	gwv1beta1.PathMatchPathPrefix:        api.HTTPPathMatchPrefix,
	gwv1beta1.PathMatchRegularExpression: api.HTTPPathMatchRegularExpression,
}

var queryMatchTypeTranslation = map[gwv1beta1.QueryParamMatchType]api.HTTPQueryMatchType{
	gwv1beta1.QueryParamMatchExact:             api.HTTPQueryMatchExact,
	gwv1beta1.QueryParamMatchRegularExpression: api.HTTPQueryMatchRegularExpression,
}

func (t ResourceTranslator) translateHTTPMatch(match gwv1beta1.HTTPRouteMatch) api.HTTPMatch {
	headers := ConvertSliceFunc(match.Headers, t.translateHTTPHeaderMatch)
	queries := ConvertSliceFunc(match.QueryParams, t.translateHTTPQueryMatch)

	return api.HTTPMatch{
		Headers: headers,
		Query:   queries,
		Path:    DerefConvertFunc(match.Path, t.translateHTTPPathMatch),
		Method:  api.HTTPMatchMethod(DerefStringOr(match.Method, "")),
	}
}

func (t ResourceTranslator) translateHTTPPathMatch(match gwv1beta1.HTTPPathMatch) api.HTTPPathMatch {
	return api.HTTPPathMatch{
		Match: DerefLookup(match.Type, headerPathMatchTypeTranslation),
		Value: DerefStringOr(match.Value, ""),
	}
}

func (t ResourceTranslator) translateHTTPHeaderMatch(match gwv1beta1.HTTPHeaderMatch) api.HTTPHeaderMatch {
	return api.HTTPHeaderMatch{
		Name:  string(match.Name),
		Value: match.Value,
		Match: DerefLookup(match.Type, headerMatchTypeTranslation),
	}
}

func (t ResourceTranslator) translateHTTPQueryMatch(match gwv1beta1.HTTPQueryParamMatch) api.HTTPQueryMatch {
	return api.HTTPQueryMatch{
		Name:  string(match.Name),
		Value: match.Value,
		Match: DerefLookup(match.Type, queryMatchTypeTranslation),
	}
}

func (t ResourceTranslator) translateHTTPFilters(filters []gwv1beta1.HTTPRouteFilter, resourceMap *ResourceMap, namespace string) (api.HTTPFilters, api.HTTPResponseFilters) {
	var (
		urlRewrite            *api.URLRewrite
		retryFilter           *api.RetryFilter
		timeoutFilter         *api.TimeoutFilter
		requestHeaderFilters  = []api.HTTPHeaderFilter{}
		responseHeaderFilters = []api.HTTPHeaderFilter{}
		jwtFilter             *api.JWTFilter
	)

	// Convert Gateway API filters to portions of the Consul request and response filters.
	// Multiple filters applying the same or conflicting operations are allowed but may
	// result in unexpected behavior.
	for _, filter := range filters {
		if filter.RequestHeaderModifier != nil {
			newFilter := api.HTTPHeaderFilter{}

			newFilter.Remove = append(newFilter.Remove, filter.RequestHeaderModifier.Remove...)

			if len(filter.RequestHeaderModifier.Add) > 0 {
				newFilter.Add = map[string]string{}
				for _, toAdd := range filter.RequestHeaderModifier.Add {
					newFilter.Add[string(toAdd.Name)] = toAdd.Value
				}
			}

			if len(filter.RequestHeaderModifier.Set) > 0 {
				newFilter.Set = map[string]string{}
				for _, toSet := range filter.RequestHeaderModifier.Set {
					newFilter.Set[string(toSet.Name)] = toSet.Value
				}
			}

			requestHeaderFilters = append(requestHeaderFilters, newFilter)
		}

		if filter.ResponseHeaderModifier != nil {
			newFilter := api.HTTPHeaderFilter{}

			newFilter.Remove = append(newFilter.Remove, filter.ResponseHeaderModifier.Remove...)

			if len(filter.ResponseHeaderModifier.Add) > 0 {
				newFilter.Add = map[string]string{}
				for _, toAdd := range filter.ResponseHeaderModifier.Add {
					newFilter.Add[string(toAdd.Name)] = toAdd.Value
				}
			}

			if len(filter.ResponseHeaderModifier.Set) > 0 {
				newFilter.Set = map[string]string{}
				for _, toSet := range filter.ResponseHeaderModifier.Set {
					newFilter.Set[string(toSet.Name)] = toSet.Value
				}
			}

			responseHeaderFilters = append(responseHeaderFilters, newFilter)
		}

		// we drop any path rewrites that are not prefix matches as we don't support those
		if filter.URLRewrite != nil &&
			filter.URLRewrite.Path != nil &&
			filter.URLRewrite.Path.Type == gwv1beta1.PrefixMatchHTTPPathModifier {
			urlRewrite = &api.URLRewrite{Path: DerefStringOr(filter.URLRewrite.Path.ReplacePrefixMatch, "")}
		}

		if filter.ExtensionRef != nil {
			// get crd from resources map
			crdFilter, exists := resourceMap.GetExternalFilter(*filter.ExtensionRef, namespace)
			if !exists {
				// this should never be the case because we only translate a route if it's actually valid, and if we're missing filters during the validation step, then we won't get here
				continue
			}

			switch filter.ExtensionRef.Kind {
			case v1alpha1.RouteRetryFilterKind:
				retryFilter = t.translateRouteRetryFilter(crdFilter.(*v1alpha1.RouteRetryFilter))
			case v1alpha1.RouteTimeoutFilterKind:
				timeoutFilter = t.translateRouteTimeoutFilter(crdFilter.(*v1alpha1.RouteTimeoutFilter))
			case v1alpha1.RouteAuthFilterKind:
				jwtFilter = t.translateRouteJWTFilter(crdFilter.(*v1alpha1.RouteAuthFilter))
			}
		}
	}

	requestFilter := api.HTTPFilters{
		Headers:       requestHeaderFilters,
		URLRewrite:    urlRewrite,
		RetryFilter:   retryFilter,
		TimeoutFilter: timeoutFilter,
		JWT:           jwtFilter,
	}

	responseFilter := api.HTTPResponseFilters{
		Headers: responseHeaderFilters,
	}

	return requestFilter, responseFilter
}

func (t ResourceTranslator) ToTCPRoute(route gwv1alpha2.TCPRoute, resources *ResourceMap) *api.TCPRouteConfigEntry {
	namespace := t.Namespace(route.Namespace)

	// we don't translate parent refs

	backendRefs := ConvertSliceFunc(route.Spec.Rules, func(rule gwv1alpha2.TCPRouteRule) []gwv1beta1.BackendRef { return rule.BackendRefs })
	flattenedRefs := Flatten(backendRefs)
	services := ConvertSliceFuncIf(flattenedRefs, func(ref gwv1beta1.BackendRef) (api.TCPService, bool) {
		return t.translateTCPRouteRule(route, ref, resources)
	})

	return &api.TCPRouteConfigEntry{
		Kind:      api.TCPRoute,
		Name:      route.Name,
		Namespace: namespace,
		Partition: t.ConsulPartition,
		Meta: t.addDatacenterToMeta(map[string]string{
			constants.MetaKeyKubeNS:   route.Namespace,
			constants.MetaKeyKubeName: route.Name,
		}),
		Services: services,
	}
}

func (t ResourceTranslator) translateTCPRouteRule(route gwv1alpha2.TCPRoute, ref gwv1beta1.BackendRef, resources *ResourceMap) (api.TCPService, bool) {
	// we ignore weight for now

	id := types.NamespacedName{
		Name:      string(ref.Name),
		Namespace: DerefStringOr(ref.Namespace, route.Namespace),
	}

	isServiceRef := NilOrEqual(ref.Group, "") && NilOrEqual(ref.Kind, "Service")
	if isServiceRef && resources.HasService(id) && resources.TCPRouteCanReferenceBackend(route, ref) {
		service := resources.Service(id)

		return api.TCPService{
			Name:      service.Name,
			Namespace: service.Namespace,
		}, true
	}

	isMeshServiceRef := DerefEqual(ref.Group, v1alpha1.ConsulHashicorpGroup) && DerefEqual(ref.Kind, v1alpha1.MeshServiceKind)
	if isMeshServiceRef && resources.HasMeshService(id) && resources.TCPRouteCanReferenceBackend(route, ref) {
		service := resources.MeshService(id)

		return api.TCPService{
			Name:      service.Name,
			Namespace: service.Namespace,
		}, true
	}

	return api.TCPService{}, false
}

func (t ResourceTranslator) ToFileSystemCertificate(secret corev1.Secret) *api.FileSystemCertificateConfigEntry {
	return &api.FileSystemCertificateConfigEntry{
		Kind:        api.FileSystemCertificate,
		Name:        secret.Name,
		Namespace:   t.Namespace(secret.Namespace),
		Partition:   t.ConsulPartition,
		Certificate: fmt.Sprintf("/consul/gateway-certificates/%s_%s_tls.crt", secret.Namespace, secret.Name),
		PrivateKey:  fmt.Sprintf("/consul/gateway-certificates/%s_%s_tls.key", secret.Namespace, secret.Name),
		Meta: t.addDatacenterToMeta(map[string]string{
			constants.MetaKeyKubeNS:   secret.Namespace,
			constants.MetaKeyKubeName: secret.Name,
		}),
	}
}

func EntryToNamespacedName(entry api.ConfigEntry) types.NamespacedName {
	meta := entry.GetMeta()

	return types.NamespacedName{
		Namespace: meta[constants.MetaKeyKubeNS],
		Name:      meta[constants.MetaKeyKubeName],
	}
}

func (t ResourceTranslator) addDatacenterToMeta(meta map[string]string) map[string]string {
	if t.Datacenter == "" {
		return meta
	}
	meta[constants.MetaKeyDatacenter] = t.Datacenter
	return meta
}
