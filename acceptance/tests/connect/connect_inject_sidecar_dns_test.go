// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This test proves three things:
//
//  1. The Consul Dataplane sidecar is actually injected with the DNS bind flag
//     (-consul-dns-bind-port=8600), which is what makes it act as the pod's
//     local DNS resolver.
//
//  2. The pod's DNS is redirected to that local sidecar (the pod's
//     /etc/resolv.conf points at 127.0.0.1, not the cluster DNS), so DNS
//     queries never leave the pod to reach the Consul servers directly.
//
//  3. Real traffic keeps working even after the Consul server pods are scaled
//     down. The client must still be able to resolve the other service's
//     "*.virtual.consul" name and connect to it. If DNS resolution were still
//     depending on the Consul servers (and not the sidecar), this would fail
//     once the servers are gone.
func TestConnectInject_SidecarDNSResolver(t *testing.T) {
	cfg := suite.Config()

	if !cfg.EnableTransparentProxy {
		// The sidecar-as-DNS-resolver path relies on transparent proxy DNS
		// redirection, so it can only be exercised in tproxy mode.
		t.Skipf("skipping because -enable-transparent-proxy is not set")
	}
	cfg.SkipWhenOpenshiftAndCNI(t)

	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			releaseName := helpers.RandomName()

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.Helm,
				Secure:      secure,
				ReleaseName: releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
				// Enable Consul DNS so that the dataplane sidecar is configured
				// to answer DNS for the pod. dns.enabled and dns.enableRedirection
				// are already set by the connect helper's default helm values.
				HelmValues: map[string]string{
					"dns.enabled":           "true",
					"dns.enableRedirection": "true",
				},
			}

			connHelper.Setup(t)
			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)

			if secure {
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			opts := connHelper.KubectlOptsForApp(t)

			// 1. Verify the static-client pod's dataplane sidecar was injected
			// with the Consul DNS bind flag. This is what makes the sidecar act
			// as the pod-local DNS resolver instead of the pod deferring DNS to
			// the Consul servers.
			logger.Log(t, "verifying the dataplane sidecar is configured as the pod-local DNS resolver")
			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(opts.Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
				FieldSelector: "status.phase=Running",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)

			var foundDNSBindFlag bool
			for _, container := range podList.Items[0].Spec.Containers {
				if container.Name != "consul-dataplane" {
					continue
				}
				if strings.Contains(strings.Join(container.Args, " "),
					"-consul-dns-bind-port="+strconv.Itoa(8600)) {
					foundDNSBindFlag = true
				}
			}
			require.True(t, foundDNSBindFlag,
				"expected the consul-dataplane sidecar to be started with -consul-dns-bind-port=8600 so that it serves DNS locally for the pod")

			// 2. Verify the pod's DNS is redirected to the local sidecar. When
			// Consul DNS redirection is enabled the webhook rewrites the pod's
			// DNS config to point at 127.0.0.1, so DNS queries are answered by
			// the sidecar in the same pod rather than being sent to the servers.
			logger.Log(t, "verifying the pod's DNS is pointed at the local sidecar (127.0.0.1)")
			resolvConf, err := k8s.RunKubectlAndGetOutputE(t, opts,
				"exec", "deploy/"+connhelper.StaticClientName, "-c", connhelper.StaticClientName,
				"--", "cat", "/etc/resolv.conf")
			require.NoError(t, err)
			require.Contains(t, resolvConf, "nameserver 127.0.0.1",
				"expected the pod's resolv.conf to use the local sidecar resolver at 127.0.0.1, got:\n%s", resolvConf)

			// 3. Remove the Consul server pods after the mesh is fully configured.
			// The local sidecar should still be able to resolve *.virtual.consul
			// and the client should still be able to connect. If resolution were
			// still falling back to the Consul servers, these checks would fail.
			logger.Log(t, "scaling down Consul servers to prove virtual DNS resolution does not depend on server-backed DNS")
			clusterOpts := ctx.KubectlOptions(t)
			serverStatefulSet := fmt.Sprintf("statefulset/%s-consul-server", releaseName)
			k8s.KubectlScale(t, clusterOpts, serverStatefulSet, 0)

			retry.Run(t, func(r *retry.R) {
				serverPods, listErr := ctx.KubernetesClient(t).CoreV1().Pods(clusterOpts.Namespace).List(context.Background(), metav1.ListOptions{
					LabelSelector: fmt.Sprintf("app=consul,component=server,release=%s", releaseName),
				})
				require.NoError(r, listErr)
				require.Len(r, serverPods.Items, 0, "expected all Consul server pods to be gone before verifying sidecar-local DNS resolution")
			})

			virtualHost := "static-server.virtual.consul"
			logger.Logf(t, "verifying %s still resolves from the client pod after Consul servers are down", virtualHost)
			retry.Run(t, func(r *retry.R) {
				lookupOutput, lookupErr := k8s.RunKubectlAndGetOutputE(r, opts,
					"exec", "deploy/"+connhelper.StaticClientName, "-c", connhelper.StaticClientName,
					"--", "getent", "hosts", virtualHost)
				require.NoError(r, lookupErr)
				require.Contains(r, lookupOutput, virtualHost)
			})

			virtualServerURL := "http://static-server.virtual.consul"
			logger.Logf(t, "verifying connection to %s still succeeds after Consul servers are down", virtualServerURL)
			k8s.CheckStaticServerConnectionSuccessful(t, opts, connhelper.StaticClientName, virtualServerURL)
		})
	}
}
