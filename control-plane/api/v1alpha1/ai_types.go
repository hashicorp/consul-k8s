// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// InferenceModelConfigKind is the Kind string for the InferenceModelConfig CRD.
	InferenceModelConfigKind = "InferenceModelConfig"

	// McpServerConfigKind is the Kind string for the McpServerConfig CRD.
	McpServerConfigKind = "McpServerConfig"

	// AgentConfigKind is the Kind string for the AgentConfig CRD.
	AgentConfigKind = "AgentConfig"

)

func init() {
	SchemeBuilder.Register(&InferenceModelConfig{}, &InferenceModelConfigList{})
	SchemeBuilder.Register(&McpServerConfig{}, &McpServerConfigList{})
	SchemeBuilder.Register(&AgentConfig{}, &AgentConfigList{})
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`,description="Whether the controller has successfully reconciled this resource."
// +kubebuilder:printcolumn:name="Protocol",type=string,JSONPath=`.spec.defaults.inferenceProtocol`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InferenceModelConfig defines the cluster-wide defaults for the AI inference
// interceptor injected by the Consul connect-inject controller. When enabled,
// every annotated pod receives an interceptor init-container that proxies
// OpenAI-compatible inference traffic before it reaches the upstream LLM.
type InferenceModelConfig struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of InferenceModelConfig.
	Spec InferenceModelConfigSpec `json:"spec,omitempty"`

	// Status defines the observed state of InferenceModelConfig.
	Status InferenceModelConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferenceModelConfigSpec specifies the desired state of the InferenceModelConfig CRD.
type InferenceModelConfigSpec struct {
	// Enabled controls whether the AI inference interceptor feature is active.
	// When false the controller takes no action on annotated pods.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Defaults contains the cluster-wide default settings applied to every
	// inference-annotated pod unless overridden at the pod level.
	Defaults InferenceModelDefaults `json:"defaults,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferenceModelDefaults holds the per-interceptor tunables that are injected
// as environment variables / arguments into the interceptor init-container.
type InferenceModelDefaults struct {
	// InterceptorPort is the TCP port the interceptor container listens on inside
	// the pod. Must not conflict with the application or Envoy proxy port (20000).
	// +kubebuilder:default=21101
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	InterceptorPort int32 `json:"interceptorPort,omitempty"`

	// InferencePath is the base URL path forwarded to the upstream LLM endpoint.
	// +kubebuilder:default="/v1"
	InferencePath string `json:"inferencePath,omitempty"`

	// InferenceProtocol is the wire protocol used to communicate with the upstream LLM.
	// +kubebuilder:validation:Enum=openai;anthropic;bedrock
	// +kubebuilder:default=openai
	InferenceProtocol string `json:"inferenceProtocol,omitempty"`

	// Resources defines the CPU/memory requests and limits for the interceptor
	// init-container injected into each annotated pod.
	// +kubebuilder:default={}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferenceModelConfigStatus records the observed state set by the controller.
type InferenceModelConfigStatus struct {
	// Conditions describe the current conditions of the InferenceModelConfig.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncedTime is the last time the controller successfully reconciled
	// this resource.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty"`
}

// +kubebuilder:object:root=true

// InferenceModelConfigList is a list of InferenceModelConfig resources.
type InferenceModelConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of InferenceModelConfigs.
	Items []InferenceModelConfig `json:"items"`
}

// =============================================================================
// McpServerConfig
// =============================================================================

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`,description="Whether the controller has successfully reconciled this resource."
// +kubebuilder:printcolumn:name="Transport",type=string,JSONPath=`.spec.defaults.transport`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// McpServerConfig defines the cluster-wide defaults for the MCP (Model Context
// Protocol) server sidecar injected by the Consul connect-inject controller.
// When enabled, every annotated pod receives an interceptor init-container that
// exposes an MCP-compatible endpoint for AI tool/context routing.
type McpServerConfig struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of McpServerConfig.
	Spec McpServerConfigSpec `json:"spec,omitempty"`

	// Status defines the observed state of McpServerConfig.
	Status McpServerConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true

// McpServerConfigSpec specifies the desired state of the McpServerConfig CRD.
type McpServerConfigSpec struct {
	// Enabled controls whether the MCP server interceptor feature is active.
	// When false the controller takes no action on annotated pods.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Defaults contains the cluster-wide default settings applied to every
	// mcp-annotated pod unless overridden at the pod level.
	Defaults McpServerDefaults `json:"defaults,omitempty"`
}

// +k8s:deepcopy-gen=true

// McpServerDefaults holds the per-interceptor tunables for the MCP server sidecar.
type McpServerDefaults struct {
	// InterceptorPort is the TCP port the MCP server container listens on inside
	// the pod. Must not conflict with the application or Envoy proxy port (20000).
	// +kubebuilder:default=21101
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	InterceptorPort int32 `json:"interceptorPort,omitempty"`

	// Transport is the MCP wire transport to use.
	// +kubebuilder:validation:Enum=streamable-http;sse;stdio
	// +kubebuilder:default="streamable-http"
	Transport string `json:"transport,omitempty"`

	// Path is the HTTP path the MCP server listens on.
	// +kubebuilder:default="/mcp"
	Path string `json:"path,omitempty"`

	// ProtocolVersion is the MCP protocol version the server advertises.
	// +kubebuilder:default="2025-03-26"
	ProtocolVersion string `json:"protocolVersion,omitempty"`

	// Resources defines the CPU/memory requests and limits for the MCP server
	// init-container injected into each annotated pod.
	// +kubebuilder:default={}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +k8s:deepcopy-gen=true

// McpServerConfigStatus records the observed state set by the controller.
type McpServerConfigStatus struct {
	// Conditions describe the current conditions of the McpServerConfig.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncedTime is the last time the controller successfully reconciled
	// this resource.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty"`
}

