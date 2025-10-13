// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/catalog/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	"github.com/hashicorp/consul-k8s/control-plane/helper/parsetags"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// ConsulSourceKey is the key used in the meta to track the "k8s" source.
	// ConsulSourceValue is the value of the source.
	ConsulSourceKey   = "external-source"
	ConsulSourceValue = "kubernetes"

	// ConsulK8SNS is the key used in the meta to record the namespace
	// of the service/node registration.
	ConsulK8SNS           = "external-k8s-ns"
	ConsulK8SRefKind      = "external-k8s-ref-kind"
	ConsulK8SRefValue     = "external-k8s-ref-name"
	ConsulK8SNodeName     = "external-k8s-node-name"
	ConsulK8STopologyZone = "external-k8s-topology-zone"

	// consulKubernetesCheckType is the type of health check in Consul for Kubernetes readiness status.
	consulKubernetesCheckType = "kubernetes-readiness"
	// consulKubernetesCheckName is the name of health check in Consul for Kubernetes readiness status.
	consulKubernetesCheckName  = "Kubernetes Readiness Check"
	kubernetesSuccessReasonMsg = "Kubernetes health checks passing"
	kubernetesFailureReasonMsg = "Kubernetes health checks failing"
)

type NodePortSyncType string

const (
	// Only sync NodePort services with a node's ExternalIP address.
	// Doesn't sync if an ExternalIP doesn't exist.
	ExternalOnly NodePortSyncType = "ExternalOnly"

	// Sync with an ExternalIP first, if it doesn't exist, use the
	// node's InternalIP address instead.
	ExternalFirst NodePortSyncType = "ExternalFirst"

	// Sync NodePort services using.
	InternalOnly NodePortSyncType = "InternalOnly"
)

// ServiceResource implements controller.Resource to sync Service resource
// types from K8S.
type ServiceResource struct {
	Log    hclog.Logger
	Client kubernetes.Interface
	Syncer Syncer

	// Ctx is used to cancel processes kicked off by ServiceResource.
	Ctx context.Context

	// AllowK8sNamespacesSet is a set of k8s namespaces to explicitly allow for
	// syncing. It supports the special character `*` which indicates that
	// all k8s namespaces are eligible unless explicitly denied. This filter
	// is applied before checking pod annotations.
	AllowK8sNamespacesSet mapset.Set

	// DenyK8sNamespacesSet is a set of k8s namespaces to explicitly deny
	// syncing and thus service registration with Consul. An empty set
	// means that no namespaces are removed from consideration. This filter
	// takes precedence over AllowK8sNamespacesSet.
	DenyK8sNamespacesSet mapset.Set

	// ConsulK8STag is the tag value for services registered.
	ConsulK8STag string

	//ConsulServicePrefix prepends K8s services in Consul with a prefix
	ConsulServicePrefix string

	// ExplictEnable should be set to true to require explicit enabling
	// using annotations. If this is false, then services are implicitly
	// enabled (aka default enabled).
	ExplicitEnable bool

	// ClusterIPSync set to true (the default) syncs ClusterIP-type services.
	// Setting this to false will ignore ClusterIP services during the sync.
	ClusterIPSync bool

	// LoadBalancerEndpointsSync set to true (default false) will sync ServiceTypeLoadBalancer endpoints.
	LoadBalancerEndpointsSync bool

	// MetricsConfig contains metrics configuration and has methods to determine whether
	// configuration should come from the default flags or annotations. The syncCatalog uses this to configure prometheus
	// annotations.
	MetricsConfig metrics.Config

	// NodeExternalIPSync set to true (the default) syncs NodePort services
	// using the node's external ip address. When false, the node's internal
	// ip address will be used instead.
	NodePortSync NodePortSyncType

	// AddK8SNamespaceSuffix set to true appends Kubernetes namespace
	// to the service name being synced to Consul separated by a dash.
	// For example, service 'foo' in the 'default' namespace will be synced
	// as 'foo-default'.
	AddK8SNamespaceSuffix bool

	// EnableNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which is namespace aware. It enables Consul namespaces,
	// with syncing into either a single Consul namespace or mirrored from
	// k8s namespaces.
	EnableNamespaces bool

	// ConsulDestinationNamespace is the name of the Consul namespace to register all
	// synced services into if Consul namespaces are enabled and mirroring
	// is disabled. This will not be used if mirroring is enabled.
	ConsulDestinationNamespace string

	// EnableK8SNSMirroring causes Consul namespaces to be created to match the
	// organization within k8s. Services are registered into the Consul
	// namespace that mirrors their k8s namespace.
	EnableK8SNSMirroring bool

	// K8SNSMirroringPrefix is an optional prefix that can be added to the Consul
	// namespaces created while mirroring. For example, if it is set to "k8s-",
	// then the k8s `default` namespace will be mirrored in Consul's
	// `k8s-default` namespace.
	K8SNSMirroringPrefix string

	// The Consul node name to register service with.
	ConsulNodeName string

	// serviceLock must be held for any read/write to these maps.
	serviceLock sync.RWMutex

	// serviceMap holds services we should sync to Consul. Keys are the
	// in the form <kube namespace>/<kube svc name>.
	serviceMap map[string]*corev1.Service

	// endpointSlicesMap tracks EndpointSlices associated with services that are being synced to Consul.
	// The outer map's keys represent service identifiers in the same format as serviceMap and maps
	// each service to its related EndpointSlices. The inner map's keys are EndpointSlice name keys
	// the format "<kube namespace>/<kube endpointslice name>".
	endpointSlicesMap map[string]map[string]*discoveryv1.EndpointSlice

	// EnableIngress enables syncing of the hostname from an Ingress resource
	// to the service registration if an Ingress rule matches the service.
	EnableIngress bool

	// SyncLoadBalancerIPs enables syncing the IP of the Ingress LoadBalancer
	// if we do not want to sync the hostname from the Ingress resource.
	SyncLoadBalancerIPs bool

	// ingressServiceMap uses the same keys as serviceMap but maps to the ingress
	// of each service if it exists.
	ingressServiceMap map[string]map[string]string

	// serviceHostnameMap maps the name of a service to the hostName and port that
	// is provided by the Ingress resource for the service.
	serviceHostnameMap map[string]serviceAddress

	// consulMap holds the services in Consul that we've registered from kube.
	// It's populated via Consul's API and lets us diff what is actually in
	// Consul vs. what we expect to be there.
	consulMap map[string][]*consulapi.CatalogRegistration
}

