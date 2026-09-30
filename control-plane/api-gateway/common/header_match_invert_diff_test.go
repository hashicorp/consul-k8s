// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// httpHeaderMatchesEqual — direct comparator tests
// ---------------------------------------------------------------------------

func TestEntryComparator_HTTPHeaderMatchesEqual_Invert(t *testing.T) {
	t.Parallel()

	comp := entryComparator{}

	base := api.HTTPHeaderMatch{
		Name:  "x-canary",
		Value: "v1",
		Match: api.HTTPHeaderMatchExact,
	}

	t.Run("identical structs are equal", func(t *testing.T) {
		require.True(t, comp.httpHeaderMatchesEqual(base, base))
	})

	t.Run("invert false vs false is equal", func(t *testing.T) {
		a := base
		b := base
		a.Invert = false
		b.Invert = false
		require.True(t, comp.httpHeaderMatchesEqual(a, b))
	})

	t.Run("invert true vs true is equal", func(t *testing.T) {
		a := base
		b := base
		a.Invert = true
		b.Invert = true
		require.True(t, comp.httpHeaderMatchesEqual(a, b))
	})

	t.Run("invert false vs true is NOT equal — triggers day-2 Consul write", func(t *testing.T) {
		withInvert := base
		withInvert.Invert = true
		require.False(t, comp.httpHeaderMatchesEqual(base, withInvert),
			"adding Invert=true must be detected as a diff")
	})

	t.Run("invert true vs false is NOT equal — triggers day-2 Consul write", func(t *testing.T) {
		withInvert := base
		withInvert.Invert = true
		require.False(t, comp.httpHeaderMatchesEqual(withInvert, base),
			"removing Invert must be detected as a diff")
	})

	t.Run("name change is still detected independently of invert", func(t *testing.T) {
		other := base
		other.Name = "x-other"
		require.False(t, comp.httpHeaderMatchesEqual(base, other))
	})

	t.Run("value change is still detected independently of invert", func(t *testing.T) {
		other := base
		other.Value = "v2"
		require.False(t, comp.httpHeaderMatchesEqual(base, other))
	})

	t.Run("match type change is still detected independently of invert", func(t *testing.T) {
		other := base
		other.Match = api.HTTPHeaderMatchPrefix
		require.False(t, comp.httpHeaderMatchesEqual(base, other))
	})
}

// ---------------------------------------------------------------------------
// httpMatchesEqual — Invert propagates through the nested comparator
// ---------------------------------------------------------------------------

func TestEntryComparator_HTTPMatchesEqual_InvertPropagates(t *testing.T) {
	t.Parallel()

	comp := entryComparator{}

	base := api.HTTPMatch{
		Headers: []api.HTTPHeaderMatch{
			{Name: "x-canary", Match: api.HTTPHeaderMatchExact, Value: "v1", Invert: false},
		},
	}

	withInvert := api.HTTPMatch{
		Headers: []api.HTTPHeaderMatch{
			{Name: "x-canary", Match: api.HTTPHeaderMatchExact, Value: "v1", Invert: true},
		},
	}

	require.True(t, comp.httpMatchesEqual(base, base))
	require.True(t, comp.httpMatchesEqual(withInvert, withInvert))
	require.False(t, comp.httpMatchesEqual(base, withInvert),
		"httpMatchesEqual must be false when Invert differs")
}

// ---------------------------------------------------------------------------
// EntriesEqual (HTTPRoute) — end-to-end diff check for Invert
// ---------------------------------------------------------------------------

func TestEntriesEqual_HTTPRoute_InvertDetected(t *testing.T) {
	t.Parallel()

	makeRoute := func(invert bool) *api.HTTPRouteConfigEntry {
		return &api.HTTPRouteConfigEntry{
			Kind:      api.HTTPRoute,
			Name:      "my-route",
			Namespace: "default",
			Rules: []api.HTTPRouteRule{
				{
					Matches: []api.HTTPMatch{
						{
							Headers: []api.HTTPHeaderMatch{
								{
									Name:   "x-canary",
									Value:  "v1",
									Match:  api.HTTPHeaderMatchExact,
									Invert: invert,
								},
							},
						},
					},
					Services:        []api.HTTPService{{Name: "svc"}},
					Filters:         api.HTTPFilters{Headers: []api.HTTPHeaderFilter{}},
					ResponseFilters: api.HTTPResponseFilters{Headers: []api.HTTPHeaderFilter{}},
				},
			},
		}
	}

	t.Run("same invert value → equal (no spurious Consul write)", func(t *testing.T) {
		require.True(t, EntriesEqual(makeRoute(false), makeRoute(false)))
		require.True(t, EntriesEqual(makeRoute(true), makeRoute(true)))
	})

	t.Run("different invert value → not equal (Consul write triggered)", func(t *testing.T) {
		require.False(t, EntriesEqual(makeRoute(false), makeRoute(true)),
			"EntriesEqual must detect Invert false→true as a change")
		require.False(t, EntriesEqual(makeRoute(true), makeRoute(false)),
			"EntriesEqual must detect Invert true→false as a change")
	})
}
