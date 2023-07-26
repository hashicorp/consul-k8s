package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&HTTPTrafficFilter{}, &HTTPTrafficFilterList{})
}

//TODO if we end up doing it this way, we would probably want an interface representing all of our filter types

//If we just have one filter with all of our settings this would be easier and we could enforce just one filter

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// HTTPTrafficFilter is the Schema for the httptrafficfilters API
type HTTPTrafficFilter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HTTPTrafficFilterSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HTTPTrafficFilterList contains a list of HTTPTrafficFilter
type HTTPTrafficFilterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HTTPTrafficFilter `json:"items"`
}

// HTTPTrafficFilterSpec defines the desired state of HTTPTrafficFilter
type HTTPTrafficFilterSpec struct {
	NumRetries int32 `json:"numRetries"`
}