type serviceAddress struct {
	hostName string
	port     int32
}

// Informer implements the controller.Resource interface.
func (t *ServiceResource) Informer() cache.SharedIndexInformer {
	// Watch all k8s namespaces. Events will be filtered out as appropriate
	// based on the allow and deny lists in the `shouldSync` function.
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Client.CoreV1().Services(metav1.NamespaceAll).List(t.Ctx, options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Client.CoreV1().Services(metav1.NamespaceAll).Watch(t.Ctx, options)
			},
		},
		&corev1.Service{},
		0,
		cache.Indexers{},
	)
}

// Upsert implements the controller.Resource interface.
func (t *ServiceResource) Upsert(key string, raw interface{}) error {
	// We expect a Service. If it isn't a service then just ignore it.
	service, ok := raw.(*corev1.Service)
	if !ok {
		t.Log.Warn("upsert got invalid type", "raw", raw)
		return nil
	}

	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()

	if t.serviceMap == nil {
		t.serviceMap = make(map[string]*corev1.Service)
	}

	if !t.shouldSync(service) {
		// Check if its in our map and delete it.
		if _, ok := t.serviceMap[key]; ok {
			t.Log.Info("service should no longer be synced", "service", key)
			t.doDelete(key)
		} else {
			t.Log.Debug("[ServiceResource.Upsert] syncing disabled for service, ignoring", "key", key)
		}
		return nil
	}

	// Syncing is enabled, let's keep track of this service.
	t.serviceMap[key] = service
	t.Log.Debug("[ServiceResource.Upsert] adding service to serviceMap", "key", key, "service", service)

	// If we care about endpoints, we should load the associated endpoint slices.
	if t.shouldTrackEndpoints(key) {
		allEndpointSlices := make(map[string]*discoveryv1.EndpointSlice)
		labelSelector := fmt.Sprintf("%s=%s", discoveryv1.LabelServiceName, service.Name)
		continueToken := ""
		limit := int64(100)

		for {
			opts := metav1.ListOptions{
				LabelSelector: labelSelector,
				Limit:         limit,
				Continue:      continueToken,
			}
			endpointSliceList, err := t.Client.DiscoveryV1().
				EndpointSlices(service.Namespace).
				List(t.Ctx, opts)

			if err != nil {
				t.Log.Warn("error loading endpoint slices list",
					"key", key,
					"err", err)
				break
			}

			for _, endpointSlice := range endpointSliceList.Items {
				endptKey := service.Namespace + "/" + endpointSlice.Name
				allEndpointSlices[endptKey] = &endpointSlice
			}

			if endpointSliceList.Continue != "" {
				continueToken = endpointSliceList.Continue
			} else {
				break
			}
		}

		if t.endpointSlicesMap == nil {
			t.endpointSlicesMap = make(map[string]map[string]*discoveryv1.EndpointSlice)
		}
		t.endpointSlicesMap[key] = allEndpointSlices
		t.Log.Debug("[ServiceResource.Upsert] adding service's endpoint slices to endpointSlicesMap", "key", key, "service", service, "endpointSlices", allEndpointSlices)
	}

	// Update the registration and trigger a sync
	t.generateRegistrations(key)
	t.sync()
	t.Log.Info("upsert", "key", key)
	return nil
}

