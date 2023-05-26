// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package translation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul/api"
)

type TranslatorFn func(api.ConfigEntry) []types.NamespacedName

type secretTransfomer func(context.Context) func(client.Object) []reconcile.Request

type resourceGetter interface {
	Get(api.ResourceReference) api.ConfigEntry
}

// ConsulToNamespaceNameTranslator handles translating consul config entries to k8s namespaced names.
type ConsulToNamespaceNameTranslator struct {
	cache resourceGetter
}

// NewConsulToNamespaceNameTranslator creates an instance of the ConsulToNSNTranslator.
func NewConsulToNamespaceNameTranslator(cache resourceGetter) ConsulToNamespaceNameTranslator {
	return ConsulToNamespaceNameTranslator{cache: cache}
}

// BuildConsulGatewayTranslator creates a slice k8s types.NamespacedName from the meta fields of the api gateway config entry.
func (c ConsulToNamespaceNameTranslator) BuildConsulGatewayTranslator(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		meta, ok := metaToK8sNamespacedName(config)
		if !ok {
			return nil
		}

		return []types.NamespacedName{meta}
	}
}

// BuildConsulHTTPRouteTranslator creates a slice of k8s types.NamespacedName from the meta fields of the http route parent refs.
func (c ConsulToNamespaceNameTranslator) BuildConsulHTTPRouteTranslator(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		route, ok := config.(*api.HTTPRouteConfigEntry)
		if !ok {
			return nil
		}

		return consulRefsToNSN(c.cache, route.Parents)
	}
}

// BuildConsulTCPRouteTranslator creates a slice of k8s types.NamespacedName from the meta fields of the tcp route parent refs.
func (c ConsulToNamespaceNameTranslator) BuildConsulTCPRouteTranslator(ctx context.Context) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		route, ok := config.(*api.TCPRouteConfigEntry)
		if !ok {
			return nil
		}

		return consulRefsToNSN(c.cache, route.Parents)
	}
}

// BuildConsulInlineCertificateTranslator creates a slice of k8s types.NamespacedName from the meta fields of the secret. It does this
// by using a secret transformer function to get a list of reconcile requests from k8s for the given secret and then converts
// those requests to the slice of NamespaceName.
func (c ConsulToNamespaceNameTranslator) BuildConsulInlineCertificateTranslator(ctx context.Context, secretTransformer secretTransfomer) TranslatorFn {
	return func(config api.ConfigEntry) []types.NamespacedName {
		meta, ok := metaToK8sNamespacedName(config)
		if !ok {
			return nil
		}

		return requestsToRefs(secretTransformer(ctx)(&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      meta.Name,
				Namespace: meta.Namespace,
			},
		}))
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

func requestsToRefs(objects []reconcile.Request) []types.NamespacedName {
	var refs []types.NamespacedName
	for _, object := range objects {
		refs = append(refs, object.NamespacedName)
	}
	return refs
}
