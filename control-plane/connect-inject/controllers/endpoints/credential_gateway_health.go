// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// isCredentialInjectionGateway reports whether pod is a gateway whose
// credentials are rendered by a Vault Agent sidecar for the credential
// processor.
func isCredentialInjectionGateway(pod corev1.Pod) bool {
	if !isGateway(pod) {
		return false
	}
	var agent, processor bool
	for _, container := range pod.Spec.Containers {
		switch container.Name {
		case constants.CredentialVaultAgentContainerName:
			agent = true
		case constants.CredentialProcessorContainerName:
			processor = true
		}
	}
	return agent && processor
}

// credentialGatewayHealth returns the Consul health status for a gateway pod
// and whether it overrides the EndpointSlice readiness. A credential-injection
// gateway stays passing while only its Vault Agent sidecar is not ready: the
// processor keeps serving still-valid cached credentials and fails closed per
// binding once they expire, so an Agent restart must not withdraw every
// service behind the gateway. Any other unready container, a pod that is not
// running or is terminating, or an unsatisfied readiness gate keeps the
// EndpointSlice result.
func credentialGatewayHealth(pod corev1.Pod, endpointHealth string) (string, bool) {
	if endpointHealth == api.HealthPassing || !isCredentialInjectionGateway(pod) {
		return endpointHealth, false
	}
	if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return endpointHealth, false
	}
	for _, gate := range pod.Spec.ReadinessGates {
		if !podConditionTrue(pod, gate.ConditionType) {
			return endpointHealth, false
		}
	}

	ready := make(map[string]bool, len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	for _, status := range pod.Status.ContainerStatuses {
		ready[status.Name] = status.Ready
	}
	for _, status := range pod.Status.InitContainerStatuses {
		ready[status.Name] = status.Ready
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == constants.CredentialVaultAgentContainerName {
			continue
		}
		if !ready[container.Name] {
			return endpointHealth, false
		}
	}
	// Native sidecars (restartable init containers) count toward readiness too.
	for _, container := range pod.Spec.InitContainers {
		if container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways && !ready[container.Name] {
			return endpointHealth, false
		}
	}
	return api.HealthPassing, true
}

func podConditionTrue(pod corev1.Pod, conditionType corev1.PodConditionType) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// requestsForCredentialGatewayPod maps a credential-injection gateway pod to
// the Services whose EndpointSlices reference it. EndpointSlices only record
// pod readiness, so without this a container readiness change that does not
// flip pod readiness (for example the processor failing while the Vault Agent
// is restarting) would leave a stale health check in Consul.
func (r *Controller) requestsForCredentialGatewayPod(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || !isCredentialInjectionGateway(*pod) {
		return nil
	}
	var slices discoveryv1.EndpointSliceList
	if err := r.Client.List(ctx, &slices, client.InNamespace(pod.Namespace)); err != nil {
		r.Log.Error(err, "failed to list EndpointSlices for credential-injection gateway pod", "name", pod.Name, "ns", pod.Namespace)
		return nil
	}
	seen := make(map[string]bool)
	var requests []reconcile.Request
	for _, slice := range slices.Items {
		serviceName := slice.Labels[discoveryv1.LabelServiceName]
		if serviceName == "" || seen[serviceName] {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name == pod.Name {
				seen[serviceName] = true
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{Namespace: pod.Namespace, Name: serviceName},
				})
				break
			}
		}
	}
	return requests
}
