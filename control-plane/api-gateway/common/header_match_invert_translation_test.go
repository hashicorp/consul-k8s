// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	logrtest "github.com/go-logr/logr/testing"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeInvertFilter(name, namespace string, headerNames []string) *v1alpha1.RouteHeaderMatchInvertFilter {
	return &v1alpha1.RouteHeaderMatchInvertFilter{
		TypeMeta: metav1.TypeMeta{
			// Kind must match what ExtensionRef.Kind carries so the ResourceMap
			// key lookup (which uses GVK.Kind) resolves correctly.
			Kind: v1alpha1.RouteHeaderMatchInvertFilterKind,
		},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       v1alpha1.RouteHeaderMatchInvertFilterSpec{HeaderNames: headerNames},
	}
}

func makeExtRef(kind, name string) gwv1.LocalObjectReference {
	return gwv1.LocalObjectReference{Kind: gwv1.Kind(kind), Name: gwv1.ObjectName(name)}
}

func invertExtRefFilter(name string) gwv1.HTTPRouteFilter {
	return gwv1.HTTPRouteFilter{
		Type: gwv1.HTTPRouteFilterExtensionRef,
		ExtensionRef: &gwv1.LocalObjectReference{
			Kind: gwv1.Kind(v1alpha1.RouteHeaderMatchInvertFilterKind),
			Name: gwv1.ObjectName(name),
		},
	}
}

func defaultTranslator() ResourceTranslator {
	return ResourceTranslator{EnableConsulNamespaces: true, EnableK8sMirroring: true}
}

// ---------------------------------------------------------------------------
// TestInvertedHeaderNamesForRule
// ---------------------------------------------------------------------------

func TestInvertedHeaderNamesForRule(t *testing.T) {
	t.Parallel()

	tr := defaultTranslator()
	const ns = "default"

	t.Run("no filters returns nil", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		got := tr.invertedHeaderNamesForRule(nil, resources, ns)
		require.Nil(t, got)
	})

	t.Run("non-ExtensionRef filters are ignored", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		filters := []gwv1.HTTPRouteFilter{
			{Type: gwv1.HTTPRouteFilterRequestHeaderModifier},
		}
		got := tr.invertedHeaderNamesForRule(filters, resources, ns)
		require.Nil(t, got)
	})

	t.Run("ExtensionRef that is not in the resource map returns nil", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		filters := []gwv1.HTTPRouteFilter{invertExtRefFilter("missing-filter")}
		got := tr.invertedHeaderNamesForRule(filters, resources, ns)
		require.Nil(t, got)
	})

	t.Run("wrong kind ExtensionRef is ignored", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		filter := gwv1.HTTPRouteFilter{
			Type: gwv1.HTTPRouteFilterExtensionRef,
			ExtensionRef: &gwv1.LocalObjectReference{
				Kind: gwv1.Kind("RouteRetryFilter"),
				Name: "some-retry",
			},
		}
		got := tr.invertedHeaderNamesForRule([]gwv1.HTTPRouteFilter{filter}, resources, ns)
		require.Nil(t, got)
	})

	t.Run("single header name is lowercased in the returned set", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		resources.AddExternalFilter(makeInvertFilter("invert-canary", ns, []string{"X-Canary"}))

		got := tr.invertedHeaderNamesForRule(
			[]gwv1.HTTPRouteFilter{invertExtRefFilter("invert-canary")},
			resources, ns,
		)
		require.NotNil(t, got)
		_, ok := got["x-canary"]
		require.True(t, ok, "expected lowercase key 'x-canary' in set")
		_, wrongCase := got["X-Canary"]
		require.False(t, wrongCase)
	})

	t.Run("multiple header names are all lowercased", func(t *testing.T) {
		resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
		resources.AddExternalFilter(makeInvertFilter("invert-multi", ns, []string{"X-Canary", "X-Version", "Accept-Encoding"}))

		got := tr.invertedHeaderNamesForRule(
			[]gwv1.HTTPRouteFilter{invertExtRefFilter("invert-multi")},
			resources, ns,
		)
		require.NotNil(t, got)
		for _, name := range []string{"x-canary", "x-version", "accept-encoding"} {
			_, ok := got[name]
			require.True(t, ok, "expected key %q", name)
		}
	})
}

