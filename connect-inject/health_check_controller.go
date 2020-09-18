package connectinject

import (
	ctx "context"
	"fmt"
	"reflect"
	"strings"
	"time"

	log "github.com/hashicorp/go-hclog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	// labelInject is the label which is applied by the connect-inject webhook to all pods
	// this is the key by which the controller will filter it's watch/list and reconcile code
	labelInject = "consul.hashicorp.com/connect-inject-status"

	annotationServiceID                  = "consul.hashicorp.com/consul-service-id"
	annotationConsulDestinationNamespace = "consul.hashicorp.com/consul-destination-namespace"
)

// HealthCheckController struct defines how a controller should encapsulate
// logging, client connectivity, informing (list and watching)
// queueing, and handling of resource changes
type HealthCheckController struct {
	Log        log.Logger
	Clientset  kubernetes.Interface
	Queue      workqueue.RateLimitingInterface
	Informer   cache.SharedIndexInformer
	Handle     HCHandler
	MaxRetries int
	Namespace  string
}

func (c *HealthCheckController) setupInformer() {
	c.Informer = cache.NewSharedIndexInformer(
		// ListWatch takes a List and Watch function which we filter based on label which was injected
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx.Background(),
					metav1.ListOptions{LabelSelector: labelInject})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelInject
				return c.Clientset.CoreV1().Pods(metav1.NamespaceAll).Watch(ctx.Background(),
					metav1.ListOptions{LabelSelector: labelInject})
			},
		},
		&corev1.Pod{}, // the target type (Pod)
		0,             // no resync (period of 0)
		cache.Indexers{},
	)
}

func (c *HealthCheckController) setupWorkQueue() {
	// create a queue so that when the informer gets a resource that we are watching or listing
	// we can add it with an identifier key so processNextItem() can later process it.
	// The queue will be indexed via keys in the format of :  OPTION/namespace/resource
	// where OPTION will be one of UPDATE/CREATE/DELETE
	c.Queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
}

func (c *HealthCheckController) addEventHandlers() {
	// add event handlers to handle the three types of events for resources
	// We will only implement Update as all transition events that we care about are considered object Updates:
	// Create: pod.Status.Phase corev1.PodPending->corev1.PodRunning
	// Update: pod.Status.PodConditions.["Ready"] True->False || False->True
	// Delete: handled by connect-inject webhook
	c.Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			// AddFunc is a no-op as we handle ObjectCreate path in the UpdateFunc
			return
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newPod := newObj.(*corev1.Pod)
			oldPod := oldObj.(*corev1.Pod)
			if newPod.Annotations[annotationInject] != "true" {
				return
			}
			// Check to see if the object really was modified
			if reflect.DeepEqual(oldObj, newObj) == false {
				c.Log.Info("pod was updated : " + newPod.Name)
			} else {
				c.Log.Info("pod was not updated " + newPod.Name)
				return
			}
			// First we check if this is a transition from Pending to Running, at this point
			// we have a Pod scheduled and running on a host so we have a hostIP that we can
			// reference. This is the ObjectCreate path
			if oldPod.Status.Phase == corev1.PodPending && newPod.Status.Phase == corev1.PodRunning {
				key, err := cache.MetaNamespaceKeyFunc(newObj)
				c.Log.Info("Add Pod: %s", key)
				if err == nil {
					c.Queue.Add("ADD/" + key)
				}
				// We return here, due to startup timing on probes there is a case where we receive
				// the failed readiness probe before processing the transition from Pending to Running.
				// When we process ObjectCreate from processNextItem() we will append an ObjectUpdate()
				// which has the effect of setting the health status of the newly created TTL to the current state
				return
			}
			// We will only process events for PodRunning Pods
			if newPod.Status.Phase == corev1.PodRunning {
				// Only queue events which satisfy the condition of a pod Status Condition transition
				// from Ready/NotReady or NotReady/Ready
				// In this context "Ready" is the name of the Condition field and not the actual Status
				oldPodStatus := c.getReadyStatus(oldPod)
				newPodStatus := c.getReadyStatus(newPod)
				// If the Pod Status has changed, we queue the newObj and set the TTL to the newObj status
				if oldPodStatus != newPodStatus {
					key, err := cache.MetaNamespaceKeyFunc(newObj)
					c.Log.Info("Update pod: %s", key)
					if err == nil {
						c.Queue.Add("UPDATE/" + key)
					}
				}
			}
		},
		DeleteFunc: func(obj interface{}) {
			// Deletion is handled by connect-inject preStop!
			return
		},
	})
}

func (c *HealthCheckController) getReadyStatus(pod *corev1.Pod) corev1.ConditionStatus {
	for _, y := range pod.Status.Conditions {
		if y.Type == "Ready" {
			return y.Status
		}
	}
	return corev1.ConditionTrue
}

