// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// InferenceModelConfigKind is the Kind string for the InferenceModelConfig CRD.
	InferenceModelConfigKind = "InferenceModelConfig"

	// McpServerConfigKind is the Kind string for the McpServerConfig CRD.
	McpServerConfigKind = "McpServerConfig"

	// AgentConfigKind is the Kind string for the AgentConfig CRD.
	AgentConfigKind = "AgentConfig"

	// InferencePoolConfigKind is the Kind string for the InferencePoolConfig CRD.
	InferencePoolConfigKind = "InferencePoolConfig"
)

func init() {
	SchemeBuilder.Register(&InferenceModelConfig{}, &InferenceModelConfigList{})
	SchemeBuilder.Register(&McpServerConfig{}, &McpServerConfigList{})
	SchemeBuilder.Register(&AgentConfig{}, &AgentConfigList{})
	SchemeBuilder.Register(&InferencePoolConfig{}, &InferencePoolConfigList{})
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

// =============================================================================
// InferencePoolConfig
// =============================================================================

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=='Ready')].status`,description="Whether the controller has successfully reconciled this resource."
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InferencePoolConfig defines a namespace-scoped pool of inference backends
// (models/upstreams) that can be referenced by AI workloads in the same
// namespace. The pool is attached to one or more parent resources (e.g. an
// InferenceModelConfig or an Envoy virtual-host) via parentRefs. The Consul
// connect-inject controller reconciles the pool state and exposes the
// validated configuration to Consul; Consul is responsible for the actual
// xDS cluster/route generation.
type InferencePoolConfig struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired pool state.
	Spec InferencePoolConfigSpec `json:"spec,omitempty"`

	// Status defines the observed state managed by the controller.
	Status InferencePoolConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolConfigSpec specifies the desired state of the InferencePoolConfig.
type InferencePoolConfigSpec struct {
	// Enabled controls whether this pool is active. When false the controller
	// marks the pool as standing-by and Consul will not advertise the backends.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// ParentRefs attaches this pool to one or more InferenceGateway resources.
	// Each entry must resolve to an InferenceGateway object (to be introduced as
	// a separate CRD and operator). The InferenceGateway operator is responsible
	// for discovering all InferencePoolConfigs that reference it and building the
	// composite routing/rate-limit configuration it enforces. The controller for
	// this resource validates that every referenced parent exists before marking
	// the pool Ready.
	// +kubebuilder:validation:MinItems=1
	ParentRefs []InferencePoolParentRef `json:"parentRefs"`

	// RateLimit defines optional token- and request-rate-limiting rules for
	// this pool. When omitted, no rate limiting is applied.
	// +optional
	RateLimit *InferencePoolRateLimit `json:"rateLimit,omitempty"`

	// Routing defines optional traffic-routing, failover, retry, and caching
	// rules for this pool. When omitted, Consul applies its built-in defaults.
	// +optional
	Routing *InferencePoolRouting `json:"routing,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolParentRef identifies the InferenceGateway to which this pool is
