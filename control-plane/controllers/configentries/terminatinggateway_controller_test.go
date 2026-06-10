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
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestTerminatingGatewayController_transformSecret(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	gwWithSecret := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gw-trigger-secret-rotation", Namespace: "default"},
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

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(gwWithSecret, gwWithoutSecret, gwOtherNamespace).
		WithIndex(&consulv1alpha1.TerminatingGateway{}, secretOwnerKey, termGWSecretIndexer).
		Build()
	controller := &TerminatingGatewayController{Client: fakeClient}

	requests := controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tls-secret", Namespace: "default"}})
	require.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: secretTriggerPrefix + "gw-trigger-secret-rotation", Namespace: "default"}}}, requests)

	requests = controller.transformSecret(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "non-matching", Namespace: "default"}})
	require.Empty(t, requests)
}

func TestTerminatingGatewayController_termGWSecretIndexer(t *testing.T) {
	t.Parallel()

	indexed := termGWSecretIndexer(&consulv1alpha1.TerminatingGateway{
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "api", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-a"}},
				{Name: "db"},
				{Name: "metrics", SecretRef: &consulv1alpha1.SecretReference{Name: "tls-b"}},
				{Name: "empty", SecretRef: &consulv1alpha1.SecretReference{}},
			},
		},
	})

	require.Equal(t, []string{"tls-a", "tls-b"}, indexed)
}

func TestTerminatingGatewayController_ReconcileSecretTriggerPrefixDoesNotTrimGatewayName(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	kubeNS := "default"
	kubeName := "gw-trigger-secret-rotation"
	secretName := "tls-secret"
	metaKey := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", secretName)

	termGW := &consulv1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: kubeName, Namespace: kubeNS},
		Spec: consulv1alpha1.TerminatingGatewaySpec{
			Services: []consulv1alpha1.LinkedService{
				{Name: "svc", SecretRef: &consulv1alpha1.SecretReference{Name: secretName}},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(termGW, terminatingGatewayHelmValuesConfigMap(kubeNS)).
		WithStatusSubresource(termGW).
		Build()

	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	testClient.TestServer.WaitForServiceIntentions(t)
	consulClient := testClient.APIClient

	reconciler := &TerminatingGatewayController{
		Client:           fakeClient,
		ReleaseName:      "consul",
		ReleaseNamespace: kubeNS,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	secretReq := reconcile.Request{NamespacedName: types.NamespacedName{Name: secretTriggerPrefix + kubeName, Namespace: kubeNS}}
	resp, err := reconciler.Reconcile(context.Background(), secretReq)
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	entry, _, err := consulClient.ConfigEntries().Get(capi.TerminatingGateway, kubeName, nil)
	require.NoError(t, err)

	tgEntry, ok := entry.(*capi.TerminatingGatewayConfigEntry)
	require.True(t, ok)
	require.Equal(t, kubeName, tgEntry.Name)
	_, ok = tgEntry.Meta[metaKey]
	require.True(t, ok)
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
		_, parseErr := time.Parse(time.RFC3339Nano, v)
		require.NoError(t, parseErr)
	}

	err = controller.MutateConsulEntry(obj, &capi.ServiceConfigEntry{}, reconcile.Request{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected TerminatingGatewayConfigEntry")
}

// TestTerminatingGatewayController_terminatingGatewayConsulNamespace verifies the
// resolution order for the Consul namespace a terminating gateway registers into,
// including the namespace-mirroring fallback used when Admin Partitions are enabled.
func TestTerminatingGatewayController_terminatingGatewayConsulNamespace(t *testing.T) {
	t.Parallel()

	const kubeNS = "consul"

	newTermGW := func(specNS, gatewayName string) *consulv1alpha1.TerminatingGateway {
		return &consulv1alpha1.TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "terminating-gateway", Namespace: kubeNS},
			Spec: consulv1alpha1.TerminatingGatewaySpec{
				Deployment: consulv1alpha1.TerminatingGatewayDeploymentSpec{
					ConsulNamespace: specNS,
					GatewayName:     gatewayName,
				},
			},
		}
	}

	cases := map[string]struct {
		termGW     *consulv1alpha1.TerminatingGateway
		helm       *helmvalues.HelmValues
		cec        *ConfigEntryController
		expectedNS string
	}{
		"explicit spec.deployment.consulNamespace wins": {
			termGW: newTermGW("explicit-ns", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: "explicit-ns",
		},
		"per-gateway helm override wins over defaults": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "defaults-ns"},
					Gateways: []helmvalues.Gateway{{Name: "terminating-gateway", ConsulNamespace: "per-gw-ns"}},
				},
			},
			cec:        &ConfigEntryController{EnableConsulNamespaces: true},
			expectedNS: "per-gw-ns",
		},
		"non-default defaults.consulNamespace is respected": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "defaults-ns"},
				},
			},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: "defaults-ns",
		},
		"mirroring maps to the K8s namespace when defaults is the default sentinel": {
			termGW: newTermGW("", ""),
			helm: &helmvalues.HelmValues{
				TerminatingGateways: helmvalues.TerminatingGatewaysConfig{
					Defaults: helmvalues.Defaults{ConsulNamespace: "default"},
				},
			},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
			},
			expectedNS: kubeNS,
		},
		"mirroring with prefix maps to prefixed K8s namespace": {
			termGW: newTermGW("", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces: true,
				EnableNSMirroring:      true,
				NSMirroringPrefix:      "k8s-",
			},
			expectedNS: "k8s-" + kubeNS,
		},
		"destination namespace used when mirroring disabled": {
			termGW: newTermGW("", ""),
			helm:   &helmvalues.HelmValues{},
			cec: &ConfigEntryController{
				EnableConsulNamespaces:     true,
				ConsulDestinationNamespace: "dest-ns",
			},
			expectedNS: "dest-ns",
		},
		"falls back to default when namespaces disabled": {
			termGW:     newTermGW("", ""),
			helm:       &helmvalues.HelmValues{},
			cec:        &ConfigEntryController{EnableConsulNamespaces: false},
			expectedNS: "default",
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := &TerminatingGatewayController{ConfigEntryController: tc.cec}
			require.Equal(t, tc.expectedNS, r.terminatingGatewayConsulNamespace(tc.termGW, tc.helm))
		})
	}
}
