// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import mapset "github.com/deckarep/golang-set"

// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
type K8sNamespaceConfig struct {
	// Only endpoints in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Endpoints in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
}

// ConsulTenancyConfig manages settings related to Consul namespaces and partitions.
type ConsulTenancyConfig struct {
	// EnableConsulPartitions indicates that a user is running Consul Enterprise.
	EnableConsulPartitions bool
	// ConsulPartition is the Consul Partition to which this controller belongs.
	ConsulPartition string
	// EnableConsulNamespaces indicates that a user is running Consul Enterprise.
	EnableConsulNamespaces bool
	// ConsulDestinationNamespace is the name of the Consul namespace to create
	// all config entries in. If EnableNSMirroring is true this is ignored.
	ConsulDestinationNamespace string
	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Config entries will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool
	// NSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	NSMirroringPrefix string
}
