package catalog

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/consul-k8s/helper/controller"
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

	// ConsulK8STag is the tag value for services registered.
	ConsulK8STag = "k8s"
)

// ServiceResource implements controller.Resource to sync Service resource
// types from K8S.
type ServiceResource struct {
	Log       hclog.Logger
	Client    kubernetes.Interface
	Syncer    Syncer
	Namespace string // K8S namespace to watch

	// ExplictEnable should be set to true to require explicit enabling
	// using annotations. If this is false, then services are implicitly
	// enabled (aka default enabled).
	ExplicitEnable bool

	// ClusterIPSync set to true (the default) syncs ClusterIP-type services.
	// Setting this to false will ignore ClusterIP services during the sync.
	ClusterIPSync bool

	// serviceMap is a mapping of unique key (given by controller) to
	// the service structure. endpointsMap is the mapping of the same
	// uniqueKey to a set of endpoints.
	//
	// serviceLock must be held for any read/write to these maps.
	serviceLock  sync.RWMutex
	serviceMap   map[string]*apiv1.Service
	endpointsMap map[string]*apiv1.Endpoints
	consulMap    map[string][]*consulapi.CatalogRegistration
}

// Informer implements the controller.Resource interface.
func (t *ServiceResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Client.CoreV1().Services(t.namespace()).List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Client.CoreV1().Services(t.namespace()).Watch(options)
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

	if !t.shouldSync(service) {
		t.Log.Debug("syncing disabled for service, ignoring", "key", key)
		return nil
	}

	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()

	// Syncing is enabled, let's keep track of this service.
	if t.serviceMap == nil {
		t.serviceMap = make(map[string]*apiv1.Service)
	}
	t.serviceMap[key] = service

	// If we care about endpoints, we should do the initial endpoints load.
	if t.shouldTrackEndpoints(key) {
		endpoints, err := t.Client.CoreV1().
			Endpoints(t.namespace()).
			Get(service.Name, metav1.GetOptions{})
		if err != nil {
			t.Log.Warn("error loading initial endpoints",
				"key", key,
				"err", err)
		} else {
			if t.endpointsMap == nil {
				t.endpointsMap = make(map[string]*apiv1.Endpoints)
			}
			t.endpointsMap[key] = endpoints
		}
	}

	// Update the registration and trigger a sync
	t.generateRegistrations(key)
	t.sync()
	t.Log.Info("upsert", "key", key)
	return nil
}

// Delete implements the controller.Resource interface.
func (t *ServiceResource) Delete(key string) error {
	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()
	delete(t.serviceMap, key)
	delete(t.endpointsMap, key)

	// If there were registrations related to this service, then
	// delete them and sync.
	if _, ok := t.consulMap[key]; ok {
		delete(t.consulMap, key)
		t.sync()
	}

	t.Log.Info("delete", "key", key)
	return nil
}

// Run implements the controller.Backgrounder interface.
func (t *ServiceResource) Run(ch <-chan struct{}) {
	t.Log.Info("starting runner for endpoints")
	(&controller.Controller{
		Log:      t.Log.Named("controller/endpoints"),
		Resource: &serviceEndpointsResource{Service: t},
	}).Run(ch)
}

