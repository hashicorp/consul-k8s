package catalog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/helper/controller"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	apiv1 "k8s.io/api/core/v1"
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
	ConsulK8SNS = "external-k8s-ns"
)

type NodePortSyncType string

const (
	// Only sync NodePort services with a node's ExternalIP address.
	// Doesn't sync if an ExternalIP doesn't exist
	ExternalOnly NodePortSyncType = "ExternalOnly"

	// Sync with an ExternalIP first, if it doesn't exist, use the
	// node's InternalIP address instead
	ExternalFirst NodePortSyncType = "ExternalFirst"

	// Sync NodePort services using
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
	serviceMap map[string]*apiv1.Service

	// endpointsMap uses the same keys as serviceMap but maps to the endpoints
	// of each service.
	endpointsMap map[string]*apiv1.Endpoints

	// consulMap holds the services in Consul that we've registered from kube.
	// It's populated via Consul's API and lets us diff what is actually in
	// Consul vs. what we expect to be there.
	consulMap map[string][]*consulapi.CatalogRegistration
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
		&apiv1.Service{},
		0,
		cache.Indexers{},
	)
}

// Upsert implements the controller.Resource interface.
func (t *ServiceResource) Upsert(key string, raw interface{}) error {
	// We expect a Service. If it isn't a service then just ignore it.
	service, ok := raw.(*apiv1.Service)
	if !ok {
		t.Log.Warn("upsert got invalid type", "raw", raw)
		return nil
	}

	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()

	if t.serviceMap == nil {
		t.serviceMap = make(map[string]*apiv1.Service)
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

	// If we care about endpoints, we should do the initial endpoints load.
	if t.shouldTrackEndpoints(key) {
		endpoints, err := t.Client.CoreV1().
			Endpoints(service.Namespace).
			Get(t.Ctx, service.Name, metav1.GetOptions{})
		if err != nil {
			t.Log.Warn("error loading initial endpoints",
				"key", key,
				"err", err)
		} else {
			if t.endpointsMap == nil {
				t.endpointsMap = make(map[string]*apiv1.Endpoints)
			}
			t.endpointsMap[key] = endpoints
			t.Log.Debug("[ServiceResource.Upsert] adding service's endpoints to endpointsMap", "key", key, "service", service, "endpoints", endpoints)
		}
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
// Precondition: assumes t.serviceLock is held
func (t *ServiceResource) doDelete(key string) {
	delete(t.serviceMap, key)
	t.Log.Debug("[doDelete] deleting service from serviceMap", "key", key)
	delete(t.endpointsMap, key)
	t.Log.Debug("[doDelete] deleting endpoints from endpointsMap", "key", key)
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
	(&controller.Controller{
		Log:      t.Log.Named("controller/endpoints"),
		Resource: &serviceEndpointsResource{Service: t, Ctx: t.Ctx},
	}).Run(ch)
}

// shouldSync returns true if resyncing should be enabled for the given service.
func (t *ServiceResource) shouldSync(svc *apiv1.Service) bool {
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
	if svc.Spec.Type == apiv1.ServiceTypeClusterIP && !t.ClusterIPSync {
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
// Precondition: this requires the lock to be held
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

	return svc.Spec.Type == apiv1.ServiceTypeNodePort ||
		svc.Spec.Type == apiv1.ServiceTypeClusterIP ||
		(t.LoadBalancerEndpointsSync && svc.Spec.Type == apiv1.ServiceTypeLoadBalancer)
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
	baseNode := consulapi.CatalogRegistration{
		SkipNodeUpdate: true,
		Node:           t.ConsulNodeName,
		Address:        "127.0.0.1",
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
		var port int
		isNodePort := svc.Spec.Type == apiv1.ServiceTypeNodePort

		// If a specific port is specified, then use that port value
		portAnnotation, ok := svc.Annotations[annotationServicePort]
		if ok {
			if v, err := strconv.ParseInt(portAnnotation, 0, 0); err == nil {
				port = int(v)
				overridePortNumber = port
			} else {
				overridePortName = portAnnotation
			}
		}

		// For when the port was a name instead of an int
		if overridePortName != "" {
			// Find the named port
			for _, p := range svc.Spec.Ports {
				if p.Name == overridePortName {
					if isNodePort && p.NodePort > 0 {
						port = int(p.NodePort)
					} else {
						port = int(p.Port)
						// NOTE: for cluster IP services we always use the endpoint
						// ports so this will be overridden.
					}
					break
				}
			}
		}

		// If the port was not set above, set it with the first port
		// based on the service type.
		if port == 0 {
			if isNodePort {
				// Find first defined NodePort
				for _, p := range svc.Spec.Ports {
					if p.NodePort > 0 {
						port = int(p.NodePort)
						break
					}
				}
			} else {
				port = int(svc.Spec.Ports[0].Port)
				// NOTE: for cluster IP services we always use the endpoint
				// ports so this will be overridden.
			}
		}

		baseService.Port = port

		// Add all the ports as annotations
		for _, p := range svc.Spec.Ports {
			// Set the tag
			baseService.Meta["port-"+p.Name] = strconv.FormatInt(int64(p.Port), 10)
		}
	}

	// Parse any additional tags
	if tags, ok := svc.Annotations[annotationServiceTags]; ok {
		for _, t := range strings.Split(tags, ",") {
			baseService.Tags = append(baseService.Tags, strings.TrimSpace(t))
		}
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
			t.consulMap[key] = append(t.consulMap[key], &r)
		}

		return
	}

	switch svc.Spec.Type {
	// For LoadBalancer type services, we create a service instance for
	// each LoadBalancer entry. We only support entries that have an IP
	// address assigned (not hostnames).
	// If LoadBalancerEndpointsSync is true sync LB endpoints instead of loadbalancer ingress.
	case apiv1.ServiceTypeLoadBalancer:
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

				if _, ok := seen[addr]; ok {
					continue
				}
				seen[addr] = struct{}{}

				r := baseNode
				rs := baseService
				r.Service = &rs
				r.Service.ID = serviceID(r.Service.Service, addr)
				r.Service.Address = addr

				t.consulMap[key] = append(t.consulMap[key], &r)
			}
		}

	// For NodePort services, we create a service instance for each
	// endpoint of the service, which corresponds to the nodes the service's
	// pods are running on. This way we don't register _every_ K8S
	// node as part of the service.
	case apiv1.ServiceTypeNodePort:
		if t.endpointsMap == nil {
			return
		}

		endpoints := t.endpointsMap[key]
		if endpoints == nil {
			return
		}

		for _, subset := range endpoints.Subsets {
			for _, subsetAddr := range subset.Addresses {
				// Check that the node name exists
				// subsetAddr.NodeName is of type *string
				if subsetAddr.NodeName == nil {
					continue
				}

				// Look up the node's ip address by getting node info
				node, err := t.Client.CoreV1().Nodes().Get(t.Ctx, *subsetAddr.NodeName, metav1.GetOptions{})
				if err != nil {
					t.Log.Warn("error getting node info", "error", err)
					continue
				}

				// Set the expected node address type
				var expectedType apiv1.NodeAddressType
				if t.NodePortSync == InternalOnly {
					expectedType = apiv1.NodeInternalIP
				} else {
					expectedType = apiv1.NodeExternalIP
				}

				// Find the ip address for the node and
				// create the Consul service using it
				var found bool
				for _, address := range node.Status.Addresses {
					if address.Type == expectedType {
						found = true
						r := baseNode
						rs := baseService
						r.Service = &rs
						r.Service.ID = serviceID(r.Service.Service, subsetAddr.IP)
						r.Service.Address = address.Address

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
						if address.Type == apiv1.NodeInternalIP {
							r := baseNode
							rs := baseService
							r.Service = &rs
							r.Service.ID = serviceID(r.Service.Service, subsetAddr.IP)
							r.Service.Address = address.Address

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

	// For ClusterIP services, we register a service instance
	// for each endpoint.
	case apiv1.ServiceTypeClusterIP:
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

	if t.endpointsMap == nil {
		return
	}

	endpoints := t.endpointsMap[key]
	if endpoints == nil {
		return
	}

	seen := map[string]struct{}{}
	for _, subset := range endpoints.Subsets {
		// For ClusterIP services and if LoadBalancerEndpointsSync is true, we use the endpoint port instead
		// of the service port because we're registering each endpoint
		// as a separate service instance.
		epPort := baseService.Port
		if overridePortName != "" {
			// If we're supposed to use a specific named port, find it.
			for _, p := range subset.Ports {
				if overridePortName == p.Name {
					epPort = int(p.Port)
					break
				}
			}
		} else if overridePortNumber == 0 {
			// Otherwise we'll just use the first port in the list
			// (unless the port number was overridden by an annotation).
			for _, p := range subset.Ports {
				epPort = int(p.Port)
				break
			}
		}
		for _, subsetAddr := range subset.Addresses {
			addr := subsetAddr.IP
			if addr == "" && useHostname {
				addr = subsetAddr.Hostname
			}
			if addr == "" {
				continue
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
			r.Service.Port = epPort

			t.consulMap[key] = append(t.consulMap[key], &r)
		}
	}
}

// sync calls the Syncer.Sync function from the generated registrations.
//
// Precondition: lock must be held
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
	Service *ServiceResource
	Ctx     context.Context
}

func (t *serviceEndpointsResource) Informer() cache.SharedIndexInformer {
	// Watch all k8s namespaces. Events will be filtered out as appropriate in the
	// `shouldTrackEndpoints` function which checks whether the service is marked
	// to be tracked by the `shouldSync` function which uses the allow and deny
	// namespace lists.
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Service.Client.CoreV1().
					Endpoints(metav1.NamespaceAll).
					List(t.Ctx, options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Service.Client.CoreV1().
					Endpoints(metav1.NamespaceAll).
					Watch(t.Ctx, options)
			},
		},
		&apiv1.Endpoints{},
		0,
		cache.Indexers{},
	)
}

func (t *serviceEndpointsResource) Upsert(key string, raw interface{}) error {
	svc := t.Service
	endpoints, ok := raw.(*apiv1.Endpoints)
	if !ok {
		svc.Log.Warn("upsert got invalid type", "raw", raw)
		return nil
	}

	svc.serviceLock.Lock()
	defer svc.serviceLock.Unlock()

	// Check if we care about endpoints for this service
	if !svc.shouldTrackEndpoints(key) {
		return nil
	}

	// We are tracking this service so let's keep track of the endpoints
	if svc.endpointsMap == nil {
		svc.endpointsMap = make(map[string]*apiv1.Endpoints)
	}
	svc.endpointsMap[key] = endpoints

	// Update the registration and trigger a sync
	svc.generateRegistrations(key)
	svc.sync()
	svc.Log.Info("upsert endpoint", "key", key)
	return nil
}

func (t *serviceEndpointsResource) Delete(key string, _ interface{}) error {
	t.Service.serviceLock.Lock()
	defer t.Service.serviceLock.Unlock()

	// This is a bit of an optimization. We only want to force a resync
	// if we were tracking this endpoint to begin with and that endpoint
	// had associated registrations.
	if _, ok := t.Service.endpointsMap[key]; ok {
		delete(t.Service.endpointsMap, key)
		if _, ok := t.Service.consulMap[key]; ok {
			delete(t.Service.consulMap, key)
			t.Service.sync()
		}
	}

	t.Service.Log.Info("delete endpoint", "key", key)
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
