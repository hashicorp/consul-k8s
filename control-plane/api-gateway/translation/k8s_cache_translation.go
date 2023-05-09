package translation

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	"github.com/hashicorp/consul/api"
)

type TranslatorFn func(api.ConfigEntry) []types.NamespacedName

func TranslateConsulGateway(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		meta, ok := metaToK8sMeta(config)
		if !ok {
			return nil
		}
		return []types.NamespacedName{meta}
	}
}

func metaToK8sMeta(config api.ConfigEntry) (types.NamespacedName, bool) {
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
