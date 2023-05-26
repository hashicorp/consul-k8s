// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestIsNil(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		value    interface{}
		expected bool
	}{
		"nil pointer": {
			value:    (*string)(nil),
			expected: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, isNil(tt.value))
		})
	}
}

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
			first:    pointerTo(""),
			second:   nil,
			expected: false,
		},
		"first nil": {
			first:    nil,
			second:   pointerTo(""),
			expected: false,
		},
		"both equal": {
			first:    pointerTo(""),
			second:   pointerTo(""),
			expected: true,
		},
		"both not equal": {
			first:    pointerTo("1"),
			second:   pointerTo("2"),
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, bothNilOrEqual(tt.first, tt.second))
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
			value:    pointerTo("value"),
			or:       "test",
			expected: "value",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, valueOr(tt.value, tt.or))
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
			value:    pointerTo("test"),
			check:    "test",
			expected: true,
		},
		"unequal values": {
			value:    pointerTo("value"),
			check:    "test",
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, nilOrEqual(tt.value, tt.check))
		})
	}
}

func TestObjectToMeta(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		object   metav1.Object
		expected types.NamespacedName
	}{
		"gateway": {
			object:   &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "test"}},
			expected: types.NamespacedName{Namespace: "test", Name: "test"},
		},
		"secret": {
			object:   &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "secret", Name: "secret"}},
			expected: types.NamespacedName{Namespace: "secret", Name: "secret"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, objectToMeta(tt.object))
		})
	}
}

func TestIsDeleted(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		object   client.Object
		expected bool
	}{
		"deleted gateway": {
			object:   &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: pointerTo(metav1.Now())}},
			expected: true,
		},
		"non-deleted http route": {
			object:   &gwv1beta1.HTTPRoute{},
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, isDeleted(tt.object))
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
			finalizers: []string{gatewayFinalizer},
		},
		"gateway other finalizer": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"other"}}},
			expected:   true,
			finalizers: []string{"other", gatewayFinalizer},
		},
		"gateway already has finalizer": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{gatewayFinalizer}}},
			expected:   false,
			finalizers: []string{gatewayFinalizer},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, ensureFinalizer(tt.object))
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
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{gatewayFinalizer, gatewayFinalizer}}},
			expected:   true,
			finalizers: []string{},
		},
		"gateway mixed finalizers": {
			object:     &gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{"other", gatewayFinalizer}}},
			expected:   true,
			finalizers: []string{"other"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.expected, removeFinalizer(tt.object))
			require.Equal(t, tt.finalizers, tt.object.GetFinalizers())
		})
	}
}
