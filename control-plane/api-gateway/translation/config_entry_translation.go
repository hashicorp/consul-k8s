// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package translation handles translating resources between different types
package translation

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
)

const (
	metaKeyManagedBy       = "managed-by"
	metaValueManagedBy     = "consul-k8s-gateway-controller"
	metaKeyKubeNS          = "k8s-namespace"
	metaKeyKubeServiceName = "k8s-service-name"
	metaKeyKubeName        = "k8s-name"

	// AnnotationGateway is the annotation used to override the gateway name.
	AnnotationGateway = "consul.hashicorp.com/gateway"
	// AnnotationHTTPRoute is the annotation used to override the http route name.
	AnnotationHTTPRoute = "consul.hashicorp.com/http-route"
	// AnnotationTCPRoute is the annotation used to override the tcp route name.
	AnnotationTCPRoute = "consul.hashicorp.com/tcp-route"
	// AnnotationInlineCertificate is the annotation used to override the inline certificate name.
	AnnotationInlineCertificate = "consul.hashicorp.com/inline-certificate"
)

func translateListenerProtocol[T ~string](protocol T) string {
	return strings.ToLower(string(protocol))
}

// K8sToConsulTranslator handles translating K8s resources into Consul config entries.
type K8sToConsulTranslator struct {
	EnableConsulNamespaces bool
	ConsulDestNamespace    string
	EnableK8sMirroring     bool
	MirroringPrefix        string
	ConsulPartition        string
}

// GatewayToAPIGateway translates a kuberenetes API gateway into a Consul APIGateway Config Entry.
func (t K8sToConsulTranslator) GatewayToAPIGateway(k8sGW gwv1beta1.Gateway, certs map[types.NamespacedName]api.ResourceReference) capi.APIGatewayConfigEntry {
	listeners := make([]capi.APIGatewayListener, 0, len(k8sGW.Spec.Listeners))
	for _, listener := range k8sGW.Spec.Listeners {
		var certificates []capi.ResourceReference
		if listener.TLS != nil {
			certificates = make([]capi.ResourceReference, 0, len(listener.TLS.CertificateRefs))
			for _, certificate := range listener.TLS.CertificateRefs {
				k8sNS := ""
				if certificate.Namespace != nil {
					k8sNS = string(*certificate.Namespace)
				}
				nsn := types.NamespacedName{Name: string(certificate.Name), Namespace: k8sNS}
				certRef, ok := certs[nsn]
				if !ok {
					// we don't have a ref for this certificate in consul
					// drop the ref from the created gateway
					continue
				}
				c := capi.ResourceReference{
					Kind:      capi.InlineCertificate,
					Name:      certRef.Name,
					Partition: certRef.Partition,
					Namespace: certRef.Namespace,
				}
				certificates = append(certificates, c)
			}
		}
		hostname := ""
		if listener.Hostname != nil {
			hostname = string(*listener.Hostname)
		}
		l := capi.APIGatewayListener{
			Name:     string(listener.Name),
			Hostname: hostname,
			Port:     int(listener.Port),
			Protocol: translateListenerProtocol(listener.Protocol),
			TLS: capi.APIGatewayTLSConfiguration{
				Certificates: certificates,
			},
		}

		listeners = append(listeners, l)
	}
	gwName := k8sGW.Name

	if gwNameFromAnnotation, ok := k8sGW.Annotations[AnnotationGateway]; ok && gwNameFromAnnotation != "" && !strings.Contains(gwNameFromAnnotation, ",") {
		gwName = gwNameFromAnnotation
	}

	return capi.APIGatewayConfigEntry{
		Kind: capi.APIGateway,
		Name: gwName,
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          k8sGW.GetObjectMeta().GetNamespace(),
			metaKeyKubeServiceName: k8sGW.GetObjectMeta().GetName(),
			metaKeyKubeName:        k8sGW.GetObjectMeta().GetName(),
		},
		Listeners: listeners,
		Partition: t.ConsulPartition,
		Namespace: t.getConsulNamespace(k8sGW.GetObjectMeta().GetNamespace()),
	}
}

func (t K8sToConsulTranslator) ReferenceForGateway(k8sGW *gwv1beta1.Gateway) api.ResourceReference {
	gwName := k8sGW.Name
	if gwNameFromAnnotation, ok := k8sGW.Annotations[AnnotationGateway]; ok && gwNameFromAnnotation != "" && !strings.Contains(gwNameFromAnnotation, ",") {
		gwName = gwNameFromAnnotation
	}
	return api.ResourceReference{
		Kind:      api.APIGateway,
		Name:      gwName,
		Namespace: t.getConsulNamespace(k8sGW.GetObjectMeta().GetNamespace()),
	}
}

