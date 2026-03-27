// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"fmt"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestTerminatingGatewayController_transformSecret(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	s.AddKnownTypes(consulv1alpha1.GroupVersion,
		&consulv1alpha1.TerminatingGateway{},
		&consulv1alpha1.TerminatingGatewayList{},
	)

	gwWithSecret := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-with-secret", Namespace: "default"},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}},
				{Name: "api-dup", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}},
			},
		},
	}
	gwWithoutSecret := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-without-secret", Namespace: "default"},
		Spec:       consulv1alpha1.TerminatingGatewaySpec{Services: []consulv1alpha1.LinkedService{{Name: "db"}}},
	}
	gwOtherNamespace := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-other-ns", Namespace: "other"},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-secret"}}},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(gwWithSecret, gwWithoutSecret, gwOtherNamespace).Build()
	controller := &TerminatingGatewayController{Client: fakeClient}

	requests := controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "default"}})
	require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: "gw-with-secret" + SecretTriggerSuffix, Namespace: "default"}}}, requests)

	requests = controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "non-matching", Namespace: "default"}})
	require.Empty(t, requests)
}

func TestTerminatingGatewayController_MutateConsulEntry(t *testing.T) {
	t.Parallel()

	controller := &TerminatingGatewayController{}
	obj := &consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "svc-a", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
				{Name: "svc-b"},
				{Name: "svc-c", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-b"}},
				{Name: "svc-d", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
			},
		},
	}

	entry := &capi.TerminatingGatewayConfigEntry{Meta: map[string]string{"existing": "value"}}
	err := controller.MutateConsulEntry(obj, entry, reconcile.Request{})
	require.NoError(t, err)

	require.Equal(t, "value", entry.Meta["existing"])
	for _, secret := range []string{"tls-a", "tls-b"} {
		k := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", secret)
		v, ok := entry.Meta[k]
		require.True(t, ok, "expected metadata key for secret %q", secret)
		_, parseErr := time.Parse(time.RFC3339, v)
		require.NoError(t, parseErr)
	}

	err = controller.MutateConsulEntry(obj, &capi.ServiceConfigEntry{}, reconcile.Request{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected TerminatingGatewayConfigEntry")
}
