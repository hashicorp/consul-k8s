// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
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
func (t ResourceTranslator) ToAPIGateway(gateway gwv1beta1.Gateway, resources *ResourceMap) *api.APIGatewayConfigEntry {
	namespace := t.Namespace(gateway.Namespace)

	listeners := ConvertSliceFuncIf(gateway.Spec.Listeners, func(listener gwv1beta1.Listener) (api.APIGatewayListener, bool) {
		return t.toAPIGatewayListener(gateway, listener, resources)
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

func (t ResourceTranslator) toAPIGatewayListener(gateway gwv1beta1.Gateway, listener gwv1beta1.Listener, resources *ResourceMap) (api.APIGatewayListener, bool) {
	namespace := gateway.Namespace

	var certificates []api.ResourceReference

	if listener.TLS != nil {
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
				certificates = append(certificates, t.NonNormalizedConfigEntryReference(api.InlineCertificate, ref))
			}
		}
	}

	return api.APIGatewayListener{
		Name:     string(listener.Name),
		Hostname: DerefStringOr(listener.Hostname, ""),
		Port:     int(listener.Port),
		Protocol: listenerProtocolMap[strings.ToLower(string(listener.Protocol))],
		TLS: api.APIGatewayTLSConfiguration{
			Certificates: certificates,
		},
	}, true
}

func (t ResourceTranslator) ToHTTPRoute(route gwv1beta1.HTTPRoute, resources *ResourceMap) *api.HTTPRouteConfigEntry {
	namespace := t.Namespace(route.Namespace)

	// we don't translate parent refs

	hostnames := StringLikeSlice(route.Spec.Hostnames)
	rules := ConvertSliceFuncIf(route.Spec.Rules, func(rule gwv1beta1.HTTPRouteRule) (api.HTTPRouteRule, bool) {
		return t.translateHTTPRouteRule(route, rule, resources)
	})

	return &api.HTTPRouteConfigEntry{
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
}

func (t ResourceTranslator) translateHTTPRouteRule(route gwv1beta1.HTTPRoute, rule gwv1beta1.HTTPRouteRule, resources *ResourceMap) (api.HTTPRouteRule, bool) {
	services := ConvertSliceFuncIf(rule.BackendRefs, func(ref gwv1beta1.HTTPBackendRef) (api.HTTPService, bool) {
		return t.translateHTTPBackendRef(route, ref, resources)
	})

	if len(services) == 0 {
		return api.HTTPRouteRule{}, false
	}

	matches := ConvertSliceFunc(rule.Matches, t.translateHTTPMatch)
	filters := t.translateHTTPFilters(rule.Filters)

	return api.HTTPRouteRule{
		Services: services,
		Matches:  matches,
		Filters:  filters,
	}, true
}

func (t ResourceTranslator) translateHTTPBackendRef(route gwv1beta1.HTTPRoute, ref gwv1beta1.HTTPBackendRef, resources *ResourceMap) (api.HTTPService, bool) {
	id := types.NamespacedName{
		Name:      string(ref.Name),
		Namespace: DerefStringOr(ref.Namespace, route.Namespace),
	}

	isServiceRef := NilOrEqual(ref.Group, "") && NilOrEqual(ref.Kind, "Service")

	if isServiceRef && resources.HasService(id) && resources.HTTPRouteCanReferenceBackend(route, ref.BackendRef) {
		filters := t.translateHTTPFilters(ref.Filters)
		service := resources.Service(id)

		return api.HTTPService{
			Name:      service.Name,
			Namespace: service.Namespace,
			Partition: t.ConsulPartition,
			Filters:   filters,
			Weight:    DerefIntOr(ref.Weight, 1),
		}, true
	}

	isMeshServiceRef := DerefEqual(ref.Group, v1alpha1.ConsulHashicorpGroup) && DerefEqual(ref.Kind, v1alpha1.MeshServiceKind)
	if isMeshServiceRef && resources.HasMeshService(id) && resources.HTTPRouteCanReferenceBackend(route, ref.BackendRef) {
		filters := t.translateHTTPFilters(ref.Filters)
		service := resources.MeshService(id)

		return api.HTTPService{
			Name:      service.Name,
			Namespace: service.Namespace,
			Partition: t.ConsulPartition,
			Filters:   filters,
			Weight:    DerefIntOr(ref.Weight, 1),
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

func (t ResourceTranslator) translateHTTPFilters(filters []gwv1beta1.HTTPRouteFilter) api.HTTPFilters {
	var urlRewrite *api.URLRewrite
	consulFilter := api.HTTPHeaderFilter{
		Add: make(map[string]string),
		Set: make(map[string]string),
	}

	for _, filter := range filters {
		if filter.RequestHeaderModifier != nil {
			consulFilter.Remove = append(consulFilter.Remove, filter.RequestHeaderModifier.Remove...)

			for _, toAdd := range filter.RequestHeaderModifier.Add {
				consulFilter.Add[string(toAdd.Name)] = toAdd.Value
			}

			for _, toSet := range filter.RequestHeaderModifier.Set {
				consulFilter.Set[string(toSet.Name)] = toSet.Value
			}
		}

		// we drop any path rewrites that are not prefix matches as we don't support those
		if filter.URLRewrite != nil &&
			filter.URLRewrite.Path != nil &&
			filter.URLRewrite.Path.Type == gwv1beta1.PrefixMatchHTTPPathModifier {
			urlRewrite = &api.URLRewrite{Path: DerefStringOr(filter.URLRewrite.Path.ReplacePrefixMatch, "")}
		}
	}
	return api.HTTPFilters{
		Headers:    []api.HTTPHeaderFilter{consulFilter},
		URLRewrite: urlRewrite,
	}
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

func (t ResourceTranslator) ToInlineCertificate(secret corev1.Secret) (*api.InlineCertificateConfigEntry, error) {
	certificate, privateKey, err := ParseCertificateData(secret)
	if err != nil {
		return nil, err
	}

	err = ValidateKeyLength(privateKey)
	if err != nil {
		return nil, err
	}

	namespace := t.Namespace(secret.Namespace)

	return &api.InlineCertificateConfigEntry{
		Kind:        api.InlineCertificate,
		Name:        secret.Name,
		Namespace:   namespace,
		Partition:   t.ConsulPartition,
		Certificate: strings.TrimSpace(certificate),
		PrivateKey:  strings.TrimSpace(privateKey),
		Meta: t.addDatacenterToMeta(map[string]string{
			constants.MetaKeyKubeNS:   secret.Namespace,
			constants.MetaKeyKubeName: secret.Name,
		}),
	}, nil
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