// Init is used at startup to force a Reconcile phase
func (c *HealthCheckController) Init(stopCh <-chan struct{}) {
	if err := c.Handle.Init(); err != nil {
		c.Log.Error("Error during Reconcile phase: %v", err)
	}
}

// Run is the main path of execution for the controller loop
func (c *HealthCheckController) Run(stopCh <-chan struct{}) {
	c.Log.Debug("Controller.Run: initializing")
	// Setup the Informer
	c.setupInformer()
	// Next setup the work queue
	c.setupWorkQueue()
	// Next add eventHandlers, these are responsible for defining Create/Update/Delete functionality
	c.addEventHandlers()

	// handle a panic with logging and exiting
	defer utilruntime.HandleCrash()
	// block new items in the Queue in case of shutdown, drain the queue and exit
	defer c.Queue.ShutDown()

	// run the Informer to start listing and watching resources
	go c.Informer.Run(stopCh)

	// do the initial synchronization (one time) to populate resources
	if !cache.WaitForCacheSync(stopCh, c.HasSynced) {
		utilruntime.HandleError(fmt.Errorf("error syncing cache"))
		return
	}
	// run the runWorker method every second with a stop channel
	wait.Until(c.runWorker, time.Second, stopCh)
}

// HasSynced allows us to satisfy the HealthCheckController interface
// by wiring up the Informer's HasSynced method to it
func (c *HealthCheckController) HasSynced() bool {
	return c.Informer.HasSynced()
}

// runWorker executes the loop to process new items added to the Queue
func (c *HealthCheckController) runWorker() {
	c.Log.Debug("Controller.runWorker: starting")
	// invoke processNextItem to fetch and consume the next change
	// to a watched or listed resource
	for c.processNextItem() {
		c.Log.Debug("Controller.runWorker: processing next item")
	}
	c.Log.Debug("Controller.runWorker: completed")
}

// processNextItem retrieves each Queued item and takes the
// necessary Handle action based off of if the item was created, updated or deleted
func (c *HealthCheckController) processNextItem() bool {
	c.Log.Debug("Controller.processNextItem: start")

	// fetch the next item (blocking) from the Queue to process or
	// if a shutdown is requested then return out to stop
	key, quit := c.Queue.Get()
	if quit {
		c.Log.Error("controller.processNextItem: shutting down")
		return false
	}
	// Key format is as follows :  CREATE/namespace/name, DELETE/namespace/name, UPDATE/namespace/name
	// also keep track if this is an create
	create := true
	formattedKey := strings.Split(key.(string), "/")
	if formattedKey[0] != "ADD" {
		create = false
	}
	keyRaw := strings.Join(formattedKey[1:], "/")

	// item will contain the complex object for the resource and
	// exists is a bool that'll indicate whether or not the
	// resource was created (true) or deleted (false)
	//
	// if there is an error in getting the key from the index
	// then we want to retry this particular Queue key a certain
	// number of times (c.MaxRetries) before we forget the Queue key
	// and throw an error
	item, exists, err := c.Informer.GetIndexer().GetByKey(keyRaw)
	if err != nil {
		if c.Queue.NumRequeues(key) < c.MaxRetries {
			c.Log.Error("controller.processNextItem: Failed processing item with key %s with error %v, retrying", key, err)
			c.Queue.AddRateLimited(key)
		} else {
			c.Log.Error("controller.processNextItem: Failed processing item with key %s with error %v, no more retries", key, err)
			c.Queue.Forget(key)
			utilruntime.HandleError(err)
		}
	}

	// if the object does exist that indicates that the object
	// was created or updated so run the ObjectCreated/ObjectUpdated method
	// dequeue the key to indicate success, requeue it on failure
	if exists {
		if create == true {
			// This is a Pod Create
			c.Log.Debug("controller.processNextItem: object create detected: %s", keyRaw)
			err = c.Handle.ObjectCreated(item)
			if err == nil {
				c.Log.Debug("controller.processNextItem: object update as part of ObjectCreate: %s", keyRaw)
				err = c.Handle.ObjectUpdated(item)
			}
		} else {
			// This is a Pod Status Update
			c.Log.Debug("controller.processNextItem: object update detected: %s", keyRaw)
			err = c.Handle.ObjectUpdated(item)
		}
		if err == nil {
			// Indicates success
			c.Queue.Forget(key)
		} else if c.Queue.NumRequeues(key) < c.MaxRetries {
			c.Log.Error("unable to process request, retrying")
			c.Queue.AddRateLimited(key)
		}
	}
	if err == nil {
		// marking Done removes the key from the queue entirely
		c.Queue.Done(key)
	}
	// keep the worker loop running by returning true
	return true
}
