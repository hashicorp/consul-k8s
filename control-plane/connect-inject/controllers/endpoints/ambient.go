// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"fmt"
	"net"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// AmbientConfig configures registration of pods served by the node-level ambient proxy.
type AmbientConfig struct {
	// Enabled turns on ambient mesh registration.
	Enabled bool
	// InboundPort is the port the node proxy accepts inbound mesh traffic on, on its own pod IP.
	InboundPort int
	// OutboundPort is the transparent outbound listener port inside each pod netns.
	OutboundPort int
	// NodeProxySelector selects the node proxy DaemonSet pods in NodeProxyNamespace.
	NodeProxySelector labels.Selector
	// NodeProxyNamespace is the namespace of the node proxy DaemonSet pods.
	NodeProxyNamespace string
}

func ambientNodeProxyCheckID(proxySvcID string) string {
	return fmt.Sprintf("%s/ambient-node-proxy", proxySvcID)
}

// ambientPodReady reports whether the CNI plugin has finished setting up the pod for the ambient
// mesh. Until then the node proxy cannot bind sockets in the pod network namespace.
func ambientPodReady(pod corev1.Pod) bool {
	return pod.Annotations[constants.KeyAmbientMeshStatus] == constants.AmbientMeshStatusComplete &&
		pod.Annotations[constants.AnnotationAmbientNetNS] != ""
}

// validateAmbientServiceAccount applies connect-init's identity check to ambient pods: with ACLs
// enabled, a pod may only be registered as the Consul service named after its Kubernetes service
// account. Sidecar pods are checked by connect-init before their own ACL token is used. Ambient
// pods have neither, and the node proxy may write every service, so without this check a pod could
// take another service's identity (and leaf certificate) via the connect-service annotation.
func (r *Controller) validateAmbientServiceAccount(pod corev1.Pod, serviceEndpoints corev1.Endpoints) error {
	if r.AuthMethod == "" {
		return nil
	}
	svcName := serviceName(pod, serviceEndpoints)
	if pod.Spec.ServiceAccountName != svcName {
		return fmt.Errorf("service account name %q doesn't match Consul service name %q", pod.Spec.ServiceAccountName, svcName)
	}
	return nil
}

// registerAmbientServicesAndHealthCheck registers the service and its proxy for a pod served by the
// node-level ambient proxy. The service registration is identical to sidecar mode; the proxy
// registration points at the node proxy's inbound listener on the node proxy pod's IP.
func (r *Controller) registerAmbientServicesAndHealthCheck(ctx context.Context, apiClient *api.Client, pod corev1.Pod, podIP string, serviceEndpoints corev1.Endpoints, healthStatus string) error {
	if pod.Labels[constants.KeyManagedBy] != constants.ManagedByValue {
		return nil
	}

	serviceRegistration, proxyServiceRegistration, proxyRoutable, err := r.createAmbientServiceRegistrations(ctx, pod, podIP, serviceEndpoints, healthStatus)
	if err != nil {
		return err
	}

	r.Log.Info("registering ambient service with Consul", "name", serviceRegistration.Service.Service,
		"id", serviceRegistration.Service.ID, "health", healthStatus)
	if _, err = apiClient.Catalog().Register(serviceRegistration, nil); err != nil {
		return err
	}
	if err = assignServiceVirtualIP(r.Context, apiClient, serviceRegistration.Service); err != nil {
		r.Log.Error(err, "failed to add ip to virtual ip table", "name", serviceRegistration.Service.Service)
	}

	if !proxyRoutable {
		// The proxy must only ever be reachable at the node proxy's address. Until the node proxy
		// pod has an IP, keep the proxy out of the catalog; a node proxy pod update re-reconciles.
		r.Log.Info("ambient node proxy has no IP yet; deregistering ambient proxy service",
			"id", proxyServiceRegistration.Service.ID, "node", pod.Spec.NodeName)
		_, err = apiClient.Catalog().Deregister(&api.CatalogDeregistration{
			Node:      proxyServiceRegistration.Node,
			ServiceID: proxyServiceRegistration.Service.ID,
			Namespace: proxyServiceRegistration.Service.Namespace,
			Partition: proxyServiceRegistration.Partition,
		}, nil)
		return err
	}

	r.Log.Info("registering ambient proxy service with Consul", "name", proxyServiceRegistration.Service.Service,
		"id", proxyServiceRegistration.Service.ID, "node", pod.Spec.NodeName)
	_, err = apiClient.Catalog().Register(proxyServiceRegistration, nil)
	return err
}

