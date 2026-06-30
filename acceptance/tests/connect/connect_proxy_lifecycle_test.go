// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
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
			helmGracePeriodSecondsKey: "5",
		}},
		{secure: true, helmValues: map[string]string{
			helmDrainListenersKey:     "true",
			helmGracePeriodSecondsKey: "5",
		}},
		{secure: false, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "5",
		}},
		{secure: true, helmValues: map[string]string{
			helmDrainListenersKey:     "false",
			helmGracePeriodSecondsKey: "5",
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
			// 5s should be a good amount of time to confirm the pod doesn't terminate
			gracePeriodSeconds = 5
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

			retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
				peers, err := connHelper.ConsulClient.Status().Peers()
				require.NoError(r, err)
				require.Len(r, peers, 1)
			})

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
					logger.Logf(r, "checking for %s service in Consul catalog", name)
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
			var pods *corev1.PodList
			var ns string
			var err error
			retry.Run(t, func(r *retry.R) {
				// Get static-client pod name
				ns = ctx.KubectlOptions(r).Namespace
				pods, err = ctx.KubernetesClient(r).CoreV1().Pods(ns).List(
					context.Background(),
					metav1.ListOptions{
						LabelSelector: "app=static-client",
					},
				)
				require.NoError(r, err)
				require.Len(r, pods.Items, 1)
			})
			clientPodName := pods.Items[0].Name

			// We should terminate the pods shortly after envoy gracefully shuts down in our 5s test cases.
			var terminationGracePeriod int64 = 6
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
				ocpPodGoneEarly := false
			gracePeriodLoop:
				for {
					select {
					case <-gracePeriodTimer.C:
						break gracePeriodLoop
					default:
						retrier := &retry.Counter{Count: 3, Wait: 1 * time.Second}
						retry.RunWith(retrier, t, func(r *retry.R) {
							logger.Logf(r, "checking connectivity to static-server from terminating pod %s", clientPodName)
							output, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t), args...)
							if err != nil {
								// On OCP, containers may terminate faster than the grace period;
								// "not found" in output means the pod already exited cleanly.
								if cfg.EnableOpenshift && strings.Contains(output, "not found") {
									ocpPodGoneEarly = true
									return
								}
								r.Errorf("%v", err.Error())
								return
							}
							require.Condition(r, func() bool {
								return !strings.Contains(output, "curl: (7) Failed to connect")
							}, fmt.Sprintf("Error: %s", output))
						})

						if ocpPodGoneEarly {
							break gracePeriodLoop
						}

						// If listener draining is disabled, ensure inbound
						// requests are accepted during grace period.
						if !drainListenersEnabled {
							connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
						}
						// TODO: check that the connection is unsuccessful when drainListenersEnabled is true
						// dans note: I found it isn't sufficient to use the existing TestConnectionFailureWithoutIntention

						time.Sleep(2 * time.Second)
					}
				}
			} else {
				// Ensure outbound requests fail because proxy has terminated
				retry.RunWith(&retry.Timer{Timeout: time.Duration(terminationGracePeriod) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
					output, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), args...)
					require.Error(r, err)
					require.Condition(r, func() bool {
						exists := false
						if strings.Contains(output, "curl: (7) Failed to connect") {
							exists = true
						}
						// On OCP, containers exit before curl runs; kubectl exec reports
						// "container is not created or running" instead of curl output.
						if cfg.EnableOpenshift && strings.Contains(output, "container is not created or running") {
							exists = true
						}
						return exists
					})
				})
			}

			// Checks are done, now ensure the pod is fully removed from k8s and Consul.
			logger.Logf(t, "scaling down the static-client deployment to 0 replicas to clean up the terminating pod %q", clientPodName)
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "scale", "deploy/static-client", "--replicas=0")

			// Wait for the pod to be fully deleted
			// This ensures that the ACL token associated with pod had also been cleaned up
			retrier := &retry.Counter{Count: 60, Wait: 2 * time.Second}
			retry.RunWith(retrier, t, func(r *retry.R) {
				err = ctx.KubernetesClient(r).CoreV1().Pods(ns).Delete(context.Background(), clientPodName, metav1.DeleteOptions{})
				if err != nil {
					if strings.Contains(err.Error(), "not found") {
						logger.Logf(r, "pod %q successfully deleted", clientPodName)
						return
					}
					r.Errorf("error deleting pod %q: %v", clientPodName, err)
				} else {
					r.Errorf("pod %q still exists", clientPodName)
				}
			})

			logger.Log(t, "ensuring pod is deregistered after termination")
			// We wait an arbitrarily long time here. With the deployment rollout creating additional endpoints reconciles,
			// This can cause the re-queued reconcile used to come back and clean up the service registration to be re-re-queued at
			// 2-3X the intended grace period.
			retry.RunWith(&retry.Timer{Timeout: time.Duration(30) * time.Second, Wait: 2 * time.Second}, t, func(r *retry.R) {
				for _, name := range []string{
					"static-client",
					"static-client-sidecar-proxy",
				} {
					logger.Logf(r, "checking for %s service in Consul catalog", name)
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
			logger.Logf(r, "checking for %s service in Consul catalog", name)
			instances, _, err := connHelper.ConsulClient.Catalog().Service(name, "", nil)
			r.Check(err)

			if len(instances) != 1 {
				r.Errorf("expected 1 instance of %s", name)

			}
		}
	})

	// Iterate over the Job cases and test connection.
	for path, gracePeriod := range cases {
		jobPath := path
		if cfg.EnableOpenshift {
			// Job base fixtures hardcode namespace: default, but the OCP test namespace
			// is consul. Create a temp kustomize overlay that overrides the namespace
			// to match the actual test namespace so kubectl apply does not reject it.
			// Use os.MkdirTemp inside the fixture's parent so kustomize relative-path
			// resolution stays within the same directory tree (avoids macOS /private symlink issues).
			absFixture, err := filepath.Abs(path)
			require.NoError(t, err)
			absFixture, err = filepath.EvalSymlinks(absFixture)
			require.NoError(t, err)
			tmpDir, err := os.MkdirTemp(filepath.Dir(absFixture), "ocp-ns-overlay-*")
			require.NoError(t, err)
			t.Cleanup(func() { os.RemoveAll(tmpDir) })
			relPath, err := filepath.Rel(tmpDir, absFixture)
			require.NoError(t, err)
			ns := ctx.KubectlOptions(t).Namespace
			kustomContent := fmt.Sprintf(
				"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n- %s\nnamespace: %s\n",
				relPath, ns,
			)
			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "kustomization.yaml"), []byte(kustomContent), 0644))
			jobPath = tmpDir
			// On OCP: the sidecar readiness probe (TCP :20000) fails after the job container
			// exits so the pod never reaches Ready — bypass DeployJob's kubectl wait entirely.
			//
			// Force-delete any stale job+pods (left by a previous iteration or --no-cleanup-on-failure run).
			// Stale sidecar pods may hang in Terminating indefinitely (consul cluster torn down),
			// so we use --force --grace-period=0 for pods and wait only for the Job *object*
			// (not the pods) to disappear — Job objects delete in < 5 s.
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "job", "job-client", "--ignore-not-found")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "pods",
				"--selector", "job-name=job-client", "--ignore-not-found", "--grace-period=0", "--force")
			// Wait for the Job object to leave the API server (fast). We deliberately do NOT
			// wait for pods to terminate — Terminating pods are filtered out below.
			jobGoneRetrier := &retry.Counter{Count: 24, Wait: 5 * time.Second}
			retry.RunWith(jobGoneRetrier, t, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t),
					"get", "job", "job-client", "--no-headers", "--ignore-not-found")
				if err == nil && strings.TrimSpace(out) != "" {
					r.Errorf("waiting for job-client Job object to leave the API server")
				}
			})
			// Apply the fresh job with the namespace overlay.
			k8s.KubectlApplyK(t, ctx.KubectlOptions(t), jobPath)
			t.Cleanup(func() {
				k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), jobPath)
			})
			// Wait for a fresh (non-Terminating) pod whose job-client container is Running.
			// Stale Terminating pods from the previous iteration are ignored via deletionTimestamp.
			freshPodRetrier := &retry.Counter{Count: 60, Wait: 5 * time.Second}
			retry.RunWith(freshPodRetrier, t, func(r *retry.R) {
				podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(
					context.Background(),
					metav1.ListOptions{LabelSelector: "app=job-client"},
				)
				require.NoError(r, err)
				for i := range podList.Items {
					pod := podList.Items[i]
					if pod.DeletionTimestamp != nil {
						continue // skip Terminating pods from previous iterations/runs
					}
					for _, cs := range pod.Status.ContainerStatuses {
						if cs.Name == connhelper.JobName {
							if cs.State.Running == nil {
								r.Errorf("job-client container not yet Running in pod %s (state: %+v)", pod.Name, cs.State)
							}
							return // fresh pod with Running job-client container found
						}
					}
					// pod exists but container statuses not populated yet
					r.Errorf("job-client container status not yet available in pod %s", pod.Name)
					return
				}
				r.Errorf("no non-terminating job-client pod found yet")
			})
		} else {
			connHelper.DeployJob(t, jobPath)
		}

		logger.Log(t, "waiting for job-client to be registered with Consul")
		retry.RunWith(&retry.Timer{Timeout: 300 * time.Second, Wait: 5 * time.Second}, t, func(r *retry.R) {
			for _, name := range []string{
				"job-client",
				"job-client-sidecar-proxy",
			} {
				logger.Logf(r, "checking for %s service in Consul catalog", name)
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
		// curl --max-time 2 -s -f -XPOST http://localhost:20600/graceful_shutdown
		sendProxyShutdownArgs := []string{"exec", jobName, "-c", connhelper.JobName, "--", "curl", "--max-time", "2", "-s", "-f", "-XPOST", "http://localhost:20600/graceful_shutdown"}
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
				output, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), args...)
				require.NoError(r, err)
				require.True(r, !strings.Contains(output, "curl: (7) Failed to connect"))
			})
			//wait for the grace period to end after successful request
			time.Sleep(time.Duration(gracePeriod) * time.Second)
		}

		// Test that requests fail once grace period has ended, or there was no grace period set.
		logger.Log(t, "Checking that requests fail now that proxy is killed...")
		retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
			output, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(r), args...)
			require.Error(r, err)
			require.True(r, strings.Contains(output, "curl: (7) Failed to connect"))
		})

		// Wait for the job to complete.
		retry.RunWith(&retry.Timer{Timeout: 4 * time.Minute, Wait: 30 * time.Second}, t, func(r *retry.R) {
			logger.Log(r, "Checking if job completed...")
			jobs, err := ctx.KubernetesClient(r).BatchV1().Jobs(ns).List(
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
				logger.Logf(r, "checking for %s service in Consul catalog", name)
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
