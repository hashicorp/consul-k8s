// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aicleanup

import (
	"context"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))
	return s
}

// TestRun_noCRDs verifies the command exits 0 when the scheme has no AI types
// registered — simulating a cluster where the CRDs were never installed or
// were already deleted. The fake client returns "no kind registered" errors.
func TestRun_noCRDs(t *testing.T) {
	t.Parallel()

	// Scheme with only core types — AI CRDs absent.
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))

	fc := fake.NewClientBuilder().WithScheme(s).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))
}

// TestRun_noResources verifies the command exits 0 when CRDs are present but
// no CRs exist.
func TestRun_noResources(t *testing.T) {
	t.Parallel()

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))
}

// TestRun_stripsFinalizersAndDeletes verifies that InferenceModelConfig,
// McpServerConfig, AgentConfig, and InferencePoolConfig CRs with finalizers
// are patched (finalizers removed) and deleted before the command exits.
func TestRun_stripsFinalizersAndDeletes(t *testing.T) {
	t.Parallel()

	imc := &v1alpha1.InferenceModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "consul-ai-gateway",
			Finalizers: []string{"inference-model-config-exists-finalizer.consul.hashicorp.com"},
		},
	}
	msc := &v1alpha1.McpServerConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "consul-mcp-server",
			Finalizers: []string{"mcp-server-config-exists-finalizer.consul.hashicorp.com"},
		},
	}
	ac := &v1alpha1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "consul-agent",
			Finalizers: []string{"agent-config-exists-finalizer.consul.hashicorp.com"},
		},
	}
	ipc := &v1alpha1.InferencePoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-pool",
			Namespace:  "default",
			Finalizers: []string{"inference-pool-config-exists-finalizer.consul.hashicorp.com"},
		},
		Spec: v1alpha1.InferencePoolConfigSpec{
			Enabled: true,
			ParentRefs: []v1alpha1.InferencePoolParentRef{
				{Kind: "InferenceGateway", Name: "gw"},
			},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(imc, msc, ac, ipc).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}

	require.Equal(t, 0, cmd.Run([]string{}))

	// All four CRs must be gone.
	gotIMC := &v1alpha1.InferenceModelConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, gotIMC)
	require.True(t, isNotFound(err), "InferenceModelConfig should be deleted")

	gotMSC := &v1alpha1.McpServerConfig{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "consul-mcp-server"}, gotMSC)
	require.True(t, isNotFound(err), "McpServerConfig should be deleted")

	gotAC := &v1alpha1.AgentConfig{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "consul-agent"}, gotAC)
	require.True(t, isNotFound(err), "AgentConfig should be deleted")

	gotIPC := &v1alpha1.InferencePoolConfig{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "my-pool", Namespace: "default"}, gotIPC)
	require.True(t, isNotFound(err), "InferencePoolConfig should be deleted")
}

// TestRun_multipleResources verifies all CRs are cleaned up when more than one
// of each kind exists, including multiple InferencePoolConfigs across namespaces.
func TestRun_multipleResources(t *testing.T) {
	t.Parallel()

	imcObjs := []v1alpha1.InferenceModelConfig{
		{ObjectMeta: metav1.ObjectMeta{Name: "imc-1", Finalizers: []string{"fin"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "imc-2", Finalizers: []string{"fin"}}},
	}
	mcpObjs := []v1alpha1.McpServerConfig{
		{ObjectMeta: metav1.ObjectMeta{Name: "msc-1", Finalizers: []string{"fin"}}},
	}
	ipcObjs := []v1alpha1.InferencePoolConfig{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-a", Namespace: "ns-1", Finalizers: []string{"fin"}},
			Spec: v1alpha1.InferencePoolConfigSpec{
				Enabled:    true,
				ParentRefs: []v1alpha1.InferencePoolParentRef{{Kind: "InferenceGateway", Name: "gw"}},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pool-b", Namespace: "ns-2", Finalizers: []string{"fin"}},
			Spec: v1alpha1.InferencePoolConfigSpec{
				Enabled:    true,
				ParentRefs: []v1alpha1.InferencePoolParentRef{{Kind: "InferenceGateway", Name: "gw"}},
			},
		},
	}

	builder := fake.NewClientBuilder().WithScheme(testScheme(t))
	for i := range imcObjs {
		builder = builder.WithObjects(&imcObjs[i])
	}
	for i := range mcpObjs {
		builder = builder.WithObjects(&mcpObjs[i])
	}
	for i := range ipcObjs {
		builder = builder.WithObjects(&ipcObjs[i])
	}
	fc := builder.Build()

	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))

	for _, name := range []string{"imc-1", "imc-2"} {
		got := &v1alpha1.InferenceModelConfig{}
		err := fc.Get(context.Background(), types.NamespacedName{Name: name}, got)
		require.True(t, isNotFound(err), "%s should be deleted", name)
	}
	gotMSC := &v1alpha1.McpServerConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "msc-1"}, gotMSC)
	require.True(t, isNotFound(err), "msc-1 should be deleted")

	for _, tc := range []struct{ name, ns string }{{"pool-a", "ns-1"}, {"pool-b", "ns-2"}} {
		got := &v1alpha1.InferencePoolConfig{}
		err := fc.Get(context.Background(), types.NamespacedName{Name: tc.name, Namespace: tc.ns}, got)
		require.True(t, isNotFound(err), "%s/%s should be deleted", tc.ns, tc.name)
	}
}

// TestRun_inferencePoolConfigOnly verifies the command exits 0 and deletes
// InferencePoolConfig CRs even when no other AI CRs exist.
func TestRun_inferencePoolConfigOnly(t *testing.T) {
	t.Parallel()

	ipc := &v1alpha1.InferencePoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-pool",
			Namespace:  "team-a",
			Finalizers: []string{"inference-pool-config-exists-finalizer.consul.hashicorp.com"},
		},
		Spec: v1alpha1.InferencePoolConfigSpec{
			Enabled: true,
			ParentRefs: []v1alpha1.InferencePoolParentRef{
				{Kind: "InferenceGateway", Name: "gw"},
			},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(ipc).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))

	got := &v1alpha1.InferencePoolConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "my-pool", Namespace: "team-a"}, got)
	require.True(t, isNotFound(err), "InferencePoolConfig should be deleted")
}

// TestRun_inferencePoolConfigNoFinalizer verifies that an InferencePoolConfig
// with no finalizer is deleted cleanly (the patch is a no-op and Delete succeeds).
func TestRun_inferencePoolConfigNoFinalizer(t *testing.T) {
	t.Parallel()

	ipc := &v1alpha1.InferencePoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "no-fin-pool",
			Namespace: "default",
			// No finalizer — the patch is a no-op.
		},
		Spec: v1alpha1.InferencePoolConfigSpec{
			Enabled: true,
			ParentRefs: []v1alpha1.InferencePoolParentRef{
				{Kind: "InferenceGateway", Name: "gw"},
			},
		},
	}

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(ipc).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))

	got := &v1alpha1.InferencePoolConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "no-fin-pool", Namespace: "default"}, got)
	require.True(t, isNotFound(err), "InferencePoolConfig should be deleted even without a finalizer")
}

// TestRun_idempotent verifies that running the command twice (no CRs on second
// run) still exits 0.
func TestRun_idempotent(t *testing.T) {
	t.Parallel()

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	require.Equal(t, 0, cmd.Run([]string{}))
	require.Equal(t, 0, cmd.Run([]string{}))
}

func isNotFound(err error) bool {
	return k8serrors.IsNotFound(err)
}
