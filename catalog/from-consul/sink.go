package catalog

import (
	"sync"

	"github.com/hashicorp/go-hclog"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Sink is the destination where services are registered.
//
// While in practice we only have one sink (K8S), the interface abstraction
// makes it easy and possible to test the Source in isolation.
type Sink interface {
	// SetServices is called with the services that should be created.
	// The key is the service name and the destination is the external DNS
	// entry to point to.
	SetServices(map[string]string)
}

// K8SSink is a Sink implementation that registers services with Kubernetes.
//
// K8SSink also implements controller.Resource and is meant to run as a K8S
// controller that watches services. This is the primary way that the
// sink should be run.
type K8SSink struct {
	Client    kubernetes.Interface // Client is the K8S API client
	Namespace string               // Namespace is the namespace to sync to
	Log       hclog.Logger         // Logger

	lock           sync.Mutex
	sourceServices map[string]string
	serviceMap     map[string]*apiv1.Service
}

// SetServices implements Sink
func (s *K8SSink) SetServices(svcs map[string]string) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.sourceServices = svcs
}

// Informer implements the controller.Resource interface.
func (s *K8SSink) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return s.Client.CoreV1().Services(s.namespace()).List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return s.Client.CoreV1().Services(s.namespace()).Watch(options)
			},
		},
		&apiv1.Service{},
		0,
		cache.Indexers{},
	)
}

// Upsert implements the controller.Resource interface.
func (s *K8SSink) Upsert(key string, raw interface{}) error {
	// We expect a Service. If it isn't a service then just ignore it.
	service, ok := raw.(*apiv1.Service)
	if !ok {
		s.Log.Warn("upsert got invalid type", "raw", raw)
		return nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	if s.serviceMap == nil {
		s.serviceMap = make(map[string]*apiv1.Service)
	}
	s.serviceMap[key] = service

	s.Log.Info("upsert", "key", key)
	return nil
}

// Delete implements the controller.Resource interface.
func (s *K8SSink) Delete(key string) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.serviceMap, key)
	s.Log.Info("delete", "key", key)
	return nil
}

// namespace returns the K8S namespace to setup the resource watchers in.
func (s *K8SSink) namespace() string {
	if s.Namespace != "" {
		return s.Namespace
	}

	// Default to the default namespace. This should not be "all" since we
	// want a specific namespace to watch and write to.
	return metav1.NamespaceDefault
}
