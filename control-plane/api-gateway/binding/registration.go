// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"fmt"
	"net"
	"strconv"

	gatewaycommon "github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	metaKeySyntheticNode       = "synthetic-node"
	kubernetesSuccessReasonMsg = "Kubernetes health checks passing"

	// consulKubernetesCheckType is the type of health check in Consul for Kubernetes readiness status.
	consulKubernetesCheckType = "kubernetes-readiness"

	// consulKubernetesCheckName is the name of health check in Consul for Kubernetes readiness status.
	consulKubernetesCheckName = "Kubernetes Readiness Check"

	// metricsConfiguration is the configuration key for binding a prometheus port to the envoy instance.
	metricsConfiguration = "envoy_prometheus_bind_addr"
)

func registrationsForPods(metrics gatewaycommon.MetricsConfig, namespace string, gateway gwv1beta1.Gateway, pods []corev1.Pod) []api.CatalogRegistration {
	registrations := []api.CatalogRegistration{}
	for _, pod := range pods {
		// Skip registration if pod doesn't have complete node information yet
		if pod.Spec.NodeName == "" || pod.Status.PodIP == "" || pod.Status.HostIP == "" {
			continue
		}
		registrations = append(registrations, registrationForPod(metrics, namespace, gateway, pod))
	}
	return registrations
}

func registrationForPod(metrics gatewaycommon.MetricsConfig, namespace string, gateway gwv1beta1.Gateway, pod corev1.Pod) api.CatalogRegistration {
	healthStatus := api.HealthCritical
	if isPodReady(pod) {
		healthStatus = api.HealthPassing
	}

	var proxyConfigOverrides *api.AgentServiceConnectProxyConfig
	if metrics.Enabled {
		proxyConfigOverrides = &api.AgentServiceConnectProxyConfig{
			Config: map[string]interface{}{
				metricsConfiguration: net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(metrics.Port)),
			},
		}
	}
	nodeName := common.ConsulNodeNameFromK8sNode(pod.Spec.NodeName)

	return api.CatalogRegistration{
		Node:    nodeName,
		Address: pod.Status.HostIP,
		NodeMeta: map[string]string{
			metaKeySyntheticNode: "true",
		},
		Service: &api.AgentService{
			Kind:      api.ServiceKindAPIGateway,
			ID:        pod.Name,
			Service:   gateway.Name,
			Address:   pod.Status.PodIP,
			Namespace: namespace,
			Proxy:     proxyConfigOverrides,
			Meta: map[string]string{
				constants.MetaKeyPodName:         pod.Name,
				constants.MetaKeyKubeNS:          pod.Namespace,
				constants.MetaKeyKubeServiceName: gateway.Name,
				"external-source":                "consul-api-gateway",
			},
		},
		Check: &api.AgentCheck{
			CheckID:   fmt.Sprintf("%s/%s", pod.Namespace, pod.Name),
			Name:      consulKubernetesCheckName,
			Type:      consulKubernetesCheckType,
			Status:    healthStatus,
			ServiceID: pod.Name,
			Output:    getHealthCheckStatusReason(healthStatus, pod.Name, pod.Namespace),
			Namespace: namespace,
		},
		SkipNodeUpdate: true,
	}
}

func getHealthCheckStatusReason(healthCheckStatus, podName, podNamespace string) string {
	if healthCheckStatus == api.HealthPassing {
		return kubernetesSuccessReasonMsg
	}

	return fmt.Sprintf("Pod \"%s/%s\" is not ready", podNamespace, podName)
}

func isPodReady(pod corev1.Pod) bool {
	if corev1.PodRunning != pod.Status.Phase {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
