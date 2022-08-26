package v1alpha1

import (
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"time"
)

const TerminatingGatewayServiceKubeKind = "terminatinggatewayservices"

func init() {
	SchemeBuilder.Register(&TerminatingGatewayService{}, &TerminatingGatewayServiceList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TerminatingGatewayService is the Schema for the terminatinggatewayservices
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type TerminatingGatewayService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerminatingGatewayServiceSpec   `json:"spec,omitempty"`
	Status TerminatingGatewayServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TerminatingGatewayServiceList contains a list of TerminatingGatewayService.
type TerminatingGatewayServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TerminatingGatewayService `json:"items"`
}

// TerminatingGatewayServiceSpec defines the desired state of TerminatingGatewayService.
type TerminatingGatewayServiceSpec struct {
	// CatalogRegistration contains information about a service, needed for registration or de-registration.
	CatalogRegistration *CatalogRegistration `json:"service,omitempty"`
}

type CatalogRegistration struct {
	// Node specifies the node ID to register.
	Node string `json:"node,omitempty"`
	// Address specifies the address to register.
	Address string `json:"address,omitempty"`
	// Datacenter specifies the datacenter, which defaults to the agent's datacenter if not provided.
	Datacenter string `json:"datacenter,omitempty"`
	// TaggedAddresses specifies the tagged addresses.
	TaggedAddresses map[string]string `json:"taggedAddresses,omitempty"`
	// NodeMeta specifies arbitrary KV metadata pairs for filtering purposes.
	NodeMeta map[string]string `json:"nodeMeta,omitempty"`
	// Service specifies information about a service.
	Service AgentService `json:"service,omitempty"`
	// Check specifies to register a check.
	Check *AgentCheck `json:"check,omitempty"`
	// Checks allows you to provide multiple checks by replacing Check with Checks and sending an array of Check objects.
	Checks HealthChecks `json:"checks,omitempty"`
	// SkipNodeUpdate specifies whether to skip updating the node's information in the registration.
	SkipNodeUpdate bool `json:"SkipNodeUpdate,omitempty"`
}

type AgentService struct {
	// ID specifies to register a service. If ID is not provided, it will be defaulted to the value of the ServiceName property.
	ID string `json:"ID,omitempty"`
	// Service specifies the logical name of the service.
	Service string `json:"service,omitempty"`
	// ServiceTags specifies a list of tags to assign to the service. These tags can be used for later filtering and are exposed via the APIs.
	Tags []ServiceTag `json:"tags,omitempty"`
	// Meta specifies arbitrary KV metadata linked to the service instance.
	Meta map[string]string `json:"meta,omitempty"`
	// Port specifies the port of the service.
	Port int `json:"port,omitempty"`
	// Address specifies the address of the service. If not provided, the agent's address is used as the address for the service during DNS queries.
	Address string `json:"address,omitempty"`
	// TaggedAddresses specifies a map of explicit LAN and WAN addresses for the service instance.
	TaggedAddresses map[string]ServiceAddress `json:"taggedAddresses,omitempty"`
	// Weights specifies weights for the service. If this field is not provided weights will default to {"Passing": 1, "Warning": 1}.
	Weights AgentWeights `json:"weights,omitempty"`
	// EnableTagOverride specifies to disable the anti-entropy feature for this service's tags.
	// If set to true then external agents can update this service in the catalog and modify the tags.
	EnableTagOverride bool `json:"enableTagOverride,omitempty"`
	// Proxy: From 1.2.3 on, specifies the configuration for a "Connect" service proxy instance.
	Proxy *AgentServiceConnectProxyConfig `json:"proxy,omitempty"`
}

type ServiceTag string

type ServiceAddress struct {
	// Address specifies the address of the service.
	Address string `json:"address,omitempty"`
	// Port specifies the port of the service.
	Port int `json:"port,omitempty"`
}
type AgentWeights struct {
	// Passing specifies the weight of the passing field for a Service's Weight.
	Passing int `json:"passing,omitempty"`
	// Warning specifies the weight of the warning field for a Service's Weight.
	Warning int `json:"warning,omitempty"`
}
type AgentCheck struct {
	// Node specifies the node associated with this AgentCheck
	Node string `json:"node,omitempty"`
	// CheckID can be omitted and will default to the value of Name.
	CheckID string `json:"checkID,omitempty"`
	// Name specifies the name of the check.
	Name string `json:"name,omitempty"`
	// Status must be one of passing, warning, or critical.
	Status string `json:"status,omitempty"`
	// Notes is an opaque field that is meant to hold human-readable text.
	Notes string `json:"notes,omitempty"`
	// Output contains information about check.
	Output string `json:"output,omitempty"`
	// ServiceID: If a ServiceID is provided that matches the ID of a service on that node, the check is treated as a service level health check
	// instead of a node level health check.
	ServiceID string `json:"serviceID,omitempty"`
	// ServiceName specifies the service associated with the check.
	ServiceName string `json:"serviceName,omitempty"`
	// Type specifies the type of the check.
	Type string `json:"type,omitempty"`
	// ExposedPort specifies the exposed port associated with the check.
	ExposedPort int `json:"exposedPort,omitempty"`
	// Definition specifies the definition associated with the check.
	Definition HealthCheckDefinition `json:"definition,omitempty"`
}

type HealthCheckDefinition struct {
	// HTTP specifies an HTTP check to perform a GET request against the value of HTTP (expected to be a URL) every Interval.
	HTTP string `json:"http,omitempty"`
	// Header specifies a set of headers that should be set for HTTP checks
	Header map[string][]string `json:"header,omitempty"`
	// Method specifies a different HTTP method to be used for an HTTP check.
	Method string `json:"method,omitempty"`
	// Body specifies a body that should be sent with HTTP checks.
	Body string `json:"body,omitempty"`
	// TLSServerName specifies an optional string used to set the SNI host when connecting via TLS.
	TLSServerName string `json:"TLSServerName,omitempty"`
	// TLSSkipVerify specifies if the certificate for an HTTPS check should not be verified.
	TLSSkipVerify bool `json:"TLSSkipVerify,omitempty"`
	// TCP specifies a TCP to connect against the value of TCP (expected to be an IP or hostname plus port combination) every Interval
	TCP string `json:"TCP,omitempty"`
	// UDP specifies the UDP associated with this HealthCheckDefinition
	UDP string `json:"UDP,omitempty"`
	// GRPC specifies a gRPC check's endpoint that supports the standard gRPC health checking protocol.
	GRPC string `json:"GRPC,omitempty"`
	// GRPCUseTLS specifies whether to use TLS for this gRPC health check.
	GRPCUseTLS bool `json:"GRPCUseTLS,omitempty"`
	// IntervalDuration specifies the frequency at which to run this check. This is required for HTTP and TCP checks.
	IntervalDuration time.Duration `json:"intervalDuration,omitempty"`
	// TimeoutDuration specifies a timeout for outgoing connections in the case of a Script, HTTP, TCP, or gRPC check.
	TimeoutDuration time.Duration `json:"timeoutDuration,omitempty"`
	// DeregisterCriticalServiceAfterDuration specifies that checks associated with a service should deregister after this time.
	DeregisterCriticalServiceAfterDuration time.Duration `json:"deregisterCriticalServiceAfterDuration,omitempty"`
	// Interval DEPRECATED in Consul 1.4.1. Use the above time.Duration fields instead.
	Interval api.ReadableDuration `json:"interval,omitempty"`
	// Timeout DEPRECATED in Consul 1.4.1. Use the above time.Duration fields instead.
	Timeout api.ReadableDuration `json:"timeout,omitempty"`
	// DeregisterCriticalServiceAfter DEPRECATED in Consul 1.4.1. Use the above time.Duration fields instead.
	DeregisterCriticalServiceAfter api.ReadableDuration `json:"DeregisterCriticalServiceAfter,omitempty"`
}
type HealthChecks []*HealthCheck

type HealthCheck struct {
	// Node specifies the node associated with this HealthCheck
	Node string `json:"node,omitempty"`
	// CheckID specifies a unique ID for this check on the node.
	// This defaults to the "Name" parameter, but it may be necessary to provide an ID for uniqueness.
	CheckID string `json:"checkID,omitempty"`
	// Name specifies the name of the check.
	Name string `json:"name,omitempty"`
	// Status specifies the initial status of the health check.
	Status string `json:"status,omitempty"`
	// Notes specifies arbitrary information for humans.
	Notes string `json:"notes,omitempty"`
	// Output contains information about check.
	Output string `json:"output,omitempty"`
	// ServiceID: If a ServiceID is provided that matches the ID of a service on that node, the check is treated as a service level health check
	// instead of a node level health check.
	ServiceID string `json:"serviceID,omitempty"`
	// ServiceName specifies the service associated with the check.
	ServiceName string `json:"serviceName,omitempty"`
	// ServiceTags specifies the serviceTags of the check.
	ServiceTags []string `json:"serviceTags,omitempty"`
	// Type specifies the type of the check.
	Type string `json:"type,omitempty"`
}
type AgentServiceConnectProxyConfig struct {
	// DestinationServiceName: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	DestinationServiceName string `json:"destinationServiceName,omitempty"`
	// DestinationServiceID: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	DestinationServiceID string `json:"destinationServiceID,omitempty"`
	// DestinationName: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	DestinationName string `json:"destinationName,omitempty"`
	// LocalServiceAddress: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	LocalServiceAddress string `json:"localServiceAddress,omitempty"`
	// LocalServicePort: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	LocalServicePort int `json:"localServicePort,omitempty"`
	// LocalServiceSocketPath: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	LocalServiceSocketPath string `json:"localServiceSocketPath,omitempty"`
	// Mode: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Mode ProxyMode `json:"mode,omitempty"`
	// TransparentProxy: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	TransparentProxy *TransparentProxyConfig `json:"transparentProxy,omitempty"`
	// Config: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Config map[string]string `json:"config,omitempty" bexpr:"-"`
	// Upstreams: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Upstreams []Upstream `json:"upstreams,omitempty"`
	// MeshGateway: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	MeshGateway MeshGatewayConfig `json:"meshGateway,omitempty"`
	// Expose: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Expose ExposeConfig `json:"expose,omitempty"`
}

type TransparentProxyConfig struct {
	// OutboundListenerPort: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	OutboundListenerPort int `json:"outboundListenerPort,omitempty" alias:"outbound_listener_port"`
	// DialedDirectly: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	DialedDirectly bool `json:"dialedDirectly,omitempty" alias:"dialed_directly"`
}

type MeshGatewayConfig struct {
	// Mode: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Mode MeshGatewayMode `json:"mode,omitempty"`
}

type ExposeConfig struct {
	// Checks: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Checks bool `json:"checks,omitempty"`
	// Paths: Managed Proxies are a deprecated method for deploying sidecar proxies, and have been removed in Consul 1.6.
	Paths []ExposePath `json:"paths,omitempty"`
}

// TerminatingGatewayServiceStatus defines the observed state of TerminatingGatewayService.
type TerminatingGatewayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	// ServiceInfoRef shows information about the service.
	ServiceInfoRef *ServiceInfoRefStatus `json:"serviceInfoRef,omitempty"`
	// Conditions indicate the latest available observations of a resource's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions Conditions `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	// LastSyncedTime is the last time the resource successfully synced with Consul.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty" description:"last time the condition transitioned from one status to another"`
}

