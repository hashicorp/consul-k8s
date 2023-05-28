// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"fmt"

	mapset "github.com/deckarep/golang-set"
	"github.com/google/go-cmp/cmp"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	Translator apigateway.ResourceTranslator
	// ControllerName is the name of the controller used in determining which
	// gateways we control, also leveraged for setting route statuses.
	ControllerName string

	// Namespaces is a map of all namespaces in Kubernetes indexed by their names for looking up labels
	// for AllowedRoutes matching purposes.
	Namespaces map[string]corev1.Namespace
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
	// Pods are any pods that are part of the Gateway deployment.
	Pods []corev1.Pod
	// Service is the deployed service associated with the Gateway deployment.
	Service *corev1.Service

	// TODO: Do we need to pass in Routes that have references to a Gateway in their statuses
	// for cleanup purposes or is the below enough for record keeping?

	// ConsulGateway is the config entry we've created in Consul.
	ConsulGateway *api.APIGatewayConfigEntry
	// ConsulHTTPRoutes are a list of HTTPRouteConfigEntry objects that currently reference the
	// Gateway we've created in Consul.
	ConsulHTTPRoutes []api.HTTPRouteConfigEntry
	// ConsulTCPRoutes are a list of TCPRouteConfigEntry objects that currently reference the
	// Gateway we've created in Consul.
	ConsulTCPRoutes []api.TCPRouteConfigEntry
	// ConsulInlineCertificates is a list of certificates that have been created in Consul.
	ConsulInlineCertificates []api.InlineCertificateConfigEntry
	// GatewayServices are the services associated with the Gateway
	ConsulGatewayServices []api.CatalogService

	// Resources is a map containing all service targets to verify
	// against the routing backends.
	Resources *apigateway.ResourceMap
}

// Binder is used for generating a Snapshot of all operations that should occur both
// in Kubernetes and Consul as a result of binding routes to a Gateway.
type Binder struct {
	statusSetter           *setter
	key                    types.NamespacedName
	nonNormalizedConsulKey api.ResourceReference
	normalizedConsulKey    api.ResourceReference
	config                 BinderConfig
}

// NewBinder creates a Binder object with the given configuration.
func NewBinder(config BinderConfig) *Binder {
	id := client.ObjectKeyFromObject(&config.Gateway)

	return &Binder{
		config:                 config,
		statusSetter:           newSetter(config.ControllerName),
		key:                    id,
		nonNormalizedConsulKey: config.Translator.NonNormalizedConfigEntryReference(api.APIGateway, id),
		normalizedConsulKey:    config.Translator.ConfigEntryReference(api.APIGateway, id),
	}
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
	snapshot := Snapshot{}

	registrationPods := []corev1.Pod{}
	// filter out any pod that is being deleted
	for _, pod := range b.config.Pods {
		if !isDeleted(&pod) {
			registrationPods = append(registrationPods, pod)
		}
	}

	gatewayClassConfig := b.config.GatewayClassConfig

	isGatewayDeleted := b.isGatewayDeleted()

	var gatewayValidation gatewayValidationResult
	var listenerValidation listenerValidationResults

	if !isGatewayDeleted {
		var updated bool

		gatewayClassConfig, updated = serializeGatewayClassConfig(&b.config.Gateway, gatewayClassConfig)

		// we don't have a deletion but if we add a finalizer for the gateway, then just add it and return
		// otherwise try and resolve as much as possible
		if ensureFinalizer(&b.config.Gateway) || updated {
			// if we've added the finalizer or serialized the class config, then update
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
			return snapshot
		}

		fmt.Println("VALIDATING")
		// calculate the status for the gateway
		gatewayValidation = validateGateway(b.config.Gateway, registrationPods, b.config.ConsulGateway)
		listenerValidation = validateListeners(b.config.Gateway.Namespace, b.config.Gateway.Spec.Listeners, b.config.Resources)
	}

	// used for tracking how many routes have successfully bound to which listeners
	// on a gateway for reporting the number of bound routes in a gateway listener's
	// status
	boundCounts := make(map[gwv1beta1.SectionName]int)

	// attempt to bind all routes

	fmt.Println("BINDING HTTPROUTES")
	for _, r := range b.config.HTTPRoutes {
		snapshot = b.bindRoute(pointerTo(r), boundCounts, snapshot)
	}
	fmt.Println("BINDING TCPROUTES")
	for _, r := range b.config.TCPRoutes {
		snapshot = b.bindRoute(pointerTo(r), boundCounts, snapshot)
	}

	// process secrets
	fmt.Println("PROCESSING SECRETS")
	gatewaySecrets := secretsForGateway(b.config.Gateway)
	certs := mapset.NewSet()
	for secret := range gatewaySecrets.Iter() {
		// ignore the error if the certificate cannot be processed and just don't add it into the final
		// sync set
		if err := b.config.Resources.TranslateInlineCertificate(secret.(types.NamespacedName)); err != nil {
			certs.Add(secret)
		}
	}

	// now cleanup any routes or certificates that we haven't already processed

	fmt.Println("CHECKING GC")
	snapshot.Consul.Deletions = b.config.Resources.ResourcesToGC(b.key)
	fmt.Println("CHECKING UPDATES")
	snapshot.Consul.Updates = b.config.Resources.Mutations()

	// finally, handle the gateway itself

	// we only want to upsert the gateway into Consul or update its status
	// if the gateway hasn't been marked for deletion
	if !isGatewayDeleted {
		fmt.Println("GATEWAY NOT DELETED!!!")

		snapshot.GatewayClassConfig = gatewayClassConfig
		snapshot.UpsertGatewayDeployment = true

		entry := b.config.Translator.ToAPIGateway(b.config.Gateway, b.config.Resources)
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, entry)

		registrations := registrationsForPods(entry.Namespace, b.config.Gateway, registrationPods)
		snapshot.Consul.Registrations = registrations

		// deregister any not explicitly registered service
		for _, service := range b.config.ConsulGatewayServices {
			found := false
			for _, registration := range registrations {
				if service.ServiceID == registration.Service.ID {
					found = true
				}
			}
			if !found {
				// we didn't register the service instance, so drop it
				snapshot.Consul.Deregistrations = append(snapshot.Consul.Deregistrations, api.CatalogDeregistration{
					Node:      service.Node,
					ServiceID: service.ServiceID,
					Namespace: service.Namespace,
				})
			}
		}

		// calculate the status for the gateway
		var status gwv1beta1.GatewayStatus
		for i, listener := range b.config.Gateway.Spec.Listeners {
			status.Listeners = append(status.Listeners, gwv1beta1.ListenerStatus{
				Name:           listener.Name,
				SupportedKinds: supportedKindsForProtocol[listener.Protocol],
				AttachedRoutes: int32(boundCounts[listener.Name]),
				Conditions:     listenerValidation.Conditions(b.config.Gateway.Generation, i),
			})
		}
		status.Conditions = gatewayValidation.Conditions(b.config.Gateway.Generation, listenerValidation.Invalid())
		status.Addresses = addressesForGateway(b.config.Service, registrationPods)

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
		snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, b.nonNormalizedConsulKey)
		for _, service := range b.config.ConsulGatewayServices {
			// deregister all gateways
			snapshot.Consul.Deregistrations = append(snapshot.Consul.Deregistrations, api.CatalogDeregistration{
				Node:      service.Node,
				ServiceID: service.ServiceID,
				Namespace: service.Namespace,
			})
		}
		if removeFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
		}
	}

	return snapshot
}

