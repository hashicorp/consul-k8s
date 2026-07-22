// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestAIConfigFromConfigMap_FullJSON(t *testing.T) {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mcp", Namespace: "default"},
		Data: map[string]string{
			aiConfigKey: `{
				"Role": "ai-agent",
				"Agent": {
					"Inference": {"Specialization": ["code-review","coding"], "Vendor": "anthropic"},
					"MCP": {
						"Port": 15101,
						"HITL": {"Port": 16101, "ApprovalTimeout": "60s"}
					},
					"RateLimits": {"ToolCallsPerMinute": 120, "ToolCallsPerHour": 3000},
					"Interceptor": {"Port": 21101}
				}
			}`,
		},
	}

	cfg, err := aiConfigFromConfigMap(cm)
	require.NoError(t, err)

	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.NotNil(t, cfg.Agent)

	// Inference.
	require.NotNil(t, cfg.Agent.Inference)
	require.Equal(t, []string{"code-review", "coding"}, cfg.Agent.Inference.Specialization)
	require.Equal(t, "anthropic", cfg.Agent.Inference.Vendor)

	// MCP.
	require.NotNil(t, cfg.Agent.MCP)
	require.Equal(t, 15101, cfg.Agent.MCP.Port)
	require.NotNil(t, cfg.Agent.MCP.HITL)
	require.Equal(t, 16101, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, "60s", cfg.Agent.MCP.HITL.ApprovalTimeout)

	// RateLimits.
	require.NotNil(t, cfg.Agent.RateLimits)
	require.Equal(t, 120, cfg.Agent.RateLimits.ToolCallsPerMinute)
	require.Equal(t, 3000, cfg.Agent.RateLimits.ToolCallsPerHour)

	// Interceptor.
	require.NotNil(t, cfg.Agent.Interceptor)
	require.Equal(t, 21101, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromConfigMap_MissingKey_UsesDefaults(t *testing.T) {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-mcp", Namespace: "default"},
		Data:       map[string]string{}, // no "ai.json" key
	}

	cfg, err := aiConfigFromConfigMap(cm)
	require.NoError(t, err)

	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.NotNil(t, cfg.Agent)
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromConfigMap_PartialJSON_FillsPortDefaults(t *testing.T) {
	// Port values intentionally omitted — should be filled by applyAIPortDefaults.
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-mcp", Namespace: "default"},
		Data: map[string]string{
			aiConfigKey: `{
				"Agent": {
					"Inference": {"Specialization": ["coding"]},
					"RateLimits": {"ToolCallsPerMinute": 60}
				}
			}`,
		},
	}

	cfg, err := aiConfigFromConfigMap(cm)
	require.NoError(t, err)

	// Role always forced to ai-agent.
	require.Equal(t, constants.AIAgentRole, cfg.Role)

	// Ports defaulted.
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)

	// Explicitly set values preserved.
	require.Equal(t, []string{"coding"}, cfg.Agent.Inference.Specialization)
	require.Equal(t, 60, cfg.Agent.RateLimits.ToolCallsPerMinute)
}

func TestAIConfigFromConfigMap_InvalidJSON_ReturnsError(t *testing.T) {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-mcp", Namespace: "default"},
		Data: map[string]string{
			aiConfigKey: `{not valid json`,
		},
	}

	_, err := aiConfigFromConfigMap(cm)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestDefaultAIConfig(t *testing.T) {
	cfg := defaultAIConfig()

	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.NotNil(t, cfg.Agent)
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)
}