// Delete implements the controller.Resource interface.
func (t *ServiceResource) Delete(key string, _ interface{}) error {
	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()
	t.doDelete(key)
	t.Log.Info("delete", "key", key)
	return nil
}

// doDelete is a helper function for deletion.
//
// Precondition: assumes t.serviceLock is held.
func (t *ServiceResource) doDelete(key string) {
	delete(t.serviceMap, key)
	t.Log.Debug("[doDelete] deleting service from serviceMap", "key", key)
	delete(t.endpointSlicesMap, key)
	t.Log.Debug("[doDelete] deleting endpoints from endpointSlicesMap", "key", key)
	// If there were registrations related to this service, then
	// delete them and sync.
	if _, ok := t.consulMap[key]; ok {
		delete(t.consulMap, key)
		t.sync()
	}
}

// Run implements the controller.Backgrounder interface.
func (t *ServiceResource) Run(ch <-chan struct{}) {
	t.Log.Info("starting runner for endpoints")
	// Register a controller for Endpoints which subsequently registers a
	// controller for the Ingress resource.
	(&controller.Controller{
		Resource: &serviceEndpointsResource{
			Service: t,
			Ctx:     t.Ctx,
			Log:     t.Log.Named("controller/endpoints"),
			Resource: &serviceIngressResource{
				Service:             t,
				Ctx:                 t.Ctx,
				SyncLoadBalancerIPs: t.SyncLoadBalancerIPs,
				EnableIngress:       t.EnableIngress,
			},
		},
		Log: t.Log.Named("controller/service"),
	}).Run(ch)
}

// shouldSync returns true if resyncing should be enabled for the given service.
func (t *ServiceResource) shouldSync(svc *corev1.Service) bool {
	// Namespace logic
	// If in deny list, don't sync
	if t.DenyK8sNamespacesSet.Contains(svc.Namespace) {
		t.Log.Debug("[shouldSync] service is in the deny list", "svc.Namespace", svc.Namespace, "service", svc)
		return false
	}

	// If not in allow list or allow list is not *, don't sync
	if !t.AllowK8sNamespacesSet.Contains("*") && !t.AllowK8sNamespacesSet.Contains(svc.Namespace) {
		t.Log.Debug("[shouldSync] service not in allow list", "svc.Namespace", svc.Namespace, "service", svc)
		return false
	}

	// Ignore ClusterIP services if ClusterIP sync is disabled
	if svc.Spec.Type == corev1.ServiceTypeClusterIP && !t.ClusterIPSync {
		t.Log.Debug("[shouldSync] ignoring clusterip service", "svc.Namespace", svc.Namespace, "service", svc)
		return false
	}

	raw, ok := svc.Annotations[annotationServiceSync]
	if !ok {
		// If there is no explicit value, then set it to our current default.
		return !t.ExplicitEnable
	}

	v, err := strconv.ParseBool(raw)
	if err != nil {
		t.Log.Warn("error parsing service-sync annotation",
			"service-name", t.addPrefixAndK8SNamespace(svc.Name, svc.Namespace),
			"err", err)

		// Fallback to default
		return !t.ExplicitEnable
	}

	return v
}

// shouldTrackEndpoints returns true if the endpoints for the given key
// should be tracked.
//
// Precondition: this requires the lock to be held.
func (t *ServiceResource) shouldTrackEndpoints(key string) bool {
	// The service must be one we care about for us to watch the endpoints.
	// We care about a service that exists in our service map (is enabled
	// for syncing) and is a NodePort or ClusterIP type since only those
	// types use endpoints.
	if t.serviceMap == nil {
		return false
	}
	svc, ok := t.serviceMap[key]
	if !ok {
		return false
	}

	return svc.Spec.Type == corev1.ServiceTypeNodePort ||
		svc.Spec.Type == corev1.ServiceTypeClusterIP ||
		(t.LoadBalancerEndpointsSync && svc.Spec.Type == corev1.ServiceTypeLoadBalancer)
}

