// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package mcpserverresources

import (
	"context"
	"testing"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
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

func fullCmd() *Command {
	return &Command{
		flagConfigName:      "consul-mcp-server",
		flagHeritage:        "Helm",
		flagChart:           "consul-helm",
		flagApp:             "consul",
		flagRelease:         "test",
		flagComponent:       "mcp-server",
		flagEnabled:         true,
		flagInterceptorPort: 21101,
		flagTransport:       "streamable-http",
		flagPath:            "/mcp",
		flagProtocolVersion: "2025-03-26",
		flagRequestsMemory:  "128Mi",
		flagRequestsCPU:     "250m",
		flagLimitsMemory:    "256Mi",
		flagLimitsCPU:       "500m",
	}
}

func TestRun_flagValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate      func(*Command)
		expectedErr string
	}{
		"config-name required": {
			mutate:      func(c *Command) { c.flagConfigName = "" },
			expectedErr: "-config-name must be set",
		},
		"heritage required": {
			mutate:      func(c *Command) { c.flagHeritage = "" },
			expectedErr: "-heritage must be set",
		},
		"chart required": {
			mutate:      func(c *Command) { c.flagChart = "" },
			expectedErr: "-chart must be set",
		},
		"app required": {
			mutate:      func(c *Command) { c.flagApp = "" },
			expectedErr: "-app must be set",
		},
		"release required": {
			mutate:      func(c *Command) { c.flagRelease = "" },
			expectedErr: "-release-name must be set",
		},
		"invalid transport": {
			mutate:      func(c *Command) { c.flagTransport = "grpc" },
			expectedErr: `-transport must be one of streamable-http|sse|stdio, got "grpc"`,
		},
		"port too low": {
			mutate:      func(c *Command) { c.flagInterceptorPort = 80 },
			expectedErr: "-interceptor-port must be between 1024 and 65535, got 80",
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := fullCmd()
			tt.mutate(cmd)
			require.EqualError(t, cmd.validateFlags(), tt.expectedErr)
		})
	}
}

func TestForceMcpServerConfig_create(t *testing.T) {
	t.Parallel()
	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, err := cmd.buildResources()
	require.NoError(t, err)

	desired := &v1alpha1.McpServerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.McpServerConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.McpServerDefaults{
				InterceptorPort: int32(cmd.flagInterceptorPort),
				Transport:       cmd.flagTransport,
				Path:            cmd.flagPath,
				ProtocolVersion: cmd.flagProtocolVersion,
				Resources:       resources,
			},
		},
	}

	require.NoError(t, forceMcpServerConfig(context.Background(), fc, desired))

	got := &v1alpha1.McpServerConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-mcp-server"}, got))
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, "streamable-http", got.Spec.Defaults.Transport)
	require.Equal(t, "/mcp", got.Spec.Defaults.Path)
	require.Equal(t, "2025-03-26", got.Spec.Defaults.ProtocolVersion)
	require.True(t, got.Spec.Enabled)
}

func TestForceMcpServerConfig_update(t *testing.T) {
	t.Parallel()
	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, _ := cmd.buildResources()

	existing := &v1alpha1.McpServerConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.McpServerConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.McpServerDefaults{
				InterceptorPort: int32(cmd.flagInterceptorPort),
				Transport:       cmd.flagTransport,
				Resources:       resources,
			},
		},
	}
	require.NoError(t, fc.Create(context.Background(), existing))

	cmd.flagTransport = "sse"
	cmd.flagProtocolVersion = "2024-11-05"
	resources, _ = cmd.buildResources()

	updated := existing.DeepCopy()
	updated.Spec.Defaults.Transport = cmd.flagTransport
	updated.Spec.Defaults.ProtocolVersion = cmd.flagProtocolVersion
	updated.Spec.Defaults.Resources = resources

	require.NoError(t, forceMcpServerConfig(context.Background(), fc, updated))

	got := &v1alpha1.McpServerConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-mcp-server"}, got))
	require.Equal(t, "sse", got.Spec.Defaults.Transport)
	require.Equal(t, "2024-11-05", got.Spec.Defaults.ProtocolVersion)
}

func TestRun_success(t *testing.T) {
	t.Parallel()
	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := &Command{
		UI:        cli.NewMockUi(),
		k8sClient: fc,
		ctx:       context.Background(),
	}
	args := []string{
		"-config-name=consul-mcp-server",
		"-heritage=Helm", "-chart=consul-helm", "-app=consul", "-release-name=test",
		"-enabled=true",
		"-interceptor-port=21101",
		"-transport=streamable-http",
		"-path=/mcp",
		"-protocol-version=2025-03-26",
		"-requests-memory=128Mi", "-requests-cpu=250m",
		"-limits-memory=256Mi", "-limits-cpu=500m",
	}
	require.Equal(t, 0, cmd.Run(args))

	got := &v1alpha1.McpServerConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-mcp-server"}, got))
	require.True(t, got.Spec.Enabled)
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, "streamable-http", got.Spec.Defaults.Transport)
	require.Equal(t, "/mcp", got.Spec.Defaults.Path)
	require.Equal(t, "2025-03-26", got.Spec.Defaults.ProtocolVersion)
	require.Equal(t, "128Mi", got.Spec.Defaults.Resources.Requests.Memory().String())
	require.Equal(t, "250m", got.Spec.Defaults.Resources.Requests.Cpu().String())
	require.Equal(t, "256Mi", got.Spec.Defaults.Resources.Limits.Memory().String())
	require.Equal(t, "500m", got.Spec.Defaults.Resources.Limits.Cpu().String())
}

func TestBuildResources_invalid(t *testing.T) {
	t.Parallel()
	cmd := fullCmd()
	cmd.flagRequestsMemory = "not-valid"
	_, err := cmd.buildResources()
	require.Error(t, err)
	require.Contains(t, err.Error(), "requests-memory")
}
