package connectinject

import (
	"fmt"
	"net/url"
	"sync"
	"time"

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

	// kubernetesSuccessReasonMsg will be passed for passing health check's Reason to Consul.
	kubernetesSuccessReasonMsg = "Kubernetes Health Checks Passing"
)

type HealthCheckResource struct {
	Log                 hclog.Logger
	KubernetesClientset kubernetes.Interface

	// ConsulUrl holds the url information for client connections.
	ConsulUrl *url.URL
	// ReconcilePeriod is the period by which reconcile gets called.
	// default to 1 minute.
	ReconcilePeriod time.Duration

	Ctx  context.Context
	lock sync.Mutex
}

// Run is the long-running runloop for periodically running Reconcile.
// It initially reconciles at startup and is then invoked after every
// ReconcilePeriod expires.
func (h *HealthCheckResource) Run(stopCh <-chan struct{}) {
	err := h.Reconcile()
	if err != nil {
		h.Log.Error("reconcile returned an error", "err", err)
	}

	reconcileTimer := time.NewTimer(h.ReconcilePeriod)
	defer reconcileTimer.Stop()

	for {
		select {
		case <-stopCh:
			h.Log.Info("received stop signal, shutting down")
			return

		case <-reconcileTimer.C:
			if err := h.Reconcile(); err != nil {
				h.Log.Error("reconcile returned an error", "err", err)
			}
			reconcileTimer.Reset(h.ReconcilePeriod)
		}
	}
}

// Delete is not implemented because it is handled by the preStop phase whereby all services
// related to the pod are deregistered which also deregisters health checks.
func (h *HealthCheckResource) Delete(string) error {
	return nil
}

// Informer starts a sharedindex informer which watches and lists corev1.Pod objects
// which meet the filter of labelInject.
func (h *HealthCheckResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		// ListWatch takes a List and Watch function which we filter based on label which was injected.
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return h.KubernetesClientset.CoreV1().Pods(metav1.NamespaceAll).List(h.Ctx,
					metav1.ListOptions{LabelSelector: labelInject})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return h.KubernetesClientset.CoreV1().Pods(metav1.NamespaceAll).Watch(h.Ctx,
					metav1.ListOptions{LabelSelector: labelInject})
			},
		},
		&corev1.Pod{}, // the target type (Pod)
		0,             // no resync (period of 0)
		cache.Indexers{},
	)
}

// Upsert processes a create or update event.
// Two primary use cases are handled, new pods will get a new consul TTL health check
// registered against their respective agent and service, and updates to pods will have
// this TTL health check updated to reflect the pod's readiness status.
func (h *HealthCheckResource) Upsert(key string, raw interface{}) error {
	pod, ok := raw.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("failed to cast to a pod object")
	}
	err := h.reconcilePod(pod)
	if err != nil {
		h.Log.Error("unable to update pod", "err", err)
		return err
	}
	return nil
}

// Reconcile iterates through all Pods with the appropriate label and compares the
// current health check status against that which is stored in Consul and updates
// the consul health check accordingly. If the health check doesn't yet exist it will create it.
func (h *HealthCheckResource) Reconcile() error {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.Log.Debug("starting reconcile")
	// First grab the list of Pods which have the label labelInject.
	podList, err := h.KubernetesClientset.CoreV1().Pods(corev1.NamespaceAll).List(h.Ctx,
		metav1.ListOptions{LabelSelector: labelInject})
	if err != nil {
		h.Log.Error("unable to get pods", "err", err)
		return err
	}
	// Reconcile the state of each pod in the podList.
	for _, pod := range podList.Items {
		err = h.reconcilePod(&pod)
		if err != nil {
			h.Log.Error("unable to update pod", "err", err)
		}
	}
	h.Log.Debug("finished reconcile")
	return nil
}

// reconcilePod will reconcile a pod. This is the common work for both Upsert and Reconcile.
func (h *HealthCheckResource) reconcilePod(pod *corev1.Pod) error {
	h.Log.Debug("processing pod", "name", pod.Name)
	if !h.shouldProcess(pod) {
		// Skip pods that are not running or have not been properly injected.
		return nil
	}
	// Fetch the identifiers we will use to interact with the Consul agent for this pod.
	serviceID := h.getConsulServiceID(pod)
	healthCheckID := h.getConsulHealthCheckID(pod)
	status, reason, err := h.getReadyStatusAndReason(pod)
	if err != nil {
		return fmt.Errorf("unable to get pod status: %s", err)
	}
	// Get a client connection to the correct agent.
	client, err := h.getConsulClient(pod)
	if err != nil {
		return fmt.Errorf("unable to get Consul client connection for %s: %s", pod.Name, err)
	}
	// Retrieve the health check that would exist if the service had one registered for this pod.
	serviceCheck, err := h.getServiceCheck(client, healthCheckID)
	if err != nil {
		return fmt.Errorf("unable to get agent health checks: serviceID=%s, checkID=%s, %s", serviceID, healthCheckID, err)
	}
	if serviceCheck == nil {
		// Create a new health check.
		h.Log.Debug("registering new health check", "name", pod.Name, "id", healthCheckID)
		err = h.registerConsulHealthCheck(client, healthCheckID, serviceID, status)
		if err != nil {
			return fmt.Errorf("unable to register health check: %s", err)
		}
		// Also update it, the reason this is separate is there is no way to set the Output field of the health check
		// at creation time, and this is what is displayed on the UI as opposed to the Notes field.
		err = h.updateConsulHealthCheckStatus(client, healthCheckID, status, reason)
		if err != nil {
			return fmt.Errorf("error updating health check: %s", err)
		}
	} else if serviceCheck.Status != status {
		// Update the healthCheck.
		err = h.updateConsulHealthCheckStatus(client, healthCheckID, status, reason)
		if err != nil {
			return fmt.Errorf("error updating health check: %s", err)
		}
	}
	return nil
}

