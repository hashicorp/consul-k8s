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
	Service *CatalogService `json:"service,omitempty"`
}

type CatalogService struct {
	Node                     string            `json:"node,omitempty"`
	Address                  string            `json:"address,omitempty"`
	Datacenter               string            `json:"datacenter,omitempty"`
	TaggedAddresses          map[string]string `json:"taggedAddresses,omitempty"`
	NodeMeta                 map[string]string `json:"nodeMeta,omitempty"`
	ServiceID                string            `json:"serviceId,omitempty"`
	ServiceName              string            `json:"serviceName,omitempty"`
	ServiceAddress           string            `json:"serviceAddress,omitempty"`
	ServiceTags              []ServiceTag      `json:"serviceTags,omitempty"`
	ServiceMeta              map[string]string `json:"serviceMeta,omitempty"`
	ServicePort              int               `json:"servicePort,omitempty"`
	ServiceEnableTagOverride bool              `json:"serviceEnableTagOverride,omitempty"`
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
