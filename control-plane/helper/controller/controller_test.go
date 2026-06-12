// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

func TestController_impl(t *testing.T) {
	var _ cache.Controller = &Controller{}
}

// Test that data that exists before is synced.
func TestController_initialData(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, deleted, _ := testResource(client)

	// Add some initial data before the controller starts
	_, err := client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), testService("foo"), metav1.CreateOptions{})
	require.NoError(err)
	_, err = client.CoreV1().Services(metav1.NamespaceDefault).Create(context.Background(), testService("bar"), metav1.CreateOptions{})
	require.NoError(err)

	// Start the controller
	closer := TestControllerRun(resource)

	// Wait some period of time
	time.Sleep(200 * time.Millisecond)
	closer()
	require.Len(data, 2)
	require.Len(deleted, 0)
}

// Test that created data after starting is loaded.
func TestController_create(t *testing.T) {
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, deleted, dataLock := testResource(client)

	// ✅ Start controller first
	closer := TestControllerRun(resource)
	defer closer()

	// ✅ Create AFTER start
	_, err := client.CoreV1().
		Services(metav1.NamespaceDefault).
		Create(context.Background(), testService("foo"), metav1.CreateOptions{})
	require.NoError(err)

	_, err = client.CoreV1().
		Services(metav1.NamespaceDefault).
		Create(context.Background(), testService("bar"), metav1.CreateOptions{})
	require.NoError(err)

	// ✅ Wait (must be > resync period ~1s)
	require.Eventually(func() bool {
		dataLock.Lock()
		defer dataLock.Unlock()
		return len(data) == 2
	}, 3*time.Second, 50*time.Millisecond)

	dataLock.Lock()
	defer dataLock.Unlock()

	require.Len(data, 2)
	require.Len(deleted, 0)
}

// Test that data that is created and deleted is properly removed.
func TestController_createDelete(t *testing.T) {
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, deleted, dataLock := testResource(client)

	closer := TestControllerRun(resource)
	defer closer()

	// --- CREATE ---
	_, err := client.CoreV1().
		Services(metav1.NamespaceDefault).
		Create(context.Background(), testService("foo"), metav1.CreateOptions{})
	require.NoError(err)

	barSvc, err := client.CoreV1().
		Services(metav1.NamespaceDefault).
		Create(context.Background(), testService("bar"), metav1.CreateOptions{})
	require.NoError(err)

	// ✅ WAIT until BOTH are observed (critical fix)
	require.Eventually(func() bool {
		dataLock.Lock()
		defer dataLock.Unlock()
		return len(data) == 2
	}, 3*time.Second, 50*time.Millisecond)

	// --- DELETE ---
	require.NoError(client.CoreV1().
		Services(metav1.NamespaceDefault).
		Delete(context.Background(), barSvc.Name, metav1.DeleteOptions{}))

	// ✅ WAIT for delete to be observed
	require.Eventually(func() bool {
		dataLock.Lock()
		defer dataLock.Unlock()
		return len(data) == 1 && len(deleted) == 1
	}, 3*time.Second, 50*time.Millisecond)

	dataLock.Lock()
	defer dataLock.Unlock()

	require.Len(data, 1)
	require.Len(deleted, 1)
	require.Contains(deleted, "default/bar")

	deletedSvc, ok := deleted["default/bar"].(*apiv1.Service)
	require.True(ok)
	require.Equal("bar", deletedSvc.Name)
}

// Test that data is properly updated.
func TestController_update(t *testing.T) {
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, data, _, dataLock := testResource(client)

	// CREATE first
	svc, err := client.CoreV1().
		Services(metav1.NamespaceDefault).
		Create(context.Background(), testService("foo"), metav1.CreateOptions{})
	require.NoError(err)

	// Start controller
	closer := TestControllerRun(resource)
	defer closer()

	// Wait for initial state
	require.Eventually(func() bool {
		dataLock.Lock()
		defer dataLock.Unlock()

		v, ok := data["default/foo"]
		return ok && v != nil
	}, 2*time.Second, 50*time.Millisecond)

	// Validate initial
	dataLock.Lock()
	initial := data["default/foo"].(*apiv1.Service)
	dataLock.Unlock()
	require.Equal(apiv1.ServiceTypeClusterIP, initial.Spec.Type)

	// UPDATE
	svc.Spec.Type = apiv1.ServiceTypeNodePort
	_, err = client.CoreV1().
		Services(metav1.NamespaceDefault).
		Update(context.Background(), svc, metav1.UpdateOptions{})
	require.NoError(err)

	// Wait for update
	require.Eventually(func() bool {
		dataLock.Lock()
		defer dataLock.Unlock()

		v, ok := data["default/foo"]
		if !ok || v == nil {
			return false
		}

		return v.(*apiv1.Service).Spec.Type == apiv1.ServiceTypeNodePort
	}, 3*time.Second, 50*time.Millisecond)
}

// Test that backgrounders are started and stopped.
func TestController_backgrounder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	client := fake.NewSimpleClientset()
	resource, _, _, _ := testResource(client)
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

func TestController_informerDeleteHandler(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		Input interface{}
		Exp   *Event
	}{
		"nil obj": {
			Input: nil,
			Exp:   nil,
		},
		"service delete": {
			Input: testService("foo"),
			Exp: &Event{
				Key: "default/foo",
				Obj: testService("foo"),
			},
		},
		// Test that we unwrap DeletedFinalStateUnknown objects.
		"DeletedFinalStateUnknown": {
			Input: cache.DeletedFinalStateUnknown{
				Key: "default/foo",
				Obj: testService("foo"),
			},
			Exp: &Event{
				Key: "default/foo",
				Obj: testService("foo"),
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctrl := &Controller{Log: hclog.Default()}
			queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
			defer queue.ShutDown()
			ctrl.informerDeleteHandler(queue)(c.Input)

			if c.Exp == nil {
				require.Equal(t, queue.Len(), 0)
			} else {
				rawEvent, quit := queue.Get()
				require.False(t, quit)
				require.Equal(t, *c.Exp, rawEvent)
			}
		})
	}
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
			Name:      name,
			Namespace: "default",
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
				return client.CoreV1().Services(metav1.NamespaceDefault).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return watch.NewEmptyWatch(), nil
			},
		},
		&apiv1.Service{},
		500*time.Millisecond,
		cache.Indexers{},
	)
}

// testResource creates a Resource implementation that keeps track of the
// callback data. It returns two maps. The first is a map from resource keys to resources
// based on the callbacks that have occurred. The second is a map of the resources
// that have been deleted.
// To access the data safely, the lock should be held.
func testResource(client kubernetes.Interface) (Resource, map[string]interface{}, map[string]interface{}, *sync.Mutex) {
	var lock sync.Mutex
	m := make(map[string]interface{})
	deleted := make(map[string]interface{})

	return NewResource(testInformer(client),
		func(key string, v interface{}) error {
			lock.Lock()
			m[key] = v
			lock.Unlock()
			return nil
		},
		func(key string, v interface{}) error {
			lock.Lock()
			delete(m, key)
			deleted[key] = v
			lock.Unlock()
			return nil
		},
	), m, deleted, &lock
}
