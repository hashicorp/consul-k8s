// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"fmt"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestTerminatingGatewayController_reconcileSecretTriggerUpdatesConsulMeta(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kubeNS := "default"
	kubeName := "tgw"
	secretName := "tls-secret"
	metaKey := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", secretName)

	termGW := &v1alpha1.TerminatingGateway{
		ObjectMeta: metav1.ObjectMeta{Name: kubeName, Namespace: kubeNS},
		Spec: v1alpha1.TerminatingGatewaySpec{
			Services: []v1alpha1.LinkedService{
				{Name: "svc", SecretRef: &v1alpha1.SecretReference{Name: secretName}},
			},
		},
	}

	s := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
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
		Log:              logrtest.New(t),
		ReleaseName:      "consul",
		ReleaseNamespace: kubeNS,
		ConfigEntryController: &ConfigEntryController{
			ConsulClientConfig:  testClient.Cfg,
			ConsulServerConnMgr: testClient.Watcher,
			DatacenterName:      datacenterName,
		},
	}

	baseReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: kubeName, Namespace: kubeNS}}
	resp, err := reconciler.Reconcile(ctx, baseReq)
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	secretReq := ctrl.Request{NamespacedName: types.NamespacedName{Name: secretTriggerPrefix + kubeName, Namespace: kubeNS}}
	resp, err = reconciler.Reconcile(ctx, secretReq)
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	entry, _, err := consulClient.ConfigEntries().Get(capi.TerminatingGateway, kubeName, nil)
	require.NoError(t, err)
	tgEntry, ok := entry.(*capi.TerminatingGatewayConfigEntry)
	require.True(t, ok)

	firstRotation, ok := tgEntry.Meta[metaKey]
	require.True(t, ok)
	_, parseErr := time.Parse(time.RFC3339Nano, firstRotation)
	require.NoError(t, parseErr)

	var secondRotation string
	require.Eventually(t, func() bool {
		resp, err = reconciler.Reconcile(ctx, secretReq)
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		entry, _, err = consulClient.ConfigEntries().Get(capi.TerminatingGateway, kubeName, nil)
		require.NoError(t, err)
		tgEntry, ok = entry.(*capi.TerminatingGatewayConfigEntry)
		require.True(t, ok)

		secondRotation, ok = tgEntry.Meta[metaKey]
		require.True(t, ok)

		_, parseErr = time.Parse(time.RFC3339Nano, secondRotation)
		require.NoError(t, parseErr)
		return secondRotation != firstRotation
	}, 2*time.Second, 25*time.Millisecond)
}
