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

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject tests that Connect works in a default and a secure installation using Helm CLI.
func TestConnectInject(t *testing.T) {

	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			releaseName := helpers.RandomName()
			connHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             ctx,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}

// TestConnectInject_VirtualIPFailover ensures that KubeDNS entries are saved to the virtual IP address table in Consul.
func TestConnectInject_VirtualIPFailover(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableTransparentProxy {
		// This can only be tested in transparent proxy mode.
		t.SkipNow()
	}
	ctx := suite.Environment().DefaultContext(t)

	releaseName := helpers.RandomName()
	connHelper := connhelper.ConnectHelper{
		ClusterKind:     consul.Helm,
		Secure:          true,
		ReleaseName:     releaseName,
		Ctx:             ctx,
		UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
		Cfg:             cfg,
	}

	connHelper.Setup(t)

	connHelper.Install(t)
	connHelper.CreateResolverRedirect(t)
	connHelper.DeployClientAndServer(t)

	opts := connHelper.KubectlOptsForApp(t)
	k8s.CheckStaticServerConnectionSuccessful(t, opts, "static-client", "http://resolver-redirect")
}

// Test the endpoints controller cleans up force-killed pods.
func TestConnectInject_CleanupKilledPods(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()

			cfg.SkipWhenOpenshiftAndCNI(t)

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled":        "true",
				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating static-client deployment")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			logger.Log(t, "waiting for static-client to be registered with Consul")
			consulClient, _ := consulCluster.SetupConsulClient(t, secure)
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					if len(instances) != 1 {
						r.Errorf("expected 1 instance of %s", name)
					}
				}
			})

			ns := ctx.KubectlOptions(t).Namespace
			pods, err := ctx.KubernetesClient(t).CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-client"})
			require.NoError(t, err)
			require.Len(t, pods.Items, 1)
			podName := pods.Items[0].Name

			// Ensure the token exists
			if secure {
				retry.Run(t, func(r *retry.R) {
					tokens, _, err := consulClient.ACL().TokenListFiltered(
						api.ACLTokenFilterOptions{ServiceName: "static-client"}, nil)
					require.NoError(r, err)
					// Ensure that the tokens exist. Note that we must iterate over the tokens and scan for the name,
					// because older versions of Consul do not support the filtered query param and will return
					// the full list of tokens instead.
					count := 0
					for _, t := range tokens {
						if len(t.ServiceIdentities) > 0 && t.ServiceIdentities[0].ServiceName == "static-client" {
							count++
						}
					}
					require.Greater(r, count, 0)
				})
			}
			logger.Logf(t, "force killing the static-client pod %q", podName)
			var gracePeriod int64 = 0
			err = ctx.KubernetesClient(t).CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			require.NoError(t, err)

			logger.Log(t, "ensuring pod is deregistered")
			retry.Run(t, func(r *retry.R) {
				for _, name := range []string{"static-client", "static-client-sidecar-proxy"} {
					instances, _, err := consulClient.Catalog().Service(name, "", nil)
					r.Check(err)

					for _, instance := range instances {
						if strings.Contains(instance.ServiceID, podName) {
							r.Errorf("%s is still registered", instance.ServiceID)
						}
					}
				}
			})
			// Ensure the token is cleaned up
			if secure {
				retry.Run(t, func(r *retry.R) {
					tokens, _, err := consulClient.ACL().TokenList(nil)
					require.NoError(r, err)
					for _, t := range tokens {
						if strings.Contains(t.Description, podName) {
							r.Errorf("Found a token that was supposed to be deleted for pod %v", podName)
						}
					}
				})
			}
		})
	}
}

const multiport = "multiport"
const multiportAdmin = "multiport-admin"