// generateRegistrations generates the necessary Consul registrations for
// the given key. This is best effort: if there isn't enough information
// yet to register a service, then no registration will be generated.
//
// Precondition: the lock t.lock is held.
func (t *ServiceResource) generateRegistrations(key string) {
	// Get the service. If it doesn't exist, then we can't generate.
	svc, ok := t.serviceMap[key]
	if !ok {
		return
	}

	t.Log.Debug("[generateRegistrations] generating registration", "key", key)

	// Initialize our consul service map here if it isn't already.
	if t.consulMap == nil {
		t.consulMap = make(map[string][]*consulapi.CatalogRegistration)
	}

	// Begin by always clearing the old value out since we'll regenerate
	// a new one if there is one.
	delete(t.consulMap, key)

	// baseNode and baseService are the base that should be modified with
	// service-type specific changes. These are not pointers, they should be
	// shallow copied for each instance.
	addr := "127.0.0.1"
	if os.Getenv(constants.ConsulDualStackEnvVar) == "true" {
		addr = "::1"
	}
	baseNode := consulapi.CatalogRegistration{
		SkipNodeUpdate: true,
		Node:           t.ConsulNodeName,
		Address:        addr,
		NodeMeta: map[string]string{
			ConsulSourceKey: ConsulSourceValue,
		},
	}

	baseService := consulapi.AgentService{
		Service: t.addPrefixAndK8SNamespace(svc.Name, svc.Namespace),
		Tags:    []string{t.ConsulK8STag},
		Meta: map[string]string{
			ConsulSourceKey: ConsulSourceValue,
			ConsulK8SNS:     svc.Namespace,
		},
	}

	// If the name is explicitly annotated, adopt that name
	if v, ok := svc.Annotations[annotationServiceName]; ok {
		baseService.Service = strings.TrimSpace(v)
	}

	// Update the Consul namespace based on namespace settings
	consulNS := namespaces.ConsulNamespace(svc.Namespace,
		t.EnableNamespaces,
		t.ConsulDestinationNamespace,
		t.EnableK8SNSMirroring,
		t.K8SNSMirroringPrefix)
	if consulNS != "" {
		t.Log.Debug("[generateRegistrations] namespace being used", "key", key, "namespace", consulNS)
		baseService.Namespace = consulNS
	}

	// Determine the default port and set port annotations
	var overridePortName string
	var overridePortNumber int
	if len(svc.Spec.Ports) > 0 {

		servicePorts := make(consulapi.ServicePorts, 0)
		var defaultPort consulapi.ServicePort

		isNodePort := svc.Spec.Type == corev1.ServiceTypeNodePort

		// If a specific port is specified, then use that port value
		portAnnotation, ok := svc.Annotations[annotationServicePort]
		if ok {
			if v, err := strconv.ParseInt(portAnnotation, 0, 0); err == nil {
				port := int(v)
				defaultPort = consulapi.ServicePort{
					Port:    port,
					Name:    "default",
					Default: true,
				}
				overridePortNumber = port
			} else {
				overridePortName = portAnnotation
			}
		}

		for idx, p := range svc.Spec.Ports {
			var port int
			if overridePortName != "" && p.Name == overridePortName {
				if isNodePort && p.NodePort > 0 {
					port = int(p.NodePort)
				} else {
					port = int(p.Port)
					// NOTE: for cluster IP services we always use the endpoint
					// ports so this will be overridden.
				}
				defaultPort = consulapi.ServicePort{
					Port:    port,
					Name:    getPortName(p.Name, idx+1),
					Default: true,
				}
			} else {
				if isNodePort && p.NodePort > 0 {
					port = int(p.NodePort)
				} else {
					port = int(p.Port)
				}

				servicePorts = append(servicePorts, consulapi.ServicePort{
					Port:    port,
					Name:    getPortName(p.Name, idx+1),
					Default: false,
				})
			}
		}

		if defaultPort.Port > 0 {
			servicePorts = append(consulapi.ServicePorts{defaultPort}, servicePorts...)
		}

		// If there are no default ports, make first port as default
		if len(servicePorts) > 0 && !servicePorts.HasDefault() {
			servicePorts[0].Default = true
		}

		baseService.Ports = servicePorts

		// Add all the ports as annotations
		// We keep this for backward compatibility
		for _, p := range svc.Spec.Ports {
			// Set the tag
			baseService.Meta["port-"+p.Name] = strconv.FormatInt(int64(p.Port), 10)
		}
	}

	// Parse any additional tags
	if rawTags, ok := svc.Annotations[annotationServiceTags]; ok {
		baseService.Tags = append(baseService.Tags, parsetags.ParseTags(rawTags)...)
	}

	// Parse any additional meta
	for k, v := range svc.Annotations {
		if strings.HasPrefix(k, annotationServiceMetaPrefix) {
			k = strings.TrimPrefix(k, annotationServiceMetaPrefix)
			baseService.Meta[k] = v
		}
	}

	// Always log what we generated
	defer func() {
		t.Log.Debug("generated registration",
			"key", key,
			"service", baseService.Service,
			"namespace", baseService.Namespace,
			"instances", len(t.consulMap[key]))
	}()

	// If there are external IPs then those become the instance registrations
	// for any type of service.
	if ips := svc.Spec.ExternalIPs; len(ips) > 0 {
		for _, ip := range ips {
			r := baseNode
			rs := baseService
			r.Service = &rs
			r.Service.ID = serviceID(r.Service.Service, ip)
			r.Service.Address = ip
			// Adding information about service weight.
			// Overrides the existing weight if present.
			if weight, ok := svc.Annotations[annotationServiceWeight]; ok && weight != "" {
				weightI, err := getServiceWeight(weight)
				if err == nil {
					r.Service.Weights = consulapi.AgentWeights{
						Passing: weightI,
					}
				} else {
					t.Log.Debug("[generateRegistrations] service weight err: ", err)
				}
			}

			t.consulMap[key] = append(t.consulMap[key], &r)
		}

		return
	}

	switch svc.Spec.Type {
	// For LoadBalancer type services, we create a service instance for
	// each LoadBalancer entry. We only support entries that have an IP
	// address assigned (not hostnames).
	// If LoadBalancerEndpointsSync is true sync LB endpoints instead of loadbalancer ingress.
	case corev1.ServiceTypeLoadBalancer:
		if t.LoadBalancerEndpointsSync {
			t.registerServiceInstance(baseNode, baseService, key, overridePortName, overridePortNumber, false)
		} else {
			seen := map[string]struct{}{}
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				addr := ingress.IP
				if addr == "" {
					addr = ingress.Hostname
				}
				if addr == "" {
					continue
				}

				if _, ok = seen[addr]; ok {
					continue
				}
				seen[addr] = struct{}{}

				r := baseNode
				rs := baseService
				r.Service = &rs
				r.Service.ID = serviceID(r.Service.Service, addr)
				r.Service.Address = addr

				// Adding information about service weight.
				// Overrides the existing weight if present.
				if weight, ok := svc.Annotations[annotationServiceWeight]; ok && weight != "" {
					weightI, err := getServiceWeight(weight)
					if err == nil {
						r.Service.Weights = consulapi.AgentWeights{
							Passing: weightI,
						}
					} else {
						t.Log.Debug("[generateRegistrations] service weight err: ", err)
					}
				}

				t.consulMap[key] = append(t.consulMap[key], &r)
			}
		}

	// For NodePort services, we create a service instance for each
	// endpoint of the service, which corresponds to the nodes the service's
	// pods are running on. This way we don't register _every_ K8S
	// node as part of the service.
	case corev1.ServiceTypeNodePort:
		if t.endpointSlicesMap == nil {
			return
		}

		endpointSliceList := t.endpointSlicesMap[key]
		if endpointSliceList == nil {
			return
		}

		for _, endpointSlice := range endpointSliceList {
			for _, endpoint := range endpointSlice.Endpoints {
				// Check that the node name exists
				// subsetAddr.NodeName is of type *string
				if endpoint.NodeName == nil {
					continue
				}
				// Look up the node's ip address by getting node info
				node, err := t.Client.CoreV1().Nodes().Get(t.Ctx, *endpoint.NodeName, metav1.GetOptions{})
				if err != nil {
					t.Log.Error("error getting node info", "error", err)
					continue
				}

				// Set the expected node address type
				var expectedType corev1.NodeAddressType
				if t.NodePortSync == InternalOnly {
					expectedType = corev1.NodeInternalIP
				} else {
					expectedType = corev1.NodeExternalIP
				}

				for _, endpointAddr := range endpoint.Addresses {

					// Find the ip address for the node and
					// create the Consul service using it
					var found bool
					for _, address := range node.Status.Addresses {
						if address.Type == expectedType {
							found = true
							r := baseNode
							rs := baseService
							r.Service = &rs
							r.Service.ID = serviceID(r.Service.Service, endpointAddr)
							r.Service.Address = address.Address
							r.Service.Meta = updateServiceMeta(baseService.Meta, endpoint)
							t.consulMap[key] = append(t.consulMap[key], &r)
							// Only consider the first address that matches. In some cases
							// there will be multiple addresses like when using AWS CNI.
							// In those cases, Kubernetes will ensure eth0 is always the first
							// address in the list.
							// See https://github.com/kubernetes/kubernetes/blob/b559434c02f903dbcd46ee7d6c78b216d3f0aca0/staging/src/k8s.io/legacy-cloud-providers/aws/aws.go#L1462-L1464
							break
						}
					}

					// If an ExternalIP wasn't found, and ExternalFirst is set,
					// use an InternalIP
					if t.NodePortSync == ExternalFirst && !found {
						for _, address := range node.Status.Addresses {
							if address.Type == corev1.NodeInternalIP {
								r := baseNode
								rs := baseService
								r.Service = &rs
								r.Service.ID = serviceID(r.Service.Service, endpointAddr)
								r.Service.Address = address.Address
								r.Service.Meta = updateServiceMeta(baseService.Meta, endpoint)
								t.consulMap[key] = append(t.consulMap[key], &r)
								// Only consider the first address that matches. In some cases
								// there will be multiple addresses like when using AWS CNI.
								// In those cases, Kubernetes will ensure eth0 is always the first
								// address in the list.
								// See https://github.com/kubernetes/kubernetes/blob/b559434c02f903dbcd46ee7d6c78b216d3f0aca0/staging/src/k8s.io/legacy-cloud-providers/aws/aws.go#L1462-L1464
								break
							}
						}
					}
				}
			}
		}

	// For ClusterIP services, we register a service instance
	// for each endpoint.
	case corev1.ServiceTypeClusterIP:
		t.registerServiceInstance(baseNode, baseService, key, overridePortName, overridePortNumber, true)
	}
}

