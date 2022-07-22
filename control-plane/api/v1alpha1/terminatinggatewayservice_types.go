package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	// Service contains information about a service, needed for registration or de-registration.
	Service *CatalogService `json:"service,omitempty"`
}

type CatalogService struct {
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
	// ServiceID specifies to register a service. If ID is not provided, it will be defaulted to the value of the ServiceName property.
	ServiceID string `json:"serviceId,omitempty"`
	// ServiceName specifies the logical name of the service.
	ServiceName string `json:"serviceName,omitempty"`
	// ServiceAddress specifies the address of the service. If not provided, the agent's address is used as the address for the service during DNS queries.
	ServiceAddress string `json:"serviceAddress,omitempty"`
	// ServiceTags specifies a list of tags to assign to the service. These tags can be used for later filtering and are exposed via the APIs.
	ServiceTags []ServiceTag `json:"serviceTags,omitempty"`
	// SericeMeta specifies arbitrary KV metadata linked to the service instance.
	ServiceMeta map[string]string `json:"serviceMeta,omitempty"`
	// ServicePort specifies the port of the service.
	ServicePort int `json:"servicePort,omitempty"`
	// ServiceEnableTagOverride specifies to disable the anti-entropy feature for this service's tags.
	// If set to true then external agents can update this service in the catalog and modify the tags.
	ServiceEnableTagOverride bool `json:"serviceEnableTagOverride,omitempty"`
}

type ServiceTag string

// TerminatingGatewayServiceStatus defines the observed state of TerminatingGatewayService.
type TerminatingGatewayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	// ServiceInfoRefStatus shows information about the service.
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
	ServiceName string `json:"serviceInfo,omitempty"`
	PolicyName  string `json:"service,omitempty"`
}

func (tas *TerminatingGatewayService) ServiceInfo() *CatalogService {
	return tas.Spec.Service
}
func (tas *TerminatingGatewayService) ServiceInfoRef() *ServiceInfoRefStatus {
	return tas.Status.ServiceInfoRef
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
