// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestBothNilOrEqual(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		first    *string
		second   *string
		expected bool
	}{
		"both nil": {
			first:    nil,
			second:   nil,
			expected: true,
		},
		"second nil": {
			first:    PointerTo(""),
			second:   nil,
			expected: false,
		},
		"first nil": {
			first:    nil,
			second:   PointerTo(""),
			expected: false,
		},
		"both equal": {
			first:    PointerTo(""),
			second:   PointerTo(""),
			expected: true,
		},
		"both not equal": {
			first:    PointerTo("1"),
			second:   PointerTo("2"),
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, BothNilOrEqual(tt.first, tt.second))
		})
	}
}

func TestValueOr(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		value    *string
		or       string
		expected string
	}{
		"nil value": {
			value:    nil,
			or:       "test",
			expected: "test",
		},
		"set value": {
			value:    PointerTo("value"),
			or:       "test",
			expected: "value",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, ValueOr(tt.value, tt.or))
		})
	}
}

func TestNilOrEqual(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		value    *string
		check    string
		expected bool
	}{
		"nil value": {
			value:    nil,
			check:    "test",
			expected: true,
		},
		"equal values": {
			value:    PointerTo("test"),
			check:    "test",
			expected: true,
		},
		"unequal values": {
			value:    PointerTo("value"),
			check:    "test",
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, NilOrEqual(tt.value, tt.check))
		})
	}
}

func TestEnsureFinalizer(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		object     client.Object
		expected   bool
		finalizers []string
	}{
		"gateway no finalizer": {
			object:     &gwv1beta1.Gateway{},
			expected:   true,
			finalizers: []string{GatewayFinalizer},
		},
		"gateway other finalizer": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"other"}}},
			expected:   true,
			finalizers: []string{"other", GatewayFinalizer},
		},
		"gateway already has finalizer": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{GatewayFinalizer}}},
			expected:   false,
			finalizers: []string{GatewayFinalizer},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, EnsureFinalizer(tt.object))
			require.Equal(t, tt.finalizers, tt.object.GetFinalizers())
		})
	}
}

func TestRemoveFinalizer(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		object     client.Object
		expected   bool
		finalizers []string
	}{
		"gateway no finalizer": {
			object:     &gwv1beta1.Gateway{},
			expected:   false,
			finalizers: []string{},
		},
		"gateway other finalizer": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"other"}}},
			expected:   false,
			finalizers: []string{"other"},
		},
		"gateway multiple finalizers": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{GatewayFinalizer, GatewayFinalizer}}},
			expected:   true,
			finalizers: []string{},
		},
		"gateway mixed finalizers": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"other", GatewayFinalizer}}},
			expected:   true,
			finalizers: []string{"other"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, RemoveFinalizer(tt.object))
			require.Equal(t, tt.finalizers, tt.object.GetFinalizers())
		})
	}
}