func (t *ServiceResource) registerServiceInstance(
	baseNode consulapi.CatalogRegistration,
	baseService consulapi.AgentService,
	key string,
	overridePortName string,
	overridePortNumber int,
	useHostname bool) {

	if t.endpointSlicesMap == nil {
		return
	}

	endpointSliceList := t.endpointSlicesMap[key]
	if endpointSliceList == nil {
		return
	}

	seen := map[string]struct{}{}
	for _, endpointSlice := range endpointSliceList {
		// For ClusterIP services and if LoadBalancerEndpointsSync is true, we use the endpoint port instead
		// of the service port because we're registering each endpoint
		// as a separate service instance.
		epPorts := make(consulapi.ServicePorts, 0)

		if overridePortNumber > 0 {
			// Make this as default port
			epPorts = append(epPorts, consulapi.ServicePort{
				Port:    overridePortNumber,
				Name:    "default",
				Default: true,
			})
		}

		for idx, p := range endpointSlice.Ports {
			if overridePortName != "" && p.Name != nil && overridePortName == *p.Name {
				// This will only trigger if overridePortNumber = 0 since the annotation can only have either port or name
				epPort := int(*p.Port)
				defaultPort := consulapi.ServicePort{
					Port:    epPort,
					Name:    getPortName(*p.Name, idx+1),
					Default: true,
				}

				// We keep default port as first just for convinience and consistency
				epPorts = append(consulapi.ServicePorts{defaultPort}, epPorts...)
			} else {
				epPorts = append(epPorts, consulapi.ServicePort{
					Port:    int(*p.Port),
					Name:    getPortName(*p.Name, idx+1),
					Default: false,
				})
			}
		}

		if len(epPorts) > 0 && !epPorts.HasDefault() {
			// If there is no default port, make the first one the default
			epPorts[0].Default = true
		}

		var epPort int

		for _, endpoint := range endpointSlice.Endpoints {
			for _, endpointAddr := range endpoint.Addresses {

				var addr string
				// Use the address and port from the Ingress resource if
				// ingress-sync is enabled and the service has an ingress
				// resource that references it.
				if t.EnableIngress && t.isIngressService(key) {
					addr = t.serviceHostnameMap[key].hostName
					epPort = int(t.serviceHostnameMap[key].port)
				} else {
					addr = endpointAddr
					if addr == "" && useHostname {
						addr = *endpoint.Hostname
					}
					if addr == "" {
						continue
					}
				}

				// Its not clear whether K8S guarantees ready addresses to
				// be unique so we maintain a set to prevent duplicates just
				// in case.
				if _, ok := seen[addr]; ok {
					continue
				}
				seen[addr] = struct{}{}

				r := baseNode
				rs := baseService
				r.Service = &rs
				r.Service.ID = serviceID(r.Service.Service, addr)
				r.Service.Address = addr

				// We don't support multi port for ingress sync
				if epPort > 0 {
					r.Service.Port = epPort
					// We need to reset Ports since service registration will error out if both `Port` and `Ports` are set.
					r.Service.Ports = make(consulapi.ServicePorts, 0)
				} else {
					r.Service.Ports = epPorts
					r.Service.Port = 0
				}

				r.Service.Meta = updateServiceMeta(baseService.Meta, endpoint)
				r.Check = &consulapi.AgentCheck{
					CheckID:   consulHealthCheckID(endpointSlice.Namespace, serviceID(r.Service.Service, addr)),
					Name:      consulKubernetesCheckName,
					Namespace: baseService.Namespace,
					Type:      consulKubernetesCheckType,
					ServiceID: serviceID(r.Service.Service, addr),
				}

				// Consider endpoint health state for registered consul service
				if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
					r.Check.Status = consulapi.HealthPassing
					r.Check.Output = kubernetesSuccessReasonMsg
				} else {
					r.Check.Status = consulapi.HealthCritical
					r.Check.Output = kubernetesFailureReasonMsg
				}
				t.consulMap[key] = append(t.consulMap[key], &r)
			}
		}
	}
}

