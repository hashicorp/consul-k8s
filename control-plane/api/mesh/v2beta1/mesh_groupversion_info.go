// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package v2beta1 contains API Schema definitions for the consul.hashicorp.com v2beta1 API group
// +kubebuilder:object:generate=true
// +groupName=mesh.consul.hashicorp.com
package v2beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (

	// MeshGroup is a collection of mesh resources.
	MeshGroup = "mesh.consul.hashicorp.com"

	// MeshGroupVersion is group version used to register these objects.
	MeshGroupVersion = schema.GroupVersion{Group: MeshGroup, Version: "v2beta1"}

	// MeshSchemeBuilder is used to add go types to the GroupVersionKind scheme.
	MeshSchemeBuilder = &scheme.Builder{GroupVersion: MeshGroupVersion}

	// AddMeshToScheme adds the types in this group-version to the given scheme.
	AddMeshToScheme = MeshSchemeBuilder.AddToScheme
)
