// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

type LifecycleShutdownConfig struct {
	secure     bool
	helmValues map[string]string
}

const (
	helmDrainListenersKey     = "connectInject.sidecarProxy.lifecycle.defaultEnableShutdownDrainListeners"
	helmGracePeriodSecondsKey = "connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds"
)

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_ProxyLifecycleShutdown(t *testing.T) {
	cfg := suite.Config()
	cfg.SkipWhenOpenshiftAndCNI(t)

	for _, testCfg := range []LifecycleShutdownConfig{
		{secure: false, helmValues: map[string]string{
			helmDrainListenersKey:     "true",
			helmGracePeriodSecondsKey: "15",
		}},
		{secure: true, helmValues: map[string]string{
			helmDrainListenersKey:     "true",
			helmGracePeriodSecondsKey: "15",
		}},
		{secure: false, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "15",
		}},
		{secure: true, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "15",
		}},
		{secure: false, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "0",
		}},
		{secure: true, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "0",
		}},
	} {
		// Determine if listeners should be expected to drain inbound connections
		var drainListenersEnabled bool
		var err error
		val, ok := testCfg.helmValues[helmDrainListenersKey]
		if ok {
			drainListenersEnabled, err = strconv.ParseBool(val)
			require.NoError(t, err)
		}

		// Determine expected shutdown grace period
		var gracePeriodSeconds int64
		val, ok = testCfg.helmValues[helmGracePeriodSecondsKey]
		if ok {
			gracePeriodSeconds, err = strconv.ParseInt(val, 10, 64)
			require.NoError(t, err)
		} else {
			// Half of the helm default to speed tests up
			gracePeriodSeconds = 15
		}

		name := fmt.Sprintf("secure: %t, drainListeners: %t, gracePeriodSeconds: %d", testCfg.secure, drainListenersEnabled, gracePeriodSeconds)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			releaseName := helpers.RandomName()

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.Helm,
				Secure:      testCfg.secure,
				ReleaseName: releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
				HelmValues:  testCfg.helmValues,
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
					instances, _, err := connHelper.ConsulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					if len(instances) != 1 {
						r.Errorf("expected 1 instance of %s", name)
					}
				}
			})

			if testCfg.secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}

			connHelper.TestConnectionSuccess(t)

			// Get static-client pod name
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

			// We should terminate the pods shortly after envoy gracefully shuts down in our 15s test cases.
			var terminationGracePeriod int64 = 16
			logger.Logf(t, "killing the %q pod with %dseconds termination grace period", clientPodName, terminationGracePeriod)
			err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), clientPodName, metav1.DeleteOptions{GracePeriodSeconds: &terminationGracePeriod})
			require.NoError(t, err)

			// Exec into terminating pod, not just any static-client pod
			args := []string{"exec", clientPodName, "-c", connhelper.StaticClientName, "--", "curl", "-vvvsSf"}

			if cfg.EnableTransparentProxy {
				args = append(args, "http://static-server")
			} else {
				args = append(args, "http://localhost:1234")
			}

			if gracePeriodSeconds > 0 {
				// Ensure outbound requests are still successful during grace period.
				gracePeriodTimer := time.NewTimer(time.Duration(gracePeriodSeconds) * time.Second)
			gracePeriodLoop:
				for {
					select {
					case <-gracePeriodTimer.C:
						break gracePeriodLoop
					default:
						retrier := &retry.Counter{Count: 3, Wait: 1 * time.Second}
						retry.RunWith(retrier, t, func(r *retry.R) {
							output, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t), args...)
							if err != nil {
								r.Errorf(err.Error())
								return
							}
							require.Condition(r, func() bool {
								return !strings.Contains(output, "curl: (7) Failed to connect")
							}, fmt.Sprintf("Error: %s", output))
						})

						// If listener draining is enabled, ensure inbound
						// requests are rejected during grace period.
						if !drainListenersEnabled {
							connHelper.TestConnectionSuccess(t)
						}
						// TODO: check that the connection is unsuccessful when drainListenersEnabled is true
						// dans note: I found it isn't sufficient to use the existing TestConnectionFailureWithoutIntention

						time.Sleep(2 * time.Second)
					}
				}
			} else {
				// Ensure outbound requests fail because proxy has terminated
				retry.RunWith(&retry.Timer{Timeout: time.Duration(terminationGracePeriod) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
					output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
					require.Error(r, err)
					require.Condition(r, func() bool {
						exists := false
						if strings.Contains(output, "curl: (7) Failed to connect") {
							exists = true
						}
						return exists
					})
				})
			}

			logger.Log(t, "ensuring pod is deregistered after termination")
			// We wait an arbitrarily long time here. With the deployment rollout creating additional endpoints reconciles,
			// This can cause the re-queued reconcile used to come back and clean up the service registration to be re-re-queued at
			// 2-3X the intended grace period.
			retry.RunWith(&retry.Timer{Timeout: time.Duration(60) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
				for _, name := range []string{
					"static-client",
					"static-client-sidecar-proxy",
				} {
					logger.Logf(t, "checking for %s service in Consul catalog", name)
					instances, _, err := connHelper.ConsulClient.Catalog().Service(name, "", nil)
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
