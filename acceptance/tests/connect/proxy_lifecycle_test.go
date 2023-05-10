// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_ProxyLifecycleShutdown(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)
			releaseName := helpers.RandomName()

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.Helm,
				Secure:      secure,
				ReleaseName: releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
				HelmValues:  map[string]string{},
			}

			connHelper.Setup(t)
			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)

			// TODO: should this move into connhelper.DeployClientAndServer?
			logger.Log(t, "waiting for static-client and static-server to be registered with Consul")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{
					"static-client",
					"static-client-sidecar-proxy",
					"static-server",
					"static-server-sidecar-proxy",
				} {
					logger.Logf(t, "checking for %s service in Consul catalog", name)
					instances, _, err := connHelper.consulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					if len(instances) != 1 {
						r.Errorf("expected 1 instance of %s", name)
					}
				}
			})

			if secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}

			connHelper.TestConnectionSuccess(t)

			// Get static-client pod name
			// TODO: is this necessary?
			ns := ctx.KubectlOptions(t).Namespace
			pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(
				context.Background(),
				metav1.ListOptions{
					LabelSelector: "app=static-client",
				},
			)
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			clientPodName := pods.Items[0].Name

			var gracePeriod int64 = 30
			logger.Logf(t, "killing the %q pod with %dseconds termination grace period", clientPodName, gracePeriod)
			err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), clientPodName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			require.NoError(t, err)

			// Exec into terminating pod, not just any static-client pod
			args := []string{"exec", clientPodName, "-c", connhelper.StaticClientName, "--", "curl", "-vvvsSf"}

			if cfg.EnableTransparentProxy {
				args = append(args, "http://static-server")
			} else {
				args = append(args, "http://localhost:1234")
			}

			failureMessages := []string{
				"curl: (7) Failed to connect",
			}

			// CheckStaticServerConnectionMultipleFailureMessages will retry until the Envoy sidecar container
			// shuts down, causing the connection to fail. We are expecting a "connection reset by peer" error
			// because during pod shutdown, there will be no healthy proxy host to connect to. We can't assert
			// that we receive an empty reply from server, because that is the case when a connection is
			// unsuccessful due to intentions in other tests.
			retrier := &retry.Timer{Timeout: 30 * time.Second, Wait: 2 * time.Second}
			retry.RunWith(retrier, t, func(r *retry.R) {
				output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
				require.Error(r, err)
				require.Condition(r, func() bool {
					exists := false
					for _, msg := range failureMessages {
						if strings.Contains(output, msg) {
							exists = true
						}
					}
					return exists
				})
			})

			logger.Log(t, "ensuring pod is deregistered after termination")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{
					"static-client",
					"static-client-sidecar-proxy",
				} {
					logger.Logf(t, "checking for %s service in Consul catalog", name)
					instances, _, err := connHelper.consulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					for _, instance := range instances {
						if strings.Contains(instance.ServiceID, clientPodName) {
							r.Errorf("%s is still registered", instance.ServiceID)
						}
					}
				}
			})
		})
	}
}
