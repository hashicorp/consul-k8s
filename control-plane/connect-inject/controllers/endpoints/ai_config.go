// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// aiConfigKey is the key in the MCP agent ConfigMap whose value is the JSON
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
const aiConfigKey = "ai.json"

// aiConfigFromConfigMap parses a ConfigMap produced by the operator for an AI
// agent pod and returns the equivalent api.AgentServiceAI struct. It reads the
// JSON value stored under the "ai.json" key and unmarshals it. If the key is
// absent the function returns a minimal struct with only the role set to the
// value of constants.AIAgentRole and defaults for all ports.
func aiConfigFromConfigMap(cm corev1.ConfigMap) (*api.AgentServiceAI, error) {
	raw, ok := cm.Data[aiConfigKey]
	if !ok {
		// Fallback: return a minimal valid AI config using well-known defaults.
		return defaultAIConfig(), nil
	}

	var cfg api.AgentServiceAI
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal %q from configmap %s/%s: %w",
			aiConfigKey, cm.Namespace, cm.Name, err)
	}

	// Ensure the role discriminator is always set to "ai-agent".
	cfg.Role = constants.AIAgentRole

	// Fill in port defaults for any sub-structs that were omitted.
	applyAIPortDefaults(&cfg)

	return &cfg, nil
}

// defaultAIConfig returns a minimal api.AgentServiceAI populated entirely from
// the well-known port constants. Used when the ConfigMap contains no "ai.json"
// key.
func defaultAIConfig() *api.AgentServiceAI {
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

// applyAIPortDefaults fills in zero port values with the well-known defaults so
// that Consul always receives explicit port numbers even when the ConfigMap
// omits them.
func applyAIPortDefaults(cfg *api.AgentServiceAI) {
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
