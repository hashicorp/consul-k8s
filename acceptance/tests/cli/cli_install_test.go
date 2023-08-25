// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ipv4RegEx = "(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)"

// TestInstall tests that we can install consul service mesh with the CLI
// and see that services can connect.
func TestInstall(t *testing.T) {
	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cli, err := cli.NewCLI()
			require.NoError(t, err)

			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.CLI,
				Secure:      c.secure,
				ReleaseName: consul.CLIReleaseName,
				Ctx:         ctx,
				Cfg:         cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			// Run proxy list and get the two results.
			listOut, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "list")
			require.NoError(t, err)
			logger.Log(t, string(listOut))
			list := translateListOutput(listOut)
			require.Equal(t, 2, len(list))
			for _, proxyType := range list {
				require.Equal(t, "Sidecar", proxyType)
			}

			// Run proxy read and check that the connection is present in the output.
			retrier := &retry.Timer{Timeout: 160 * time.Second, Wait: 2 * time.Second}
			retry.RunWith(retrier, t, func(r *retry.R) {
				for podName := range list {
					out, err := cli.Run(t, ctx.KubectlOptions(t), "proxy", "read", podName)
					require.NoError(r, err)

					output := string(out)
					logger.Log(t, output)

					// Both proxies must see their own local agent and app as clusters.
					require.Regexp(r, "consul-dataplane.*STATIC", output)
					require.Regexp(r, "local_app.*STATIC", output)

					// Static Client must have Static Server as a cluster and endpoint.
					if strings.Contains(podName, "static-client") {
						require.Regexp(r, "static-server.*static-server\\.default\\.dc1\\.internal.*EDS", output)
						require.Regexp(r, ipv4RegEx+".*static-server", output)
					}
				}
			})

			// Troubleshoot: Get the client pod so we can portForward to it and get the 'troubleshoot upstreams' output
			clientPod, err := connHelper.Ctx.KubernetesClient(t).CoreV1().Pods(connHelper.Ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
			})
			require.NoError(t, err)

			clientPodName := clientPod.Items[0].Name
			upstreamsOut, err := cli.Run(t, ctx.KubectlOptions(t), "troubleshoot", "upstreams", "-pod", clientPodName)
			logger.Log(t, string(upstreamsOut))
			require.NoError(t, err)

			if cfg.EnableTransparentProxy {
				// If tproxy is enabled we are looking for the upstream ip which is the ClusterIP of the Kubernetes Service
				serverService, err := connHelper.Ctx.KubernetesClient(t).CoreV1().Services(connHelper.Ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
					FieldSelector: "metadata.name=static-server",
				})
				require.NoError(t, err)
				serverIP := serverService.Items[0].Spec.ClusterIP

				proxyOut, err := cli.Run(t, ctx.KubectlOptions(t), "troubleshoot", "proxy", "-pod", clientPodName, "-upstream-ip", serverIP)
				require.NoError(t, err)
				require.Regexp(t, "Upstream resources are valid", string(proxyOut))
				logger.Log(t, string(proxyOut))
			} else {
				// With tproxy disabled and explicit upstreams we need the envoy-id of the server
				require.Regexp(t, "static-server", string(upstreamsOut))

				proxyOut, err := cli.Run(t, ctx.KubectlOptions(t), "troubleshoot", "proxy", "-pod", clientPodName, "-upstream-envoy-id", "static-server")
				require.NoError(t, err)
				require.Regexp(t, "Upstream resources are valid", string(proxyOut))
				logger.Log(t, string(proxyOut))
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// translateListOutput takes the raw output from the proxy list command and
// translates the table into a map.
func translateListOutput(raw []byte) map[string]string {
	formatted := make(map[string]string)
	for _, pod := range strings.Split(strings.TrimSpace(string(raw)), "\n")[3:] {
		row := strings.Split(strings.TrimSpace(pod), "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else if len(row) == 2 {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