// HTTPRouteToHTTPRoute translates a k8s HTTPRoute into a Consul HTTPRoute Config Entry.
func (t K8sToConsulTranslator) HTTPRouteToHTTPRoute(k8sHTTPRoute *gwv1beta1.HTTPRoute, parentRefs map[types.NamespacedName]api.ResourceReference, k8sServices map[types.NamespacedName]api.CatalogService, meshServices map[types.NamespacedName]v1alpha1.MeshService) *capi.HTTPRouteConfigEntry {
	routeName := k8sHTTPRoute.Name
	if routeNameFromAnnotation, ok := k8sHTTPRoute.Annotations[AnnotationHTTPRoute]; ok && routeNameFromAnnotation != "" && !strings.Contains(routeNameFromAnnotation, ",") {
		routeName = routeNameFromAnnotation
	}

	consulHTTPRoute := &capi.HTTPRouteConfigEntry{
		Kind: capi.HTTPRoute,
		Name: routeName,
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          k8sHTTPRoute.GetObjectMeta().GetNamespace(),
			metaKeyKubeServiceName: k8sHTTPRoute.GetObjectMeta().GetName(),
			metaKeyKubeName:        k8sHTTPRoute.GetObjectMeta().GetName(),
		},
		Partition: t.ConsulPartition,

		Namespace: t.getConsulNamespace(k8sHTTPRoute.GetObjectMeta().GetNamespace()),
	}

	// translate hostnames
	hostnames := make([]string, 0, len(k8sHTTPRoute.Spec.Hostnames))
	for _, k8Hostname := range k8sHTTPRoute.Spec.Hostnames {
		hostnames = append(hostnames, string(k8Hostname))
	}
	consulHTTPRoute.Hostnames = hostnames

	// translate parent refs
	consulHTTPRoute.Parents = translateRouteParentRefs(k8sHTTPRoute.Spec.CommonRouteSpec.ParentRefs, parentRefs)

	// translate rules
	consulHTTPRoute.Rules = t.translateHTTPRouteRules(k8sHTTPRoute.Namespace, k8sHTTPRoute.Spec.Rules, k8sServices, meshServices)

	return consulHTTPRoute
}

func (t K8sToConsulTranslator) ReferenceForHTTPRoute(k8sHTTPRoute *gwv1beta1.HTTPRoute) api.ResourceReference {
	routeName := k8sHTTPRoute.Name
	if routeNameFromAnnotation, ok := k8sHTTPRoute.Annotations[AnnotationHTTPRoute]; ok && routeNameFromAnnotation != "" && !strings.Contains(routeNameFromAnnotation, ",") {
		routeName = routeNameFromAnnotation
	}
	return api.ResourceReference{
		Kind:      api.HTTPRoute,
		Name:      routeName,
		Namespace: t.getConsulNamespace(k8sHTTPRoute.GetObjectMeta().GetNamespace()),
	}
}

// translates parent refs for Routes into Consul Resource References.
func translateRouteParentRefs(k8sParentRefs []gwv1beta1.ParentReference, parentRefs map[types.NamespacedName]api.ResourceReference) []capi.ResourceReference {
	parents := make([]capi.ResourceReference, 0, len(k8sParentRefs))
	for _, k8sParentRef := range k8sParentRefs {
		namespace := ""
		if k8sParentRef.Namespace != nil {
			namespace = string(*k8sParentRef.Namespace)
		}
		parentRef, ok := parentRefs[types.NamespacedName{Name: string(k8sParentRef.Name), Namespace: namespace}]
		if !(ok && isRefAPIGateway(k8sParentRef)) {
			// we drop any parent refs that consul does not know about
			continue
		}
		sectionName := ""
		if k8sParentRef.SectionName != nil {
			sectionName = string(*k8sParentRef.SectionName)
		}
		ref := capi.ResourceReference{
			Kind:        capi.APIGateway, // Will this ever not be a gateway? is that something we need to handle?
			Name:        parentRef.Name,
			SectionName: sectionName,
			Partition:   parentRef.Partition,
			Namespace:   parentRef.Namespace,
		}
		parents = append(parents, ref)
	}
	return parents
}

