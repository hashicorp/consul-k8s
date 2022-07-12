package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

func init() {
	SchemeBuilder.Register(&TerminatingGatewayService{}, &TerminatingGatewayServiceList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TerminatingGatewayService is the Schema for the terminatinggatewayservices API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
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
	Node                     string
	Address                  string
	Datacenter               string
	TaggedAddresses          map[string]string
	NodeMeta                 map[string]string
	ServiceID                string
	ServiceName              string
	ServiceAddress           string
	ServiceTags              []string
	ServiceMeta              map[string]string
	ServicePort              int
	ServiceEnableTagOverride bool
}

// TerminatingGatewayServiceStatus defines the observed state of TerminatingGatewayService.
type TerminatingGatewayServiceStatus struct {
	// Important: Run "make" to regenerate code after modifying this file
	// LatestPeeringVersion is the latest version of the resource that was reconciled.
	LatestPeeringVersion *uint64 `json:"latestPeeringVersion,omitempty"`
	// LastReconcileTime is the last time the resource was reconciled.
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty" description:"last time the resource was reconciled"`
	// ReconcileError shows any errors during the last reconciliation of this resource.
	// +optional
	ReconcileError *ReconcileErrorStatus `json:"reconcileError,omitempty"`
	// ServiceInfoRefStatus shows information about the service.
	ServiceInfoRef *ServiceInfoRefStatus `json:"serviceInfoRef,omitempty"`
}

type ServiceInfoRefStatus struct {
	ServiceName string `json:"serviceInfo,inline"`
	PolicyName  string `json:"service"`
}

func (tas *TerminatingGatewayService) ServiceInfo() *CatalogService {
	return tas.Spec.Service
}
func (tas *TerminatingGatewayService) ServiceInfoRef() *ServiceInfoRefStatus {
	return tas.Status.ServiceInfoRef
}
