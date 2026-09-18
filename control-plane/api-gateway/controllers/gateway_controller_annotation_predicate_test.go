// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

func gatewayWithAnnotations(annotations map[string]string) client.Object {
	return &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-gw",
			Namespace:   "default",
			Annotations: annotations,
		},
	}
}

func protocolAnnotation(section string) string {
	return common.ListenerProtocolAnnotationPrefix + section + common.ListenerProtocolAnnotationSuffix
}

func TestHasListenerProtocolAnnotation(t *testing.T) {
	for name, tc := range map[string]struct {
		annotations map[string]string
		expected    bool
	}{
		"listener protocol annotation": {
			annotations: map[string]string{protocolAnnotation("grpc"): "grpc"},
			expected:    true,
		},
		"one of several annotations": {
			annotations: map[string]string{
				"some.other/annotation":       "value",
				protocolAnnotation("h2"):      "http2",
				"consul.hashicorp.com/config": "{}",
			},
			expected: true,
		},
		"no annotations at all": {
			annotations: nil,
			expected:    false,
		},
		"unrelated annotations only": {
			annotations: map[string]string{"some.other/annotation": "value"},
			expected:    false,
		},
		// Guards against a loosened prefix-only match. A key sharing the prefix
		// but missing the -protocol suffix is a different annotation.
		"right prefix but wrong suffix": {
			annotations: map[string]string{
				common.ListenerProtocolAnnotationPrefix + "grpc-timeout": "5s",
			},
			expected: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected,
				hasListenerProtocolAnnotation(gatewayWithAnnotations(tc.annotations)))
		})
	}

	t.Run("nil object", func(t *testing.T) {
		require.False(t, hasListenerProtocolAnnotation(nil))
	})
}

// TestListenerProtocolAnnotationPredicate pins the behaviour that makes
// listener-protocol annotation edits take effect. The controller's primary
// Gateway watch is filtered by a component=api-gateway label selector that
// user-authored Gateways never carry, so without this predicate an annotation
// edit produces no reconcile and the Consul listener keeps its old protocol.
func TestListenerProtocolAnnotationPredicate(t *testing.T) {
	p := listenerProtocolAnnotationPredicate()
	withAnnotation := gatewayWithAnnotations(map[string]string{protocolAnnotation("grpc"): "grpc"})
	changedAnnotation := gatewayWithAnnotations(map[string]string{protocolAnnotation("grpc"): "http2"})
	withoutAnnotation := gatewayWithAnnotations(nil)

	t.Run("create is admitted only with the annotation", func(t *testing.T) {
		require.True(t, p.Create(event.CreateEvent{Object: withAnnotation}))
		require.False(t, p.Create(event.CreateEvent{Object: withoutAnnotation}))
	})

	t.Run("annotation added is admitted", func(t *testing.T) {
		require.True(t, p.Update(event.UpdateEvent{
			ObjectOld: withoutAnnotation,
			ObjectNew: withAnnotation,
		}))
	})

	// The regression case: the new object has no annotation, so a predicate
	// that only inspected ObjectNew would drop this event and strand the
	// listener on the protocol the operator just removed.
	t.Run("annotation removed is admitted", func(t *testing.T) {
		require.True(t, p.Update(event.UpdateEvent{
			ObjectOld: withAnnotation,
			ObjectNew: withoutAnnotation,
		}))
	})

	t.Run("annotation value changed is admitted", func(t *testing.T) {
		require.True(t, p.Update(event.UpdateEvent{
			ObjectOld: withAnnotation,
			ObjectNew: changedAnnotation,
		}))
	})

	t.Run("unrelated update is not admitted", func(t *testing.T) {
		require.False(t, p.Update(event.UpdateEvent{
			ObjectOld: withoutAnnotation,
			ObjectNew: withoutAnnotation,
		}))
	})

	t.Run("delete and generic follow the annotation", func(t *testing.T) {
		require.True(t, p.Delete(event.DeleteEvent{Object: withAnnotation}))
		require.False(t, p.Delete(event.DeleteEvent{Object: withoutAnnotation}))
		require.True(t, p.Generic(event.GenericEvent{Object: withAnnotation}))
		require.False(t, p.Generic(event.GenericEvent{Object: withoutAnnotation}))
	})
}
