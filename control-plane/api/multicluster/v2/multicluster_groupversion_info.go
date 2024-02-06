// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package v2 contains API Schema definitions for the consul.hashicorp.com v2 API group
// +kubebuilder:object:generate=true
// +groupName=multicluster.consul.hashicorp.com
package v2

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (

	// MultiClusterGroup is a collection of multi-cluster resources.
	MultiClusterGroup = "multicluster.consul.hashicorp.com"

	// MultiClusterGroupVersion is group version used to register these objects.
	MultiClusterGroupVersion = schema.GroupVersion{Group: MultiClusterGroup, Version: "v2"}

	// MultiClusterSchemeBuilder is used to add go types to the GroupVersionKind scheme.
	MultiClusterSchemeBuilder = &scheme.Builder{GroupVersion: MultiClusterGroupVersion}

	// AddMultiClusterToScheme adds the types in this group-version to the given scheme.
	AddMultiClusterToScheme = MultiClusterSchemeBuilder.AddToScheme
)
