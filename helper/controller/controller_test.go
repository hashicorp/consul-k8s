package controller

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func TestController_impl(t *testing.T) {
	var _ cache.Controller = &Controller{}
}

// Test that data that exists before is synced
func TestController_initialData(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, _ := testResource(client)

	// Add some initial data before the controller starts
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
	require.NoError(err)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("bar"))
	require.NoError(err)

	// Start the controller
	closer := TestControllerRun(resource)

	// Wait some period of time
	time.Sleep(200 * time.Millisecond)
	closer()
	require.Len(data, 2)
}

// Test that created data after starting is loaded
func TestController_create(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, _ := testResource(client)

	// Start the controller
	closer := TestControllerRun(resource)

	// Wait some period of time
	time.Sleep(100 * time.Millisecond)

	// Add some initial data before the controller starts
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
	require.NoError(err)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("bar"))
	require.NoError(err)

	// Wait some period of time
	time.Sleep(100 * time.Millisecond)
	closer()

	require.Len(data, 2)
}

// Test that data that is created and deleted is properly removed.
func TestController_createDelete(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, _ := testResource(client)

	// Start the controller
	closer := TestControllerRun(resource)

	// Wait some period of time
	time.Sleep(100 * time.Millisecond)

	// Add some initial data before the controller starts
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
	require.NoError(err)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("bar"))
	require.NoError(err)

	// Wait a bit so that the create hopefully propagates
	time.Sleep(50 * time.Millisecond)
	require.NoError(client.CoreV1().Services(metav1.NamespaceDefault).Delete("bar", nil))

	// Wait some period of time
	time.Sleep(100 * time.Millisecond)
	closer()

	require.Len(data, 1)
}

// Test that data is properly updated.
func TestController_update(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, dataLock := testResource(client)

	// Start the controller
	closer := TestControllerRun(resource)

	// Wait some period of time
	time.Sleep(100 * time.Millisecond)

	// Add some initial data before the controller starts
	svc, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(testService("foo"))
	require.NoError(err)

	{
		// Verify the type is correctly set
		time.Sleep(50 * time.Millisecond)
		dataLock.Lock()
		actual := data["default/foo"].(*apiv1.Service)
		dataLock.Unlock()
		require.Equal(apiv1.ServiceTypeClusterIP, actual.Spec.Type)
	}

	// Update
	svc.Spec.Type = apiv1.ServiceTypeNodePort
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Update(svc)
	require.NoError(err)

	{
		// Verify the type is correctly set
		time.Sleep(50 * time.Millisecond)
		dataLock.Lock()
		actual := data["default/foo"].(*apiv1.Service)
		dataLock.Unlock()
		require.Equal(apiv1.ServiceTypeNodePort, actual.Spec.Type)
	}

	// Wait some period of time
	closer()
}

// Test that backgrounders are started and stopped.
func TestController_backgrounder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, _, _ := testResource(client)
	bgresource := &testBackgrounder{Resource: resource}

	// Start the controller
	closer := TestControllerRun(bgresource)

	// Wait some period of time
	time.Sleep(50 * time.Millisecond)
	require.True(bgresource.Running(), "running")

	// Wait some period of time
	closer()
	require.False(bgresource.Running(), "running")
}

// testBackgrounder implements Backgrounder and has a simple func to check
// if its running.
type testBackgrounder struct {
	sync.Mutex
	Resource

	running bool
}

func (r *testBackgrounder) Running() bool {
	r.Lock()
	defer r.Unlock()
	return r.running
}

func (r *testBackgrounder) Run(ch <-chan struct{}) {
	r.Lock()
	r.running = true
	r.Unlock()

	<-ch

	r.Lock()
	r.running = false
	r.Unlock()
}

// testService returns a bare bones apiv1.Service structure with the
// given name set. This is useful with the fake client.
func testService(name string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},

		Spec: apiv1.ServiceSpec{
			Type: apiv1.ServiceTypeClusterIP,
		},
	}
}

// testInformer creates an Informer that operates on the given K8S client
// and watches for Service entries.
func testInformer(client kubernetes.Interface) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return client.CoreV1().Services(metav1.NamespaceDefault).List(options)
			},

			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return client.CoreV1().Services(metav1.NamespaceDefault).Watch(options)
			},
		},
		&apiv1.Service{},
		0,
		cache.Indexers{},
	)
}

// testResource creates a Resource implementation that keeps track of the
// callback data in the given map. To access the data safely, the lock
// should be held.
func testResource(client kubernetes.Interface) (Resource, map[string]interface{}, *sync.Mutex) {
	var lock sync.Mutex
	m := make(map[string]interface{})

	return NewResource(testInformer(client),
		func(key string, v interface{}) error {
			lock.Lock()
			m[key] = v
			lock.Unlock()
			return nil
		},
		func(key string) error {
			lock.Lock()
			delete(m, key)
			lock.Unlock()
			return nil
		},
	), m, &lock
}
