package controller

import (
	"k8s.io/client-go/tools/cache"
)

type ResourceUpsertFunc func(string, interface{}) error
type ResourceDeleteFunc func(string) error

// Resource should be implemented by anything that should be watchable
// by Controller. The Resource needs to be aware of how to create the Informer
// that is responsible for making API calls as well as what to do on Upsert
// and Delete.
type Resource interface {
	// Informer returns the SharedIndexInformer that the controller will
	// use to watch for changes. An Informer is the long-running task that
	// holds blocking queries to K8S and stores data in a local store.
	Informer() cache.SharedIndexInformer

	// Upsert and Delete are the callbacks called when processing the queue
	// of changes from the Informer. If an error is returned, the given item
	// will be retried.
	Upsert(string, interface{}) error
	Delete(string) error
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
func (r *basicResource) Delete(k string) error                { return r.delete(k) }
