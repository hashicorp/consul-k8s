// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	// GatewayFinalizer is the finalizer we add to any gateway object.
	GatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	// NamespaceNameLabel represents that label added automatically to namespaces in newer Kubernetes clusters.
	NamespaceNameLabel = "kubernetes.io/metadata.name"
)

var (
	// constants extracted for ease of use.
	KindGateway = "Gateway"
	KindSecret  = "Secret"
	KindService = "Service"
	BetaGroup   = gwv1beta1.GroupVersion.Group
)

// EnsureFinalizer ensures that our finalizer is set on an object
// returning whether or not it modified the object.
func EnsureFinalizer(object client.Object) bool {
	if !object.GetDeletionTimestamp().IsZero() {
		return false
	}

	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == GatewayFinalizer {
			return false
		}
	}

	object.SetFinalizers(append(finalizers, GatewayFinalizer))
	return true
}

// RemoveFinalizer ensures that our finalizer is absent from an object
// returning whether or not it modified the object.
func RemoveFinalizer(object client.Object) bool {
	found := false
	filtered := []string{}
	for _, f := range object.GetFinalizers() {
		if f == GatewayFinalizer {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	object.SetFinalizers(filtered)
	return found
}
