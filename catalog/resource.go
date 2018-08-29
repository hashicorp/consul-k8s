package catalog

import (
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

// ServiceResource implements controller.Resource to sync Service resource
// types from K8S.
type ServiceResource struct {
	Log    hclog.Logger
	Client kubernetes.Interface
	Syncer Syncer

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
				return t.Client.CoreV1().Services(metav1.NamespaceDefault).List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Client.CoreV1().Services(metav1.NamespaceDefault).Watch(options)
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
	t.Log.Info("upsert", "key", key)

	// TODO(mitchellh): check if syncing is enabled for this service

	// Syncing is enabled, let's keep track of this service.
	if t.serviceMap == nil {
		t.serviceMap = make(map[string]*apiv1.Service)
	}
	t.serviceMap[key] = service

	// TODO(mitchellh): on initial load populate the endpoints

	// Update the registration and trigger a sync
	t.generateRegistrations(key)
	t.sync()
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
		Log:      t.Log,
		Resource: &serviceEndpointsResource{Service: t},
	}).Run(ch)
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

	// baseNode and baseService are the base that should be modified with
	// service-type specific changes. These are not pointers, they should be
	// shallow copied for each instance.
	baseNode := consulapi.CatalogRegistration{
		SkipNodeUpdate: true,
		Node:           "k8s-sync",
		Address:        "127.0.0.1",
		NodeMeta: map[string]string{
			"consul-source": "k8s",
		},
	}

	baseService := consulapi.AgentService{
		Service: svc.Name,
		Tags:    []string{"k8s"},
		Meta: map[string]string{
			"consul-source":  "k8s",
			"consul-k8s-key": key,
		},
	}

	// TODO(mitchellh): read annotations and modify the base here

	switch svc.Spec.Type {
	// For LoadBalancer type services, we create a service instance for
	// each LoadBalancer entry. We only support entries that have an IP
	// address assigned (not hostnames).
	case apiv1.ServiceTypeLoadBalancer:
		t.consulMap[key] = []*consulapi.CatalogRegistration{}
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			addr := ingress.IP
			if addr == "" {
				addr = ingress.Hostname
			}
			if addr == "" {
				continue
			}

			r := baseNode
			rs := baseService
			r.Service = &rs
			r.Service.Address = addr
			t.consulMap[key] = append(t.consulMap[key], &r)
		}
	}

	t.Log.Debug("generated registration",
		"key", key,
		"service", baseService.Service,
		"instances", len(t.consulMap[key]))
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
}

func (t *serviceEndpointsResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.Service.Client.CoreV1().Endpoints(metav1.NamespaceDefault).List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.Service.Client.CoreV1().Endpoints(metav1.NamespaceDefault).Watch(options)
			},
		},
		&apiv1.Endpoints{},
		0,
		cache.Indexers{},
	)
}

func (t *serviceEndpointsResource) Upsert(key string, raw interface{}) error {
	t.Service.Log.Info("upsert", "raw", raw)
	return nil
}

func (t *serviceEndpointsResource) Delete(key string) error {
	t.Service.Log.Info("delete", "key", key)
	return nil
}
