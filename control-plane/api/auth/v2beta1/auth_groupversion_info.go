// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package v2beta1 contains API Schema definitions for the consul.hashicorp.com v2beta1 API group
// +kubebuilder:object:generate=true
// +groupName=auth.consul.hashicorp.com
package v2beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (

	// AuthGroup is a collection of auth resources.
	AuthGroup = "auth.consul.hashicorp.com"

	// AuthGroupVersion is group version used to register these objects.
	AuthGroupVersion = schema.GroupVersion{Group: AuthGroup, Version: "v2beta1"}

	// AuthSchemeBuilder is used to add go types to the GroupVersionKind scheme.
	AuthSchemeBuilder = &scheme.Builder{GroupVersion: AuthGroupVersion}

	// AddAuthToScheme adds the types in this group-version to the given scheme.
	AddAuthToScheme = AuthSchemeBuilder.AddToScheme
)
