// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// Test that when multiple terminating gateways are deployed in the same
// release, each gateway's Kubernetes Service only selects the pods of that
// gateway and not the pods of every terminating gateway in the release.
func TestTerminatingGateway_MultipleGatewaysServiceSelector(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	gatewayNames := []string{"egress-gw-a", "egress-gw-b"}

	helmValues := map[string]string{
		"connectInject.enabled":       "true",
		"terminatingGateways.enabled": "true",
	}
	for i, name := range gatewayNames {
		helmValues[fmt.Sprintf("terminatingGateways.gateways[%d].name", i)] = name
		helmValues[fmt.Sprintf("terminatingGateways.gateways[%d].replicas", i)] = "1"
	}

	logger.Log(t, "creating consul cluster")
	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	client := ctx.KubernetesClient(t)
	ns := ctx.KubectlOptions(t).Namespace

	// Collect the IPs of each gateway's pods, using the label the Deployment
	// sets on its pods.
	podIPs := make(map[string][]string)
	for _, name := range gatewayNames {
		fullName := fmt.Sprintf("%s-consul-%s", releaseName, name)
		retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
			pods, err := client.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("component=terminating-gateway,release=%s,terminating-gateway-name=%s", releaseName, fullName),
			})
			require.NoError(r, err)
			require.Len(r, pods.Items, 1)
			require.Equal(r, corev1.PodRunning, pods.Items[0].Status.Phase)
			require.NotEmpty(r, pods.Items[0].Status.PodIP)
			podIPs[name] = []string{pods.Items[0].Status.PodIP}
		})
	}
	require.NotEqual(t, podIPs[gatewayNames[0]], podIPs[gatewayNames[1]])

	for _, name := range gatewayNames {
		fullName := fmt.Sprintf("%s-consul-%s", releaseName, name)

		logger.Logf(t, "checking service selector for %s", fullName)
		svc, err := client.CoreV1().Services(ns).Get(context.Background(), fullName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, fullName, svc.Spec.Selector["terminating-gateway-name"])

		logger.Logf(t, "checking endpoints for %s only contain its own pods", fullName)
		retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
			slices, err := client.DiscoveryV1().EndpointSlices(ns).List(context.Background(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("%s=%s", discoveryv1.LabelServiceName, fullName),
			})
			require.NoError(r, err)

			var ips []string
			for _, slice := range slices.Items {
				for _, ep := range slice.Endpoints {
					ips = append(ips, ep.Addresses...)
				}
			}
			sort.Strings(ips)
			require.Equal(r, podIPs[name], ips)
		})
	}
}