// sync calls the Syncer.Sync function from the generated registrations.
//
// Precondition: lock must be held.
func (t *ServiceResource) sync() {
	// NOTE(mitchellh): This isn't the most efficient way to do this and
	// the times that sync are called are also not the most efficient. All
	// of these are implementation details so lets improve this later when
	// it becomes a performance issue and just do the easy thing first.
	rs := make([]*consulapi.CatalogRegistration, 0, len(t.consulMap)*4)
	for _, set := range t.consulMap {
		rs = append(rs, set...)
	}

	// Sync, which should be non-blocking in real-world cases
	t.Syncer.Sync(rs)
}

// serviceEndpointsResource implements controller.Resource and starts
// a background watcher on endpoints that is used by the ServiceResource
// to keep track of changing endpoints for registered services.
type serviceEndpointsResource struct {
	Service  *ServiceResource
	Ctx      context.Context
	Log      hclog.Logger
	Resource controller.Resource
}

// Run implements the controller.Backgrounder interface.
func (t *serviceEndpointsResource) Run(ch <-chan struct{}) {
	t.Log.Info("starting runner for ingress")
	(&controller.Controller{
		Log:      t.Log.Named("controller/ingress"),
		Resource: t.Resource,
	}).Run(ch)
}

func (t *serviceEndpointsResource) Informer() cache.SharedIndexInformer {
	// Watch all k8s namespaces. Events will be filtered out as appropriate in the
	// `shouldTrackEndpoints` function which checks whether the service is marked
	// to be tracked by the `shouldSync` function which uses the allow and deny
	// namespace lists.
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Service.Client.DiscoveryV1().
					EndpointSlices(metav1.NamespaceAll).
					List(t.Ctx, options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Service.Client.DiscoveryV1().
					EndpointSlices(metav1.NamespaceAll).
					Watch(t.Ctx, options)
			},
		},
		&discoveryv1.EndpointSlice{},
		0,
		cache.Indexers{},
	)
}