// isRefAPIGateway checks if the parent resource is an APIGateway.
func isRefAPIGateway(ref gwv1beta1.ParentReference) bool {
	return ref.Kind != nil && *ref.Kind == gwv1beta1.Kind("Gateway") || ref.Group != nil && string(*ref.Group) == gwv1beta1.GroupName
}

// translate the rules portion of a HTTPRoute.
func (t K8sToConsulTranslator) translateHTTPRouteRules(namespace string, k8sRules []gwv1beta1.HTTPRouteRule, k8sServices map[types.NamespacedName]api.CatalogService, meshServices map[types.NamespacedName]v1alpha1.MeshService) []capi.HTTPRouteRule {
	rules := make([]capi.HTTPRouteRule, 0, len(k8sRules))
	for _, k8sRule := range k8sRules {
		rule := capi.HTTPRouteRule{}
		// translate matches
		rule.Matches = translateHTTPMatches(k8sRule.Matches)

		// translate filters
		rule.Filters = translateHTTPFilters(k8sRule.Filters)

		// translate services
		rule.Services = t.translateHTTPServices(namespace, k8sRule.BackendRefs, k8sServices, meshServices)

		rules = append(rules, rule)
	}
	return rules
}

var headerMatchTypeTranslation = map[gwv1beta1.HeaderMatchType]capi.HTTPHeaderMatchType{
	gwv1beta1.HeaderMatchExact:             capi.HTTPHeaderMatchExact,
	gwv1beta1.HeaderMatchRegularExpression: capi.HTTPHeaderMatchRegularExpression,
}

var headerPathMatchTypeTranslation = map[gwv1beta1.PathMatchType]capi.HTTPPathMatchType{
	gwv1beta1.PathMatchExact:             capi.HTTPPathMatchExact,
	gwv1beta1.PathMatchPathPrefix:        capi.HTTPPathMatchPrefix,
	gwv1beta1.PathMatchRegularExpression: capi.HTTPPathMatchRegularExpression,
}

var queryMatchTypeTranslation = map[gwv1beta1.QueryParamMatchType]capi.HTTPQueryMatchType{
	gwv1beta1.QueryParamMatchExact:             capi.HTTPQueryMatchExact,
	gwv1beta1.QueryParamMatchRegularExpression: capi.HTTPQueryMatchRegularExpression,
}

// translate the http matches section.
func translateHTTPMatches(k8sMatches []gwv1beta1.HTTPRouteMatch) []capi.HTTPMatch {
	matches := make([]capi.HTTPMatch, 0, len(k8sMatches))
	for _, k8sMatch := range k8sMatches {
		// translate header matches
		headers := make([]capi.HTTPHeaderMatch, 0, len(k8sMatch.Headers))
		for _, k8sHeader := range k8sMatch.Headers {
			header := capi.HTTPHeaderMatch{
				Name:  string(k8sHeader.Name),
				Value: k8sHeader.Value,
			}
			if k8sHeader.Type != nil {
				header.Match = headerMatchTypeTranslation[*k8sHeader.Type]
			}
			headers = append(headers, header)
		}

		// translate query matches
		queries := make([]capi.HTTPQueryMatch, 0, len(k8sMatch.QueryParams))
		for _, k8sQuery := range k8sMatch.QueryParams {
			query := capi.HTTPQueryMatch{
				Name:  k8sQuery.Name,
				Value: k8sQuery.Value,
			}
			if k8sQuery.Type != nil {
				query.Match = queryMatchTypeTranslation[*k8sQuery.Type]
			}
			queries = append(queries, query)
		}

		match := capi.HTTPMatch{
			Headers: headers,
			Query:   queries,
		}
		if k8sMatch.Method != nil {
			match.Method = capi.HTTPMatchMethod(*k8sMatch.Method)
		}
		if k8sMatch.Path != nil {
			if k8sMatch.Path.Type != nil {
				match.Path.Match = headerPathMatchTypeTranslation[*k8sMatch.Path.Type]
			}
			if k8sMatch.Path.Value != nil {
				match.Path.Value = string(*k8sMatch.Path.Value)
			}
		}
		matches = append(matches, match)
	}
	return matches
}

