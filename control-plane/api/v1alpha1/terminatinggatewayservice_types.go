package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&TerminatingGatewayService{}, &TerminatingGatewayServiceList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TerminatingGatewayService is the Schema for the terminatinggatewayservices API
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
	// LastReconcileTime is the last time the resource was reconciled.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty" description:"last time the resource was reconciled"`
	// ServiceInfoRefStatus shows information about the service.
	ServiceInfoRef *ServiceInfoRefStatus `json:"serviceInfoRef,omitempty"`
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
