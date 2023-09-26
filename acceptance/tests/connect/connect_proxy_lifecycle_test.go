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
	cfg.SkipWhenOpenshiftAndCNI(t)

	t.Skipf("TODO(flaky-1.17): NET-XXXX")

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
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})

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

	if cfg.EnableTransparentProxy {
		// TODO t-eckert: Come back and review this with wise counsel.
		t.Skip("Skipping test because transparent proxy is enabled")
	}

	defaultGracePeriod := 5

	cases := map[string]int{
		"../fixtures/cases/jobs/job-client-inject":                  defaultGracePeriod,
		"../fixtures/cases/jobs/job-client-inject-grace-period-0s":  0,
		"../fixtures/cases/jobs/job-client-inject-grace-period-10s": 10,
	}

	// Set up the installation and static-server once.
	ctx := suite.Environment().DefaultContext(t)
	releaseName := helpers.RandomName()

	connHelper := connhelper.ConnectHelper{
		ClusterKind: consul.Helm,
		ReleaseName: releaseName,
		Ctx:         ctx,
		Cfg:         cfg,
		HelmValues: map[string]string{
			"connectInject.sidecarProxy.lifecycle.defaultShutdownGracePeriodSeconds": strconv.FormatInt(int64(defaultGracePeriod), 10),
			"connectInject.sidecarProxy.lifecycle.defaultEnabled":                    strconv.FormatBool(true),
		},
	}

	connHelper.Setup(t)
	connHelper.Install(t)
	connHelper.DeployServer(t)

	logger.Log(t, "waiting for static-server to be registered with Consul")
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		for _, name := range []string{
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

	// Iterate over the Job cases and test connection.
	for path, gracePeriod := range cases {
		connHelper.DeployJob(t, path) // Default case.

		logger.Log(t, "waiting for job-client to be registered with Consul")
		retry.RunWith(&retry.Timer{Timeout: 300 * time.Second, Wait: 5 * time.Second}, t, func(r *retry.R) {
			for _, name := range []string{
				"job-client",
				"job-client-sidecar-proxy",
			} {
				logger.Logf(t, "checking for %s service in Consul catalog", name)
				instances, _, err := connHelper.ConsulClient.Catalog().Service(name, "", nil)
				r.Check(err)

				if len(instances) != 1 {
					r.Errorf("expected 1 instance of %s", name)
				}
			}
		})

		connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{ClientType: connhelper.JobName})

		// Get job-client pod name
		ns := ctx.KubectlOptions(t).Namespace
		pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(
			context.Background(),
			metav1.ListOptions{
				LabelSelector: "app=job-client",
			},
		)
		require.NoError(t, err)
		require.Len(t, pods.Items, 1)
		jobName := pods.Items[0].Name

		// Exec into job and send shutdown request to running proxy.
		// curl --max-time 2 -s -f -XPOST http://127.0.0.1:20600/graceful_shutdown
		sendProxyShutdownArgs := []string{"exec", jobName, "-c", connhelper.JobName, "--", "curl", "--max-time", "2", "-s", "-f", "-XPOST", "http://127.0.0.1:20600/graceful_shutdown"}
		_, err = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), sendProxyShutdownArgs...)
		require.NoError(t, err)

		logger.Log(t, "Proxy killed...")

		args := []string{"exec", jobName, "-c", connhelper.JobName, "--", "curl", "-vvvsSf"}
		if cfg.EnableTransparentProxy {
			args = append(args, "http://static-server")
		} else {
			args = append(args, "http://localhost:1234")
		}

		if gracePeriod > 0 {
			logger.Log(t, "Checking if connection successful within grace period...")
			retry.RunWith(&retry.Timer{Timeout: time.Duration(gracePeriod) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
				output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
				require.NoError(r, err)
				require.True(r, !strings.Contains(output, "curl: (7) Failed to connect"))
			})
			//wait for the grace period to end after successful request
			time.Sleep(time.Duration(gracePeriod) * time.Second)
		}

		// Test that requests fail once grace period has ended, or there was no grace period set.
		logger.Log(t, "Checking that requests fail now that proxy is killed...")
		retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
			output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), args...)
			require.Error(r, err)
			require.True(r, strings.Contains(output, "curl: (7) Failed to connect"))
		})

		// Wait for the job to complete.
		retry.RunWith(&retry.Timer{Timeout: 4 * time.Minute, Wait: 30 * time.Second}, t, func(r *retry.R) {
			logger.Log(t, "Checking if job completed...")
			jobs, err := ctx.KubernetesClient(t).BatchV1().Jobs(ns).List(
				context.Background(),
				metav1.ListOptions{
					LabelSelector: "app=job-client",
				},
			)
			require.NoError(r, err)
			require.True(r, jobs.Items[0].Status.Succeeded == 1)
		})

		// Delete the job and its associated Pod.
		pods, err = ctx.KubernetesClient(t).CoreV1().Pods(ns).List(
			context.Background(),
			metav1.ListOptions{
				LabelSelector: "app=job-client",
			},
		)
		require.NoError(t, err)
		podName := pods.Items[0].Name

		err = ctx.KubernetesClient(t).BatchV1().Jobs(ns).Delete(context.Background(), "job-client", metav1.DeleteOptions{})
		require.NoError(t, err)

		err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
		require.NoError(t, err)

		logger.Log(t, "ensuring job is deregistered after termination")
		retry.RunWith(&retry.Timer{Timeout: 4 * time.Minute, Wait: 30 * time.Second}, t, func(r *retry.R) {
			for _, name := range []string{
				"job-client",
				"job-client-sidecar-proxy",
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
	}
}