// translate the http filters section.
func translateHTTPFilters(k8sFilters []gwv1beta1.HTTPRouteFilter) capi.HTTPFilters {
	add := make(map[string]string)
	set := make(map[string]string)
	remove := make([]string, 0)
	var urlRewrite *capi.URLRewrite
	for _, k8sFilter := range k8sFilters {
		for _, adder := range k8sFilter.RequestHeaderModifier.Add {
			add[string(adder.Name)] = adder.Value
		}

		for _, setter := range k8sFilter.RequestHeaderModifier.Set {
			set[string(setter.Name)] = setter.Value
		}

		remove = append(remove, k8sFilter.RequestHeaderModifier.Remove...)

		// we drop any path rewrites that are not prefix matches as we don't support those
		if k8sFilter.URLRewrite != nil && k8sFilter.URLRewrite.Path.Type == gwv1beta1.PrefixMatchHTTPPathModifier {
			urlRewrite = &capi.URLRewrite{Path: *k8sFilter.URLRewrite.Path.ReplacePrefixMatch}
		}

	}
	filter := capi.HTTPFilters{
		Headers: []capi.HTTPHeaderFilter{
			{
				Add:    add,
				Remove: remove,
				Set:    set,
			},
		},
		URLRewrite: urlRewrite,
	}

	return filter
}

// translate the backendrefs into services.
func (t K8sToConsulTranslator) translateHTTPServices(namespace string, k8sBackendRefs []gwv1beta1.HTTPBackendRef, k8sServices map[types.NamespacedName]api.CatalogService, meshServices map[types.NamespacedName]v1alpha1.MeshService) []capi.HTTPService {
	services := make([]capi.HTTPService, 0, len(k8sBackendRefs))

	for _, k8sRef := range k8sBackendRefs {
		backendRef := k8sRef.BackendObjectReference

		nsn := types.NamespacedName{
			Name:      string(backendRef.Name),
			Namespace: valueOr(backendRef.Namespace, namespace),
		}

		isServiceRef := nilOrEqual(backendRef.Group, "") && nilOrEqual(backendRef.Kind, "Service")
		isMeshServiceRef := derefEqual(backendRef.Group, v1alpha1.ConsulHashicorpGroup) && derefEqual(backendRef.Kind, v1alpha1.MeshServiceKind)

		k8sService, k8sServiceFound := k8sServices[nsn]
		meshService, meshServiceFound := meshServices[nsn]

		if isServiceRef && k8sServiceFound {
			service := capi.HTTPService{
				Name:      strings.TrimSuffix(k8sService.ServiceName, "-sidecar-proxy"),
				Namespace: t.getConsulNamespace(k8sService.Namespace),
				Filters:   translateHTTPFilters(k8sRef.Filters),
			}
			if k8sRef.Weight != nil {
				service.Weight = int(*k8sRef.Weight)
			}
			services = append(services, service)
		} else if isMeshServiceRef && meshServiceFound {
			service := capi.HTTPService{
				Name:      meshService.Spec.Name,
				Namespace: t.getConsulNamespace(meshService.Namespace),
				Filters:   translateHTTPFilters(k8sRef.Filters),
			}
			if k8sRef.Weight != nil {
				service.Weight = int(*k8sRef.Weight)
			}
			services = append(services, service)
		}
	}

	return services
}

// TCPRouteToTCPRoute translates a Kuberenetes TCPRoute into a Consul TCPRoute Config Entry.
func (t K8sToConsulTranslator) TCPRouteToTCPRoute(k8sRoute *gwv1alpha2.TCPRoute, parentRefs map[types.NamespacedName]api.ResourceReference, k8sServices map[types.NamespacedName]api.CatalogService, meshServices map[types.NamespacedName]v1alpha1.MeshService) *capi.TCPRouteConfigEntry {
	routeName := k8sRoute.Name
	if routeNameFromAnnotation, ok := k8sRoute.Annotations[AnnotationTCPRoute]; ok && routeNameFromAnnotation != "" && !strings.Contains(routeNameFromAnnotation, ",") {
		routeName = routeNameFromAnnotation
	}

	consulRoute := &capi.TCPRouteConfigEntry{
		Kind: capi.TCPRoute,
		Name: routeName,
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          k8sRoute.GetObjectMeta().GetNamespace(),
			metaKeyKubeServiceName: k8sRoute.GetObjectMeta().GetName(),
			metaKeyKubeName:        k8sRoute.GetObjectMeta().GetName(),
		},
		Partition: t.ConsulPartition,

		Namespace: t.getConsulNamespace(k8sRoute.GetObjectMeta().GetNamespace()),
	}

	// translate parent refs
	consulRoute.Parents = translateRouteParentRefs(k8sRoute.Spec.CommonRouteSpec.ParentRefs, parentRefs)

	// translate the services
	consulRoute.Services = make([]capi.TCPService, 0)
	for _, rule := range k8sRoute.Spec.Rules {
		for _, k8sref := range rule.BackendRefs {
			backendRef := k8sref.BackendObjectReference

			nsn := types.NamespacedName{
				Name:      string(backendRef.Name),
				Namespace: valueOr(backendRef.Namespace, k8sRoute.Namespace),
			}

			isServiceRef := nilOrEqual(backendRef.Group, "") && nilOrEqual(backendRef.Kind, "Service")
			isMeshServiceRef := derefEqual(backendRef.Group, v1alpha1.ConsulHashicorpGroup) && derefEqual(backendRef.Kind, v1alpha1.MeshServiceKind)

			k8sService, k8sServiceFound := k8sServices[nsn]
			meshService, meshServiceFound := meshServices[nsn]

			if isServiceRef && k8sServiceFound {
				service := capi.TCPService{
					Name:      strings.TrimSuffix(k8sService.ServiceName, "-sidecar-proxy"),
					Namespace: t.getConsulNamespace(k8sService.Namespace),
				}
				consulRoute.Services = append(consulRoute.Services, service)
			} else if isMeshServiceRef && meshServiceFound {
				service := capi.TCPService{
					Name:      meshService.Spec.Name,
					Namespace: t.getConsulNamespace(meshService.Namespace),
				}
				consulRoute.Services = append(consulRoute.Services, service)
			}
		}
	}

	return consulRoute
}