// ---------------------------------------------------------------------------
// TestTranslateHTTPHeaderMatchWithInvert
// ---------------------------------------------------------------------------

func TestTranslateHTTPHeaderMatchWithInvert(t *testing.T) {
	t.Parallel()

	tr := defaultTranslator()

	t.Run("nil invertedHeaders never inverts", func(t *testing.T) {
		match := gwv1.HTTPHeaderMatch{
			Name:  "x-canary",
			Value: "true",
			Type:  PointerTo(gwv1.HeaderMatchExact),
		}
		got := tr.translateHTTPHeaderMatchWithInvert(match, nil)
		require.False(t, got.Invert)
		require.Equal(t, "x-canary", got.Name)
		require.Equal(t, api.HTTPHeaderMatchExact, got.Match)
	})

	t.Run("header name in set → Invert true (case-insensitive)", func(t *testing.T) {
		invertSet := map[string]struct{}{"x-canary": {}}

		// CRD stores any casing; match.Name arrives from Gateway API
		for _, headerName := range []string{"x-canary", "X-Canary", "X-CANARY"} {
			t.Run(headerName, func(t *testing.T) {
				match := gwv1.HTTPHeaderMatch{
					Name: gwv1.HTTPHeaderName(headerName),
					Type: PointerTo(gwv1.HeaderMatchExact),
				}
				got := tr.translateHTTPHeaderMatchWithInvert(match, invertSet)
				require.True(t, got.Invert, "expected Invert=true for header %q", headerName)
			})
		}
	})

	t.Run("header name NOT in set → Invert false", func(t *testing.T) {
		invertSet := map[string]struct{}{"x-canary": {}}
		match := gwv1.HTTPHeaderMatch{
			Name: "x-version",
			Type: PointerTo(gwv1.HeaderMatchExact),
		}
		got := tr.translateHTTPHeaderMatchWithInvert(match, invertSet)
		require.False(t, got.Invert)
	})

	t.Run("all five match types propagate correctly", func(t *testing.T) {
		invertSet := map[string]struct{}{"x-header": {}}

		cases := []struct {
			gwType       *gwv1.HeaderMatchType
			expectedType api.HTTPHeaderMatchType
		}{
			{PointerTo(gwv1.HeaderMatchExact), api.HTTPHeaderMatchExact},
			{PointerTo(gwv1.HeaderMatchRegularExpression), api.HTTPHeaderMatchRegularExpression},
			// nil type → DerefLookup returns zero value of api.HTTPHeaderMatchType ("")
			{nil, api.HTTPHeaderMatchType("")},
		}
		for _, c := range cases {
			match := gwv1.HTTPHeaderMatch{Name: "x-header", Type: c.gwType}
			got := tr.translateHTTPHeaderMatchWithInvert(match, invertSet)
			require.True(t, got.Invert)
			require.Equal(t, c.expectedType, got.Match)
		}
	})
}

// ---------------------------------------------------------------------------
// TestToHTTPRoute_WithInvertFilter — end-to-end translation
// ---------------------------------------------------------------------------