func (t *serviceEndpointsResource) Upsert(endptKey string, raw interface{}) error {
	svc := t.Service

	endpointSlice, ok := raw.(*discoveryv1.EndpointSlice)
	if !ok {
		svc.Log.Error("upsert got invalid type", "raw", raw)
		return nil
	}

	svc.serviceLock.Lock()
	defer svc.serviceLock.Unlock()

	// Extract service name and format the service key
	svcKey := endpointSlice.Namespace + "/" + endpointSlice.Labels[discoveryv1.LabelServiceName]

	// Check if we care about endpoints for this service
	if !svc.shouldTrackEndpoints(svcKey) {
		return nil
	}

	// We are tracking this service so let's keep track of the endpoints
	if svc.endpointSlicesMap == nil {
		svc.endpointSlicesMap = make(map[string]map[string]*discoveryv1.EndpointSlice)
	}
	if _, ok := svc.endpointSlicesMap[svcKey]; !ok {
		svc.endpointSlicesMap[svcKey] = make(map[string]*discoveryv1.EndpointSlice)
	}
	svc.endpointSlicesMap[svcKey][endptKey] = endpointSlice

	// Update the registration and trigger a sync
	svc.generateRegistrations(svcKey)
	svc.sync()
	svc.Log.Info("upsert endpoint", "key", endptKey)
	return nil
}

func (t *serviceEndpointsResource) Delete(endptKey string, raw interface{}) error {

	endpointSlice, ok := raw.(*discoveryv1.EndpointSlice)
	if !ok {
		t.Service.Log.Error("upsert got invalid type", "raw", raw)
		return nil
	}

	t.Service.serviceLock.Lock()
	defer t.Service.serviceLock.Unlock()

	// Extract service name and format key
	svcName := endpointSlice.Labels[discoveryv1.LabelServiceName]
	svcKey := endpointSlice.Namespace + "/" + svcName

	// This is a bit of an optimization. We only want to force a resync
	// if we were tracking this endpoint to begin with and that endpoint
	// had associated registrations.
	if _, ok := t.Service.endpointSlicesMap[svcKey]; ok {
		if _, ok := t.Service.endpointSlicesMap[svcKey][endptKey]; ok {
			delete(t.Service.endpointSlicesMap[svcKey], endptKey)
			if _, ok := t.Service.consulMap[svcKey]; ok {
				delete(t.Service.consulMap, svcKey)
				t.Service.sync()
			}
		}
	}

	t.Service.Log.Info("delete endpoint", "key", endptKey)
	return nil
}

// serviceIngressResource implements controller.Resource and starts
// a background watcher on ingress resources that is used by the ServiceResource
// to keep track of changing ingress for registered services.
type serviceIngressResource struct {
	Service             *ServiceResource
	Resource            controller.Resource
	Ctx                 context.Context
	EnableIngress       bool
	SyncLoadBalancerIPs bool
}

func (t *serviceIngressResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Service.Client.NetworkingV1().
					Ingresses(metav1.NamespaceAll).
					List(t.Ctx, options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Service.Client.NetworkingV1().
					Ingresses(metav1.NamespaceAll).
					Watch(t.Ctx, options)
			},
		},
		&networkingv1.Ingress{},
		0,
		cache.Indexers{},
	)
}

