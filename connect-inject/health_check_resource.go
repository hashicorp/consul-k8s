package connectinject

import (
	ctx "context"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// labelInject is the label which is applied by the connect-inject webhook to all pods.
	// This is the key the controller will use on the label filter for its lister, watcher and reconciler.
	labelInject = "consul.hashicorp.com/connect-inject-status"

	// kubernetesSuccessReasonMsg will be passed for passing health check's Reason to Consul
	kubernetesSuccessReasonMsg = "Kubernetes Health Checks Passing"

	// passing/failing strings passed into Consul health check as Status
	healthCheckPassing  = "passing"
	healthCheckCritical = "critical"
)

type HealthCheckResource struct {
	Log          hclog.Logger
	Clientset    kubernetes.Interface
	ClientConfig *api.Config

	// ConsulPort will be 8500/8501 based on TLS enabled
	ConsulPort string

	// SyncPeriod is the period by which reconcile gets called, default to 1 minute
	SyncPeriod time.Duration

	lock sync.Mutex
}

// Run is the long-running runloop for periodically running Reconcile
// it initially starts a Reconcile phase at startup and then is called again
// once every SyncPeriod time
func (h *HealthCheckResource) Run(stopCh <-chan struct{}) {
	// Start the background watchers
	h.Reconcile(stopCh)

	reconcileTimer := time.NewTimer(h.SyncPeriod)
	defer reconcileTimer.Stop()

	for {
		select {
		case <-stopCh:
			h.Log.Info("ConsulSyncer quitting")
			return

		case <-reconcileTimer.C:
			h.Reconcile(stopCh)
			reconcileTimer.Reset(h.SyncPeriod)
		}
	}
}

// Delete is not implemented because it is handled by the preStop phase whereby all services
// related to the pod are deregistered which also deregisters health checks
func (h *HealthCheckResource) Delete(string) error {
	return nil
}

// Informer starts a sharedindex informer which watches and lists corev1.Pod objects
// which meet the filter of labelInject
func (h *HealthCheckResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		// ListWatch takes a List and Watch function which we filter based on label which was injected
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return h.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx.Background(),
					metav1.ListOptions{LabelSelector: labelInject})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return h.Clientset.CoreV1().Pods(metav1.NamespaceAll).Watch(ctx.Background(),
					metav1.ListOptions{LabelSelector: labelInject})
			},
		},
		&corev1.Pod{}, // the target type (Pod)
		0,             // no resync (period of 0)
		cache.Indexers{},
	)
}

// Upsert determines if an event should be processed and handled accordingly
// Two primary use cases are handled, new pods will get a new consul TTL health check
// registered against their respective agent and service, and updates to pods will have
// this TTL health check updated to reflect the pod status.
func (h *HealthCheckResource) Upsert(key string, raw interface{}) error {
	pod := raw.(*corev1.Pod)
	if !h.shouldProcess(pod) {
		return nil
	}
	// First grab the IDs which we will use for interacting with Consul agent
	healthCheckID := h.getConsulHealthCheckID(pod)
	serviceID := h.getConsulServiceID(pod)
	consulStatus, reason, err := h.getReadyStatusAndReason(pod)
	if err != nil {
		h.Log.Error("unable to get ready status", err)
		return err
	}
	// create a new client connection to the respective agent for this pod+service
	client, err := h.getConsulClient(pod)
	if err != nil {
		return err
	}
	// Check to see if a serviceCheck exists for this service+healthCheckID
	serviceCheck, err := h.getServiceCheck(client, pod, serviceID, healthCheckID)
	if err != nil {
		h.Log.Error("error getting service check : ", err)
		return err
		// health check does not exist, some other error
	} else if serviceCheck == nil {
		//
		// create a new health check
		//
		h.Log.Info("upsert registering new health check for ", pod.Name)
		err = h.registerConsulHealthCheck(client, pod, healthCheckID, serviceID, consulStatus, reason)
		if err != nil {
			h.Log.Error("error registering health check : ", err)
			return err
		}
		return nil
	} else if serviceCheck.Status != consulStatus {
		//
		// Update the health check
		//
		h.Log.Info("upsert updating existing health check")
		err = h.updateConsulHealthCheckStatus(client, pod, healthCheckID, serviceID, consulStatus, reason)
		if err != nil {
			h.Log.Error("error updating health check : ", err)
			return err
		}
		return nil
	}
	h.Log.Info("no update needed for pod", pod.Name)
	return nil
}

