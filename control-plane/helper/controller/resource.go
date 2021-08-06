package controller

import (
	"k8s.io/client-go/tools/cache"
)

// ResourceUpsertFunc and ResourceDeleteFunc are the callback types for
// when a resource is inserted, updated, or deleted.
type ResourceUpsertFunc func(string, interface{}) error
type ResourceDeleteFunc func(string, interface{}) error

// Resource should be implemented by anything that should be watchable
// by Controller. The Resource needs to be aware of how to create the Informer
// that is responsible for making API calls as well as what to do on Upsert
// and Delete.
type Resource interface {
	// Informer returns the SharedIndexInformer that the controller will
	// use to watch for changes. An Informer is the long-running task that
	// holds blocking queries to K8S and stores data in a local store.
	Informer() cache.SharedIndexInformer

	// Upsert is the callback called when processing the queue
	// of changes from the Informer. If an error is returned, the given item
	// will be retried.
	Upsert(key string, obj interface{}) error
	// Delete is called on object deletion.
	// obj is the last known state of the object before deletion. In some
	// cases, it may not be up to date with the latest state of the object.
	// If an error is returned, the given item will be retried.
	Delete(key string, obj interface{}) error
}

// Backgrounder should be implemented by a Resource that requires additional
// background processing. If a Resource implements this, then the Controller
// will automatically Run the Backgrounder for the duration of the controller.
//
// The channel will be closed when the Controller is quitting. The Controller
// will block until the Backgrounder completes.
type Backgrounder interface {
	Run(<-chan struct{})
}

// NewResource returns a Resource implementation for the given informer,
// upsert handler, and delete handler.
func NewResource(
	informer cache.SharedIndexInformer,
	upsert ResourceUpsertFunc,
	delete ResourceDeleteFunc,
) Resource {
	return &basicResource{
		informer: informer,
		upsert:   upsert,
		delete:   delete,
	}
}

// basicResource is a Resource implementation where all components are given
// as struct fields. This can only be created with NewResource.
type basicResource struct {
	informer cache.SharedIndexInformer
	upsert   ResourceUpsertFunc
	delete   ResourceDeleteFunc
}

func (r *basicResource) Informer() cache.SharedIndexInformer  { return r.informer }
func (r *basicResource) Upsert(k string, v interface{}) error { return r.upsert(k, v) }
func (r *basicResource) Delete(k string, v interface{}) error { return r.delete(k, v) }
