// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"fmt"
	"net"
	"strconv"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// IsAIAgent returns true when the pod carries consul.hashicorp.com/ai-role=ai-agent.
// That is the CAMP gate for envelope credentials, the dataplane Local Credential
// Broker, and both OBO sidecars (matches enterprise ServiceNeedsOAuthCredential).
func IsAIAgent(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == constants.AIAgentRole
}

// IsMCPServer returns true when the pod carries ai-role: mcp-server.
// Such pods are registered in the Consul catalog with an AI.MCPServer block
// so consul-enterprise xDS injects the mcp_router filter into their Envoy listener.
// They participate in OAuth DCR but do not receive OBO sidecars or the dataplane
// credential broker (enterprise ServiceParticipatesInOAuthDCR without
// ServiceNeedsOAuthCredential).
func IsMCPServer(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == "mcp-server"
}

// AIServiceFromMCPServerPod builds the *capi.AgentServiceAI block for a pod
// annotated with ai-role: mcp-server. Values are resolved with a two-level
// precedence: pod annotation wins over McpServerConfig CRD default.
// The resulting block is stamped onto the Consul catalog registration so
// consul-enterprise xDS sees the mcp-server role and injects the mcp_router
// filter into the pod's Envoy inbound listener.
func AIServiceFromMCPServerPod(pod corev1.Pod, defaults v1alpha1.McpServerDefaults) *capi.AgentServiceAI {
	transport := pod.Annotations[constants.AnnotationAIMCPServerTransport]
	if transport == "" {
		transport = defaults.Transport
	}
	if transport == "" {
		transport = "streamable-http"
	}

	path := pod.Annotations[constants.AnnotationAIMCPServerPath]
	if path == "" {
		path = defaults.Path
	}
	if path == "" {
		path = "/mcp"
	}

	version := pod.Annotations[constants.AnnotationAIMCPServerProtocolVersion]
	if version == "" {
		version = defaults.ProtocolVersion
	}
	if version == "" {
		version = "2025-03-26"
	}

	return &capi.AgentServiceAI{
		Role: "mcp-server",
		MCPServer: &capi.AgentAIMCPServer{
			Transport:       transport,
			Path:            path,
			ProtocolVersion: version,
		},
	}
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

// AIConfigFromAgentCRD selects the AgentConfig named by the ai-agent-config
// annotation, or "consul-ai-agent" when the annotation is absent.
//
// Per-pod port and timeout annotations override the selected CRD defaults.
// The resolved values become the Consul catalog AI block and traffic exclusions.
// Remaining zero ports use built-in defaults (see ApplyAIPortDefaults).
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
		Name: configName,
	}, &agentCfg); err != nil {
		return nil, fmt.Errorf("failed to get AgentConfig %s: %w",
			configName, err)
	}

	defaults, err := ResolveAIAgentDefaults(pod, agentCfg.Spec.Defaults)
	if err != nil {
		return nil, err
	}
	return AIConfigFromAgentDefaults(defaults), nil
}

// ResolveAIAgentDefaults applies non-empty pod annotations over CRD defaults.
// Port overrides use the same unprivileged range as the AgentConfig schema.
func ResolveAIAgentDefaults(pod corev1.Pod, defaults v1alpha1.AgentDefaults) (v1alpha1.AgentDefaults, error) {
	for _, field := range []struct {
		annotation string
		port       *int32
	}{
		{constants.AnnotationAIAgentInterceptorPort, &defaults.InterceptorPort},
		{constants.AnnotationAIAgentMCPPort, &defaults.McpPort},
		{constants.AnnotationAIAgentHITLPort, &defaults.HITL.Port},
	} {
		value, err := DetermineAndValidatePort(pod, field.annotation, strconv.Itoa(int(*field.port)), false)
		if err != nil {
			return v1alpha1.AgentDefaults{}, err
		}
		port, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return v1alpha1.AgentDefaults{}, fmt.Errorf("invalid %s port: %w", field.annotation, err)
		}
		*field.port = int32(port)
	}
	if timeout := pod.Annotations[constants.AnnotationAIAgentHITLApprovalTimeout]; timeout != "" {
		defaults.HITL.ApprovalTimeout = timeout
	}
	if _, _, err := ValidateAIAgentAddress(pod, defaults.InterceptorPort); err != nil {
		return v1alpha1.AgentDefaults{}, err
	}
	return defaults, nil
}

// ValidateAIAgentAddress validates the optional MCP TCP listener override and
// returns its port. Only the port is used: the ext_proc listener is
// unauthenticated, so it always binds 127.0.0.1, which is where Envoy dials
// it. The host may be empty or loopback; any other host is rejected.
func ValidateAIAgentAddress(pod corev1.Pod, interceptorPort int32) (int32, bool, error) {
	addr := pod.Annotations[constants.AnnotationAIAgentAddr]
	if addr == "" {
		return 0, false, nil
	}
	host, portValue, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, false, fmt.Errorf(
			"invalid %s %q: must use :port or a loopback host:port",
			constants.AnnotationAIAgentAddr, addr)
	}
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsLoopback()) {
		return 0, false, fmt.Errorf(
			"invalid %s %q: host must be empty or loopback; the listener always binds 127.0.0.1",
			constants.AnnotationAIAgentAddr, addr)
	}
	port, err := strconv.ParseInt(portValue, 10, 32)
	if err != nil || port < 1024 || port > 65535 {
		return 0, false, fmt.Errorf(
			"invalid %s %q: port must be between 1024 and 65535",
			constants.AnnotationAIAgentAddr, addr)
	}
	if port == constants.DefaultOBOInboundPort || port == constants.DefaultOBOOutboundPort {
		return 0, false, fmt.Errorf(
			"invalid %s %q: port conflicts with an OBO listener",
			constants.AnnotationAIAgentAddr, addr)
	}
	if interceptorPort == 0 {
		interceptorPort = constants.DefaultAIInterceptorPort
	}
	if int32(port) != interceptorPort {
		return 0, false, fmt.Errorf(
			"invalid %s %q: port must match the resolved AI agent interceptor port %d",
			constants.AnnotationAIAgentAddr, addr, interceptorPort)
	}
	return interceptorPort, true, nil
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