// TestConnectInject_MultiportRegistrationGate validates the default-off gate at
// the Kubernetes API boundary. Unit tests cover the full conversion behavior;
// this test verifies that the rendered injector accepts and rejects Pods using
// the same first-application-container rules.
func TestConnectInject_MultiportRegistrationGate(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, map[string]string{
		"connectInject.enabled": "true",
	}, ctx, cfg, releaseName)
	consulCluster.Create(t)

	namespace := ctx.KubectlOptions(t).Namespace
	client := ctx.KubernetesClient(t).CoreV1().Pods(namespace)

	tests := map[string]struct {
		annotations  map[string]string
		portCounts   []int
		wantError    string
		wantInjected bool
	}{
		"injection disabled": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "false",
				"consul.hashicorp.com/connect-service-port": "http,metrics",
			},
			portCounts: []int{2},
		},
		"single-port service": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "http",
			},
			portCounts:   []int{1},
			wantInjected: true,
		},
		"multi-port workload selecting one named port": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "metrics",
			},
			portCounts:   []int{2},
			wantInjected: true,
		},
		"multi-port workload selecting one numeric port": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "8081",
			},
			portCounts:   []int{2},
			wantInjected: true,
		},
		"multi-port registration rejected": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "http,metrics",
			},
			portCounts: []int{2},
			wantError:  "multi-port Consul service registration is disabled",
		},
		"transparent proxy false does not bypass gate": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/transparent-proxy":    "false",
				"consul.hashicorp.com/connect-service-port": "http,metrics",
			},
			portCounts: []int{2},
			wantError:  "multi-port Consul service registration is disabled",
		},
		"invalid transparent proxy wins": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/transparent-proxy":    "invalid",
				"consul.hashicorp.com/connect-service-port": "http,metrics",
			},
			portCounts: []int{2},
			wantError:  "couldn't check if transparent proxy is enabled",
		},
		// No connect-service-port annotation: defaultAnnotations derives it from
		// the first container only, so the second container's extra port is not
		// part of the selection and the gate must not fire.
		"later multi-port container does not trigger gate": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject": "true",
			},
			portCounts:   []int{1, 2},
			wantInjected: true,
		},
		// Explicitly selecting two ports is rejected wherever the ports are
		// declared, including when a later container is single-port.
		"explicit two-port selection across containers triggers gate": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "http,c1-p1",
			},
			portCounts: []int{1, 2},
			wantError:  "multi-port Consul service registration is disabled",
		},
		"multi-port first container triggers gate": {
			annotations: map[string]string{
				"consul.hashicorp.com/connect-inject":       "true",
				"consul.hashicorp.com/connect-service-port": "http,metrics",
			},
			portCounts: []int{2, 1},
			wantError:  "multi-port Consul service registration is disabled",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pod := multiportGateTestPod(helpers.RandomName(), tt.annotations, tt.portCounts...)
			created, err := client.Create(context.Background(), pod, metav1.CreateOptions{})
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = client.Delete(context.Background(), created.Name, metav1.DeleteOptions{})
			})

			if tt.wantInjected {
				require.Equal(t, "injected", created.Annotations["consul.hashicorp.com/connect-inject-status"])
				require.Greater(t, len(created.Spec.Containers), len(tt.portCounts))
			} else {
				require.Empty(t, created.Annotations["consul.hashicorp.com/connect-inject-status"])
				require.Len(t, created.Spec.Containers, len(tt.portCounts))
			}
		})
	}
}

func multiportGateTestPod(name string, annotations map[string]string, portCounts ...int) *corev1.Pod {
	containers := make([]corev1.Container, 0, len(portCounts))
	nextPort := int32(8080)
	for containerIndex, portCount := range portCounts {
		container := corev1.Container{
			Name:    fmt.Sprintf("application-%d", containerIndex),
			Image:   "busybox:1.36",
			Command: []string{"sh", "-c", "sleep 3600"},
		}
		for portIndex := 0; portIndex < portCount; portIndex++ {
			portName := fmt.Sprintf("c%d-p%d", containerIndex, portIndex)
			if containerIndex == 0 && portIndex == 0 {
				portName = "http"
			} else if containerIndex == 0 && portIndex == 1 {
				portName = "metrics"
			}
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          portName,
				ContainerPort: nextPort,
			})
			nextPort++
		}
		containers = append(containers, container)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
		Spec:       corev1.PodSpec{Containers: containers},
	}
}