// Reconcile iterates through all Pods with the appropriate label and compares the
// current health check status against that which is stored in Consul and updates
// the consul health check accordingly. if the health check doesnt yet exist it will
func (h *HealthCheckResource) Reconcile(stopCh <-chan struct{}) error {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.Log.Debug("starting reconcile")
	// First grab the list of Pods which have the label labelInject
	podList, err := h.Clientset.CoreV1().Pods(corev1.NamespaceAll).List(context.Background(),
		metav1.ListOptions{LabelSelector: labelInject})
	if err != nil {
		h.Log.Error("unable to get pods failing handler, err:%v", err)
		return err
	}
	// For each pod in the podlist, determine if a new health check needs to be registered
	// or: if a health check exists, determine if it needs to be updated
	for _, pod := range podList.Items {
		h.Log.Debug("processing Pod %v", pod.Name)
		if pod.Status.Phase != corev1.PodRunning {
			h.Log.Info("pod is not running, skipping", pod.Name, pod.Status.Phase)
			continue
		}
		// fetch the identifiers we will use to interact with the Consul agent for this pod
		serviceID := h.getConsulServiceID(&pod)
		healthCheckID := h.getConsulHealthCheckID(&pod)
		consulStatus, reason, err := h.getReadyStatusAndReason(&pod)
		if err != nil {
			h.Log.Error("unable to get pod status: %v", err)
			continue
		}
		// get a client connection to the correct agent
		client, err := h.getConsulClient(&pod)
		if err != nil {
			h.Log.Error("unable to set client connection for %v", pod.Name)
			continue
		}
		// retrieve the health check that would exist if the service had one registered for this pod
		serviceCheck, err := h.getServiceCheck(client, &pod, serviceID, healthCheckID)
		if err != nil {
			h.Log.Error("unable to get agent health checks for %v, %v", healthCheckID, err)
			continue
		} else if serviceCheck == nil {
			//
			// create a new health check
			//
			h.Log.Debug("registering new health check for %v", pod.Name, healthCheckID)
			err = h.registerConsulHealthCheck(client, &pod, healthCheckID, serviceID, consulStatus, reason)
			if err != nil {
				h.Log.Error("unable to register health check %v", err)
			}
			continue
		} else if serviceCheck.Status != consulStatus {
			//
			// update the healthCheck
			//
			err = h.updateConsulHealthCheckStatus(client, &pod, healthCheckID, serviceID, consulStatus, reason)
			if err != nil {
				h.Log.Error("error updating health check : ", err)
				continue
			}
		}
		h.Log.Debug("no update required for %V", pod.Name)
	}
	h.Log.Debug("finished reconcile")
	return nil
}

// TODO: for consul-ent we need to figure out how to namespace health checks on UpdateTTL() path
// it seems like consul-ent only supports namespaced Alias checks
func (h *HealthCheckResource) updateConsulHealthCheckStatus(client *api.Client, pod *corev1.Pod, consulHealthCheckID, serviceID, status, reason string) error {
	h.Log.Debug("updating health check: ", consulHealthCheckID)
	return client.Agent().UpdateTTL(consulHealthCheckID, reason, status)
}

