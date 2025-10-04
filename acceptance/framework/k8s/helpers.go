// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubernetesAPIServerHostFromOptions returns the Kubernetes API server host from options.
func KubernetesAPIServerHostFromOptions(t *testing.T, options *terratestk8s.KubectlOptions) string {
	t.Helper()

	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)

	config, err := terratestk8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)

	return config.Host
}

// WaitForAllPodsToBeReady waits until all pods with the provided podLabelSelector
// are in the ready status. It checks every 2 second for 20 minutes.
// If there is at least one container in a pod that isn't ready after that,
// it fails the test.
func WaitForAllPodsToBeReady(t *testing.T, client kubernetes.Interface, namespace, podLabelSelector string) {
	t.Helper()

	// Wait up to 20m.
	// On Azure, volume provisioning can sometimes take close to 5 min,
	// so we need to give a bit more time for pods to become healthy.
	counter := &retry.Counter{Count: 10 * 60, Wait: 2 * time.Second}
	logger.Logf(t, "Waiting %s for pods with label %q to be ready.", time.Duration(counter.Count*int(counter.Wait)), podLabelSelector)

	retry.RunWith(counter, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{LabelSelector: podLabelSelector})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items)

		var notReadyPods []string
		for _, pod := range pods.Items {
			if !IsReady(pod) {
				notReadyPods = append(notReadyPods, pod.Name)
			}
		}
		if len(notReadyPods) > 0 {
			r.Errorf("%d pods are not ready: %s", len(notReadyPods), strings.Join(notReadyPods, ","))
		}
	})
	logger.Log(t, "Finished waiting for pods to be ready.")
}

// IsReady returns true if pod is ready.
func IsReady(pod corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodPending {
		return false
	}

	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			if cond.Status == corev1.ConditionTrue {
				return true
			} else {
				return false
			}
		}
	}

	return false
}

// KubernetesAPIServerHost returns the Kubernetes API server URL depending on test configuration.
func KubernetesAPIServerHost(t *testing.T, cfg *config.TestConfig, ctx environment.TestContext) string {
	var k8sAPIHost string
	// When running on kind, the kube API address in kubeconfig will have a localhost address
	// which will not work from inside the container. That's why we need to use the endpoints address instead
	// which will point the node IP.
	if cfg.UseKind {
		// The Kubernetes AuthMethod host is read from the endpoints for the Kubernetes service.
		kubernetesEndpoint, err := ctx.KubernetesClient(t).CoreV1().Endpoints("default").Get(context.Background(), "kubernetes", metav1.GetOptions{})
		require.NoError(t, err)
		k8sAPIHost = fmt.Sprintf("https://%s", net.JoinHostPort(kubernetesEndpoint.Subsets[0].Addresses[0].IP, strconv.Itoa(int(kubernetesEndpoint.Subsets[0].Ports[0].Port))))
	} else {
		k8sAPIHost = KubernetesAPIServerHostFromOptions(t, ctx.KubectlOptions(t))
	}

	return k8sAPIHost
}

// ServiceHost returns a host for a Kubernetes service depending on test configuration.
func ServiceHost(t *testing.T, cfg *config.TestConfig, ctx environment.TestContext, serviceName string) string {
	if cfg.UseKind {
		nodeList, err := ctx.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		require.NoError(t, err)
		// Get the address of the (only) node from the Kind cluster.
		return nodeList.Items[0].Status.Addresses[0].Address
	} else {
		var host string
		// It can take some time for the load balancers to be ready and have an IP/Hostname.
		// Wait for 5 minutes before failing.
		retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 600}, t, func(r *retry.R) {
			svc, err := ctx.KubernetesClient(r).CoreV1().Services(ctx.KubectlOptions(r).Namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
			require.NoError(r, err)
			require.NotEmpty(r, svc.Status.LoadBalancer.Ingress)
			// On AWS, load balancers have a hostname for ingress, while on Azure and GCP
			// load balancers have IPs.
			if svc.Status.LoadBalancer.Ingress[0].Hostname != "" {
				host = svc.Status.LoadBalancer.Ingress[0].Hostname
			} else {
				host = svc.Status.LoadBalancer.Ingress[0].IP
			}
		})
		return host
	}
}

// CopySecret copies a Kubernetes secret from one cluster to another.
func CopySecret(t *testing.T, sourceContext, destContext environment.TestContext, secretName string) {
	t.Helper()
	var secret *corev1.Secret
	var err error
	retry.Run(t, func(r *retry.R) {
		secret, err = sourceContext.KubernetesClient(r).CoreV1().Secrets(sourceContext.KubectlOptions(r).Namespace).Get(context.Background(), secretName, metav1.GetOptions{})
		secret.ResourceVersion = ""
		require.NoError(r, err)
	})
	secret.Namespace = destContext.KubectlOptions(t).Namespace
	_, err = destContext.KubernetesClient(t).CoreV1().Secrets(destContext.KubectlOptions(t).Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
}
