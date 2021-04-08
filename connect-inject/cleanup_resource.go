package connectinject

import (
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/consul-k8s/consul"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// CleanupResource implements Resource and is used to clean up Consul service
// instances that weren't deregistered when their pods were deleted.
// Usually the preStop hook in the pods handles this but during a force delete
// or OOM the preStop hook doesn't run.
type CleanupResource struct {
	Log              hclog.Logger
	KubernetesClient kubernetes.Interface
	// ConsulClient points at the agent on the same node as this pod.
	ConsulClient    *capi.Client
	ReconcilePeriod time.Duration
	Ctx             context.Context
	// ConsulScheme is the scheme to use when making API calls to Consul,
	// i.e. "http" or "https".
	ConsulScheme string
	// ConsulPort is the port to make HTTP API calls to Consul agents on.
	ConsulPort             string
	EnableConsulNamespaces bool

	lock sync.Mutex
}

// Run starts the long-running Reconcile loop that runs on a timer.
func (c *CleanupResource) Run(stopCh <-chan struct{}) {
	reconcileTimer := time.NewTimer(c.ReconcilePeriod)
	defer reconcileTimer.Stop()

	for {
		c.reconcile()
		reconcileTimer.Reset(c.ReconcilePeriod)

		select {
		case <-stopCh:
			c.Log.Info("received stop signal, shutting down")
			return

		case <-reconcileTimer.C:
			// Fall through and continue the loop.
		}
	}
}

// reconcile checks all registered Consul connect service instances and ensures
// the pod they represent is still running. If the pod is no longer running,
// it deregisters the service instance from its agent.
func (c *CleanupResource) reconcile() {
	c.Log.Debug("starting reconcile")

	// currentPods is a map of currently existing pods. Keys are in the form
	// "namespace/pod-name". Value doesn't matter since we're using this
	// solely for quick "exists" checks.
	// Use currentPodsKey() to construct the key when accessing this map.
	currentPods := make(map[string]bool)

	// Gather needed data on nodes, services and pods.
	kubeNodes, err := c.KubernetesClient.CoreV1().Nodes().List(c.Ctx, metav1.ListOptions{})
	if err != nil {
		c.Log.Error("unable to get nodes", "error", err)
		return
	}

	// namespacesToServiceNames maps from Consul namespace to the list of service
	// names registered in that namespace.
	// If Consul namespaces are disabled, there will be only one key with value
	// "", i.e. the empty string.
	namespacesToServiceNames := make(map[string][]string)
	if c.EnableConsulNamespaces {
		namespaces, _, err := c.ConsulClient.Namespaces().List(nil)
		if err != nil {
			c.Log.Error("unable to get Consul namespaces", "error", err)
			return
		}
		for _, ns := range namespaces {
			namespacesToServiceNames[ns.Name] = nil
		}
	} else {
		// This allows us to treat OSS the same as enterprise for the rest of
		// the code path.
		namespacesToServiceNames[""] = nil
	}

	for ns := range namespacesToServiceNames {
		serviceNames, _, err := c.ConsulClient.Catalog().Services(&capi.QueryOptions{Namespace: ns})
		if err != nil {
			c.Log.Error("unable to get Consul services", "error", err)
			return
		}
		namespacesToServiceNames[ns] = keys(serviceNames)
	}

	podList, err := c.KubernetesClient.CoreV1().Pods(corev1.NamespaceAll).List(c.Ctx,
		metav1.ListOptions{LabelSelector: annotationStatus})
	if err != nil {
		c.Log.Error("unable to get pods", "error", err)
		return
	}

	// Build up our map of currently running pods.
	for _, pod := range podList.Items {
		currentPods[currentPodsKey(pod.Name, pod.Namespace)] = true
	}

	// For each registered service, find the associated pod.
	for ns, serviceNames := range namespacesToServiceNames {
		for _, serviceName := range serviceNames {
			serviceInstances, _, err := c.ConsulClient.Catalog().Service(serviceName, "", &capi.QueryOptions{Namespace: ns})
			if err != nil {
				c.Log.Error("unable to get Consul service", "name", serviceName, "error", err)
				return
			}
			for _, instance := range serviceInstances {
				podName, hasPodMeta := instance.ServiceMeta[MetaKeyPodName]
				k8sNamespace, hasNSMeta := instance.ServiceMeta[MetaKeyKubeNS]
				if hasPodMeta && hasNSMeta {

					// Check if the instance matches a running pod. If not, deregister it.
					if _, podExists := currentPods[currentPodsKey(podName, k8sNamespace)]; !podExists {
						if !nodeInCluster(kubeNodes, instance.Node) {
							c.Log.Debug("skipping deregistration because instance is from node not in this cluster",
								"pod", podName, "id", instance.ServiceID, "ns", ns, "node", instance.Node)
							continue
						}

						c.Log.Info("found service instance from terminated pod still registered", "pod", podName, "id", instance.ServiceID, "ns", ns)
						err := c.deregisterInstance(instance, instance.Address)
						if err != nil {
							c.Log.Error("unable to deregister service instance", "id", instance.ServiceID, "ns", ns, "error", err)
							continue
						}
						c.Log.Info("service instance deregistered", "id", instance.ServiceID, "ns", ns)
					}
				}
			}
		}
	}

	c.Log.Debug("finished reconcile")
	return
}

// Delete is called when a pod is deleted. It checks that the Consul service
// instance for that pod is no longer registered with Consul.
// If the instance is still registered, it deregisters it.
func (c *CleanupResource) Delete(key string, obj interface{}) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected pod, got: %#v", obj)
	}
	if pod == nil {
		return fmt.Errorf("object for key %s was nil", key)
	}
	serviceName, ok := pod.ObjectMeta.Annotations[annotationService]
	if !ok {
		return fmt.Errorf("pod did not have %s annotation", annotationService)
	}
	kubeNS := pod.Namespace
	podName := pod.Name
	// NOTE: This will be an empty string with Consul OSS.
	consulNS := pod.ObjectMeta.Annotations[annotationConsulNamespace]

	// Look for both the service and its sidecar proxy.
	consulServiceNames := []string{serviceName, fmt.Sprintf("%s-sidecar-proxy", serviceName)}

	for _, consulServiceName := range consulServiceNames {
		instances, _, err := c.ConsulClient.Catalog().Service(consulServiceName, "", &capi.QueryOptions{
			Filter:    fmt.Sprintf(`ServiceMeta[%q] == %q and ServiceMeta[%q] == %q`, MetaKeyPodName, podName, MetaKeyKubeNS, kubeNS),
			Namespace: consulNS,
		})
		if err != nil {
			c.Log.Error("unable to get Consul Services", "error", err)
			return err
		}
		if len(instances) == 0 {
			c.Log.Debug("terminated pod had no still-registered instances", "service", consulServiceName, "pod", podName, "ns", consulNS)
			continue
		}

		// NOTE: We only expect a single instance because there's only one
		// per pod but we may as well range over all of them just to be safe.
		for _, instance := range instances {
			// NOTE: We don't need to check if this instance belongs to a kube
			// node in this cluster (like we do in Reconcile) because we only
			// get the delete event if a pod in this cluster is deleted so
			// we know this is one of our instances.

			c.Log.Info("found service instance from terminated pod still registered", "pod", podName, "id", instance.ServiceID, "ns", consulNS)
			err := c.deregisterInstance(instance, pod.Status.HostIP)
			if err != nil {
				c.Log.Error("unable to deregister service instance", "id", instance.ServiceID, "error", err)
				return err
			}
			c.Log.Info("service instance deregistered", "id", instance.ServiceID, "ns", consulNS)
		}
	}
	return nil
}

