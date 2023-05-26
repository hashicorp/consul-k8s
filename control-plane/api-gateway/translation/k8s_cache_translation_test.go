// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package translation

import (
	"context"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul/api"
)

func Test_ConsulToNamespaceNameTranslator_TranslateConsulGateway(t *testing.T) {
	t.Parallel()
	type args struct {
		config *api.APIGatewayConfigEntry
	}
	tests := []struct {
		name string
		args args
		want []types.NamespacedName
	}{
		{
			name: "when name and namespace are set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeNS:   "my-ns",
						metaKeyKubeName: "api-gw-name",
					},
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "my-ns",
					Name:      "api-gw-name",
				},
			},
		},
		{
			name: "when name is not set and namespace is set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeNS: "my-ns",
					},
				},
			},
			want: nil,
		},
		{
			name: "when name is set and namespace is not set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{
						metaKeyKubeName: "api-gw-name",
					},
				},
			},
			want: nil,
		},
		{
			name: "when both name and namespace are not set",
			args: args{
				config: &api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "api-gw",
					Meta: map[string]string{},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			translator := ConsulToNamespaceNameTranslator{}
			fn := translator.BuildConsulGatewayTranslator(context.Background())
			got := fn(tt.args.config)
			if diff := cmp.Diff(got, tt.want, sortTransformer()); diff != "" {
				t.Errorf("ConsulToNSNTranslator.TranslateConsulGateway() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConsulToNamespaceNameTranslator_TranslateConsulHTTPRoute(t *testing.T) {
	t.Parallel()
	type fields struct {
		cache resourceGetter
	}
	tests := []struct {
		name       string
		fields     fields
		parentRefs []api.ResourceReference
		want       []types.NamespacedName
	}{
		{
			name: "all refs in cache",
			fields: fields{
				cache: buildMockCache(map[api.ResourceReference]api.ConfigEntry{
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-1",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-1",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-1",
						},
						Namespace: "ns",
					}),
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-2",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-2",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-2",
						},
						Namespace: "ns",
					}),
				}),
			},
			parentRefs: []api.ResourceReference{
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-1",
					Namespace: "ns",
				},

				{
					Kind:      api.APIGateway,
					Name:      "api-gw-2",
					Namespace: "ns",
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "ns",
					Name:      "api-gw-1",
				},
				{
					Namespace: "ns",
					Name:      "api-gw-2",
				},
			},
		},
		{
			name: "some refs not in cache",
			fields: fields{
				cache: buildMockCache(map[api.ResourceReference]api.ConfigEntry{
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-1",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-1",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-1",
						},
						Namespace: "ns",
					}),
				}),
			},
			parentRefs: []api.ResourceReference{
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-1",
					Namespace: "ns",
				},
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-2",
					Namespace: "ns",
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "ns",
					Name:      "api-gw-1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ConsulToNamespaceNameTranslator{
				cache: tt.fields.cache,
			}
			config := &api.HTTPRouteConfigEntry{
				Parents: tt.parentRefs,
			}
			got := c.BuildConsulHTTPRouteTranslator(context.Background())(config)
			if diff := cmp.Diff(got, tt.want, sortTransformer()); diff != "" {
				t.Errorf("ConsulToNSNTranslator.TranslateConsulHTTPRoute() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConsulToNamespaceNameTranslator_TranslateConsulTCPRoute(t *testing.T) {
	t.Parallel()
	type fields struct {
		cache resourceGetter
	}
	tests := []struct {
		name       string
		fields     fields
		parentRefs []api.ResourceReference
		want       []types.NamespacedName
	}{
		{
			name: "all refs in cache",
			fields: fields{
				cache: buildMockCache(map[api.ResourceReference]api.ConfigEntry{
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-1",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-1",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-1",
						},
						Namespace: "ns",
					}),
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-2",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-2",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-2",
						},
						Namespace: "ns",
					}),
				}),
			},
			parentRefs: []api.ResourceReference{
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-1",
					Namespace: "ns",
				},

				{
					Kind:      api.APIGateway,
					Name:      "api-gw-2",
					Namespace: "ns",
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "ns",
					Name:      "api-gw-1",
				},
				{
					Namespace: "ns",
					Name:      "api-gw-2",
				},
			},
		},
		{
			name: "some refs not in cache",
			fields: fields{
				cache: buildMockCache(map[api.ResourceReference]api.ConfigEntry{
					{
						Kind:      api.APIGateway,
						Name:      "api-gw-1",
						Namespace: "ns",
					}: api.ConfigEntry(&api.APIGatewayConfigEntry{
						Kind: api.APIGateway,
						Name: "api-gw-1",
						Meta: map[string]string{
							metaKeyKubeNS:   "ns",
							metaKeyKubeName: "api-gw-1",
						},
						Namespace: "ns",
					}),
				}),
			},
			parentRefs: []api.ResourceReference{
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-1",
					Namespace: "ns",
				},
				{
					Kind:      api.APIGateway,
					Name:      "api-gw-2",
					Namespace: "ns",
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "ns",
					Name:      "api-gw-1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ConsulToNamespaceNameTranslator{
				cache: tt.fields.cache,
			}
			config := &api.TCPRouteConfigEntry{
				Parents: tt.parentRefs,
			}
			got := c.BuildConsulTCPRouteTranslator(context.Background())(config)
			if diff := cmp.Diff(got, tt.want, sortTransformer()); diff != "" {
				t.Errorf("ConsulToNSNTranslator.TranslateConsulTCPRoute() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_ConsulToNamespaceNameTranslator_TranslateInlineCertificate(t *testing.T) {
	t.Parallel()
	type args struct {
		config *api.InlineCertificateConfigEntry
	}
	tests := []struct {
		name string
		args args
		want []types.NamespacedName
	}{
		{
			name: "when name and namespace are set",
			args: args{
				config: &api.InlineCertificateConfigEntry{
					Kind: api.InlineCertificate,
					Name: "secret",
					Meta: map[string]string{
						metaKeyKubeNS:   "my-ns",
						metaKeyKubeName: "secret",
					},
				},
			},
			want: []types.NamespacedName{
				{
					Namespace: "my-ns",
					Name:      "secret",
				},
			},
		},
		{
			name: "when name is not set and namespace is set",
			args: args{
				config: &api.InlineCertificateConfigEntry{
					Kind: api.InlineCertificate,
					Name: "secret",
					Meta: map[string]string{
						metaKeyKubeNS: "my-ns",
					},
				},
			},
			want: nil,
		},
		{
			name: "when name is set and namespace is not set",
			args: args{
				config: &api.InlineCertificateConfigEntry{
					Kind: api.APIGateway,
					Name: "secret",
					Meta: map[string]string{
						metaKeyKubeName: "secret",
					},
				},
			},
			want: nil,
		},
		{
			name: "when both name and namespace are not set",
			args: args{
				config: &api.InlineCertificateConfigEntry{
					Kind: api.InlineCertificate,
					Name: "secret",
					Meta: map[string]string{},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			transformer := func(ctx context.Context) func(client.Object) []reconcile.Request {
				return func(o client.Object) []reconcile.Request {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()},
						},
					}
				}
			}

			translator := ConsulToNamespaceNameTranslator{}
			fn := translator.BuildConsulInlineCertificateTranslator(context.Background(), transformer)
			got := fn(tt.args.config)
			if diff := cmp.Diff(got, tt.want, sortTransformer()); diff != "" {
				t.Errorf("ConsulToNSNTranslator.TranslateConsulInlineCertificate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func sortTransformer() cmp.Option {
	return cmp.Transformer("Sort", func(in []types.NamespacedName) []types.NamespacedName {
		sort.Slice(in, func(i int, j int) bool {
			return in[i].Name < in[j].Name
		})
		return in
	})
}

type mockCache struct {
	c map[api.ResourceReference]api.ConfigEntry
}

func (m mockCache) Get(ref api.ResourceReference) api.ConfigEntry {
	val, ok := m.c[ref]
	if !ok {
		return nil
	}
	return val
}

func buildMockCache(c map[api.ResourceReference]api.ConfigEntry) mockCache {
	return mockCache{c: c}
}
