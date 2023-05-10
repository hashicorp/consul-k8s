package translation

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	"github.com/hashicorp/consul/api"
)

type TranslatorFn func(api.ConfigEntry) []types.NamespacedName

type resourceGetter interface {
	Get(api.ResourceReference) api.ConfigEntry
}

type ConsulToNSNTranslator struct {
	cache resourceGetter
}

func (c ConsulToNSNTranslator) TranslateConsulGateway(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		meta, ok := metaToK8sNamespacedName(config)
		if !ok {
			return nil
		}
		return []types.NamespacedName{meta}
	}
}

func (c ConsulToNSNTranslator) TranslateConsulHTTPRoute(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		route, ok := config.(*api.HTTPRouteConfigEntry)
		if !ok {
			return nil
		}

		return consulRefsToNSN(c.cache, route.Parents)
	}
}

func (c ConsulToNSNTranslator) TranslateConsulTCPRoute(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		route, ok := config.(*api.TCPRouteConfigEntry)
		if !ok {
			return nil
		}

		return consulRefsToNSN(c.cache, route.Parents)
	}
}

func (c ConsulToNSNTranslator) TranslateConsulInlineSecret(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		meta, ok := metaToK8sNamespacedName(config)
		if !ok {
			return nil
		}

		return []types.NamespacedName{meta}
	}
}

func metaToK8sNamespacedName(config api.ConfigEntry) (types.NamespacedName, bool) {
	meta := config.GetMeta()

	namespace, ok := meta[metaKeyKubeNS]
	if !ok {
		return types.NamespacedName{}, false
	}

	name, ok := meta[metaKeyKubeName]
	if !ok {
		return types.NamespacedName{}, false
	}

	return types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, true
}

func consulRefsToNSN(cache resourceGetter, refs []api.ResourceReference) []types.NamespacedName {
	nsnSet := make(map[types.NamespacedName]struct{})

	for _, ref := range refs {
		if parent := cache.Get(ref); parent != nil {
			if k8sNSN, ok := metaToK8sNamespacedName(parent); ok {
				nsnSet[k8sNSN] = struct{}{}
			}
		}
	}
	nsns := make([]types.NamespacedName, 0, len(nsnSet))

	for nsn := range nsnSet {
		nsns = append(nsns, nsn)
	}
	return nsns
}
