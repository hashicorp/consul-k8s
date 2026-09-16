// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"fmt"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// IsAIAgent returns true when the pod carries the AI agent role annotation with
// the expected "ai-agent" value.
func IsAIAgent(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == constants.AIAgentRole
}

// IsInferenceModel returns true when the pod carries ai-role: inference-model.
// Such pods are registered in the Consul catalog with an AI.InferenceModel block
// so the InferenceGateway proxycfg discovers them as model upstreams.
func IsInferenceModel(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == "inference-model"
}

// AIServiceFromInferenceModelPod builds the *capi.AgentServiceAI block for a
// pod annotated with ai-role: inference-model. Protocol and path are read from
// the pod annotations; both fall back to safe defaults (openai, /v1) when absent.
func AIServiceFromInferenceModelPod(pod corev1.Pod) *capi.AgentServiceAI {
	protocol := pod.Annotations[constants.AnnotationAIInferenceModelProtocol]
	if protocol == "" {
		protocol = "openai"
	}
	path := pod.Annotations[constants.AnnotationAIInferenceModelPath]
	if path == "" {
		path = "/v1"
	}
	return &capi.AgentServiceAI{
		Role: "inference-model",
		InferenceModel: &capi.AgentAIInferenceModel{
			Protocol: protocol,
			Path:     path,
		},
	}
}

// AIAgentConfigName returns the AgentConfig CRD name from the pod annotation
// consul.hashicorp.com/ai-agent-config, or an empty string if the annotation
// is absent (caller should fall back to "consul-ai-agent").
func AIAgentConfigName(pod corev1.Pod) string {
	return pod.Annotations[constants.AnnotationAIAgentConfig]
}

// AIConfigFromAgentCRD resolves the AgentConfig CRD for the pod using the
// same 3-level precedence as the mesh webhook:
//
//  1. AgentConfig named by consul.hashicorp.com/ai-agent-config annotation
//  2. AgentConfig named "consul-ai-agent" — always present, installed by Helm
//
// It then converts the resolved AgentDefaults into a *capi.AgentServiceAI so
// the endpoints controller can stamp it on the Consul catalog registration.
// Port defaults are filled in for any zero values (see ApplyAIPortDefaults).
func AIConfigFromAgentCRD(
	ctx context.Context,
	k8sClient client.Client,
	pod corev1.Pod,
) (*capi.AgentServiceAI, error) {
	configName := "consul-ai-agent"
	if annotationName := AIAgentConfigName(pod); annotationName != "" {
		configName = annotationName
	}

	var agentCfg v1alpha1.AgentConfig
	if err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      configName,
		Namespace: pod.Namespace,
	}, &agentCfg); err != nil {
		return nil, fmt.Errorf("failed to get AgentConfig %s/%s: %w",
			pod.Namespace, configName, err)
	}

	return AIConfigFromAgentDefaults(agentCfg.Spec.Defaults), nil
}

// AIConfigFromAgentDefaults converts an AgentDefaults struct (from the
// AgentConfig CRD) into the *capi.AgentServiceAI that Consul expects on a
// service catalog registration. Port defaults are applied for any zero values.
func AIConfigFromAgentDefaults(defaults v1alpha1.AgentDefaults) *capi.AgentServiceAI {
	cfg := &capi.AgentServiceAI{
		Role: constants.AIAgentRole,
		Agent: &capi.AgentAIAgent{
			MCP: &capi.AgentAIAgentMCP{
				Port: int(defaults.McpPort),
				HITL: &capi.AgentAIAgentMCPHITL{
					Port:            int(defaults.HITL.Port),
					ApprovalTimeout: defaults.HITL.ApprovalTimeout,
				},
			},
			Interceptor: &capi.AgentAIAgentInterceptor{
				Port: int(defaults.InterceptorPort),
			},
		},
	}

	// Fill in zero port values with well-known constants so Consul always
	// receives explicit non-zero port numbers.
	ApplyAIPortDefaults(cfg)

	return cfg
}

// DefaultAIConfig returns a minimal *capi.AgentServiceAI populated entirely
// from the well-known port constants. Used when the AgentConfig CRD is not
// found and no per-pod annotation overrides are present.
func DefaultAIConfig() *capi.AgentServiceAI {
	return &capi.AgentServiceAI{
		Role: constants.AIAgentRole,
		Agent: &capi.AgentAIAgent{
			MCP: &capi.AgentAIAgentMCP{
				Port: constants.DefaultAIMCPPort,
				HITL: &capi.AgentAIAgentMCPHITL{
					Port: constants.DefaultAIHITLPort,
				},
			},
			Interceptor: &capi.AgentAIAgentInterceptor{
				Port: constants.DefaultAIInterceptorPort,
			},
		},
	}
}

// ApplyAIPortDefaults fills in zero port values on a *capi.AgentServiceAI
// using the well-known port constants. It is safe to call on a struct that
// was populated from CRD defaults — only zero fields are overwritten.
func ApplyAIPortDefaults(cfg *capi.AgentServiceAI) {
	if cfg.Agent == nil {
		cfg.Agent = &capi.AgentAIAgent{}
	}
	a := cfg.Agent

	if a.MCP == nil {
		a.MCP = &capi.AgentAIAgentMCP{}
	}
	if a.MCP.Port == 0 {
		a.MCP.Port = constants.DefaultAIMCPPort
	}
	if a.MCP.HITL == nil {
		a.MCP.HITL = &capi.AgentAIAgentMCPHITL{}
	}
	if a.MCP.HITL.Port == 0 {
		a.MCP.HITL.Port = constants.DefaultAIHITLPort
	}

	if a.Interceptor == nil {
		a.Interceptor = &capi.AgentAIAgentInterceptor{}
	}
	if a.Interceptor.Port == 0 {
		a.Interceptor.Port = constants.DefaultAIInterceptorPort
	}
}