func secretsForGateway(gateway gwv1beta1.Gateway) mapset.Set {
	set := mapset.NewSet()

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil {
			continue
		}

		for _, cert := range listener.TLS.CertificateRefs {
			if apigateway.NilOrEqual(cert.Group, "") && apigateway.NilOrEqual(cert.Kind, "Secret") {
				key := apigateway.IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace)
				set.Add(key)
			}
		}
	}

	return set
}

func addressesForGateway(service *corev1.Service, pods []corev1.Pod) []gwv1beta1.GatewayAddress {
	if service == nil {
		return addressesFromPods(pods)
	}

	switch service.Spec.Type {
	case corev1.ServiceTypeLoadBalancer:
		return addressesFromLoadBalancer(service)
	case corev1.ServiceTypeClusterIP:
		return addressesFromClusterIP(service)
	case corev1.ServiceTypeNodePort:
		/* For serviceType: NodePort, there isn't a consistent way to guarantee access to the
		 * service from outside the k8s cluster. For now, we're putting the IP address of the
		 * nodes that the gateway pods are running on.
		 * The practitioner will have to understand that they may need to port forward into the
		 * cluster (in the case of Kind) or open firewall rules (in the case of GKE) in order to
		 * access the gateway from outside the cluster.
		 */
		return addressesFromPodHosts(pods)
	}

	return []gwv1beta1.GatewayAddress{}
}

func addressesFromLoadBalancer(service *corev1.Service) []gwv1beta1.GatewayAddress {
	addresses := []gwv1beta1.GatewayAddress{}

	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			addresses = append(addresses, gwv1beta1.GatewayAddress{
				Type:  pointerTo(gwv1beta1.IPAddressType),
				Value: ingress.IP,
			})
		}
		if ingress.Hostname != "" {
			addresses = append(addresses, gwv1beta1.GatewayAddress{
				Type:  pointerTo(gwv1beta1.HostnameAddressType),
				Value: ingress.Hostname,
			})
		}
	}

	return addresses
}

func addressesFromClusterIP(service *corev1.Service) []gwv1beta1.GatewayAddress {
	addresses := []gwv1beta1.GatewayAddress{}

	if service.Spec.ClusterIP != "" {
		addresses = append(addresses, gwv1beta1.GatewayAddress{
			Type:  pointerTo(gwv1beta1.IPAddressType),
			Value: service.Spec.ClusterIP,
		})
	}

	return addresses
}

func addressesFromPods(pods []corev1.Pod) []gwv1beta1.GatewayAddress {
	addresses := []gwv1beta1.GatewayAddress{}
	seenIPs := make(map[string]struct{})

	for _, pod := range pods {
		if pod.Status.PodIP != "" {
			if _, found := seenIPs[pod.Status.PodIP]; !found {
				addresses = append(addresses, gwv1beta1.GatewayAddress{
					Type:  pointerTo(gwv1beta1.IPAddressType),
					Value: pod.Status.PodIP,
				})
				seenIPs[pod.Status.PodIP] = struct{}{}
			}
		}
	}

	return addresses
}

func addressesFromPodHosts(pods []corev1.Pod) []gwv1beta1.GatewayAddress {
	addresses := []gwv1beta1.GatewayAddress{}
	seenIPs := make(map[string]struct{})

	for _, pod := range pods {
		if pod.Status.HostIP != "" {
			if _, found := seenIPs[pod.Status.HostIP]; !found {
				addresses = append(addresses, gwv1beta1.GatewayAddress{
					Type:  pointerTo(gwv1beta1.IPAddressType),
					Value: pod.Status.HostIP,
				})
				seenIPs[pod.Status.HostIP] = struct{}{}
			}
		}
	}

	return addresses
}