// updateConsulHealthCheckStatus updates the consul health check status.
func (h *HealthCheckResource) updateConsulHealthCheckStatus(client *api.Client, consulHealthCheckID, status, reason string) error {
	h.Log.Debug("updating health check", "id", consulHealthCheckID)
	return client.Agent().UpdateTTL(consulHealthCheckID, reason, status)
}

// registerConsulHealthCheck registers a TTL health check for the service on this Agent.
// The Agent is local to the Pod which has a kubernetes health check.
// This has the effect of marking the service instance healthy/unhealthy for Consul service mesh traffic.
func (h *HealthCheckResource) registerConsulHealthCheck(client *api.Client, consulHealthCheckID, serviceID, status string) error {
	h.Log.Debug("registering Consul health check", "id", consulHealthCheckID, "serviceID", serviceID)

	// Create a TTL health check in Consul associated with this service and pod.
	// The TTL time is 100000h which should ensure that the check never fails due to timeout
	// of the TTL check.
	err := client.Agent().CheckRegister(&api.AgentCheckRegistration{
		ID:        consulHealthCheckID,
		Name:      "Kubernetes Health Check",
		ServiceID: serviceID,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:                    "100000h",
			Status:                 status,
			SuccessBeforePassing:   1,
			FailuresBeforeCritical: 1,
		},
	})
	if err != nil {
		return fmt.Errorf("registering health check for service %q: %s", serviceID, err)
	}
	return nil
}

// getServiceCheck will return the health check for this pod and service if it exists.
func (h *HealthCheckResource) getServiceCheck(client *api.Client, healthCheckID string) (*api.AgentCheck, error) {
	filter := fmt.Sprintf("CheckID == `%s`", healthCheckID)
	checks, err := client.Agent().ChecksWithFilter(filter)
	if err != nil {
		return nil, fmt.Errorf("getting check %q: %s", healthCheckID, err)
	}
	// This will be nil (does not exist) or an actual check.
	return checks[healthCheckID], nil
}

// getReadyStatusAndReason returns the formatted status string to pass to Consul based on the
// ready state of the pod along with the reason message which will be passed into the Notes
// field of the Consul health check.
func (h *HealthCheckResource) getReadyStatusAndReason(pod *corev1.Pod) (string, string, error) {
	for _, cond := range pod.Status.Conditions {
		var consulStatus, reason string
		if cond.Type == corev1.PodReady {
			if cond.Status != corev1.ConditionTrue {
				consulStatus = api.HealthCritical
				reason = cond.Message
			} else {
				consulStatus = api.HealthPassing
				reason = kubernetesSuccessReasonMsg
			}
			return consulStatus, reason, nil
		}
	}
	return "", "", fmt.Errorf("no ready status for pod: %s", pod.Name)
}

// getConsulClient returns an *api.Client that points at the consul agent local to the pod.
func (h *HealthCheckResource) getConsulClient(pod *corev1.Pod) (*api.Client, error) {
	newAddr := fmt.Sprintf("%s://%s:%s", h.ConsulUrl.Scheme, pod.Status.HostIP, h.ConsulUrl.Port())
	localConfig := api.DefaultConfig()
	localConfig.Address = newAddr
	if pod.Annotations[annotationConsulNamespace] != "" {
		localConfig.Namespace = pod.Annotations[annotationConsulNamespace]
	}
	localClient, err := api.NewClient(localConfig)
	if err != nil {
		h.Log.Error("unable to get Consul API Client", "addr", newAddr, "err", err)
		return nil, err
	}
	h.Log.Debug("setting consul client to the following agent", "addr", newAddr)
	return localClient, err
}

// shouldProcess is a simple filter which determines if Upsert or Reconcile should attempt to process the pod.
// This is done without making any client api calls so it is fast.
// We only are interested in corev1.PodRunning pods as they have valid readiness probe status.
func (h *HealthCheckResource) shouldProcess(pod *corev1.Pod) bool {
	return pod.Annotations[annotationStatus] == injected && pod.Status.Phase == corev1.PodRunning
}

// getConsulHealthCheckID deterministically generates a health check ID that will be unique to the Agent
// where the health check is registered and deregistered.
func (h *HealthCheckResource) getConsulHealthCheckID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s_%s_kubernetes-health-check-ttl", pod.Namespace, h.getConsulServiceID(pod))
}

// getConsulServiceID returns the serviceID of the connect service.
func (h *HealthCheckResource) getConsulServiceID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s-%s", pod.Name, pod.Annotations[annotationService])
}