// createAmbientServiceRegistrations builds the service and proxy registrations for an ambient pod.
// proxyRoutable is false when the node proxy has no pod IP yet; the proxy registration must then
// not be registered, because its address would not point at a node proxy.
func (r *Controller) createAmbientServiceRegistrations(ctx context.Context, pod corev1.Pod, podIP string, serviceEndpoints corev1.Endpoints, healthStatus string) (serviceRegistration, proxyRegistration *api.CatalogRegistration, proxyRoutable bool, err error) {
	// Build the base registrations with sidecar-only transparent proxy handling (probe exposure,
	// pod tproxy annotations) switched off; the ambient equivalents are applied below.
	base := *pod.DeepCopy()
	base.Annotations[constants.KeyTransparentProxy] = "false"
	serviceRegistration, proxyRegistration, err = r.createServiceRegistrations(ctx, base, podIP, serviceEndpoints, healthStatus)
	if err != nil {
		return nil, nil, false, err
	}

	nodeProxy, nodeProxyErr := r.ambientNodeProxyPod(ctx, pod.Spec.NodeName)

	proxy := proxyRegistration.Service
	// Other proxies dial the node proxy, which serves every ambient workload on the node and
	// selects the destination service by SNI. The app pod IP is never used as the proxy address.
	proxy.Address = ""
	if nodeProxy != nil && nodeProxy.Status.PodIP != "" {
		proxy.Address = nodeProxy.Status.PodIP
		proxyRoutable = true
	}
	proxy.Port = r.Ambient.InboundPort
	proxy.Proxy.Mode = api.ProxyModeTransparent
	proxy.Proxy.TransparentProxy = &api.TransparentProxyConfig{OutboundListenerPort: r.Ambient.OutboundPort}
	proxy.Proxy.Expose = api.ExposeConfig{}

	// The service and proxy registrations share one meta map; ambient keys belong to the proxy only.
	meta := make(map[string]string, len(proxy.Meta)+4)
	for k, v := range proxy.Meta {
		meta[k] = v
	}
	meta[constants.MetaKeyAmbientMesh] = "true"
	meta[constants.MetaKeyAmbientNode] = pod.Spec.NodeName
	meta[constants.MetaKeyAmbientNetNS] = pod.Annotations[constants.AnnotationAmbientNetNS]
	meta[constants.MetaKeyAmbientPodIP] = podIP
	proxy.Meta = meta

	taggedAddresses, err := r.ambientVirtualTaggedAddresses(ctx, pod, serviceEndpoints, serviceRegistration.Service.Port)
	if err != nil {
		return nil, nil, false, err
	}
	if taggedAddresses != nil {
		serviceRegistration.Service.TaggedAddresses = taggedAddresses
		proxy.TaggedAddresses = taggedAddresses
	}

	nodeProxyStatus, nodeProxyOutput := ambientNodeProxyHealth(pod.Spec.NodeName, nodeProxy, nodeProxyErr)
	proxyRegistration.Checks = api.HealthChecks{{
		CheckID:   ambientNodeProxyCheckID(proxy.ID),
		Name:      constants.AmbientNodeProxyCheckName,
		Type:      constants.ConsulKubernetesCheckType,
		Status:    nodeProxyStatus,
		ServiceID: proxy.ID,
		Output:    nodeProxyOutput,
		Namespace: proxy.Namespace,
	}}

	return serviceRegistration, proxyRegistration, proxyRoutable, nil
}