// registerConsulHealthCheck registers a TTL health check for the service on this Agent.
// The Agent is local to the Pod which has a kubernetes health check.
// This has the effect of marking the endpoint health/unhealthy for Consul service mesh traffic.
func (h *HealthCheckResource) registerConsulHealthCheck(client *api.Client, pod *corev1.Pod, consulHealthCheckID, serviceID, initialStatus, reason string) error {
	h.Log.Debug("registerConsulHealthCheck: ", consulHealthCheckID, serviceID)
	// There is a chance of a race between when the Pod is transitioned to healthy by k8s and when we've initially
	// completed the registration of the service with the Consul Agent on this node. Retry a few times to be sure
	// that the service does in fact exist, otherwise it will return 500 from Consul API.
	retries := 0
	err := backoff.Retry(func() error {
		if retries > 10 {
			return &backoff.PermanentError{Err: fmt.Errorf("did not find serviceID: %v", serviceID)}
		}
		retries++
		services, err := client.Agent().Services()
		if err != nil {
			return err
		}
		for _, svc := range services {
			if svc.Service == serviceID {
				return nil
			}
		}
		return fmt.Errorf("did not find serviceID: %v", serviceID)
	}, backoff.NewConstantBackOff(1*time.Second))
	
	if err != nil {
		// We were unable to find the service on this host, this is due to :
		// 1. the pod is no longer on this host, has moved or was deregistered from the Agent by Consul
		// 2. Consul isn't working properly
		return err
	}

	// Now create a TTL health check in Consul associated with this service and pod.
	err = client.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name:      consulHealthCheckID,
		Notes:     "Kubernetes Health Check " + reason,
		ServiceID: serviceID,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:                            "100000h",
			Status:                         initialStatus,
			Notes:                          reason,
			TLSSkipVerify:                  true,
			SuccessBeforePassing:           1,
			FailuresBeforeCritical:         1,
			DeregisterCriticalServiceAfter: "",
		},
	})
	if err != nil {
		h.Log.Error("unable to register health check with Consul from k8s: %v", err)
		return err
	}
	return nil
}

// getServiceCheck will return the health check for this pod+service or nil if it doesnt exist yet
func (h *HealthCheckResource) getServiceCheck(client *api.Client, pod *corev1.Pod, serviceID, healthCheckID string) (*api.AgentCheck, error) {
	filter := fmt.Sprintf("Name == `%s`", healthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	if err != nil {
		h.Log.Error("unable to get agent health checks for %v, %v", healthCheckID, filter, err)
		return nil, err
	}
	// This will be nil (does not exist) or an actual check!
	return checks[healthCheckID], nil
}

// getReadyStatusAndReason returns the formatted status string to pass to Consul based on the
// ready state of the pod along with the reason message which will be passed into the Notes
// field of the Consul health check.
func (h *HealthCheckResource) getReadyStatusAndReason(pod *corev1.Pod) (string, string, error) {
	status := corev1.ConditionTrue
	reason := ""
	for _, cond := range pod.Status.Conditions {
		if cond.Type == "Ready" {
			status = cond.Status
			reason = cond.Message
			consulStatus := healthCheckPassing
			if status == corev1.ConditionFalse {
				consulStatus = healthCheckCritical
			} else {
				reason = kubernetesSuccessReasonMsg
			}
			return consulStatus, reason, nil
		}
	}
	return "", "", fmt.Errorf("no ready status for pod: %s", pod.Name)
}

// getConsulClient returns a new *api.Client pointed at the consul agent local to the pod
func (h *HealthCheckResource) getConsulClient(pod *corev1.Pod) (*api.Client, error) {
	httpFmt := "http"
	if h.ConsulPort == "8501" {
		httpFmt = "https"
	}
	newAddr := fmt.Sprintf("%s://%s:%s", httpFmt, pod.Status.HostIP, h.ConsulPort)
	localConfig := h.ClientConfig
	localConfig.Address = newAddr
	localClient, err := api.NewClient(localConfig)
	if err != nil {
		h.Log.Error("unable to get Consul API Client for address %s: %s", newAddr, err)
		return nil, err
	}
	h.Log.Debug("setting consul client to the following agent: %v", newAddr)
	return localClient, err
}

// shouldProcess is a simple filter which determines if Upsert should attempt to process the pod
// this is done without making any client api calls so it is fastpath
func (h *HealthCheckResource) shouldProcess(pod *corev1.Pod) bool {
	return pod.Annotations[annotationInject] == "true" &&  pod.Status.Phase == corev1.PodRunning
}

// getConsulHealthCheckID deterministically generates a health check ID that will be unique to the Agent
// where the health check is registered and deregistered.
func (h *HealthCheckResource) getConsulHealthCheckID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s_%s_kubernetes-health-check-ttl", pod.Namespace, h.getConsulServiceID(pod))
}

// getConsulServiceID returns the serviceID of the connect service
func (h *HealthCheckResource) getConsulServiceID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s-%s", pod.Name, pod.Annotations[annotationService])
}
