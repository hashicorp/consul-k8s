// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&ConsulCluster{}, &ConsulClusterList{})
}

// ConsulClusterPhase describes the lifecycle phase of a ConsulCluster.
type ConsulClusterPhase string

const (
	ConsulClusterPhaseCreating  ConsulClusterPhase = "Creating"
	ConsulClusterPhaseRunning   ConsulClusterPhase = "Running"
	ConsulClusterPhaseUpgrading ConsulClusterPhase = "Upgrading"
	ConsulClusterPhaseFailed    ConsulClusterPhase = "Failed"
)

// PVCRetentionPolicyType controls whether PVCs are deleted or retained.
type PVCRetentionPolicyType string

const (
	PVCRetentionPolicyDelete PVCRetentionPolicyType = "Delete"
	PVCRetentionPolicyRetain PVCRetentionPolicyType = "Retain"
)

// ConsulClusterPVCRetentionPolicy mirrors StatefulSet's persistentVolumeClaimRetentionPolicy.
type ConsulClusterPVCRetentionPolicy struct {
	// WhenScaled controls PVC fate when the cluster is scaled down. Delete or Retain.
	WhenScaled PVCRetentionPolicyType `json:"whenScaled,omitempty"`

	// WhenDeleted controls PVC fate when the ConsulCluster CR is deleted. Delete or Retain.
	WhenDeleted PVCRetentionPolicyType `json:"whenDeleted,omitempty"`
}

// ConsulTLSSpec configures TLS for the Consul server cluster.
type ConsulTLSSpec struct {
	// Enabled enables TLS for the server cluster.
	Enabled bool `json:"enabled,omitempty"`

	// CASecretName is the name of a Kubernetes Secret containing the CA certificate
	// under the key "tls.crt".
	CASecretName string `json:"caSecretName,omitempty"`

	// ServerCertSecretName is the name of a Kubernetes Secret containing the server
	// TLS certificate and key under keys "tls.crt" and "tls.key".
	ServerCertSecretName string `json:"serverCertSecretName,omitempty"`

	// HTTPSOnly disables the plain HTTP port (8500) when TLS is enabled.
	HTTPSOnly bool `json:"httpsOnly,omitempty"`
}

// ConsulGossipSpec configures gossip encryption for the Consul server cluster.
type ConsulGossipSpec struct {
	// SecretName is the name of a Kubernetes Secret containing the gossip encryption key.
	SecretName string `json:"secretName"`

	// SecretKey is the key within the Secret that holds the gossip encryption key.
	SecretKey string `json:"secretKey,omitempty"`
}

// ConsulMetricsSpec configures Prometheus metrics exposure.
type ConsulMetricsSpec struct {
	// Enabled adds Prometheus scrape annotations to server pods.
	Enabled bool `json:"enabled,omitempty"`

	// RetentionTime is the Prometheus metrics retention time (e.g. "60s"). Defaults to "60s".
	RetentionTime string `json:"retentionTime,omitempty"`
}

// ConsulRequestLimitsSpec configures Consul's server-side rate limiting.
type ConsulRequestLimitsSpec struct {
	// Mode is the rate limiting enforcement mode: disabled, permissive, or enforce.
	Mode string `json:"mode,omitempty"`

	// ReadRate is the maximum sustained read requests per second. -1 means unlimited.
	ReadRate float64 `json:"readRate,omitempty"`

	// WriteRate is the maximum sustained write requests per second. -1 means unlimited.
	WriteRate float64 `json:"writeRate,omitempty"`
}

// ConsulLimitsSpec groups rate-limiting configuration.
type ConsulLimitsSpec struct {
	// RequestLimits configures per-request rate limits on the server.
	RequestLimits *ConsulRequestLimitsSpec `json:"requestLimits,omitempty"`
}

// ConsulExposeServiceSpec configures an optional LoadBalancer/NodePort Service
// that exposes the Consul API outside the cluster.
type ConsulExposeServiceSpec struct {
	// Enabled creates the expose service.
	Enabled bool `json:"enabled,omitempty"`

	// Type is the Kubernetes Service type: LoadBalancer or NodePort.
	Type string `json:"type,omitempty"`

	// Annotations are extra annotations for the expose service.
	Annotations map[string]string `json:"annotations,omitempty"`

	// NodePort configures per-port NodePort values when Type is NodePort.
	NodePort *ConsulExposeServiceNodePorts `json:"nodePort,omitempty"`
}

// ConsulExposeServiceNodePorts holds explicit NodePort values.
type ConsulExposeServiceNodePorts struct {
	HTTP  int32 `json:"http,omitempty"`
	HTTPS int32 `json:"https,omitempty"`
	GRPC  int32 `json:"grpc,omitempty"`
	Serf  int32 `json:"serf,omitempty"`
	RPC   int32 `json:"rpc,omitempty"`
}

// ConsulPodDisruptionBudgetSpec configures the PodDisruptionBudget for server pods.
type ConsulPodDisruptionBudgetSpec struct {
	// Enabled creates a PodDisruptionBudget for the server pods.
	Enabled bool `json:"enabled,omitempty"`

	// MaxUnavailable is the maximum number of pods that can be unavailable during disruptions.
	// Defaults to 1 when unset.
	MaxUnavailable *int `json:"maxUnavailable,omitempty"`
}