func TestToHTTPRoute_WithInvertFilter(t *testing.T) {
	t.Parallel()

	const routeNS = "default"
	svcKey := types.NamespacedName{Name: "stable-backend", Namespace: routeNS}

	tr := defaultTranslator()

	// Build a k8s HTTPRoute with one rule that:
	//   match: x-canary present
	//   filter: RouteHeaderMatchInvertFilter { headerNames: ["x-canary"] }
	//   backend: stable-backend
	headerMatchType := gwv1.HeaderMatchExact
	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "my-route", Namespace: routeNS},
		Spec: gwv1.HTTPRouteSpec{
			Rules: []gwv1.HTTPRouteRule{
				{
					Matches: []gwv1.HTTPRouteMatch{
						{
							Headers: []gwv1.HTTPHeaderMatch{
								{
									Name:  "x-canary",
									Value: "true",
									Type:  &headerMatchType,
								},
							},
						},
					},
					Filters: []gwv1.HTTPRouteFilter{
						invertExtRefFilter("invert-x-canary"),
					},
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name:      gwv1.ObjectName(svcKey.Name),
									Namespace: PointerTo(gwv1.Namespace(svcKey.Namespace)),
								},
							},
						},
					},
				},
			},
		},
	}

	resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resources.AddService(svcKey, svcKey.Name)
	resources.AddExternalFilter(makeInvertFilter("invert-x-canary", routeNS, []string{"x-canary"}))

	got := tr.ToHTTPRoute(route, resources)

	want := &api.HTTPRouteConfigEntry{
		Kind:      api.HTTPRoute,
		Name:      "my-route",
		Namespace: routeNS,
		Hostnames: []string{},
		Meta: map[string]string{
			constants.MetaKeyKubeNS:   routeNS,
			constants.MetaKeyKubeName: "my-route",
		},
		Rules: []api.HTTPRouteRule{
			{
				Matches: []api.HTTPMatch{
					{
						Headers: []api.HTTPHeaderMatch{
							{
								Name:   "x-canary",
								Value:  "true",
								Match:  api.HTTPHeaderMatchExact,
								Invert: true, // ← key assertion
							},
						},
						Query: []api.HTTPQueryMatch{},
					},
				},
				Services: []api.HTTPService{
					{
						Name:      svcKey.Name,
						Namespace: svcKey.Namespace,
						Weight:    1,
						Filters:   api.HTTPFilters{Headers: []api.HTTPHeaderFilter{}},
						ResponseFilters: api.HTTPResponseFilters{
							Headers: []api.HTTPHeaderFilter{},
						},
					},
				},
				Filters:         api.HTTPFilters{Headers: []api.HTTPHeaderFilter{}},
				ResponseFilters: api.HTTPResponseFilters{Headers: []api.HTTPHeaderFilter{}},
			},
		},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ToHTTPRoute() with invert filter mismatch (-want +got):\n%s", diff)
	}
}

// TestToHTTPRoute_InvertFilterAbsent verifies that without the filter,
// Invert stays false — so the filter is truly opt-in.
func TestToHTTPRoute_InvertFilterAbsent(t *testing.T) {
	t.Parallel()

	const routeNS = "default"
	svcKey := types.NamespacedName{Name: "canary-backend", Namespace: routeNS}
	tr := defaultTranslator()

	headerMatchType := gwv1.HeaderMatchExact
	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "plain-route", Namespace: routeNS},
		Spec: gwv1.HTTPRouteSpec{
			Rules: []gwv1.HTTPRouteRule{
				{
					Matches: []gwv1.HTTPRouteMatch{
						{
							Headers: []gwv1.HTTPHeaderMatch{
								{Name: "x-canary", Value: "true", Type: &headerMatchType},
							},
						},
					},
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name:      gwv1.ObjectName(svcKey.Name),
									Namespace: PointerTo(gwv1.Namespace(svcKey.Namespace)),
								},
							},
						},
					},
				},
			},
		},
	}

	resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resources.AddService(svcKey, svcKey.Name)

	got := tr.ToHTTPRoute(route, resources)
	require.Len(t, got.Rules, 1)
	require.Len(t, got.Rules[0].Matches, 1)
	require.Len(t, got.Rules[0].Matches[0].Headers, 1)
	require.False(t, got.Rules[0].Matches[0].Headers[0].Invert, "Invert must be false when no filter is present")
}

// TestToHTTPRoute_MultipleHeadersPartialInvert verifies that only the listed
// header names are inverted — other headers in the same rule are left alone.
func TestToHTTPRoute_MultipleHeadersPartialInvert(t *testing.T) {
	t.Parallel()

	const routeNS = "default"
	svcKey := types.NamespacedName{Name: "svc", Namespace: routeNS}
	tr := defaultTranslator()

	headerMatchType := gwv1.HeaderMatchExact
	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-route", Namespace: routeNS},
		Spec: gwv1.HTTPRouteSpec{
			Rules: []gwv1.HTTPRouteRule{
				{
					Matches: []gwv1.HTTPRouteMatch{
						{
							Headers: []gwv1.HTTPHeaderMatch{
								{Name: "x-canary", Value: "v1", Type: &headerMatchType},  // inverted
								{Name: "x-version", Value: "v2", Type: &headerMatchType}, // not inverted
							},
						},
					},
					Filters: []gwv1.HTTPRouteFilter{
						invertExtRefFilter("invert-only-canary"),
					},
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name:      gwv1.ObjectName(svcKey.Name),
									Namespace: PointerTo(gwv1.Namespace(svcKey.Namespace)),
								},
							},
						},
					},
				},
			},
		},
	}

	resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resources.AddService(svcKey, svcKey.Name)
	// Only "x-canary" is in the invert list
	resources.AddExternalFilter(makeInvertFilter("invert-only-canary", routeNS, []string{"x-canary"}))

	got := tr.ToHTTPRoute(route, resources)
	headers := got.Rules[0].Matches[0].Headers

	require.Len(t, headers, 2)
	require.Equal(t, "x-canary", headers[0].Name)
	require.True(t, headers[0].Invert, "x-canary should be inverted")
	require.Equal(t, "x-version", headers[1].Name)
	require.False(t, headers[1].Invert, "x-version should NOT be inverted")
}