type ServiceInfoRefStatus struct {
	// ServiceName specifies the name of the service registered by the CRD.
	ServiceName string `json:"serviceName,omitempty"`
	// ServiceName specifies the name of the policy created by the CRD.
	PolicyName string `json:"policyName,omitempty"`
}

func (tas *TerminatingGatewayService) ServiceInfo() *CatalogRegistration {
	return tas.Spec.CatalogRegistration
}
func (tas *TerminatingGatewayService) ServiceInfoRef() *ServiceInfoRefStatus {
	return tas.Status.ServiceInfoRef
}
func (tas *TerminatingGatewayService) KubeKind() string {
	return TerminatingGatewayServiceKubeKind
}
func (tas *TerminatingGatewayService) KubernetesName() string {
	return tas.ObjectMeta.Name
}
func (tas *TerminatingGatewayService) Validate() error {
	var errs field.ErrorList
	// The nil checks must return since you can't do further validations.
	if tas.Spec.CatalogRegistration == nil {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("catalogRegistration"), tas.Spec.CatalogRegistration, "catalogRegistration must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: TerminatingGatewayServiceKubeKind},
			tas.KubernetesName(), errs)
	}
	if tas.Spec.CatalogRegistration.Node == "" {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("catalogRegistration").Child("node"), tas.Spec.CatalogRegistration.Node, "node must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: TerminatingGatewayServiceKubeKind},
			tas.KubernetesName(), errs)
	}
	if tas.Spec.CatalogRegistration.Service.Service == "" {
		errs = append(errs, field.Invalid(field.NewPath("spec").Child("catalogRegistration").Child("service").Child("service"), tas.Spec.CatalogRegistration.Service.Service, "service must be specified"))
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: TerminatingGatewayServiceKubeKind},
			tas.KubernetesName(), errs)
	}
	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: TerminatingGatewayServiceKubeKind},
			tas.KubernetesName(), errs)
	}
	return nil
}
func (tas *TerminatingGatewayService) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
	tas.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}