func (t K8sToConsulTranslator) ReferenceForTCPRoute(k8sTCPRoute *gwv1alpha2.TCPRoute) api.ResourceReference {
	routeName := k8sTCPRoute.Name
	if routeNameFromAnnotation, ok := k8sTCPRoute.Annotations[AnnotationTCPRoute]; ok && routeNameFromAnnotation != "" && !strings.Contains(routeNameFromAnnotation, ",") {
		routeName = routeNameFromAnnotation
	}
	return api.ResourceReference{
		Kind:      api.TCPRoute,
		Name:      routeName,
		Namespace: t.getConsulNamespace(k8sTCPRoute.GetObjectMeta().GetNamespace()),
	}
}

// SecretToInlineCertificate translates a Kuberenetes Secret into a Consul Inline Certificate Config Entry.
func (t K8sToConsulTranslator) SecretToInlineCertificate(k8sSecret corev1.Secret) capi.InlineCertificateConfigEntry {
	namespace := t.getConsulNamespace(k8sSecret.GetObjectMeta().GetNamespace())
	return capi.InlineCertificateConfigEntry{
		Kind:        capi.InlineCertificate,
		Namespace:   namespace,
		Name:        k8sSecret.Name,
		Certificate: k8sSecret.StringData[corev1.TLSCertKey],
		PrivateKey:  k8sSecret.StringData[corev1.TLSPrivateKeyKey],
		Meta: map[string]string{
			metaKeyManagedBy:       metaValueManagedBy,
			metaKeyKubeNS:          namespace,
			metaKeyKubeServiceName: string(k8sSecret.Name),
			metaKeyKubeName:        string(k8sSecret.Name),
		},
	}
}

func (t K8sToConsulTranslator) ReferenceForSecret(k8sSecret corev1.Secret) api.ResourceReference {
	return api.ResourceReference{
		Kind:      api.InlineCertificate,
		Name:      k8sSecret.Name,
		Namespace: t.getConsulNamespace(k8sSecret.GetObjectMeta().GetNamespace()),
	}
}

func EntryToNamespacedName(entry capi.ConfigEntry) types.NamespacedName {
	meta := entry.GetMeta()
	return types.NamespacedName{
		Name:      meta[metaKeyKubeName],
		Namespace: meta[metaKeyKubeNS],
	}
}

func (t K8sToConsulTranslator) getConsulNamespace(k8sNS string) string {
	return namespaces.ConsulNamespace(k8sNS, t.EnableK8sMirroring, t.ConsulDestNamespace, t.EnableK8sMirroring, t.MirroringPrefix)
}

func EntryToReference(entry capi.ConfigEntry) capi.ResourceReference {
	return capi.ResourceReference{
		Kind:      entry.GetKind(),
		Name:      entry.GetName(),
		Partition: entry.GetPartition(),
		Namespace: entry.GetNamespace(),
	}
}

func ptrTo[T any](v T) *T {
	return &v
}

func derefEqual[T ~string](v *T, check string) bool {
	if v == nil {
		return false
	}
	return string(*v) == check
}

func nilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

func valueOr[T ~string](v *T, fallback string) string {
	if v == nil {
		return fallback
	}
	return string(*v)
}