// TestConnectInject_MultiportConversionStrategies exercises the Helm
// post-upgrade hook against the Kubernetes API. It proves that NONE is a
// no-op, TRANSLATE narrows only eligible multi-port pod templates, and
// DECOMMISSION opts every mesh-eligible workload out of injection.
//
// It runs the real Job, so it is also the only coverage for the Job's RBAC:
// the injector ClusterRole must allow updating StatefulSets and DaemonSets, and
// listing Pods and reading ReplicaSets for the scan that reports workloads the
// Job cannot rewrite. Unit tests inject a client and cannot catch a missing
// permission.
func TestConnectInject_MultiportConversionStrategies(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, map[string]string{
		"connectInject.enabled":                              "true",
		"connectInject.multiportServiceRegistration.enabled": "true",
	}, ctx, cfg, releaseName)
	consulCluster.Create(t)

	client := ctx.KubernetesClient(t)
	prefix := helpers.RandomName()
	appNamespace := multiportGateTestAppNamespace(t, cfg, ctx)
	deployments := client.AppsV1().Deployments(appNamespace)
	statefulSets := client.AppsV1().StatefulSets(appNamespace)
	daemonSets := client.AppsV1().DaemonSets(appNamespace)
	objects := []*appsv1.Deployment{
		multiportGateTestDeployment(prefix+"-translate", map[string]string{
			"consul.hashicorp.com/connect-inject":               "true",
			"consul.hashicorp.com/connect-service-port":         "http,metrics",
			"consul.hashicorp.com/connect-service-default-port": "metrics",
		}, 2),
		multiportGateTestDeployment(prefix+"-later-container", map[string]string{
			"consul.hashicorp.com/connect-inject":       "true",
			"consul.hashicorp.com/connect-service-port": "http,c1-p1",
		}, 1, 2),
		multiportGateTestDeployment(prefix+"-single", map[string]string{
			"consul.hashicorp.com/connect-inject":       "true",
			"consul.hashicorp.com/connect-service-port": "http",
		}, 1),
		multiportGateTestDeployment(prefix+"-opted-out", map[string]string{
			"consul.hashicorp.com/connect-inject":       "false",
			"consul.hashicorp.com/connect-service-port": "http,metrics",
		}, 2),
		// A running replica makes the Job's Pod scan resolve a real
		// Pod -> ReplicaSet -> Deployment owner chain. The owner is a converted
		// kind, so the Job must not report it and the upgrade must still
		// succeed. This is the path a fake clientset cannot reproduce.
		multiportGateTestDeploymentWithReplicas(prefix+"-scanned", 1, map[string]string{
			"consul.hashicorp.com/connect-inject":       "true",
			"consul.hashicorp.com/connect-service-port": "http,metrics",
		}, 2),
	}
	for _, deployment := range objects {
		_, err := deployments.Create(context.Background(), deployment, metav1.CreateOptions{})
		require.NoError(t, err)
		name := deployment.Name
		t.Cleanup(func() {
			_ = deployments.Delete(context.Background(), name, metav1.DeleteOptions{})
		})
	}

	statefulSet := multiportGateTestStatefulSet(prefix+"-sts", map[string]string{
		"consul.hashicorp.com/connect-inject":       "true",
		"consul.hashicorp.com/connect-service-port": "http,metrics",
	}, 2)
	_, err := statefulSets.Create(context.Background(), statefulSet, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = statefulSets.Delete(context.Background(), statefulSet.Name, metav1.DeleteOptions{})
	})

	daemonSet := multiportGateTestDaemonSet(prefix+"-ds", map[string]string{
		"consul.hashicorp.com/connect-inject":               "true",
		"consul.hashicorp.com/connect-service-port":         "http,metrics",
		"consul.hashicorp.com/connect-service-default-port": "metrics",
	}, 2)
	_, err = daemonSets.Create(context.Background(), daemonSet, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = daemonSets.Delete(context.Background(), daemonSet.Name, metav1.DeleteOptions{})
	})

	// The Pod-to-ReplicaSet-to-Deployment hop is only covered once the
	// ReplicaSet has actually produced a Pod. Without this wait the scan can run
	// against an empty Pod list and the test would still pass while proving
	// nothing.
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		pods, err := client.CoreV1().Pods(appNamespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=" + prefix + "-scanned",
		})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items, "expected the scanned Deployment to create a Pod")
		require.NotEmpty(r, pods.Items[0].OwnerReferences, "expected the Pod to be owned by a ReplicaSet")
	})

	portAnnotation := "consul.hashicorp.com/connect-service-port"
	injectAnnotation := "consul.hashicorp.com/connect-inject"
	getAnnotation := func(name, annotation string) string {
		deployment, err := deployments.Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		return deployment.Spec.Template.Annotations[annotation]
	}
	getStatefulSetAnnotation := func(annotation string) string {
		object, err := statefulSets.Get(context.Background(), statefulSet.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return object.Spec.Template.Annotations[annotation]
	}
	getDaemonSetAnnotation := func(annotation string) string {
		object, err := daemonSets.Get(context.Background(), daemonSet.Name, metav1.GetOptions{})
		require.NoError(t, err)
		return object.Spec.Template.Annotations[annotation]
	}

	consulCluster.Upgrade(t, map[string]string{
		"connectInject.multiportServiceRegistration.enabled":            "false",
		"connectInject.multiportServiceRegistration.conversionStrategy": "NONE",
	})
	require.Equal(t, "http,metrics", getAnnotation(prefix+"-translate", portAnnotation))
	require.Equal(t, "http,metrics", getStatefulSetAnnotation(portAnnotation))
	require.Equal(t, "http,metrics", getDaemonSetAnnotation(portAnnotation))

	consulCluster.Upgrade(t, map[string]string{
		"connectInject.multiportServiceRegistration.conversionStrategy": "TRANSLATE",
	})
	require.Equal(t, "metrics", getAnnotation(prefix+"-translate", portAnnotation))
	// Two selected ports are narrowed to one wherever the ports are declared.
	// With no default-port annotation the first token wins.
	require.Equal(t, "http", getAnnotation(prefix+"-later-container", portAnnotation))
	require.Equal(t, "http", getAnnotation(prefix+"-single", portAnnotation))
	require.Equal(t, "http,metrics", getAnnotation(prefix+"-opted-out", portAnnotation))
	require.Equal(t, "http", getAnnotation(prefix+"-scanned", portAnnotation))
	require.Equal(t, "http", getStatefulSetAnnotation(portAnnotation))
	require.Equal(t, "metrics", getDaemonSetAnnotation(portAnnotation))

	consulCluster.Upgrade(t, map[string]string{
		"connectInject.multiportServiceRegistration.conversionStrategy": "DECOMMISSION",
	})
	require.Equal(t, "false", getStatefulSetAnnotation(injectAnnotation))
	require.Equal(t, "false", getDaemonSetAnnotation(injectAnnotation))
	for _, suffix := range []string{"-translate", "-later-container", "-single", "-opted-out", "-scanned"} {
		require.Equal(t, "false", getAnnotation(prefix+suffix, injectAnnotation))
	}
}

