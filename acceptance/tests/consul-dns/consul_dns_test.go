// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConsulDNS(t *testing.T) {
	cfg := suite.Config()
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}

	if cfg.UseAKS {
		t.Skipf("skipping because -use-aks is set")
	}

	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			env := suite.Environment()
			ctx := env.DefaultContext(t)
			releaseName := helpers.RandomName()

			helmValues := map[string]string{
				"dns.enabled":                  "true",
				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}
			cluster := consul.NewHelmCluster(t, helmValues, ctx, suite.Config(), releaseName)
			cluster.Create(t)

			k8sClient := ctx.KubernetesClient(t)
			contextNamespace := ctx.KubectlOptions(t).Namespace

			dnsService, err := k8sClient.CoreV1().Services(contextNamespace).Get(context.Background(), fmt.Sprintf("%s-%s", releaseName, "consul-dns"), metav1.GetOptions{})
			require.NoError(t, err)

			dnsIP := dnsService.Spec.ClusterIP

			consulServerList, err := k8sClient.CoreV1().Pods(contextNamespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=consul,component=server",
			})
			require.NoError(t, err)

			serverIPs := make([]string, len(consulServerList.Items))
			for _, serverPod := range consulServerList.Items {
				serverIPs = append(serverIPs, serverPod.Status.PodIP)
			}

			dnsPodName := fmt.Sprintf("%s-dns-pod", releaseName)
			dnsTestPodArgs := []string{
				"run", "-it", dnsPodName, "--restart", "Never", "--image", "anubhavmishra/tiny-tools", "--", "dig", fmt.Sprintf("@%s-consul-dns", releaseName), "consul.service.consul",
			}

			helpers.Cleanup(t, suite.Config().NoCleanupOnFailure, suite.Config().NoCleanup, func() {
				// Note: this delete command won't wait for pods to be fully terminated.
				// This shouldn't cause any test pollution because the underlying
				// objects are deployments, and so when other tests create these
				// they should have different pod names.
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "pod", dnsPodName)
			})

			retry.Run(t, func(r *retry.R) {
				logs, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), dnsTestPodArgs...)
				require.NoError(r, err)

				// When the `dig` request is successful, a section of it's response looks like the following:
				//
				// ;; ANSWER SECTION:
				// consul.service.consul.	0	IN	A	<consul-server-pod-ip>
				//
				// ;; Query time: 2 msec
				// ;; SERVER: <dns-ip>#<dns-port>(<dns-ip>)
				// ;; WHEN: Mon Aug 10 15:02:40 UTC 2020
				// ;; MSG SIZE  rcvd: 98
				//
				// We assert on the existence of the ANSWER SECTION, The consul-server IPs being present in the ANSWER SECTION and the the DNS IP mentioned in the SERVER: field

				require.Contains(r, logs, fmt.Sprintf("SERVER: %s", dnsIP))
				require.Contains(r, logs, "ANSWER SECTION:")
				for _, ip := range serverIPs {
					require.Contains(r, logs, fmt.Sprintf("consul.service.consul.\t0\tIN\tA\t%s", ip))
				}
			})
		})
	}
}