// ConsulPodPolicy configures pod-level settings for Consul server pods.
type ConsulPodPolicy struct {
	// Annotations are extra annotations to apply to server pods.
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels are extra labels to apply to server pods.
	Labels map[string]string `json:"labels,omitempty"`

	// NodeSelector constrains pod scheduling to nodes matching the selector.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity defines pod scheduling affinity rules.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations allow the pods to be scheduled onto nodes with taints.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints controls how pods are spread across topology domains.
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Resources sets CPU and memory requests/limits for the consul container.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// SecurityContext overrides the pod-level security context.
	SecurityContext *corev1.PodSecurityContext `json:"securityContext,omitempty"`

	// ContainerSecurityContext overrides the consul container's security context.
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// ExtraEnvVars are additional environment variables to set in the consul container.
	ExtraEnvVars []corev1.EnvVar `json:"extraEnvVars,omitempty"`

	// ExtraVolumes are additional volumes to attach to server pods.
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// ExtraVolumeMounts are additional volume mounts for the consul container.
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`

	// ExtraContainers are additional sidecar containers to run alongside consul.
	ExtraContainers []corev1.Container `json:"extraContainers,omitempty"`

	// ImagePullSecrets are references to secrets for pulling the consul image.
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ConsulClusterSpec defines the desired state of a ConsulCluster.
type ConsulClusterSpec struct {
	// Size is the number of server pods to run.
	Size int `json:"size"`

	// Version is the Consul version to run, e.g. "1.18.0".
	Version string `json:"version"`

	// Image overrides the default consul container image.
	// When empty the operator uses "hashicorp/consul:<version>".
	Image string `json:"image,omitempty"`

	// Paused suspends reconciliation without deleting the cluster.
	Paused bool `json:"paused,omitempty"`

	// DatacenterName is the Consul datacenter name. Defaults to "dc1".
	DatacenterName string `json:"datacenterName,omitempty"`

	// BootstrapExpect is how many servers must connect before electing a leader.
	// Defaults to Size.
	BootstrapExpect *int `json:"bootstrapExpect,omitempty"`

	// LogLevel sets the Consul agent log level (trace, debug, info, warn, error).
	LogLevel string `json:"logLevel,omitempty"`

	// Connect enables the Consul service mesh on the server agents.
	Connect bool `json:"connect,omitempty"`

	// EnableAgentDebug enables the pprof debug endpoint on server agents.
	EnableAgentDebug bool `json:"enableAgentDebug,omitempty"`

	// ExtraConfig is a raw JSON string merged into the server configuration.
	// Use this as an escape hatch for settings not exposed by the CR spec.
	ExtraConfig string `json:"extraConfig,omitempty"`

	// PriorityClassName assigns a PriorityClass to server pods.
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// ServiceAnnotations are extra annotations applied to the headless and client services.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// ServiceAccountAnnotations are extra annotations applied to the server ServiceAccount.
	ServiceAccountAnnotations map[string]string `json:"serviceAccountAnnotations,omitempty"`

	// Pod configures pod-level settings for server pods.
	Pod *ConsulPodPolicy `json:"pod,omitempty"`

	// Storage is the size of each server's PVC.
	Storage resource.Quantity `json:"storage,omitempty"`

	// StorageClassName is the storage class for server PVCs. Uses the cluster
	// default when unset.
	StorageClassName *string `json:"storageClassName,omitempty"`

	// PersistentVolumeClaimRetentionPolicy controls PVC lifecycle on scale-down
	// and cluster deletion. Defaults to Delete for both.
	PersistentVolumeClaimRetentionPolicy *ConsulClusterPVCRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// TLS configures TLS for the server cluster.
	TLS *ConsulTLSSpec `json:"tls,omitempty"`

	// GossipEncryption configures the gossip encryption key.
	GossipEncryption *ConsulGossipSpec `json:"gossipEncryption,omitempty"`

	// ExposeGossipAndRPCPorts exposes gossip and RPC ports as hostPorts.
	ExposeGossipAndRPCPorts bool `json:"exposeGossipAndRPCPorts,omitempty"`

	// Metrics configures Prometheus metrics exposure on server pods.
	Metrics *ConsulMetricsSpec `json:"metrics,omitempty"`

	// Limits configures rate limiting on the Consul server agents.
	Limits *ConsulLimitsSpec `json:"limits,omitempty"`

	// ExposeService creates an additional LoadBalancer or NodePort Service
	// that exposes the Consul API outside the cluster.
	ExposeService *ConsulExposeServiceSpec `json:"exposeService,omitempty"`

	// PodDisruptionBudget configures a PodDisruptionBudget for the server pods.
	PodDisruptionBudget *ConsulPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
}

// ConsulClusterStatus defines the observed state of a ConsulCluster.
type ConsulClusterStatus struct {
	// Phase is the current lifecycle phase: Creating | Running | Upgrading | Failed.
	Phase ConsulClusterPhase `json:"phase,omitempty"`

	// ReadyCount is the number of server pods that are ready.
	ReadyCount int `json:"readyCount,omitempty"`

	// Members lists the names of all server pods in the cluster.
	Members []string `json:"members,omitempty"`

	// CurrentVersion is the consul image version running across the cluster.
	CurrentVersion string `json:"currentVersion,omitempty"`

	// Conditions holds the latest available observations of the cluster's state.
	Conditions Conditions `json:"conditions,omitempty"`
}

// ConsulCluster manages a Consul server cluster as individual pods and PVCs.
type ConsulCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConsulClusterSpec   `json:"spec,omitempty"`
	Status ConsulClusterStatus `json:"status,omitempty"`
}

// ConsulClusterList contains a list of ConsulCluster.
type ConsulClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConsulCluster `json:"items"`
}
