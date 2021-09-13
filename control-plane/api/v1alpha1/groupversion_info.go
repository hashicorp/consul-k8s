// Package v1alpha1 contains API Schema definitions for the consul.hashicorp.com v1alpha1 API group
// +kubebuilder:object:generate=true
// +groupName=consul.hashicorp.com
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const ConsulHashicorpGroup string = "consul.hashicorp.com"

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "consul.hashicorp.com", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