func (t *serviceIngressResource) Upsert(key string, raw interface{}) error {
	if !t.EnableIngress {
		return nil
	}
	svc := t.Service
	ingress, ok := raw.(*networkingv1.Ingress)
	if !ok {
		svc.Log.Warn("upsert got invalid type", "raw", raw)
		return nil
	}

	svc.serviceLock.Lock()
	defer svc.serviceLock.Unlock()

	for _, rule := range ingress.Spec.Rules {
		var svcName string
		var hostName string
		var svcPort int32
		for _, path := range rule.HTTP.Paths {
			if path.Path == "/" {
				svcName = path.Backend.Service.Name
				svcPort = 80
			} else {
				continue
			}
		}
		if svcName == "" {
			continue
		}
		if t.SyncLoadBalancerIPs {
			if len(ingress.Status.LoadBalancer.Ingress) > 0 && ingress.Status.LoadBalancer.Ingress[0].IP == "" {
				continue
			}
			hostName = ingress.Status.LoadBalancer.Ingress[0].IP
		} else {
			hostName = rule.Host
		}
		for _, ingressTLS := range ingress.Spec.TLS {
			for _, host := range ingressTLS.Hosts {
				if rule.Host == host {
					svcPort = 443
				}
			}
		}

		if svc.serviceHostnameMap == nil {
			svc.serviceHostnameMap = make(map[string]serviceAddress)
		}
		// Maintain a list of the service name to the hostname from the Ingress resource.
		svc.serviceHostnameMap[fmt.Sprintf("%s/%s", ingress.Namespace, svcName)] = serviceAddress{
			hostName: hostName,
			port:     svcPort,
		}
		if svc.ingressServiceMap == nil {
			svc.ingressServiceMap = make(map[string]map[string]string)
		}
		if svc.ingressServiceMap[key] == nil {
			svc.ingressServiceMap[key] = make(map[string]string)
		}
		// Maintain a list of all the service names that map to an Ingress resource.
		svc.ingressServiceMap[key][fmt.Sprintf("%s/%s", ingress.Namespace, svcName)] = ""
	}

	// Update the registration for each matched service and trigger a sync
	for svcName := range svc.ingressServiceMap[key] {
		svc.Log.Info(fmt.Sprintf("generating registrations for %s", svcName))
		svc.generateRegistrations(svcName)
	}
	svc.sync()
	svc.Log.Info("upsert ingress", "key", key)

	return nil
}

func (t *serviceIngressResource) Delete(key string, _ interface{}) error {
	if !t.EnableIngress {
		return nil
	}
	t.Service.serviceLock.Lock()
	defer t.Service.serviceLock.Unlock()

	// This is a bit of an optimization. We only want to force a resync
	// if we were tracking this ingress to begin with and that ingress
	// had associated registrations.
	if _, ok := t.Service.ingressServiceMap[key]; ok {
		for svcName := range t.Service.ingressServiceMap[key] {
			delete(t.Service.serviceHostnameMap, svcName)
		}
		delete(t.Service.ingressServiceMap, key)
		t.Service.sync()
	}

	t.Service.Log.Info("delete ingress", "key", key)
	return nil
}

func (t *ServiceResource) addPrefixAndK8SNamespace(name, namespace string) string {
	if t.ConsulServicePrefix != "" {
		name = fmt.Sprintf("%s%s", t.ConsulServicePrefix, name)
	}

	if t.AddK8SNamespaceSuffix {
		name = fmt.Sprintf("%s-%s", name, namespace)
	}

	return name
}

// isIngressService return if a service has an Ingress resource that references it.
func (t *ServiceResource) isIngressService(key string) bool {
	return t.serviceHostnameMap != nil && t.serviceHostnameMap[key].hostName != ""
}

// consulHealthCheckID deterministically generates a health check ID based on service ID and Kubernetes namespace.
func consulHealthCheckID(k8sNS string, serviceID string) string {
	return fmt.Sprintf("%s/%s", k8sNS, serviceID)
}

// Calculates the passing service weight.
func getServiceWeight(weight string) (int, error) {
	// error validation if the input param is a number.
	weightI, err := strconv.Atoi(weight)
	if err != nil {
		return -1, err
	}

	if weightI <= 1 {
		return -1, fmt.Errorf("expecting the service annotation %s value to be greater than 1", annotationServiceWeight)
	}

	return weightI, nil
}

// deepcopy baseService.Meta into r.Service.Meta as baseService is shared between all nodes of a service.
// update service meta with k8s topology info.
func updateServiceMeta(baseServiceMeta map[string]string, endpoint discoveryv1.Endpoint) map[string]string {

	serviceMeta := make(map[string]string)

	for k, v := range baseServiceMeta {
		serviceMeta[k] = v
	}
	if endpoint.TargetRef != nil {
		serviceMeta[ConsulK8SRefValue] = endpoint.TargetRef.Name
		serviceMeta[ConsulK8SRefKind] = endpoint.TargetRef.Kind
	}
	if endpoint.NodeName != nil {
		serviceMeta[ConsulK8SNodeName] = *endpoint.NodeName
	}
	if endpoint.Zone != nil {
		serviceMeta[ConsulK8STopologyZone] = *endpoint.Zone
	}
	return serviceMeta
}

func getPortName(name string, idx int) string {
	if strings.TrimSpace(name) == "" {
		return fmt.Sprintf("port%d", idx)
	}

	return name
}
