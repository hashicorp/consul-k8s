package catalog

import (
	"sync"

	"github.com/hashicorp/consul-k8s/helper/controller"
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
	Client kubernetes.Interface
	Log    hclog.Logger

	// serviceMap is a mapping of unique key (given by controller) to
	// the service structure.
	serviceMap  map[string]*apiv1.Service
	serviceLock sync.RWMutex
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

	if t.serviceMap == nil {
		t.serviceMap = make(map[string]*apiv1.Service)
	}
	t.serviceMap[key] = service

	t.Log.Info("upsert", "raw", raw)
	return nil
}

// Delete implements the controller.Resource interface.
func (t *ServiceResource) Delete(key string) error {
	t.serviceLock.Lock()
	defer t.serviceLock.Unlock()
	delete(t.serviceMap, key)

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
