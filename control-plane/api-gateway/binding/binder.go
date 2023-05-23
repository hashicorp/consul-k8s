// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	// gatewayFinalizer is the finalizer we add to any gateway object.
	gatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	// namespaceNameLabel represents that label added automatically to namespaces in newer Kubernetes clusters.
	namespaceNameLabel = "kubernetes.io/metadata.name"
)

var (
	// constants extracted for ease of use.
	kindGateway = "Gateway"
	kindSecret  = "Secret"
	betaGroup   = gwv1beta1.GroupVersion.Group

	// the list of kinds we can support by listener protocol.
	supportedKindsForProtocol = map[gwv1beta1.ProtocolType][]gwv1beta1.RouteGroupKind{
		gwv1beta1.HTTPProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.HTTPSProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.TCPProtocolType: {{
			Group: (*gwv1alpha2.Group)(&gwv1alpha2.GroupVersion.Group),
			Kind:  "TCPRoute",
		}},
	}
)

// BinderConfig configures a binder instance with all of the information
// that it needs to know to generate a snapshot of bound state.
type BinderConfig struct {
	// Translator instance initialized with proper name/namespace translation
	// configuration from helm.
	Translator translation.K8sToConsulTranslator
	// ControllerName is the name of the controller used in determining which
	// gateways we control, also leveraged for setting route statuses.
	ControllerName string

	// GatewayClassConfig is the configuration corresponding to the given
	// GatewayClass -- if it is nil we should treat the gateway as deleted
	// since the gateway is now pointing to an invalid gateway class
	GatewayClassConfig *v1alpha1.GatewayClassConfig
	// GatewayClass is the GatewayClass corresponding to the Gateway we want to
	// bind routes to. It is passed as a pointer because it could be nil. If no
	// GatewayClass corresponds to a Gateway, we ought to clean up any sort of
	// state that we may have set on the Gateway, its corresponding Routes or in
	// Consul, because we should no longer be managing the Gateway (its association
	// to our controller is through a parameter on the GatewayClass).
	GatewayClass *gwv1beta1.GatewayClass
	// Gateway is the Gateway being reconciled that we want to bind routes to.
	Gateway gwv1beta1.Gateway
	// HTTPRoutes is a list of HTTPRoute objects that ought to be bound to the Gateway.
	HTTPRoutes []gwv1beta1.HTTPRoute
	// TCPRoutes is a list of TCPRoute objects that ought to be bound to the Gateway.
	TCPRoutes []gwv1alpha2.TCPRoute
	// Secrets is a list of Secret objects that a Gateway references.
	Secrets []corev1.Secret

	// TODO: Do we need to pass in Routes that have references to a Gateway in their statuses
	// for cleanup purposes or is the below enough for record keeping?

	// ConsulHTTPRoutes are a list of HTTPRouteConfigEntry objects that currently reference the
	// Gateway we've created in Consul.
	ConsulHTTPRoutes []api.HTTPRouteConfigEntry
	// ConsulTCPRoutes are a list of TCPRouteConfigEntry objects that currently reference the
	// Gateway we've created in Consul.
	ConsulTCPRoutes []api.TCPRouteConfigEntry
	// ConsulInlineCertificates is a list of certificates that have been created in Consul.
	ConsulInlineCertificates []api.InlineCertificateConfigEntry
	// ConnectInjectedServices is a list of all services that have been injected by our connect-injector
	// and that we can, therefore reference on the mesh.
	ConnectInjectedServices []api.CatalogService

	// Namespaces is a map of all namespaces in Kubernetes indexed by their names for looking up labels
	// for AllowedRoutes matching purposes.
	Namespaces map[string]corev1.Namespace
	// ControlledGateways is a map of all Gateway objects that we currently should be interested in. This
	// is used to determine whether we should garbage collect Certificate or Route objects when they become
	// disassociated with a particular Gateway.
	ControlledGateways map[types.NamespacedName]gwv1beta1.Gateway
}

// Binder is used for generating a Snapshot of all operations that should occur both
// in Kubernetes and Consul as a result of binding routes to a Gateway.
type Binder struct {
	statusSetter *setter
	config       BinderConfig
}

// NewBinder creates a Binder object with the given configuration.
func NewBinder(config BinderConfig) *Binder {
	return &Binder{config: config, statusSetter: newSetter(config.ControllerName)}
}

// gatewayRef returns a Consul-based reference for the given Kubernetes gateway to
// be used for marking a deletion that is needed in Consul.
func (b *Binder) gatewayRef() api.ResourceReference {
	return b.config.Translator.ReferenceForGateway(&b.config.Gateway)
}

// isGatewayDeleted returns whether we should treat the given gateway as a deleted object.
// This is true if the gateway has a deleted timestamp, if its GatewayClass does not match
// our controller name, or if the GatewayClass it references doesn't exist.
func (b *Binder) isGatewayDeleted() bool {
	gatewayClassMismatch := b.config.GatewayClass == nil || b.config.ControllerName != string(b.config.GatewayClass.Spec.ControllerName)
	isGatewayDeleted := isDeleted(&b.config.Gateway) || gatewayClassMismatch || b.config.GatewayClassConfig == nil
	return isGatewayDeleted
}

