package v1alpha1

import (
	consul "github.com/hashicorp/consul/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&ControlPlaneRequestLimit{}, &ControlPlaneRequestLimitList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ControlPlaneRequestLimit is the Schema for the controlplanerequestlimits API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ControlPlaneRequestLimit struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ControlPlaneRequestLimitSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ControlPlaneRequestLimitList contains a list of ControlPlaneRequestLimit
type ControlPlaneRequestLimitList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ControlPlaneRequestLimit `json:"items"`
}

// ControlPlaneRequestLimitSpec defines the desired state of ControlPlaneRequestLimit
type ControlPlaneRequestLimitSpec struct {
	//limits specific to a type of call
	ACL            *consul.ReadWriteRatesConfig `json:acl",omitempty"`
	Catalog        *consul.ReadWriteRatesConfig `json:catalog",omitempty"`
	ConfigEntry    *consul.ReadWriteRatesConfig `json:configEntry",omitempty"`
	ConnectCA      *consul.ReadWriteRatesConfig `json:connectCA",omitempty"`
	Coordinate     *consul.ReadWriteRatesConfig `json:coordinate",omitempty"`
	DiscoveryChain *consul.ReadWriteRatesConfig `json:discoveryChain",omitempty"`
	Health         *consul.ReadWriteRatesConfig `json:health",omitempty"`
	Intention      *consul.ReadWriteRatesConfig `json:intention",omitempty"`
	KV             *consul.ReadWriteRatesConfig `json:kv",omitempty"`
	Tenancy        *consul.ReadWriteRatesConfig `json:tenancy",omitempty"`
	PreparedQuery  *consul.ReadWriteRatesConfig `json:perparedQuery",omitempty"`
	Session        *consul.ReadWriteRatesConfig `json:session",omitempty"`
	Txn            *consul.ReadWriteRatesConfig `json:txn",omitempty"`
}
