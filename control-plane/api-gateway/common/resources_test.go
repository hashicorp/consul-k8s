// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestResourceMap_JWTProvider(t *testing.T) {
	resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))
	require.Empty(t, resourceMap.jwtProviders)
	provider := &v1alpha1.JWTProvider{
		TypeMeta: metav1.TypeMeta{
			Kind: "JWTProvider",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-jwt",
		},
		Spec: v1alpha1.JWTProviderSpec{},
	}

	key := api.ResourceReference{
		Name: provider.Name,
		Kind: "JWTProvider",
	}

	resourceMap.AddJWTProvider(provider)

	require.Len(t, resourceMap.jwtProviders, 1)
	require.NotNil(t, resourceMap.jwtProviders[key])
	require.Equal(t, resourceMap.jwtProviders[key], provider)
}

func TestInheritedTLSSDSClusterForHTTPRoute_OmittedSectionUsesCompatibleListener(t *testing.T) {
	resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))

	resourceMap.ReferenceCountGateway(gwv1.Gateway{
		TypeMeta: metav1.TypeMeta{Kind: KindGateway},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{Listeners: []gwv1.Listener{
			{
				Name:     "foo",
				Protocol: gwv1.HTTPSProtocolType,
				TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
					gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "cluster-foo",
					gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "cert-foo",
				}},
				Hostname: PointerTo(gwv1.Hostname("foo.example.com")),
			},
			{
				Name:     "bar",
				Protocol: gwv1.HTTPSProtocolType,
				TLS: &gwv1.ListenerTLSConfig{Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
					gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "cluster-bar",
					gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "cert-bar",
				}},
				Hostname: PointerTo(gwv1.Hostname("bar.example.com")),
			},
		}},
	})

	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
		Spec: gwv1.HTTPRouteSpec{
			Hostnames: []gwv1.Hostname{"foo.example.com"},
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{
				Name: "gw",
			}}},
		},
	}

	cluster, inherited := resourceMap.InheritedTLSSDSClusterForHTTPRoute(route)
	require.True(t, inherited)
	require.Equal(t, "cluster-foo", cluster)
}

func TestInheritedTLSSDSClusterForHTTPRoute_IgnoresNonTerminateTLSListener(t *testing.T) {
	resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))

	resourceMap.ReferenceCountGateway(gwv1.Gateway{
		TypeMeta: metav1.TypeMeta{Kind: KindGateway},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "default",
		},
		Spec: gwv1.GatewaySpec{Listeners: []gwv1.Listener{
			{
				Name:     "terminate",
				Protocol: gwv1.HTTPSProtocolType,
				TLS: &gwv1.ListenerTLSConfig{
					Mode: PointerTo(gwv1.TLSModeTerminate),
					Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
						gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "cluster-terminate",
						gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "cert-terminate",
					},
				},
				Hostname: PointerTo(gwv1.Hostname("app.example.com")),
			},
			{
				Name:     "passthrough",
				Protocol: gwv1.HTTPSProtocolType,
				TLS: &gwv1.ListenerTLSConfig{
					Mode: PointerTo(gwv1.TLSModePassthrough),
					Options: map[gwv1.AnnotationKey]gwv1.AnnotationValue{
						gwv1.AnnotationKey(TLSSDSClusterNameAnnotationKey):  "cluster-passthrough",
						gwv1.AnnotationKey(TLSSDSCertResourceAnnotationKey): "cert-passthrough",
					},
				},
				Hostname: PointerTo(gwv1.Hostname("app.example.com")),
			},
		}},
	})

	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "route", Namespace: "default"},
		Spec: gwv1.HTTPRouteSpec{
			Hostnames: []gwv1.Hostname{"app.example.com"},
			CommonRouteSpec: gwv1.CommonRouteSpec{ParentRefs: []gwv1.ParentReference{{
				Name: "gw",
			}}},
		},
	}

	cluster, inherited := resourceMap.InheritedTLSSDSClusterForHTTPRoute(route)
	require.True(t, inherited)
	require.Equal(t, "cluster-terminate", cluster)
}