// TestConnectInject_MultiportConversionReportsStrandedWorkloads proves the
// failure path through the real post-upgrade hook.
//
// A multi-port workload the Job cannot rewrite — here a bare Pod, standing in
// for an Argo Rollout or an operator-owned CRD — must fail the Helm upgrade with
// an actionable message. Without this the workload is not rejected at upgrade
// time; it keeps running until its Pod is recreated and only then starts failing
// admission, long after the operator has moved on.
func TestConnectInject_MultiportConversionReportsStrandedWorkloads(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, map[string]string{
		"connectInject.enabled":                              "true",
		"connectInject.multiportServiceRegistration.enabled": "true",
	}, ctx, cfg, releaseName)
	consulCluster.Create(t)

	releaseNamespace := ctx.KubectlOptions(t).Namespace
	client := ctx.KubernetesClient(t)
	appNamespace := multiportGateTestAppNamespace(t, cfg, ctx)
	pods := client.CoreV1().Pods(appNamespace)

	// A bare Pod has no controlling owner, so the Job can name it but cannot
	// rewrite it.
	strandedName := helpers.RandomName() + "-stranded"
	stranded := multiportGateTestPod(strandedName, map[string]string{
		"consul.hashicorp.com/connect-inject":       "true",
		"consul.hashicorp.com/connect-service-port": "http,metrics",
	}, 2)
	_, err := pods.Create(context.Background(), stranded, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = pods.Delete(context.Background(), strandedName, metav1.DeleteOptions{})
	})

	jobName := fmt.Sprintf("%s-consul-connect-inject-multiport-conversion", releaseName)
	t.Cleanup(func() {
		propagation := metav1.DeletePropagationBackground
		_ = client.BatchV1().Jobs(releaseNamespace).Delete(context.Background(), jobName, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		})
	})

	err = consulCluster.UpgradeE(t, map[string]string{
		"connectInject.multiportServiceRegistration.enabled":            "false",
		"connectInject.multiportServiceRegistration.conversionStrategy": "TRANSLATE",
	})
	require.Error(t, err, "a workload the conversion cannot rewrite must fail the Helm upgrade")

	// The Job is retained on failure by its hook-delete-policy, so both its
	// status and its logs are still available to the operator.
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		job, err := client.BatchV1().Jobs(releaseNamespace).Get(context.Background(), jobName, metav1.GetOptions{})
		require.NoError(r, err)
		require.NotZero(r, job.Status.Failed, "expected the conversion Job to record a failure")
	})

	releasePods := client.CoreV1().Pods(releaseNamespace)
	jobPods, err := releasePods.List(context.Background(), metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	require.NoError(t, err)
	require.NotEmpty(t, jobPods.Items, "expected the conversion Job to have created a Pod")

	logs, err := releasePods.GetLogs(jobPods.Items[0].Name, &corev1.PodLogOptions{}).DoRaw(context.Background())
	require.NoError(t, err)
	require.Contains(t, string(logs), strandedName,
		"the failure must name the workload so the operator knows what to fix")
	require.Contains(t, string(logs), "consul.hashicorp.com/connect-service-port",
		"the failure must say how to fix it")
}