// +kubebuilder:object:root=true

// McpServerConfigList is a list of McpServerConfig resources.
type McpServerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of McpServerConfigs.
	Items []McpServerConfig `json:"items"`
}

// =============================================================================
// AgentConfig
// =============================================================================

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`,description="Whether the controller has successfully reconciled this resource."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// AgentConfig defines the cluster-wide defaults for the AI agent sidecar
// injected by the Consul connect-inject controller. When enabled, every
// annotated pod receives an agent init-container that handles interceptor
// proxying, MCP connectivity, and optional human-in-the-loop (HITL) approval.
type AgentConfig struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of AgentConfig.
	Spec AgentConfigSpec `json:"spec,omitempty"`

	// Status defines the observed state of AgentConfig.
	Status AgentConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true

// AgentConfigSpec specifies the desired state of the AgentConfig CRD.
type AgentConfigSpec struct {
	// Enabled controls whether the AI agent sidecar feature is active.
	// When false the controller takes no action on annotated pods.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Defaults contains the cluster-wide default settings applied to every
	// agent-annotated pod unless overridden at the pod annotation level.
	Defaults AgentDefaults `json:"defaults,omitempty"`
}

// +k8s:deepcopy-gen=true

// AgentDefaults holds the per-agent tunables injected into the agent sidecar.
type AgentDefaults struct {
	// InterceptorPort is the TCP port the agent interceptor listens on inside
	// the pod. Must not conflict with the application or Envoy proxy port (20000).
	// +kubebuilder:default=21101
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	InterceptorPort int32 `json:"interceptorPort,omitempty"`

	// McpPort is the TCP port used for MCP connectivity within the pod.
	// +kubebuilder:default=15101
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	McpPort int32 `json:"mcpPort,omitempty"`

	// HITL contains human-in-the-loop approval settings for the agent.
	HITL AgentHITL `json:"hitl,omitempty"`

	// Resources defines the CPU/memory requests and limits for the agent
	// init-container injected into each annotated pod.
	// +kubebuilder:default={}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +k8s:deepcopy-gen=true

// AgentHITL contains settings for the human-in-the-loop approval flow.
type AgentHITL struct {
	// Port is the TCP port the HITL approval server listens on inside the pod.
	// +kubebuilder:default=16101
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// ApprovalTimeout is the duration the agent waits for a human approval
	// before timing out and taking a default action.
	// +kubebuilder:default="60s"
	ApprovalTimeout string `json:"approvalTimeout,omitempty"`
}

// +k8s:deepcopy-gen=true

// AgentConfigStatus records the observed state set by the controller.
type AgentConfigStatus struct {
	// Conditions describe the current conditions of the AgentConfig.
	// +listType=map
	// +listMapKey=type
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncedTime is the last time the controller successfully reconciled
	// this resource.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty"`
}

// +kubebuilder:object:root=true

// AgentConfigList is a list of AgentConfig resources.
type AgentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of AgentConfigs.
	Items []AgentConfig `json:"items"`
}