// shouldSync returns true if resyncing should be enabled for the given service.
func (t *ServiceResource) shouldSync(svc *apiv1.Service) bool {
	// If we're listening on all namespaces, we explicitly ignore the
	// system namespace. The user can explicitly enable this by starting
	// a sync for that namespace.
	if t.namespace() == metav1.NamespaceAll && svc.Namespace == metav1.NamespaceSystem {
		t.Log.Debug("ignoring system service since we're listening on all namespaces",
			"service-name", svc.Name)
		return false
	}

	// Ignore ClusterIP services if ClusterIP sync is disabled
	if svc.Spec.Type == apiv1.ServiceTypeClusterIP && !t.ClusterIPSync {
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
			"service-name", svc.Name,
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
	// for syncing) and is a NodePort type. Only NodePort type services
	// use the endpoints at all.
	if t.serviceMap == nil {
		return false
	}
	svc, ok := t.serviceMap[key]
	if !ok {
		return false
	}

	return svc.Spec.Type == apiv1.ServiceTypeNodePort || svc.Spec.Type == apiv1.ServiceTypeClusterIP
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
		Node:           "k8s-sync",
		Address:        "127.0.0.1",
		NodeMeta: map[string]string{
			ConsulSourceKey: ConsulSourceValue,
		},
	}

	baseService := consulapi.AgentService{
		Service: svc.Name,
		Tags:    []string{ConsulK8STag},
		Meta: map[string]string{
			ConsulSourceKey: ConsulSourceValue,
			ConsulK8SNS:     t.namespace(),
		},
	}

	// If the name is explicitly annotated, adopt that name
	if v, ok := svc.Annotations[annotationServiceName]; ok {
		baseService.Service = strings.TrimSpace(v)
	}

	// Determine the default port
	if len(svc.Spec.Ports) > 0 {
		nodePort := svc.Spec.Type == apiv1.ServiceTypeNodePort
		main := svc.Spec.Ports[0].Name

		// If a specific port is specified, then use that port value
		if target, ok := svc.Annotations[annotationServicePort]; ok {
			main = target
			if v, err := strconv.ParseInt(target, 0, 0); err == nil {
				baseService.Port = int(v)
			}
		}

		// Go through the ports so we can add them to the service meta. We
		// also use this opportunity to find our default port.
		for _, p := range svc.Spec.Ports {
			target := p.Port
			if nodePort && p.NodePort > 0 {
				target = p.NodePort
			}

			// Set the tag
			baseService.Meta["port-"+p.Name] = strconv.FormatInt(int64(target), 10)

			// If the name matches our main port, set our main port
			if p.Name == main {
				baseService.Port = int(target)
			}
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
	case apiv1.ServiceTypeLoadBalancer:
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

	// For NodePort services, we register each K8S
	// node as part of the service.
	case apiv1.ServiceTypeNodePort:
		// Get all nodes to be able to reference their ip addresses
		nodes, err := t.Client.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil || len(nodes.Items) == 0 {
			t.Log.Warn("error getting nodes", "error", err)
			return
		}

		// Create a service instance for each node
		for _, node := range nodes.Items {
			for _, address := range node.Status.Addresses {
				if address.Type == apiv1.NodeExternalIP {
					r := baseNode
					rs := baseService
					r.Service = &rs
					r.Service.ID = serviceID(r.Service.Service, address.Address)
					r.Service.Address = address.Address
					r.Address = address.Address

					if node.Name != "" {
						r.Node = node.Name
					}

					t.consulMap[key] = append(t.consulMap[key], &r)
				}
			}
		}

	case apiv1.ServiceTypeClusterIP:
		if t.endpointsMap == nil {
			return
		}

		endpoints := t.endpointsMap[key]
		if endpoints == nil {
			return
		}

		seen := map[string]struct{}{}
		for _, subset := range endpoints.Subsets {
			for _, subsetAddr := range subset.Addresses {
				addr := subsetAddr.IP
				if addr == "" {
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
				if subsetAddr.NodeName != nil {
					r.Node = *subsetAddr.NodeName
					r.Address = addr
				}

				t.consulMap[key] = append(t.consulMap[key], &r)
			}
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

// namespace returns the K8S namespace to setup the resource watchers in.
func (t *ServiceResource) namespace() string {
	if t.Namespace != "" {
		return t.Namespace
	}

	return metav1.NamespaceAll
}

// serviceEndpointsResource implements controller.Resource and starts
// a background watcher on endpoints that is used by the ServiceResource
// to keep track of changing endpoints for registered services.
type serviceEndpointsResource struct {
	Service *ServiceResource
}

func (t *serviceEndpointsResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Service.Client.CoreV1().
					Endpoints(t.Service.namespace()).
					List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Service.Client.CoreV1().
					Endpoints(t.Service.namespace()).
					Watch(options)
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

func (t *serviceEndpointsResource) Delete(key string) error {
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