// attached. When the InferenceGateway CRD and its operator are introduced, the
// gateway operator will watch all InferencePoolConfigs whose parentRefs point to
// it and compose their rateLimit and routing rules into the gateway's xDS config.
type InferencePoolParentRef struct {
	// Kind is the Kind of the parent resource.
	// Expected value once the gateway CRD is available: "InferenceGateway".
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Name is the name of the parent InferenceGateway resource.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the parent InferenceGateway. When omitted
	// defaults to "default". Cross-namespace references require both resources
	// to share the same Consul admin partition.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolConfigStatus records the observed state set by the controller.
type InferencePoolConfigStatus struct {
	// Conditions describe the current conditions of the InferencePoolConfig.
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

// InferencePoolConfigList is a list of InferencePoolConfig resources.
type InferencePoolConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of InferencePoolConfigs.
	Items []InferencePoolConfig `json:"items"`
}

// =============================================================================
// InferencePoolRateLimit and supporting types
// =============================================================================

// +k8s:deepcopy-gen=true

// InferencePoolRateLimit defines token- and request-rate-limiting rules for an
// InferencePoolConfig. All fields are optional; omitting a field leaves the
// corresponding Consul default in effect.
type InferencePoolRateLimit struct {
	// Enabled controls whether rate limiting is active for this pool.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Enforcement sets the enforcement mode for rate-limit violations.
	// Typical values: "enforce" (hard-block), "shadow" (observe only).
	// +optional
	Enforcement string `json:"enforcement,omitempty"`

	// Mode selects the rate-limit algorithm (e.g. "local", "global").
	// +optional
	Mode string `json:"mode,omitempty"`

	// CountMode determines what is counted against limits.
	// Typical values: "request", "token".
	// +optional
	CountMode string `json:"countMode,omitempty"`

	// Dimensions lists the request attributes used to partition rate-limit
	// counters (e.g. ["identity", "model"]).
	// +optional
	Dimensions []string `json:"dimensions,omitempty"`

	// DegradeMode controls the degradation behaviour when the rate-limit
	// store is unavailable (e.g. "allow", "deny").
	// +optional
	DegradeMode string `json:"degradeMode,omitempty"`

	// Default is the fallback limit pair applied to requests that do not
	// match a more specific tier or model limit.
	// +optional
	Default *InferencePoolLimitPair `json:"default,omitempty"`

	// Global is an aggregate limit pair applied across all identities and
	// models for this pool.
	// +optional
	Global *InferencePoolLimitPair `json:"global,omitempty"`

	// TierLimits sets per-tier request and token limits.
	// +optional
	TierLimits []InferencePoolTierLimit `json:"tierLimits,omitempty"`

	// ModelLimits sets per-model request and token limits.
	// +optional
	ModelLimits []InferencePoolModelLimit `json:"modelLimits,omitempty"`

	// TierBindings maps SPIFFE identities to rate-limit tiers.
	// +optional
	TierBindings []InferencePoolTierBinding `json:"tierBindings,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolLimit defines a single rate-limit threshold expressed as a count
// over a time window.
type InferencePoolLimit struct {
	// Count is the maximum allowed number of requests or tokens in the window.
	// +optional
	Count int64 `json:"count,omitempty"`

	// Window is the duration of the sliding or fixed window expressed as a
	// Go duration string (e.g. "1s", "1m", "1h").
	// +optional
	Window string `json:"window,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolLimitPair bundles a request limit and a token limit together.
// Either sub-field may be omitted to leave that dimension unlimited.
type InferencePoolLimitPair struct {
	// Requests defines the request-count rate limit.
	// +optional
	Requests *InferencePoolLimit `json:"requests,omitempty"`

	// Tokens defines the token-count rate limit (for LLM token budgeting).
	// +optional
	Tokens *InferencePoolLimit `json:"tokens,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolTierLimit defines request and token limits for a named tier.
type InferencePoolTierLimit struct {
	// Tier is the name of the rate-limit tier (matches a TierBinding.Tier).
	// +optional
	Tier string `json:"tier,omitempty"`

	// Requests defines the per-tier request-count rate limit.
	// +optional
	Requests *InferencePoolLimit `json:"requests,omitempty"`

	// Tokens defines the per-tier token-count rate limit.
	// +optional
	Tokens *InferencePoolLimit `json:"tokens,omitempty"`

	// MaxCompletionTokensCap caps the max_completion_tokens field injected
	// into requests from this tier. 0 means no cap.
	// +optional
	MaxCompletionTokensCap int `json:"maxCompletionTokensCap,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolModelLimit defines request and token limits scoped to a specific
// model name.
type InferencePoolModelLimit struct {
	// Model is the LLM model identifier this limit applies to
	// (e.g. "gpt-4o", "claude-3-5-sonnet").
	// +optional
	Model string `json:"model,omitempty"`

	// Requests defines the per-model request-count rate limit.
	// +optional
	Requests *InferencePoolLimit `json:"requests,omitempty"`

	// Tokens defines the per-model token-count rate limit.
	// +optional
	Tokens *InferencePoolLimit `json:"tokens,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolTierBinding maps a set of SPIFFE identities to a rate-limit tier
// name, optionally scoped to a Consul partition and namespace.
type InferencePoolTierBinding struct {
	// Tier is the rate-limit tier name this binding grants membership of.
	// +optional
	Tier string `json:"tier,omitempty"`

	// SPIFFEIDs is the list of SPIFFE IDs whose traffic is assigned this tier.
	// +optional
	SPIFFEIDs []string `json:"spiffeIDs,omitempty"`

	// Partition scopes this binding to a Consul admin partition.
	// +optional
	Partition string `json:"partition,omitempty"`

	// Namespace scopes this binding to a Consul namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// =============================================================================
// InferencePoolRouting and supporting types
// =============================================================================

// +k8s:deepcopy-gen=true

// InferencePoolRouting defines traffic-routing, failover, retry, caching, and
// scoring rules for an InferencePoolConfig. All fields are optional.
type InferencePoolRouting struct {
	// MatchRules is an ordered list of request-matching rules that select
	// which model candidates are eligible for a given request. Rules are
	// evaluated in order; the first matching rule wins.
	// +optional
	MatchRules []InferencePoolMatchRule `json:"matchRules,omitempty"`

	// ComplianceMap maps a compliance label to its configuration. Use this
	// to restrict certain model backends to specific regulatory regimes.
	// +optional
	ComplianceMap map[string]InferencePoolCompliance `json:"complianceMap,omitempty"`

	// FallbackChain lists backend names in priority order for cross-provider
	// failover.
	// Deprecated: fallback membership and order now come from the Consul
	// catalog (each model's capabilities set + priority.<capability> meta).
	// Retained for backward compatibility; unused by rendering.
	// +optional
	FallbackChain []string `json:"fallbackChain,omitempty"`

	// Fallback tunes cross-provider failover behaviour for capability pools.
	// +optional
	Fallback *InferencePoolFallback `json:"fallback,omitempty"`

	// Retry configures automatic retry behaviour for upstream requests.
	// +optional
	Retry *InferencePoolRetry `json:"retry,omitempty"`

	// Timeout configures per-request and per-retry timeouts.
	// +optional
	Timeout *InferencePoolTimeout `json:"timeout,omitempty"`

	// Scoring tunes the backend-selection scoring model (e.g. latency-aware
	// weighted round-robin).
	// +optional
	Scoring *InferencePoolScoring `json:"scoring,omitempty"`

	// ConfigValidation sets the strictness of config validation at
	// admission time (e.g. "strict", "warn", "off").
	// +optional
	ConfigValidation string `json:"configValidation,omitempty"`

	// Budget is an open-ended map for cost/token-budget settings passed
	// directly to Consul without further validation by the controller.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Budget *apiextensionsv1.JSON `json:"budget,omitempty"`

	// Cache is an open-ended object for semantic-cache settings passed directly
	// to Consul without further validation by the controller.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Cache *apiextensionsv1.JSON `json:"cache,omitempty"`

	// Mirror is an open-ended object for traffic-mirroring settings passed
	// directly to Consul without further validation by the controller.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Mirror *apiextensionsv1.JSON `json:"mirror,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolMatchRule selects a set of model candidates for requests that
// satisfy its When predicate.
type InferencePoolMatchRule struct {
	// When defines the predicate that must be satisfied for this rule to apply.
	// +optional
	When InferencePoolMatch `json:"when,omitempty"`

	// RequireCapabilities filters candidates to only those that advertise all
	// of the listed capability labels in the Consul catalog.
	// +optional
	RequireCapabilities []string `json:"requireCapabilities,omitempty"`

	// Candidates explicitly lists backend names eligible for matching requests.
	// When empty, all pool models are candidates.
	// +optional
	Candidates []string `json:"candidates,omitempty"`

	// FallbackChain lists backend names to try (in order) when every
	// Candidate is unavailable or returns a retriable error.
	// +optional
	FallbackChain []string `json:"fallbackChain,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolMatch defines the predicate for an InferencePoolMatchRule.
type InferencePoolMatch struct {
	// Path matches requests whose URL path has the given prefix or exact value.
	// +optional
	Path string `json:"path,omitempty"`

	// BodyHas is a list of JSON pointer expressions; a request matches only
	// when all listed fields are present and non-empty in the request body.
	// +optional
	BodyHas []string `json:"bodyHas,omitempty"`

	// Identity matches requests based on the caller's SPIFFE identity.
	// +optional
	Identity *InferencePoolIdentityMatch `json:"identity,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolIdentityMatch matches a specific Consul service identity, optionally
// scoped to a partition and namespace.
type InferencePoolIdentityMatch struct {
	// Service is the Consul service name of the calling workload.
	// +optional
	Service string `json:"service,omitempty"`

	// Partition scopes the match to a Consul admin partition.
	// +optional
	Partition string `json:"partition,omitempty"`

	// Namespace scopes the match to a Consul namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolCompliance holds the compliance configuration for a single
// regulatory regime label (e.g. "hipaa", "gdpr").
type InferencePoolCompliance struct {
	// AllowedRegions is the list of cloud regions where data may be processed
	// under this compliance regime.
	// +optional
	AllowedRegions []string `json:"allowedRegions,omitempty"`

	// DenyModels explicitly forbids specific model identifiers from being
	// served under this compliance regime.
	// +optional
	DenyModels []string `json:"denyModels,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolFallback tunes cross-provider failover behaviour for capability pools.
type InferencePoolFallback struct {
	// RetryOn is the list of conditions that trigger a failover attempt to the
	// next provider (e.g. ["5xx", "reset", "connect-failure"]).
	// +optional
	RetryOn []string `json:"retryOn,omitempty"`

	// MaxTiers is the maximum number of provider-level failover hops before
	// the request is failed.
	// +optional
	MaxTiers int `json:"maxTiers,omitempty"`

	// PerTryTimeout is the timeout applied to each individual failover attempt
	// expressed as a Go duration string (e.g. "30s").
	// +optional
	PerTryTimeout string `json:"perTryTimeout,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolRetry configures automatic retry behaviour for upstream requests.
type InferencePoolRetry struct {
	// MaxAttempts is the maximum number of retry attempts per request.
	// +optional
	MaxAttempts int `json:"maxAttempts,omitempty"`

	// RetryOn is the list of conditions that trigger a retry attempt
	// (e.g. ["5xx", "reset", "connect-failure"]).
	// +optional
	RetryOn []string `json:"retryOn,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolTimeout configures connection and request timeouts for pool traffic.
type InferencePoolTimeout struct {
	// Connect is the timeout for establishing a connection to the upstream,
	// expressed as a Go duration string (e.g. "5s").
	// +optional
	Connect string `json:"connect,omitempty"`

	// Request is the end-to-end timeout for a complete request/response cycle
	// expressed as a Go duration string (e.g. "120s").
	// +optional
	Request string `json:"request,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolScoring tunes the backend-selection scoring model used by Consul
// when choosing between candidates.
type InferencePoolScoring struct {
	// Scorers is the ordered list of scoring strategies to apply
	// (e.g. ["latency-aware", "round-robin"]).
	// +optional
	Scorers []string `json:"scorers,omitempty"`

	// WeightedSplit defines an explicit weighted split across backend clusters.
	// When set, traffic is distributed according to the declared weights rather
	// than scorer output.
	// +optional
	WeightedSplit []InferencePoolWeightedTarget `json:"weightedSplit,omitempty"`
}

// +k8s:deepcopy-gen=true

// InferencePoolWeightedTarget declares a backend cluster and its relative
// traffic weight for use in a weighted split routing strategy.
type InferencePoolWeightedTarget struct {
	// Cluster is the Consul upstream service name for this target.
	Cluster string `json:"cluster"`

	// Weight is the relative traffic weight assigned to this cluster.
	Weight int `json:"weight"`
}
