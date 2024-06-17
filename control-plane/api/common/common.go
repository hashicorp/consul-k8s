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