// Snapshot generates a snapshot of operations that need to occur in Kubernetes and Consul
// in order for a Gateway to be reconciled.
func (b *Binder) Snapshot() Snapshot {
	// at this point we assume all tcp routes and http routes
	// actually reference this gateway
	tracker := b.references()
	serviceMap := serviceMap(b.config.ConnectInjectedServices)
	seenRoutes := map[api.ResourceReference]struct{}{}
	snapshot := Snapshot{}
	gwcc := b.config.GatewayClassConfig

	isGatewayDeleted := b.isGatewayDeleted()
	if !isGatewayDeleted {
		var updated bool
		gwcc, updated = serializeGatewayClassConfig(&b.config.Gateway, gwcc)

		// we don't have a deletion but if we add a finalizer for the gateway, then just add it and return
		// otherwise try and resolve as much as possible
		if ensureFinalizer(&b.config.Gateway) || updated {
			// if we've added the finalizer or serialized the class config, then update
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
			return snapshot
		}
	}

	httpRouteBinder := b.newHTTPRouteBinder(tracker, serviceMap)
	tcpRouteBinder := b.newTCPRouteBinder(tracker, serviceMap)

	// used for tracking how many routes have successfully bound to which listeners
	// on a gateway for reporting the number of bound routes in a gateway listener's
	// status
	boundCounts := make(map[gwv1beta1.SectionName]int)

	// attempt to bind all routes

	for _, r := range b.config.HTTPRoutes {
		snapshot = httpRouteBinder.bind(pointerTo(r), boundCounts, seenRoutes, snapshot)
	}

	for _, r := range b.config.TCPRoutes {
		snapshot = tcpRouteBinder.bind(pointerTo(r), boundCounts, seenRoutes, snapshot)
	}

	// now cleanup any routes that we haven't already processed

	for _, r := range b.config.ConsulHTTPRoutes {
		snapshot = b.cleanHTTPRoute(pointerTo(r), seenRoutes, snapshot)
	}

	for _, r := range b.config.ConsulTCPRoutes {
		snapshot = b.cleanTCPRoute(pointerTo(r), seenRoutes, snapshot)
	}

	// process certificates

	seenCerts := make(map[types.NamespacedName]api.ResourceReference)
	for _, secret := range b.config.Secrets {
		if isGatewayDeleted {
			// we bypass the secret creation since we want to be able to GC if necessary
			continue
		}

		certificate := b.config.Translator.SecretToInlineCertificate(secret)
		certificateRef := translation.EntryToReference(&certificate)

		// mark the certificate as processed
		seenCerts[objectToMeta(&secret)] = certificateRef
		// add the certificate to the set of upsert operations needed in Consul
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &certificate)
	}

	// clean up any inline certs that are now stale and can be GC'd
	for _, cert := range b.config.ConsulInlineCertificates {
		certRef := translation.EntryToNamespacedName(&cert)
		if _, ok := seenCerts[certRef]; !ok {
			// check to see if nothing is now referencing the certificate
			if tracker.canGCSecret(certRef) {
				ref := translation.EntryToReference(&cert)
				// we can GC this now since it's not referenced by any Gateway
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, ref)
			}
		}
	}

	// we only want to upsert the gateway into Consul or update its status
	// if the gateway hasn't been marked for deletion
	if !isGatewayDeleted {
		snapshot.GatewayClassConfig = gwcc

		entry := b.config.Translator.GatewayToAPIGateway(b.config.Gateway, seenCerts)
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &entry)

		// calculate the status for the gateway
		var status gwv1beta1.GatewayStatus
		gatewayValidation := validateGateway(b.config.Gateway)
		listenerValidation := validateListeners(b.config.Gateway.Namespace, b.config.Gateway.Spec.Listeners, b.config.Secrets)
		for i, listener := range b.config.Gateway.Spec.Listeners {
			status.Listeners = append(status.Listeners, gwv1beta1.ListenerStatus{
				Name:           listener.Name,
				SupportedKinds: supportedKindsForProtocol[listener.Protocol],
				AttachedRoutes: int32(boundCounts[listener.Name]),
				Conditions:     listenerValidation.Conditions(b.config.Gateway.Generation, i),
			})
		}
		// TODO: addresses
		status.Conditions = gatewayValidation.Conditions(b.config.Gateway.Generation, listenerValidation.Invalid())
		status.Addresses = []gwv1beta1.GatewayAddress{}

		// only mark the gateway as needing a status update if there's a diff with its old
		// status, this keeps the controller from infinitely reconciling
		if !cmp.Equal(status, b.config.Gateway.Status, cmp.FilterPath(func(p cmp.Path) bool {
			path := p.String()
			return path == "Listeners.Conditions.LastTransitionTime" || path == "Conditions.LastTransitionTime"
		}, cmp.Ignore())) {
			b.config.Gateway.Status = status
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, &b.config.Gateway)
		}
	} else {
		// if the gateway has been deleted, unset whatever we've set on it
		snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, b.gatewayRef())
		if removeFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
		}
	}

	return snapshot
}

// serviceMap constructs a map of services indexed by their Kubernetes namespace and name
// from the annotations that are set on the service.
func serviceMap(services []api.CatalogService) map[types.NamespacedName]api.CatalogService {
	smap := make(map[types.NamespacedName]api.CatalogService)
	for _, service := range services {
		smap[serviceToNamespacedName(&service)] = service
	}
	return smap
}

// serviceToNamespacedName returns the Kubernetes namespace and name of a Consul catalog service
// based on the Metadata annotations written on the service.
func serviceToNamespacedName(s *api.CatalogService) types.NamespacedName {
	var (
		metaKeyKubeNS          = "k8s-namespace"
		metaKeyKubeServiceName = "k8s-service-name"
	)
	return types.NamespacedName{
		Namespace: s.ServiceMeta[metaKeyKubeNS],
		Name:      s.ServiceMeta[metaKeyKubeServiceName],
	}
}
