// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package agentresources

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
		flagConfigName:      "consul-ai-agent",
		flagHeritage:        "Helm",
		flagChart:           "consul-helm",
		flagApp:             "consul",
		flagRelease:         "test",
		flagComponent:       "ai-agent",
		flagEnabled:         true,
		flagInterceptorPort: 21101,
		flagMcpPort:         15101,
		flagHITLPort:        16101,
		flagApprovalTimeout: "60s",
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
		"interceptor-port too low": {
			mutate:      func(c *Command) { c.flagInterceptorPort = 80 },
			expectedErr: "-interceptor-port must be between 1024 and 65535, got 80",
		},
		"mcp-port too low": {
			mutate:      func(c *Command) { c.flagMcpPort = 80 },
			expectedErr: "-mcp-port must be between 1024 and 65535, got 80",
		},
		"hitl-port too low": {
			mutate:      func(c *Command) { c.flagHITLPort = 80 },
			expectedErr: "-hitl-port must be between 1024 and 65535, got 80",
		},
		"approval-timeout required": {
			mutate:      func(c *Command) { c.flagApprovalTimeout = "" },
			expectedErr: "-approval-timeout must be set",
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

func TestForceAgentConfig_create(t *testing.T) {
	t.Parallel()
	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, err := cmd.buildResources()
	require.NoError(t, err)

	desired := &v1alpha1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.AgentConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.AgentDefaults{
				InterceptorPort: int32(cmd.flagInterceptorPort),
				McpPort:         int32(cmd.flagMcpPort),
				HITL: v1alpha1.AgentHITL{
					Port:            int32(cmd.flagHITLPort),
					ApprovalTimeout: cmd.flagApprovalTimeout,
				},
				Resources: resources,
			},
		},
	}

	require.NoError(t, forceAgentConfig(context.Background(), fc, desired))

	got := &v1alpha1.AgentConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-agent"}, got))
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, int32(15101), got.Spec.Defaults.McpPort)
	require.Equal(t, int32(16101), got.Spec.Defaults.HITL.Port)
	require.Equal(t, "60s", got.Spec.Defaults.HITL.ApprovalTimeout)
	require.True(t, got.Spec.Enabled)
}

func TestForceAgentConfig_update(t *testing.T) {
	t.Parallel()
	fc := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	cmd := fullCmd()
	resources, _ := cmd.buildResources()

	existing := &v1alpha1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: cmd.flagConfigName},
		Spec: v1alpha1.AgentConfigSpec{
			Enabled: cmd.flagEnabled,
			Defaults: v1alpha1.AgentDefaults{
				InterceptorPort: int32(cmd.flagInterceptorPort),
				McpPort:         int32(cmd.flagMcpPort),
				HITL: v1alpha1.AgentHITL{
					Port:            int32(cmd.flagHITLPort),
					ApprovalTimeout: cmd.flagApprovalTimeout,
				},
				Resources: resources,
			},
		},
	}
	require.NoError(t, fc.Create(context.Background(), existing))

	cmd.flagHITLPort = 17101
	cmd.flagApprovalTimeout = "120s"

	updated := existing.DeepCopy()
	updated.Spec.Defaults.HITL.Port = int32(cmd.flagHITLPort)
	updated.Spec.Defaults.HITL.ApprovalTimeout = cmd.flagApprovalTimeout

	require.NoError(t, forceAgentConfig(context.Background(), fc, updated))

	got := &v1alpha1.AgentConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-agent"}, got))
	require.Equal(t, int32(17101), got.Spec.Defaults.HITL.Port)
	require.Equal(t, "120s", got.Spec.Defaults.HITL.ApprovalTimeout)
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
		"-config-name=consul-ai-agent",
		"-heritage=Helm", "-chart=consul-helm", "-app=consul", "-release-name=test",
		"-enabled=true",
		"-interceptor-port=21101",
		"-mcp-port=15101",
		"-hitl-port=16101",
		"-approval-timeout=60s",
		"-requests-memory=128Mi", "-requests-cpu=250m",
		"-limits-memory=256Mi", "-limits-cpu=500m",
	}
	require.Equal(t, 0, cmd.Run(args))

	got := &v1alpha1.AgentConfig{}
	require.NoError(t, fc.Get(context.Background(), types.NamespacedName{Name: "consul-ai-agent"}, got))
	require.True(t, got.Spec.Enabled)
	require.Equal(t, int32(21101), got.Spec.Defaults.InterceptorPort)
	require.Equal(t, int32(15101), got.Spec.Defaults.McpPort)
	require.Equal(t, int32(16101), got.Spec.Defaults.HITL.Port)
	require.Equal(t, "60s", got.Spec.Defaults.HITL.ApprovalTimeout)
	require.Equal(t, "128Mi", got.Spec.Defaults.Resources.Requests.Memory().String())
	require.Equal(t, "250m", got.Spec.Defaults.Resources.Requests.Cpu().String())
}

func TestBuildResources_invalid(t *testing.T) {
	t.Parallel()
	cmd := fullCmd()
	cmd.flagRequestsMemory = "not-valid"
	_, err := cmd.buildResources()
	require.Error(t, err)
	require.Contains(t, err.Error(), "requests-memory")
}
