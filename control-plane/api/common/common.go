// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package common holds code that isn't tied to a particular CRD version or type.
package common

const (
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
)