// ambientVirtualTaggedAddresses mirrors the sidecar transparent proxy behaviour: the Kubernetes
// ClusterIP is registered as the "virtual" tagged address so that upstreams can dial it.
func (r *Controller) ambientVirtualTaggedAddresses(ctx context.Context, pod corev1.Pod, serviceEndpoints corev1.Endpoints, consulServicePort int) (map[string]api.ServiceAddress, error) {
	var k8sService corev1.Service
	if err := r.Client.Get(ctx, types.NamespacedName{Name: serviceEndpoints.Name, Namespace: serviceEndpoints.Namespace}, &k8sService); err != nil {
		return nil, err
	}
	if net.ParseIP(k8sService.Spec.ClusterIP) == nil {
		r.Log.Info("skipping syncing service cluster IP to Consul", "name", k8sService.Name, "ns", k8sService.Namespace, "ip", k8sService.Spec.ClusterIP)
		return nil, nil
	}

	var k8sServicePort int32
	for _, sp := range k8sService.Spec.Ports {
		targetPortValue, err := portValueFromIntOrString(pod, sp.TargetPort)
		if err != nil {
			return nil, err
		}
		if (targetPortValue != 0 && targetPortValue == consulServicePort) || sp.Port == int32(consulServicePort) {
			k8sServicePort = sp.Port
			break
		}
	}
	return map[string]api.ServiceAddress{
		clusterIPTaggedAddressName: {Address: k8sService.Spec.ClusterIP, Port: int(k8sServicePort)},
	}, nil
}

// ambientNodeProxyHealth returns the health of the node proxy pod running on nodeName.
func ambientNodeProxyHealth(nodeName string, pod *corev1.Pod, err error) (string, string) {
	if err != nil {
		return api.HealthCritical, fmt.Sprintf("failed to look up ambient node proxy on node %s: %s", nodeName, err)
	}
	if pod == nil {
		return api.HealthCritical, fmt.Sprintf("no ambient node proxy is running on node %s", nodeName)
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return api.HealthPassing, fmt.Sprintf("ambient node proxy %s/%s is ready", pod.Namespace, pod.Name)
		}
	}
	return api.HealthCritical, fmt.Sprintf("ambient node proxy %s/%s is not ready", pod.Namespace, pod.Name)
}

func (r *Controller) ambientNodeProxyPod(ctx context.Context, nodeName string) (*corev1.Pod, error) {
	if r.Ambient.NodeProxySelector == nil {
		return nil, fmt.Errorf("ambient node proxy selector is not configured")
	}
	var pods corev1.PodList
	if err := r.Client.List(ctx, &pods, client.InNamespace(r.Ambient.NodeProxyNamespace),
		client.MatchingLabelsSelector{Selector: r.Ambient.NodeProxySelector}); err != nil {
		return nil, err
	}
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName == nodeName && pods.Items[i].DeletionTimestamp == nil {
			return &pods.Items[i], nil
		}
	}
	return nil, nil
}

// requestsForAmbientNodeProxyPod re-reconciles every service with ambient pods on the node of a
// node proxy pod, so the "Ambient Node Proxy" check follows the node proxy's readiness.
func (r *Controller) requestsForAmbientNodeProxyPod(ctx context.Context, obj client.Object) []reconcile.Request {
	nodeProxy, ok := obj.(*corev1.Pod)
	if !ok || !r.Ambient.Enabled || r.Ambient.NodeProxySelector == nil ||
		nodeProxy.Namespace != r.Ambient.NodeProxyNamespace ||
		!r.Ambient.NodeProxySelector.Matches(labels.Set(nodeProxy.Labels)) ||
		nodeProxy.Spec.NodeName == "" {
		return nil
	}

	var ambientPods corev1.PodList
	if err := r.Client.List(ctx, &ambientPods, client.MatchingLabels{
		constants.KeyAmbientMesh: constants.LabelAmbientMeshEnabled,
	}); err != nil {
		r.Log.Error(err, "failed to list ambient pods", "node", nodeProxy.Spec.NodeName)
		return nil
	}

	seen := map[types.NamespacedName]bool{}
	var requests []reconcile.Request
	for i := range ambientPods.Items {
		pod := &ambientPods.Items[i]
		if pod.Spec.NodeName != nodeProxy.Spec.NodeName {
			continue
		}
		for _, req := range r.requestsForPodServices(ctx, pod) {
			if !seen[req.NamespacedName] {
				seen[req.NamespacedName] = true
				requests = append(requests, req)
			}
		}
	}
	return requests
}

// requestsForAmbientPod re-reconciles the services of an ambient pod when the CNI plugin marks it
// complete, which can happen after its EndpointSlice was last reconciled.
func (r *Controller) requestsForAmbientPod(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || !r.Ambient.Enabled || !common.IsAmbientPod(*pod) || !ambientPodReady(*pod) {
		return nil
	}
	return r.requestsForPodServices(ctx, pod)
}
