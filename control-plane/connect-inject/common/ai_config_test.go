// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestAIConfigFromConfigMap_FullJSON(t *testing.T) {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mcp", Namespace: "default"},
		Data: map[string]string{
			AIConfigKey: `{
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

	cfg, err := AIConfigFromConfigMap(cm)
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

	cfg, err := AIConfigFromConfigMap(cm)
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
			AIConfigKey: `{
				"Agent": {
					"Inference": {"Specialization": ["coding"]},
					"RateLimits": {"ToolCallsPerMinute": 60}
				}
			}`,
		},
	}

	cfg, err := AIConfigFromConfigMap(cm)
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
			AIConfigKey: `{not valid json`,
		},
	}

	_, err := AIConfigFromConfigMap(cm)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unmarshal")
}

func TestDefaultAIConfig(t *testing.T) {
	cfg := DefaultAIConfig()

	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.NotNil(t, cfg.Agent)
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)
}

// ---------------------------------------------------------------------------
// AIConfigFromPodIF – uses kubernetes.Interface (fake clientset)
// ---------------------------------------------------------------------------

func TestAIConfigFromPodIF_NoAnnotation_ReturnsDefaults(t *testing.T) {
	cs := k8sfake.NewSimpleClientset()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "agent",
			Namespace:   "default",
			Annotations: map[string]string{}, // no MCP config annotation
		},
	}

	cfg, err := AIConfigFromPodIF(context.Background(), cs, pod)
	require.NoError(t, err)
	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromPodIF_WithConfigMap_ParsesPorts(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mcp", Namespace: "default"},
		Data: map[string]string{
			AIConfigKey: `{
				"Role": "ai-agent",
				"Agent": {
					"MCP": {"Port": 15201, "HITL": {"Port": 16201}},
					"Interceptor": {"Port": 21201}
				}
			}`,
		},
	}
	cs := k8sfake.NewSimpleClientset(cm)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationAIAgentMCPConfig: "my-mcp",
			},
		},
	}

	cfg, err := AIConfigFromPodIF(context.Background(), cs, pod)
	require.NoError(t, err)
	require.Equal(t, 15201, cfg.Agent.MCP.Port)
	require.Equal(t, 16201, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, 21201, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromPodIF_ConfigMapNotFound_ReturnsError(t *testing.T) {
	cs := k8sfake.NewSimpleClientset() // no ConfigMaps pre-loaded
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationAIAgentMCPConfig: "missing-mcp",
			},
		},
	}

	_, err := AIConfigFromPodIF(context.Background(), cs, pod)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get AI agent MCP ConfigMap")
}

// ---------------------------------------------------------------------------
// AIConfigFromPod – uses controller-runtime client.Client (fake client)
// ---------------------------------------------------------------------------

func TestAIConfigFromPod_NoAnnotation_ReturnsDefaults(t *testing.T) {
	fakeClient := ctrlfake.NewClientBuilder().Build()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "agent",
			Namespace:   "default",
			Annotations: map[string]string{},
		},
	}

	cfg, err := AIConfigFromPod(context.Background(), fakeClient, pod)
	require.NoError(t, err)
	require.Equal(t, constants.AIAgentRole, cfg.Role)
	require.Equal(t, constants.DefaultAIMCPOutboundPort, cfg.Agent.MCP.Port)
	require.Equal(t, constants.DefaultAIHITLPort, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, constants.DefaultAIInterceptorPort, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromPod_WithConfigMap_ParsesPorts(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "my-mcp", Namespace: "default"},
		Data: map[string]string{
			AIConfigKey: `{
				"Role": "ai-agent",
				"Agent": {
					"MCP": {"Port": 15301, "HITL": {"Port": 16301}},
					"Interceptor": {"Port": 21301}
				}
			}`,
		},
	}
	fakeClient := ctrlfake.NewClientBuilder().WithRuntimeObjects(cm).Build()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationAIAgentMCPConfig: "my-mcp",
			},
		},
	}

	cfg, err := AIConfigFromPod(context.Background(), fakeClient, pod)
	require.NoError(t, err)
	require.Equal(t, 15301, cfg.Agent.MCP.Port)
	require.Equal(t, 16301, cfg.Agent.MCP.HITL.Port)
	require.Equal(t, 21301, cfg.Agent.Interceptor.Port)
}

func TestAIConfigFromPod_ConfigMapNotFound_ReturnsError(t *testing.T) {
	fakeClient := ctrlfake.NewClientBuilder().Build()
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationAIAgentMCPConfig: "missing-mcp",
			},
		},
	}

	_, err := AIConfigFromPod(context.Background(), fakeClient, pod)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get AI agent MCP ConfigMap")
}
