// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package v2beta1 contains API Schema definitions for the consul.hashicorp.com v2beta1 API group
// +kubebuilder:object:generate=true
// +groupName=catalog.consul.hashicorp.com
package v2beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (

	// CatalogGroup is a collection of catalog resources.
	CatalogGroup = "catalog.consul.hashicorp.com"

	// CatalogGroupVersion is group version used to register these objects.
	CatalogGroupVersion = schema.GroupVersion{Group: CatalogGroup, Version: "v2beta1"}

	// CatalogSchemeBuilder is used to add go types to the GroupVersionKind scheme.
	CatalogSchemeBuilder = &scheme.Builder{GroupVersion: CatalogGroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = CatalogSchemeBuilder.AddToScheme
)
