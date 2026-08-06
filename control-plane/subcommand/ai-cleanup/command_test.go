// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aicleanup

import (
	"context"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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

// TestRun_stripsFinalizersAndDeletes verifies that InferenceModelConfig and
// McpServerConfig CRs with finalizers are patched (finalizers removed) and
// deleted before the command exits.
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

	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(imc, msc).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}

	require.Equal(t, 0, cmd.Run([]string{}))

	// Both CRs must be gone.
	gotIMC := &v1alpha1.InferenceModelConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, gotIMC)
	require.True(t, isNotFound(err), "InferenceModelConfig should be deleted")

	gotMSC := &v1alpha1.McpServerConfig{}
	err = fc.Get(context.Background(), types.NamespacedName{Name: "consul-mcp-server"}, gotMSC)
	require.True(t, isNotFound(err), "McpServerConfig should be deleted")
}

// TestRun_multipleResources verifies all CRs are cleaned up when more than one
// of each kind exists.
func TestRun_multipleResources(t *testing.T) {
	t.Parallel()

	imcObjs := []v1alpha1.InferenceModelConfig{
		{ObjectMeta: metav1.ObjectMeta{Name: "imc-1", Finalizers: []string{"fin"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "imc-2", Finalizers: []string{"fin"}}},
	}
	mcpObjs := []v1alpha1.McpServerConfig{
		{ObjectMeta: metav1.ObjectMeta{Name: "msc-1", Finalizers: []string{"fin"}}},
	}

	builder := fake.NewClientBuilder().WithScheme(testScheme(t))
	for i := range imcObjs {
		builder = builder.WithObjects(&imcObjs[i])
	}
	for i := range mcpObjs {
		builder = builder.WithObjects(&mcpObjs[i])
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
	got := &v1alpha1.McpServerConfig{}
	err := fc.Get(context.Background(), types.NamespacedName{Name: "msc-1"}, got)
	require.True(t, isNotFound(err), "msc-1 should be deleted")
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