func newRouteExtProc(name, namespace, mode string) *v1alpha1.RouteExtProc {
	return &v1alpha1.RouteExtProc{
		TypeMeta: metav1.TypeMeta{
			Kind: v1alpha1.RouteExtProcKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.RouteExtProcSpec{
			Mode: mode,
		},
	}
}

func TestResourceMap_GetExternalExtProcFilters(t *testing.T) {
	t.Run("returns empty when no external filters exist", func(t *testing.T) {
		resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))
		require.Empty(t, resourceMap.GetExternalExtProcFilters())
	})

	t.Run("returns only ext_proc filters", func(t *testing.T) {
		resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))

		extProc1 := newRouteExtProc("extproc-1", "default", "enabled")
		extProc2 := newRouteExtProc("extproc-2", "default", "override")

		resourceMap.AddExternalFilter(extProc1)
		resourceMap.AddExternalFilter(extProc2)

		// Add a non-ext_proc filter that should be ignored.
		resourceMap.AddExternalFilter(&v1alpha1.RouteAuthFilter{
			TypeMeta: metav1.TypeMeta{
				Kind: v1alpha1.RouteAuthFilterKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "auth-filter",
				Namespace: "default",
			},
		})

		filters := resourceMap.GetExternalExtProcFilters()
		require.Len(t, filters, 2)
		require.ElementsMatch(t, []*v1alpha1.RouteExtProc{extProc1, extProc2}, filters)
	})

	t.Run("returns empty when only non-ext_proc filters exist", func(t *testing.T) {
		resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))

		resourceMap.AddExternalFilter(&v1alpha1.RouteAuthFilter{
			TypeMeta: metav1.TypeMeta{
				Kind: v1alpha1.RouteAuthFilterKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "auth-filter",
				Namespace: "default",
			},
		})

		require.Empty(t, resourceMap.GetExternalExtProcFilters())
	})
}

func TestResourceMap_ExternalFilter_ExtProc(t *testing.T) {
	resourceMap := NewResourceMap(ResourceTranslator{}, mockReferenceValidator{}, logrtest.New(t))

	extProc := newRouteExtProc("extproc-1", "default", "enabled")
	resourceMap.AddExternalFilter(extProc)

	filterRef := gwv1.LocalObjectReference{
		Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  v1alpha1.RouteExtProcKind,
		Name:  "extproc-1",
	}

	t.Run("GetExternalFilter finds the ext_proc filter", func(t *testing.T) {
		filter, ok := resourceMap.GetExternalFilter(filterRef, "default")
		require.True(t, ok)
		require.Equal(t, extProc, filter)
	})

	t.Run("ExternalFilterExists returns true for the ext_proc filter", func(t *testing.T) {
		require.True(t, resourceMap.ExternalFilterExists(filterRef, "default"))
	})

	t.Run("ExternalFilterExists returns false for a different namespace", func(t *testing.T) {
		require.False(t, resourceMap.ExternalFilterExists(filterRef, "other"))
	})

	t.Run("ExternalFilterExists returns false for an unknown filter", func(t *testing.T) {
		unknownRef := gwv1.LocalObjectReference{
			Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
			Kind:  v1alpha1.RouteExtProcKind,
			Name:  "does-not-exist",
		}
		require.False(t, resourceMap.ExternalFilterExists(unknownRef, "default"))
	})
}

type mockReferenceValidator struct{}

func (m mockReferenceValidator) GatewayCanReferenceSecret(gateway gwv1.Gateway, secretRef gwv1.SecretObjectReference) bool {
	return true
}

func (m mockReferenceValidator) HTTPRouteCanReferenceBackend(httproute gwv1.HTTPRoute, backendRef gwv1.BackendRef) bool {
	return true
}

func (m mockReferenceValidator) TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1.BackendRef) bool {
	return true
}
