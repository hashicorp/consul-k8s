// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package common holds code that isn't tied to a particular CRD version or type.
package common

import (
	"time"

	mapset "github.com/deckarep/golang-set"
)

const (
	// NOTE: these are only used in consul types, they do not map to k8s kinds.

	// V1 config entries.
	ServiceDefaults          string = "servicedefaults"
	ProxyDefaults            string = "proxydefaults"
	ServiceResolver          string = "serviceresolver"
	ServiceRouter            string = "servicerouter"
	ServiceSplitter          string = "servicesplitter"
	ServiceIntentions        string = "serviceintentions"
	ExportedServices         string = "exportedservices"
	IngressGateway           string = "ingressgateway"
	TerminatingGateway       string = "terminatinggateway"
	SamenessGroup            string = "samenessgroup"
	JWTProvider              string = "jwtprovider"
	ControlPlaneRequestLimit string = "controlplanerequestlimit"
	RouteAuthFilter          string = "routeauthfilter"
	GatewayPolicy            string = "gatewaypolicy"
	Registration             string = "registration"

	// V2 resources.
	TrafficPermissions string = "trafficpermissions"
	GRPCRoute          string = "grpcroute"
	HTTPRoute          string = "httproute"
	TCPRoute           string = "tcproute"
	ProxyConfiguration string = "proxyconfiguration"
	MeshGateway        string = "meshgateway"
	APIGateway         string = "apigateway"
	GatewayClass       string = "gatewayclass"
	GatewayClassConfig string = "gatewayclassconfig"
	MeshConfiguration  string = "meshconfiguration"

	Global                 string = "global"
	Mesh                   string = "mesh"
	DefaultConsulNamespace string = "default"
	DefaultConsulPartition string = "default"
	WildcardNamespace      string = "*"

	SourceKey        string = "external-source"
	DatacenterKey    string = "consul.hashicorp.com/source-datacenter"
	MigrateEntryKey  string = "consul.hashicorp.com/migrate-entry"
	MigrateEntryTrue string = "true"
	SourceValue      string = "kubernetes"

	DefaultPartitionName = "default"
	DefaultNamespaceName = "default"
	DefaultPeerName      = "local"
)

// ConsulTenancyConfig manages settings related to Consul namespaces and partitions.
type ConsulTenancyConfig struct {
	// EnableConsulPartitions indicates that a user is running Consul Enterprise.
	EnableConsulPartitions bool
	// ConsulPartition is the Consul Partition to which this controller belongs.
	ConsulPartition string
	// EnableConsulNamespaces indicates that a user is running Consul Enterprise.
	EnableConsulNamespaces bool
	// ConsulDestinationNamespace is the name of the Consul namespace to create
	// all resources in. If EnableNSMirroring is true this is ignored.
	ConsulDestinationNamespace string
	// EnableNSMirroring causes Consul namespaces to be created to match the
	// k8s namespace of any config entry custom resource. Resources will
	// be created in the matching Consul namespace.
	EnableNSMirroring bool
	// NSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	NSMirroringPrefix string
}

// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
type K8sNamespaceConfig struct {
	// Only endpoints in the AllowK8sNamespacesSet are reconciled.
	AllowK8sNamespacesSet mapset.Set
	// Endpoints in the DenyK8sNamespacesSet are ignored.
	DenyK8sNamespacesSet mapset.Set
}

// ConsulConfig manages config to tell a pod where consul is located.
type ConsulConfig struct {
	Address    string
	GRPCPort   int
	HTTPPort   int
	APITimeout time.Duration
}
