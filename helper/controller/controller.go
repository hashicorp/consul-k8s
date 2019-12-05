// Package controller contains a reusable abstraction for efficiently
// watching for changes in resources in a Kubernetes cluster.
package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Controller is a generic cache.Controller implementation that watches
// Kubernetes for changes to specific set of resources and calls the configured
// callbacks as data changes.
type Controller struct {
	Log      hclog.Logger
	Resource Resource

	informer cache.SharedIndexInformer
}

// Run starts the Controller and blocks until stopCh is closed.
//
// Important: Callers must ensure that Run is only called once at a time.
func (c *Controller) Run(stopCh <-chan struct{}) {
	// Properly handle any panics
	defer utilruntime.HandleCrash()

	// Create an informer so we can keep track of all service changes.
	informer := c.Resource.Informer()
	c.informer = informer

	// Create a queue for storing items to process from the informer.
	var queueOnce sync.Once
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	shutdown := func() { queue.ShutDown() }
	defer queueOnce.Do(shutdown)

	// Add an event handler when data is received from the informer. The
	// event handlers here will block the informer so we just offload them
	// immediately into a workqueue.
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// convert the resource object into a key (in this case
			// we are just doing it in the format of 'namespace/name')
			key, err := cache.MetaNamespaceKeyFunc(obj)
			c.Log.Debug("queue", "op", "add", "key", key)
			if err == nil {
				queue.Add(key)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			c.Log.Debug("queue", "op", "update", "key", key)
			if err == nil {
				queue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			c.Log.Debug("queue", "op", "delete", "key", key)
			if err == nil {
				queue.Add(key)
			}
		},
	})

	// If the type is a background syncer, then we startup the background
	// process.
	if bg, ok := c.Resource.(Backgrounder); ok {
		ctx, cancelF := context.WithCancel(context.Background())

		// Run the backgrounder
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			bg.Run(ctx.Done())
		}()

		// Start a goroutine that automatically closes the context when we stop
		go func() {
			select {
			case <-stopCh:
				cancelF()

			case <-ctx.Done():
				// Cancelled outside
			}
		}()

		// When we exit, close the context so the backgrounder ends
		defer func() {
			cancelF()
			<-doneCh
		}()
	}

	// Run the informer to start requesting resources
	go func() {
		informer.Run(stopCh)

		// We have to shut down the queue here if we stop so that
		// wait.Until stops below too. We can't wait until the defer at
		// the top since wait.Until will block.
		queueOnce.Do(shutdown)
	}()

	// Initial sync
	if !cache.WaitForCacheSync(stopCh, informer.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("error syncing cache"))
		return
	}
	c.Log.Info("initial cache sync complete")

	// run the runWorker method every second with a stop channel
	wait.Until(func() {
		for c.processSingle(queue, informer) {
			// Process
		}
	}, time.Second, stopCh)
}

// HasSynced implements cache.Controller
func (c *Controller) HasSynced() bool {
	if c.informer == nil {
		return false
	}

	return c.informer.HasSynced()
}

// LastSyncResourceVersion implements cache.Controller
func (c *Controller) LastSyncResourceVersion() string {
	if c.informer == nil {
		return ""
	}

	return c.informer.LastSyncResourceVersion()
}

func (c *Controller) processSingle(
	queue workqueue.RateLimitingInterface,
	informer cache.SharedIndexInformer,
) bool {
	// Fetch the next item
	key, quit := queue.Get()
	if quit {
		return false
	}
	defer queue.Done(key)

	// The key should be a string. If it isn't, just ignore it.
	keyRaw, ok := key.(string)
	if !ok {
		c.Log.Warn("processSingle: dropping non-string key", "key", key)
		return true
	}

	// Get the item
	item, exists, err := informer.GetIndexer().GetByKey(keyRaw)

	// If we got the item successfully, call the proper method
	if err == nil {
		c.Log.Debug("processing object", "key", keyRaw, "exists", exists)
		c.Log.Trace("processing object", "object", item)
		if !exists {
			err = c.Resource.Delete(keyRaw)
		} else {
			err = c.Resource.Upsert(keyRaw, item)
		}

		if err == nil {
			queue.Forget(key)
		}
	}

	if err != nil {
		if queue.NumRequeues(key) < 5 {
			c.Log.Error("failed processing item, retrying", "key", keyRaw, "error", err)
			queue.AddRateLimited(key)
		} else {
			c.Log.Error("failed processing item, no more retries", "key", keyRaw, "error", err)
			queue.Forget(key)
			utilruntime.HandleError(err)
		}
	}

	return true
}
