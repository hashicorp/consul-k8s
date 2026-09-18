// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"encoding/json"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// IsAIAgent returns true when the pod carries consul.hashicorp.com/ai-role=ai-agent.
// That is the CAMP gate for envelope credentials, the dataplane Local Credential
// Broker, and both OBO sidecars (matches enterprise ServiceNeedsOAuthCredential).
// mcp-server and inference-model pods do not get OBO inject; MCP servers are
// DCR audiences only.
func IsAIAgent(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == constants.AIAgentRole
}

// IsAIMCPServer returns true when the pod carries
// consul.hashicorp.com/ai-role=mcp-server. Those workloads register a catalog
// ai {} block for DCR audience participation but do not receive OBO inject
// (matches enterprise ServiceParticipatesInOAuthDCR without ServiceNeedsOAuthCredential).
func IsAIMCPServer(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == constants.AIMCPServerRole
}

// ServiceAIFromPod returns the catalog ai {} block for the pod's ai-role, or
// nil when the pod is not a CAMP AI workload that participates in DCR.
func ServiceAIFromPod(
	ctx context.Context,
	client client.Client,
	pod corev1.Pod,
) (*api.AgentServiceAI, error) {
	switch {
	case IsAIAgent(pod):
		return AIConfigFromPod(ctx, client, pod)
	case IsAIMCPServer(pod):
		return DefaultMCPServerAIConfig(), nil
	default:
		return nil, nil
	}
}

// AIAgentMCPConfigName returns the ConfigMap name from the MCP config annotation,
// or an empty string if the annotation is absent.
func AIAgentMCPConfigName(pod corev1.Pod) string {
	return pod.Annotations[constants.AnnotationAIAgentMCPConfig]
}

// AIConfigKey is the key in the MCP agent ConfigMap whose value is the JSON
// encoding of api.AgentServiceAI. The ConfigMap must look like:
//
//	apiVersion: v1
//	kind: ConfigMap
//	metadata:
//	  name: agent-planner-mcp
//	data:
//	  ai.json: |
//	    {
//	      "Role": "ai-agent",
//	      "Agent": {
//	        "Inference": { "Specialization": ["code-review","coding"] },
//	        "MCP": {
//	          "Port": 15101,
//	          "HITL": { "Port": 16101, "ApprovalTimeout": "60s" }
//	        },
//	        "RateLimits": { "ToolCallsPerMinute": 120, "ToolCallsPerHour": 3000 },
//	        "Interceptor": { "Port": 21101 }
//	      }
//	    }
const AIConfigKey = "ai.json"

// AIConfigFromConfigMap parses a ConfigMap produced by the operator for an AI
// agent pod and returns the equivalent api.AgentServiceAI struct. It reads the
// JSON value stored under the "ai.json" key and unmarshals it. If the key is
// absent the function returns a minimal struct with only the role set to the
// value of constants.AIAgentRole and defaults for all ports.
func AIConfigFromConfigMap(cm corev1.ConfigMap) (*api.AgentServiceAI, error) {
	raw, ok := cm.Data[AIConfigKey]
	if !ok {
		// Fallback: return a minimal valid AI config using well-known defaults.
		return DefaultAIConfig(), nil
	}

	var cfg api.AgentServiceAI
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal %q from configmap %s/%s: %w",
			AIConfigKey, cm.Namespace, cm.Name, err)
	}

	// Ensure the role discriminator is always set to "ai-agent".
	cfg.Role = constants.AIAgentRole

	// Fill in port defaults for any sub-structs that were omitted.
	ApplyAIPortDefaults(&cfg)

	return &cfg, nil
}

// DefaultAIConfig returns a minimal api.AgentServiceAI populated entirely from
// the well-known port constants. Used when the ConfigMap contains no "ai.json"
// key.
func DefaultAIConfig() *api.AgentServiceAI {
	return &api.AgentServiceAI{
		Role: constants.AIAgentRole,
		Agent: &api.AgentAIAgent{
			MCP: &api.AgentAIAgentMCP{
				Port: constants.DefaultAIMCPOutboundPort,
				HITL: &api.AgentAIAgentMCPHITL{
					Port: constants.DefaultAIHITLPort,
				},
			},
			Interceptor: &api.AgentAIAgentInterceptor{
				Port: constants.DefaultAIInterceptorPort,
			},
		},
	}
}

// DefaultMCPServerAIConfig returns a minimal valid ai.role=mcp-server block for
// catalog registration. Consul DCR and Validate require the mcp_server
// sub-block (transport + protocol_version).
func DefaultMCPServerAIConfig() *api.AgentServiceAI {
	return &api.AgentServiceAI{
		Role: constants.AIMCPServerRole,
		MCPServer: &api.AgentAIMCPServer{
			Transport:       constants.DefaultAIMCPServerTransport,
			Path:            constants.DefaultAIMCPServerPath,
			ProtocolVersion: constants.DefaultAIMCPServerProtocolVersion,
		},
	}
}

// ApplyAIPortDefaults fills in zero port values with the well-known defaults so
// that Consul always receives explicit port numbers even when the ConfigMap
// omits them.
func ApplyAIPortDefaults(cfg *api.AgentServiceAI) {
	if cfg.Agent == nil {
		cfg.Agent = &api.AgentAIAgent{}
	}
	a := cfg.Agent

	if a.MCP == nil {
		a.MCP = &api.AgentAIAgentMCP{}
	}
	if a.MCP.Port == 0 {
		a.MCP.Port = constants.DefaultAIMCPOutboundPort
	}
	if a.MCP.HITL == nil {
		a.MCP.HITL = &api.AgentAIAgentMCPHITL{}
	}
	if a.MCP.HITL.Port == 0 {
		a.MCP.HITL.Port = constants.DefaultAIHITLPort
	}

	if a.Interceptor == nil {
		a.Interceptor = &api.AgentAIAgentInterceptor{}
	}
	if a.Interceptor.Port == 0 {
		a.Interceptor.Port = constants.DefaultAIInterceptorPort
	}
}

// AIConfigFromPod fetches the MCP ConfigMap named in the pod annotation
// Defaults are applied for any absent port fields,
// so the returned struct always has non-zero port values.
func AIConfigFromPodIF(
	ctx context.Context,
	clientset kubernetes.Interface,
	pod corev1.Pod,
) (*api.AgentServiceAI, error) {
	cmName := AIAgentMCPConfigName(pod)
	if cmName == "" {
		return DefaultAIConfig(), nil
	}

	cm, err := clientset.CoreV1().ConfigMaps(pod.Namespace).Get(ctx, cmName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get AI agent MCP ConfigMap %s/%s: %w", pod.Namespace, cmName, err)
	}

	return AIConfigFromConfigMap(*cm)
}

// AIConfigFromPod fetches the MCP ConfigMap named in the pod annotation
// Defaults are applied for any absent port fields,
// so the returned struct always has non-zero port values.
func AIConfigFromPod(
	ctx context.Context,
	client client.Client,
	pod corev1.Pod,
) (*api.AgentServiceAI, error) {
	cmName := AIAgentMCPConfigName(pod)
	if cmName == "" {
		return DefaultAIConfig(), nil
	}
	var cm corev1.ConfigMap
	err := client.Get(ctx, types.NamespacedName{Name: cmName, Namespace: pod.Namespace}, &cm)
	if err != nil {
		return nil, fmt.Errorf("failed to get AI agent MCP ConfigMap %s/%s: %w", pod.Namespace, cmName, err)
	}

	return AIConfigFromConfigMap(cm)
}