// multiportGateTestAppNamespace creates the namespace the conversion tests
// deploy their workloads into, following the framework's "<consul-ns>-apps"
// convention.
//
// The workloads must not live in the Consul release namespace. The conversion
// Job is passed -release-namespace and deliberately skips it, so that
// DECOMMISSION cannot roll out the Consul control plane itself. Creating test
// workloads there would mean the Job silently ignores every one of them, and
// the test would assert nothing.
func multiportGateTestAppNamespace(t *testing.T, cfg *config.TestConfig, ctx environment.TestContext) string {
	t.Helper()
	opts := ctx.KubectlOptions(t)
	name := opts.Namespace + "-apps"

	if _, err := k8s.RunKubectlAndGetOutputE(t, opts, "create", "ns", name); err != nil {
		require.Contains(t, err.Error(), "AlreadyExists")
	}
	if cfg.EnableRestrictedPSAEnforcement {
		// The fixtures run as root, which a restricted namespace rejects.
		k8s.RunKubectl(t, opts, "label", "--overwrite", "ns", name,
			"pod-security.kubernetes.io/enforce=privileged",
			"pod-security.kubernetes.io/enforce-version=v1.24",
		)
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, opts, "delete", "ns", name)
	})
	return name
}

func multiportGateTestDeployment(name string, annotations map[string]string, portCounts ...int) *appsv1.Deployment {
	return multiportGateTestDeploymentWithReplicas(name, 0, annotations, portCounts...)
}

func multiportGateTestDeploymentWithReplicas(name string, replicas int32, annotations map[string]string, portCounts ...int) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	pod := multiportGateTestPod(name, annotations, portCounts...)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: pod.Annotations},
				Spec:       pod.Spec,
			},
		},
	}
}

func multiportGateTestStatefulSet(name string, annotations map[string]string, portCounts ...int) *appsv1.StatefulSet {
	replicas := int32(0)
	labels := map[string]string{"app": name}
	pod := multiportGateTestPod(name, annotations, portCounts...)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name,
			Replicas:    &replicas,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: pod.Annotations},
				Spec:       pod.Spec,
			},
		},
	}
}

// multiportGateTestDaemonSet pins the DaemonSet to a node selector that matches
// no node. A DaemonSet has no replica count, and the conversion operates on the
// pod template, so scheduling real Pods would add runtime flakiness without
// testing anything additional.
func multiportGateTestDaemonSet(name string, annotations map[string]string, portCounts ...int) *appsv1.DaemonSet {
	labels := map[string]string{"app": name}
	pod := multiportGateTestPod(name, annotations, portCounts...)
	spec := pod.Spec
	spec.NodeSelector = map[string]string{"consul.hashicorp.com/multiport-gate-test": "unschedulable"}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: pod.Annotations},
				Spec:       spec,
			},
		},
	}
}

