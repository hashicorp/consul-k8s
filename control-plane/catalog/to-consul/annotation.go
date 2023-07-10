// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

const (
	// annotationServiceSync is the key of the annotation that determines
	// whether to sync the Service resource or not. If this isn't set then
	// the default based on the syncer configuration is chosen.
	annotationServiceSync = "consul.hashicorp.com/service-sync"

	// annotationServiceName is set to override the name of the service
	// registered. By default this will be the name of the Service resource.
	annotationServiceName = "consul.hashicorp.com/service-name"

	// annotationServicePort specifies the port to use as the service instance
	// port when registering a service. This can be a named port in the
	// service or an integer value.
	annotationServicePort = "consul.hashicorp.com/service-port"

	// annotationServiceTags specifies the tags for the registered service
	// instance. Multiple tags should be comma separated. Whitespace around
	// the tags is automatically trimmed.
	annotationServiceTags = "consul.hashicorp.com/service-tags"

	// annotationServiceMetaPrefix is the prefix for setting meta key/value
	// for a service. The remainder of the key is the meta key.
	annotationServiceMetaPrefix = "consul.hashicorp.com/service-meta-"

	// annotationServiceWeight is the key of the annotation that determines
	// the traffic weight of the service which is spanned over multiple k8s cluster.
	// e.g. Service `backend` in k8s cluster `A` receives 25% of the traffic
	// compared to same `backend` service in k8s cluster `B`.
	annotationServiceWeight = "consul.hashicorp.com/service-weight"
)
