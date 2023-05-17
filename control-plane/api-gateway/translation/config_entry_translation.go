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
			Protocol: string(listener.Protocol),
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
func (t K8sToConsulTranslator) HTTPRouteToHTTPRoute(k8sHTTPRoute *gwv1beta1.HTTPRoute, parentRefs map[types.NamespacedName]api.ResourceReference) *capi.HTTPRouteConfigEntry {
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
	consulHTTPRoute.Rules = t.translateHTTPRouteRules(k8sHTTPRoute.Spec.Rules)

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
func (t K8sToConsulTranslator) translateHTTPRouteRules(k8sRules []gwv1beta1.HTTPRouteRule) []capi.HTTPRouteRule {
	rules := make([]capi.HTTPRouteRule, 0, len(k8sRules))
	for _, k8sRule := range k8sRules {
		rule := capi.HTTPRouteRule{}
		// translate matches
		rule.Matches = translateHTTPMatches(k8sRule.Matches)

		// translate filters
		rule.Filters = translateHTTPFilters(k8sRule.Filters)

		// translate services
		rule.Services = t.translateHTTPServices(k8sRule.BackendRefs)

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
				Match: headerMatchTypeTranslation[*k8sHeader.Type],
				Name:  string(k8sHeader.Name),
				Value: k8sHeader.Value,
			}
			headers = append(headers, header)
		}

		// translate query matches
		queries := make([]capi.HTTPQueryMatch, 0, len(k8sMatch.QueryParams))
		for _, k8sQuery := range k8sMatch.QueryParams {
			query := capi.HTTPQueryMatch{
				Match: queryMatchTypeTranslation[*k8sQuery.Type],
				Name:  k8sQuery.Name,
				Value: k8sQuery.Value,
			}
			queries = append(queries, query)
		}

		match := capi.HTTPMatch{
			Headers: headers,
			Method:  capi.HTTPMatchMethod(*k8sMatch.Method),
			Path: capi.HTTPPathMatch{
				Match: headerPathMatchTypeTranslation[*k8sMatch.Path.Type],
				Value: string(*k8sMatch.Path.Value),
			},
			Query: queries,
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
func (t K8sToConsulTranslator) translateHTTPServices(k8sBackendRefs []gwv1beta1.HTTPBackendRef) []capi.HTTPService {
	services := make([]capi.HTTPService, 0, len(k8sBackendRefs))

	for _, k8sRef := range k8sBackendRefs {
		service := capi.HTTPService{
			Name:      string(k8sRef.Name),
			Weight:    int(*k8sRef.Weight),
			Filters:   translateHTTPFilters(k8sRef.Filters),
			Namespace: t.getConsulNamespace(string(*k8sRef.Namespace)),
		}
		services = append(services, service)
	}

	return services
}

// TCPRouteToTCPRoute translates a Kuberenetes TCPRoute into a Consul TCPRoute Config Entry.
func (t K8sToConsulTranslator) TCPRouteToTCPRoute(k8sRoute *gwv1alpha2.TCPRoute, parentRefs map[types.NamespacedName]api.ResourceReference) *capi.TCPRouteConfigEntry {
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
			k8srefNS := ""
			if k8sref.Namespace != nil {
				k8srefNS = string(*k8sref.Namespace)
			}
			tcpService := capi.TCPService{
				Name:      string(k8sref.Name),
				Partition: t.ConsulPartition,
				Namespace: t.getConsulNamespace(k8srefNS),
			}
			consulRoute.Services = append(consulRoute.Services, tcpService)
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