// TestToHTTPRoute_Day2UpdateInvertFilter verifies that changing which headers
// are inverted (day-2 update) produces a different config entry — the diff
// comparator must detect the change so a Consul write is triggered.
func TestToHTTPRoute_Day2UpdateInvertFilter(t *testing.T) {
	t.Parallel()

	const routeNS = "default"
	svcKey := types.NamespacedName{Name: "svc", Namespace: routeNS}
	tr := defaultTranslator()

	headerMatchType := gwv1.HeaderMatchExact
	route := gwv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "day2-route", Namespace: routeNS},
		Spec: gwv1.HTTPRouteSpec{
			Rules: []gwv1.HTTPRouteRule{
				{
					Matches: []gwv1.HTTPRouteMatch{
						{
							Headers: []gwv1.HTTPHeaderMatch{
								{Name: "x-canary", Value: "v1", Type: &headerMatchType},
							},
						},
					},
					Filters: []gwv1.HTTPRouteFilter{
						invertExtRefFilter("invert-filter"),
					},
					BackendRefs: []gwv1.HTTPBackendRef{
						{
							BackendRef: gwv1.BackendRef{
								BackendObjectReference: gwv1.BackendObjectReference{
									Name:      gwv1.ObjectName(svcKey.Name),
									Namespace: PointerTo(gwv1.Namespace(svcKey.Namespace)),
								},
							},
						},
					},
				},
			},
		},
	}

	// --- Day 1: no invert ---
	resourcesDay1 := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resourcesDay1.AddService(svcKey, svcKey.Name)
	resourcesDay1.AddExternalFilter(makeInvertFilter("invert-filter", routeNS, []string{})) // empty = no-op

	entryDay1 := tr.ToHTTPRoute(route, resourcesDay1)
	require.False(t, entryDay1.Rules[0].Matches[0].Headers[0].Invert, "day1: no invert expected")

	// --- Day 2: operator adds x-canary to the invert list ---
	resourcesDay2 := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resourcesDay2.AddService(svcKey, svcKey.Name)
	resourcesDay2.AddExternalFilter(makeInvertFilter("invert-filter", routeNS, []string{"x-canary"}))

	entryDay2 := tr.ToHTTPRoute(route, resourcesDay2)
	require.True(t, entryDay2.Rules[0].Matches[0].Headers[0].Invert, "day2: invert must be true")

	// The diff comparator must detect the change and return false (not equal).
	require.False(t, EntriesEqual(entryDay1, entryDay2),
		"EntriesEqual must return false after adding invert so Consul write is triggered")

	// --- Day 3: operator removes the invert again ---
	resourcesDay3 := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
	resourcesDay3.AddService(svcKey, svcKey.Name)
	resourcesDay3.AddExternalFilter(makeInvertFilter("invert-filter", routeNS, []string{}))

	entryDay3 := tr.ToHTTPRoute(route, resourcesDay3)
	require.False(t, entryDay3.Rules[0].Matches[0].Headers[0].Invert, "day3: invert removed")

	require.False(t, EntriesEqual(entryDay2, entryDay3),
		"EntriesEqual must return false after removing invert so Consul write is triggered")

	// Day 1 and Day 3 are structurally identical — must be equal.
	require.True(t, EntriesEqual(entryDay1, entryDay3),
		"EntriesEqual must return true when invert is the same on both sides")
}