// Test that Connect works for an application with multiple ports. The multiport application is a Pod listening on
// two ports. This tests inbound connections to each port of the multiport app, and outbound connections from the
// multiport app to static-server.
func TestConnectInject_MultiportServices(t *testing.T) {
	for _, secure := range []bool{false, true} {
		name := fmt.Sprintf("secure: %t", secure)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			cfg.SkipWhenOpenshiftAndCNI(t)

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"connectInject.enabled": "true",
				// Enable DNS so we can test that DNS redirection _isn't_ set in the pod.
				"dns.enabled": "true",

				"global.tls.enabled":           strconv.FormatBool(secure),
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			consulClient, _ := consulCluster.SetupConsulClient(t, secure)

			// Check that the ACL token is deleted.
			if secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
					retry.RunWith(retrier, t, func(r *retry.R) {
						tokens, _, err := consulClient.ACL().TokenList(nil)
						require.NoError(r, err)
						for _, token := range tokens {
							require.NotContains(r, token.Description, multiport)
							require.NotContains(r, token.Description, multiportAdmin)
							require.NotContains(r, token.Description, connhelper.StaticClientName)
							require.NotContains(r, token.Description, connhelper.StaticServerName)
						}
					})
				})
			}

			logger.Log(t, "creating multiport static-server and static-client deployments")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/multiport-app")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject-multiport")

			// Check that static-client has been injected and now has 2 containers.
			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)

			// Check that multiport has been injected and now has 4 containers.
			podList, err = ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=multiport",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 4)

			if secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:1234")
				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, "http://localhost:2234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", multiport))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: multiport,
					Sources: []*api.SourceIntention{
						{
							Name:   connhelper.StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
				logger.Log(t, fmt.Sprintf("creating intention for %s", multiportAdmin))
				_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: multiportAdmin,
					Sources: []*api.SourceIntention{
						{
							Name:   connhelper.StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {

				// Check connection from static-client to multiport.
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(r), connhelper.StaticClientName, "http://localhost:1234")

				// Check connection from static-client to multiport-admin.
				k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, ctx.KubectlOptions(r), connhelper.StaticClientName, "hello world from 9090 admin", "http://localhost:2234")
			})
			// Now that we've checked inbound connections to a multi port pod, check outbound connection from multi port
			// pod to static-server.

			// Deploy static-server.
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// For outbound connections from the multi port pod, only intentions from the first service in the multiport
			// pod need to be created, since all upstream connections are made through the first service's envoy proxy.
			if secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")

				k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), multiport, "http://localhost:3234")

				logger.Log(t, fmt.Sprintf("creating intention for %s", connhelper.StaticServerName))
				_, _, err := consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: connhelper.StaticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   multiport,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}
			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {
				// Check the connection from the multi port pod to static-server.
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(r), multiport, "http://localhost:3234")
			})

			retry.RunWith(&retry.Counter{Count: 30, Wait: 5 * time.Second}, t, func(r *retry.R) {

				// Test that kubernetes readiness status is synced to Consul. This will make the multi port pods unhealthy
				// and check inbound connections to the multi port pods' services.
				// Create the files so that the readiness probes of the multi port pod fails.
				logger.Log(t, "testing k8s -> consul health checks sync by making the multiport unhealthy")
				k8s.RunKubectl(t, ctx.KubectlOptions(r), "exec", "deploy/"+multiport, "-c", "multiport", "--", "touch", "/tmp/unhealthy-multiport")
				logger.Log(t, "testing k8s -> consul health checks sync by making the multiport-admin unhealthy")
				k8s.RunKubectl(t, ctx.KubectlOptions(r), "exec", "deploy/"+multiport, "-c", "multiport-admin", "--", "touch", "/tmp/unhealthy-multiport-admin")

				// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
				// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
				// We are expecting a "connection reset by peer" error because in a case of health checks,
				// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
				// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(r), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(r), connhelper.StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:2234")
			})
		})
	}
}
