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

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	ver, err := version.NewVersion("1.2.0")
	require.NoError(t, err)
	if cfg.ConsulDataplaneVersion != nil && cfg.ConsulDataplaneVersion.LessThan(ver) {
		t.Skipf("skipping this test because proxy lifecycle management is not supported in consul-dataplane version %v", cfg.ConsulDataplaneVersion.String())
	}

	for _, testCfg := range []LifecycleShutdownConfig{
		{secure: false, helmValues: map[string]string{}},
		{secure: true, helmValues: map[string]string{}},
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

			var terminationGracePeriod int64 = 60
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
				// Ensure outbound requests are still successful during grace
				// period.
				retry.RunWith(&retry.Timer{Timeout: time.Duration(gracePeriodSeconds) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
					output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
					require.NoError(r, err)
					require.Condition(r, func() bool {
						exists := false
						if strings.Contains(output, "curl: (7) Failed to connect") {
							exists = true
						}
						return !exists
					})
				})

				// If listener draining is enabled, ensure inbound
				// requests are rejected during grace period.
				// connHelper.TestConnectionSuccess(t)
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
			retry.Run(t, func(r *retry.R) {
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

func TestConnectInject_ProxyLifecycleShutdownJob(t *testing.T) {
	cfg := suite.Config()

	ver, err := version.NewVersion("1.2.0")
	require.NoError(t, err)
	if cfg.ConsulDataplaneVersion != nil && cfg.ConsulDataplaneVersion.LessThan(ver) {
		t.Skipf("skipping this test because proxy lifecycle management is not supported in consul-dataplane version %v", cfg.ConsulDataplaneVersion.String())
	}

	//TODO: Create 2 helm configurations. 1 secure and 1 not secure

	// Two installations of Consul, 1 secure and 1 not secure ^^. Each of the configurations will run different job versions.
	secureConfig := []bool{true, false}
	//Figure out which helm values to use
	var testCfg LifecycleShutdownConfig = LifecycleShutdownConfig{secure: false, helmValues: map[string]string{}}
	for _, config := range secureConfig {

		t.Run(fmt.Sprintf("job test, secure: %b", config), func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			releaseName := helpers.RandomName()

			connHelper := connhelper.ConnectHelper{
				ClusterKind: consul.Helm,
				Secure:      config,
				ReleaseName: releaseName,
				Ctx:         ctx,
				Cfg:         cfg,
				HelmValues:  testCfg.helmValues,
			}

			connHelper.Setup(t)
			connHelper.Install(t)
			connHelper.DeployJobAndServer(t)

			logger.Log(t, "waiting for test-job and static-server to be registered with Consul")
			retry.RunWith(&retry.Timer{Timeout: 300 * time.Second, Wait: 5 * time.Second}, t, func(r *retry.R) {
				for _, name := range []string{
					"test-job",
					"test-job-sidecar-proxy",
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

			//TODO: Modify connhelper to test the connection between job and server, create intention between job and server

			/*if testCfg.secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}*/
			//TODO :test connection success b/w job and server
			connHelper.TestConnectionSuccess(t)

			// Get test-job pod name
			ns := ctx.KubectlOptions(t).Namespace
			pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(
				context.Background(),
				metav1.ListOptions{
					LabelSelector: "app=test-job",
				},
			)
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			jobName := pods.Items[0].Name

			/*getOutputArgs := []string{"logs", "pods/" + clientPodName}
			//continue retrying until job is finished/ proxy has been killed
			retry.Run(t, func(r *retry.R) {
				output, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), getOutputArgs...)
				r.Check(err)
				if !strings.Contains(output, "Proxy Killed") {
					r.Errorf("Proxy has not been killed yet...")
				}
			}) */

			//TODO: Exec into job container and kill the proxy

			//--max-time 2 -s -f -XPOST http://127.0.0.1:20600/graceful_shutdown
			sendProxyShutdownArgs := []string{"exec", jobName, "-c", connhelper.JobName, "--", "curl", "--max-time", "2", "-s", "-f", "-XPOST", "http://127.0.0.1:20600/graceful_shutdown"}
			output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), sendProxyShutdownArgs...)
			fmt.Print(output)
			logger.Log(t, "Proxy killed...")
			//jobShutdownTime := time.Now()
			jobShutdownDurationStr, _ := pods.Items[0].GetAnnotations()["time_after_proxy_death"]
			jobShutdownDuration, _ := strconv.Atoi(jobShutdownDurationStr)

			var terminationGracePeriod int64 = 60
			/*logger.Logf(t, "killing the %q pod with %dseconds termination grace period", clientPodName, terminationGracePeriod)
			err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), clientPodName, metav1.DeleteOptions{GracePeriodSeconds: &terminationGracePeriod})
			require.NoError(t, err)
			*/
			// Exec into terminating pod, not just any test-job pod

			args := []string{"exec", jobName, "-c", connhelper.JobName, "--", "curl", "-vvvsSf"}

			if cfg.EnableTransparentProxy {
				args = append(args, "http://static-server")
			} else {
				args = append(args, "http://localhost:1234")
			}
			//Only try to send requests from the pod as long as you expect the job to be alive.
			retry.RunWith(&retry.Timer{Timeout: time.Duration(jobShutdownDuration) * time.Second, Wait: 1 * time.Second}, t, func(r *retry.R) {

				// Ensure outbound requests are still successful during grace
				// period.
				retry.RunWith(&retry.Timer{Timeout: time.Duration(jobShutdownDuration) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
					output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
					require.NoError(r, err)
					require.Condition(r, func() bool {
						exists := false
						if strings.Contains(output, "curl: (7) Failed to connect") {
							exists = true
						}
						return !exists
					})
				})

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

			})

			logger.Log(t, "ensuring pod is deregistered after termination")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{
					"test-job",
					"test-job-sidecar-proxy",
				} {
					logger.Logf(t, "checking for %s service in Consul catalog", name)
					instances, _, err := connHelper.ConsulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					for _, instance := range instances {
						if strings.Contains(instance.ServiceID, jobName) {
							r.Errorf("%s is still registered", instance.ServiceID)
						}
					}
				}
			})
		})
	}
}
