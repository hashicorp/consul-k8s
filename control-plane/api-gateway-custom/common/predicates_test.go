// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	gwv1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TestConsulAnnotationPredicate is a regression guard for a bug where the
// Gateway watch was filtered ONLY on common.ComponentLabel
// ("component=api-gateway").
//
// That label is applied by LabelsForGateway to the resources consul-k8s
// GENERATES for a gateway (Deployment, Service, Pods); it is never applied to
// the user-authored Gateway CR. The label selector therefore dropped every
// Gateway event, so metadata-only edits -- notably adding or removing
// consul.hashicorp.com/tls-enabled -- were never translated into the Consul
// api-gateway config entry, while the Gateway still reported Synced=True.
//
// ConsulAnnotationPredicate is OR'd with the label selector so annotated
// Gateways reconcile regardless of labels.
func TestConsulAnnotationPredicate(t *testing.T) {
	p := ConsulAnnotationPredicate()

	annotated := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "api-gateway",
			Namespace:   "default",
			Annotations: map[string]string{AnnotationTLSEnabled: "true"},
		},
	}
	bare := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "api-gateway", Namespace: "default"},
	}

	require.True(t, p.Create(event.CreateEvent{Object: annotated}))
	require.False(t, p.Create(event.CreateEvent{Object: bare}))

	// Adding the annotation must reconcile.
	require.True(t, p.Update(event.UpdateEvent{ObjectOld: bare, ObjectNew: annotated}))

	// REMOVING the annotation must also reconcile -- this is the case the
	// original bug hit. The new object has no Consul annotation at all, so a
	// predicate that inspected only ObjectNew would drop precisely the event
	// needed to clear the setting in Consul.
	require.True(t, p.Update(event.UpdateEvent{ObjectOld: annotated, ObjectNew: bare}),
		"annotation removal must trigger a reconcile so the Consul entry is updated")

	// An unrelated Gateway with no Consul annotations stays filtered out.
	require.False(t, p.Update(event.UpdateEvent{ObjectOld: bare, ObjectNew: bare}))

	// Listener-scoped annotations use the "api-gateway.consul.hashicorp.com/"
	// prefix and must also be honoured.
	listenerAnnotated := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{TLSMinVersionAnnotationKey: "TLSv1_2"},
		},
	}
	require.True(t, p.Create(event.CreateEvent{Object: listenerAnnotated}))

	// Non-Consul annotations must not widen the watch.
	foreign := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"example.com/other": "true"},
		},
	}
	require.False(t, p.Create(event.CreateEvent{Object: foreign}))
}

// TestGatewayWatchPredicateAdmitsUnlabeledAnnotatedGateway pins the composed
// behaviour actually registered on the Gateway watch: the ComponentLabel
// selector alone rejects a user-authored Gateway, and OR-ing in
// ConsulAnnotationPredicate is what makes it reconcile.
func TestGatewayWatchPredicateAdmitsUnlabeledAnnotatedGateway(t *testing.T) {
	labelPredicate, err := predicate.LabelSelectorPredicate(
		*metav1.SetAsLabelSelector(map[string]string{ComponentLabel: "api-gateway-consul"}),
	)
	require.NoError(t, err)

	userGateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "api-gateway",
			Namespace:   "default",
			Annotations: map[string]string{AnnotationTLSEnabled: "true"},
		},
	}
	require.Empty(t, userGateway.Labels,
		"user-authored Gateways carry no consul-k8s labels")

	// The label selector on its own is what caused the bug.
	require.False(t, labelPredicate.Update(event.UpdateEvent{
		ObjectOld: userGateway.DeepCopy(), ObjectNew: userGateway,
	}), "ComponentLabel selector alone drops user-authored Gateway events")

	combined := predicate.Or(labelPredicate, ConsulAnnotationPredicate())
	require.True(t, combined.Update(event.UpdateEvent{
		ObjectOld: userGateway.DeepCopy(), ObjectNew: userGateway,
	}), "combined predicate must admit an annotated, unlabeled Gateway")

	// Generated resources still match via the label, so existing behaviour is
	// preserved.
	generatedLabels := LabelsForGateway(userGateway)
	require.Equal(t, "api-gateway-consul", generatedLabels[ComponentLabel])
}

// TestGatewayWatchPredicateAdmitsBareGateway pins the second half of the watch
// regression: a freshly created Gateway has neither the ComponentLabel nor any
// Consul annotation, because consul.hashicorp.com/gateway-class-config is
// written by the controller during reconciliation. Filtering the root Gateway
// watch on metadata alone therefore deadlocks such a gateway -- it can never
// gain the annotation that would let it pass the filter, and it is never
// deployed at all (observed live as Accepted=Unknown with no generated
// Deployment).
//
// This test asserts the composed predicate actually registered on the watch,
// including the GatewayClass-based clause that admits bare gateways.
func TestGatewayWatchPredicateAdmitsBareGateway(t *testing.T) {
	labelPredicate, err := predicate.LabelSelectorPredicate(
		*metav1.SetAsLabelSelector(map[string]string{ComponentLabel: "api-gateway-consul"}),
	)
	require.NoError(t, err)

	classPredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
		gw, ok := o.(*gwv1.Gateway)
		return ok && gw.Spec.GatewayClassName != ""
	})

	// Exactly what `kubectl apply` of a minimal Gateway produces: no labels,
	// no annotations.
	bareGateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "bare-gw", Namespace: "default"},
		Spec:       gwv1.GatewaySpec{GatewayClassName: "consul"},
	}
	require.Empty(t, bareGateway.Labels)
	require.Empty(t, bareGateway.Annotations)

	// Metadata-only filtering drops it entirely -- this is the deadlock.
	metadataOnly := predicate.Or(labelPredicate, ConsulAnnotationPredicate())
	require.False(t, metadataOnly.Create(event.CreateEvent{Object: bareGateway}),
		"metadata-only predicate drops a bare Gateway, so it is never reconciled")

	combined := predicate.Or(labelPredicate, ConsulAnnotationPredicate(), classPredicate)
	require.True(t, combined.Create(event.CreateEvent{Object: bareGateway}),
		"bare Gateway naming a GatewayClass must be admitted")

	// A spec-only edit (for example a listener port change) must also reconcile;
	// losing these events made Consul keep a stale port while the Gateway still
	// reported Synced=True.
	updated := bareGateway.DeepCopy()
	updated.Spec.Listeners = []gwv1.Listener{{Name: "http", Port: 9090}}
	require.True(t, combined.Update(event.UpdateEvent{ObjectOld: bareGateway, ObjectNew: updated}),
		"spec-only change must trigger a reconcile")

	// A Gateway with no GatewayClass at all is still filtered out.
	noClass := &gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "default"}}
	require.False(t, combined.Create(event.CreateEvent{Object: noClass}))
}