// Upsert is a no-op because we're only interested in pods that are deleted.
func (c *CleanupResource) Upsert(_ string, _ interface{}) error {
	return nil
}

func (c *CleanupResource) Informer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.KubernetesClient.CoreV1().Pods(metav1.NamespaceAll).List(c.Ctx,
					metav1.ListOptions{LabelSelector: annotationStatus})
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.KubernetesClient.CoreV1().Pods(metav1.NamespaceAll).Watch(c.Ctx,
					metav1.ListOptions{LabelSelector: annotationStatus})
			},
		},
		&corev1.Pod{},
		// Resync is 0 because we perform our own reconcile loop on our own timer.
		0,
		cache.Indexers{},
	)
}

// deregisterInstance deregisters instance from Consul by calling the agent at
// hostIP's deregister service API.
func (c *CleanupResource) deregisterInstance(instance *capi.CatalogService, hostIP string) error {
	fullAddr := fmt.Sprintf("%s://%s:%s", c.ConsulScheme, hostIP, c.ConsulPort)
	localConfig := capi.DefaultConfig()
	if instance.Namespace != "" {
		localConfig.Namespace = instance.Namespace
	}
	localConfig.Address = fullAddr
	client, err := consul.NewClient(localConfig)
	if err != nil {
		return fmt.Errorf("constructing client for address %q: %s", hostIP, err)
	}

	return client.Agent().ServiceDeregister(instance.ServiceID)
}

// currentPodsKey should be used to construct the key to access the
// currentPods map.
func currentPodsKey(podName, k8sNamespace string) string {
	return fmt.Sprintf("%s/%s", k8sNamespace, podName)
}

// nodeInCluster returns whether nodeName is the name of a node in nodes, i.e.
// it's the name of a node in this cluster.
func nodeInCluster(nodes *corev1.NodeList, nodeName string) bool {
	for _, n := range nodes.Items {
		if n.Name == nodeName {
			return true
		}
	}
	return false
}

// keys returns the keys of m.
func keys(m map[string][]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
