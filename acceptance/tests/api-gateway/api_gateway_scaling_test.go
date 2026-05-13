// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	annotationDefaultReplicas = "consul.hashicorp.com/default-replicas"
	annotationHPAEnabled      = "consul.hashicorp.com/hpa-enabled"
	annotationHPAMinReplicas  = "consul.hashicorp.com/hpa-minimum-replicas"
	annotationHPAMaxReplicas  = "consul.hashicorp.com/hpa-maximum-replicas"
	annotationHPACPUTarget    = "consul.hashicorp.com/hpa-cpu-utilisation-target"

	testReconcileAnnotation = "test.hashicorp.com/reconcile-nonce"
)

func TestAPIGateway_Scaling_EnterpriseGateDisabledIgnoresGatewayAnnotations(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, false)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(3)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(5)),
	})

	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "2",
		annotationHPAMaxReplicas: "10",
		annotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 3)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
}

func TestAPIGateway_Scaling_EnterpriseGateEnabledStaticReplicas(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	// Restart the API Gateway controller to ensure it detects the enterprise license.
	// The controller checks IsEnterpriseDistribution at startup, so we need to restart
	// it after the license is confirmed valid.
	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, map[string]string{
		annotationDefaultReplicas: "4",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 4)
}

func TestAPIGateway_Scaling_EnterpriseGateEnabledControllerManagedHPA(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	// Restart the API Gateway controller to ensure it detects the enterprise license.
	// The controller checks IsEnterpriseDistribution at startup, so we need to restart
	// it after the license is confirmed valid.
	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "3",
		annotationHPAMaxReplicas: "25",
		annotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(3), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(25), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(70), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 3)
}

func TestAPIGateway_Scaling_EnterpriseGateEnabledPreservesManualScale(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	// Restart the API Gateway controller to ensure it detects the enterprise license.
	// The controller checks IsEnterpriseDistribution at startup, so we need to restart
	// it after the license is confirmed valid.
	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(3)),
	})

	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 2)
	scaleGatewayDeployment(t, k8sClient, gateway.Name, gateway.Namespace, 5)
	triggerGatewayReconcile(t, k8sClient, gateway.Name, gateway.Namespace)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 5)
}

func installScalingCluster(t *testing.T, scalingEnabled bool) *consul.HelmCluster {
	t.Helper()

	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"connectInject.enabled":                                        "true",
		"connectInject.apiGateway.enabled":                             "true",
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": fmt.Sprintf("%t", scalingEnabled),
		"global.logLevel":                                              "debug",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)
	return consulCluster
}

func skipUnlessEnterpriseLicenseConfigured(t *testing.T) {
	t.Helper()

	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}
	if cfg.EnterpriseLicense == "" {
		t.Skipf("skipping this test because no enterprise license is configured")
	}
}

func requireEnterpriseLicenseValid(t *testing.T, consulClient *api.Client) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		license, err := consulClient.Operator().LicenseGet(nil)
		require.NoError(r, err)
		require.NotNil(r, license)
		require.True(r, license.Valid)
	})
}

func createScalingGatewayClassResources(
	t *testing.T,
	k8sClient client.Client,
	noCleanupOnFailure bool,
	noCleanup bool,
	deploymentSpec v1alpha1.DeploymentSpec,
) string {
	t.Helper()

	gatewayClassConfigName := helpers.RandomName()
	gatewayClassName := helpers.RandomName()

	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassConfigName,
		},
		Spec: v1alpha1.GatewayClassConfigSpec{
			DeploymentSpec: deploymentSpec,
		},
	}

	err := k8sClient.Create(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), gatewayClassConfig)))
	})

	createGatewayClass(t, k8sClient, gatewayClassName, gatewayClassControllerName, &gwv1.ParametersReference{
		Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	})
	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), &gwv1.GatewayClass{
			ObjectMeta: metav1.ObjectMeta{Name: gatewayClassName},
		})))
	})

	return gatewayClassName
}

func createScalingGateway(
	t *testing.T,
	k8sClient client.Client,
	namespace string,
	gatewayClassName string,
	annotations map[string]string,
	noCleanupOnFailure bool,
	noCleanup bool,
) *gwv1.Gateway {
	t.Helper()

	gateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        helpers.RandomName(),
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwv1.ObjectName(gatewayClassName),
			Listeners: []gwv1.Listener{
				{
					Name:     gwv1.SectionName("http"),
					Port:     8080,
					Protocol: gwv1.HTTPProtocolType,
				},
			},
		},
	}

	err := k8sClient.Create(context.Background(), gateway)
	require.NoError(t, err)
	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), gateway)))
	})

	logger.Logf(t, "created gateway %s/%s", gateway.Namespace, gateway.Name)
	return gateway
}

func waitForGatewayDeploymentReplicas(t *testing.T, k8sClient client.Client, gatewayName, namespace string, want int32) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &deployment)
		require.NoError(r, err)
		require.NotNil(r, deployment.Spec.Replicas)
		require.Equal(r, want, *deployment.Spec.Replicas)
	})
}

func waitForGatewayHPA(t *testing.T, k8sClient client.Client, gatewayName, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
	t.Helper()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      fmt.Sprintf("%s-hpa", gatewayName),
			Namespace: namespace,
		}, hpa)
		require.NoError(r, err)
	})

	return hpa
}

func waitForGatewayHPAAbsent(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      fmt.Sprintf("%s-hpa", gatewayName),
			Namespace: namespace,
		}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.Error(r, err)
		require.True(r, apierrors.IsNotFound(err), "expected HPA to be absent, got %v", err)
	})
}

func scaleGatewayDeployment(t *testing.T, k8sClient client.Client, gatewayName, namespace string, replicas int32) {
	t.Helper()

	var deployment appsv1.Deployment
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &deployment)
	require.NoError(t, err)

	deployment.Spec.Replicas = ptr.To(replicas)
	err = k8sClient.Update(context.Background(), &deployment)
	require.NoError(t, err)

	logger.Logf(t, "manually scaled deployment %s/%s to %d replicas", namespace, gatewayName, replicas)
}

func triggerGatewayReconcile(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
	t.Helper()

	var gateway gwv1.Gateway
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &gateway)
	require.NoError(t, err)

	if gateway.Annotations == nil {
		gateway.Annotations = map[string]string{}
	}
	gateway.Annotations[testReconcileAnnotation] = fmt.Sprintf("%d", time.Now().UnixNano())

	err = k8sClient.Update(context.Background(), &gateway)
	require.NoError(t, err)

	logger.Logf(t, "triggered reconcile for gateway %s/%s", namespace, gatewayName)
}

func restartAPIGatewayController(t *testing.T, ctx environment.TestContext) {
	t.Helper()

	k8sClient := ctx.KubernetesClient(t)
	namespace := ctx.KubectlOptions(t).Namespace

	// Find and delete the consul-connect-injector pod (which contains the API Gateway controller)
	pods, err := k8sClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=consul,component=connect-injector",
	})
	require.NoError(t, err)
	require.NotEmpty(t, pods.Items, "no connect-injector pods found")

	for _, pod := range pods.Items {
		err = k8sClient.CoreV1().Pods(namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		require.NoError(t, err)
		logger.Logf(t, "deleted pod %s/%s to restart API Gateway controller", namespace, pod.Name)
	}

	// Wait for the new pod to be ready
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		pods, err := k8sClient.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=consul,component=connect-injector",
		})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items, "no connect-injector pods found after restart")

		for _, pod := range pods.Items {
			require.Equal(r, corev1.PodRunning, pod.Status.Phase, "pod %s not running", pod.Name)
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady {
					require.Equal(r, corev1.ConditionTrue, condition.Status, "pod %s not ready", pod.Name)
				}
			}
		}
		logger.Logf(t, "API Gateway controller restarted and ready")
	})
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledStaticReplicasBeyond8 verifies that
// a gateway can be scaled beyond the previous hard limit of 8 replicas using
// the consul.hashicorp.com/default-replicas annotation.
// This is the primary regression test for CSL-12699.
func TestAPIGateway_Scaling_EnterpriseGateEnabledStaticReplicasBeyond8(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	// Set default-replicas to 10 — beyond the old hard limit of 8.
	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, map[string]string{
		annotationDefaultReplicas: "10",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 10)

	// No HPA should be created for a static-replica configuration.
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	logger.Logf(t, "confirmed gateway %s/%s scaled to 10 replicas (beyond previous hard limit of 8)", gateway.Namespace, gateway.Name)
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledUserManagedHPA verifies that when a
// user creates their own HPA targeting the gateway deployment, the controller
// detects it, enters hpa-user mode, and does NOT create or overwrite the user HPA.
func TestAPIGateway_Scaling_EnterpriseGateEnabledUserManagedHPA(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(4)),
	})

	// Create a gateway with no scaling annotations so that it starts up cleanly
	// using the deprecated GCC deployment spec.
	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 2)

	// Now simulate the user creating their own HPA for the gateway deployment.
	userHPAName := fmt.Sprintf("%s-user-hpa", gateway.Name)
	userMinReplicas := int32(3)
	userMaxReplicas := int32(15)
	userHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userHPAName,
			Namespace: namespace,
			// No OwnerReferences — this is intentionally NOT controller-managed.
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       gateway.Name,
			},
			MinReplicas: &userMinReplicas,
			MaxReplicas: userMaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptr.To(int32(60)),
						},
					},
				},
			},
		},
	}
	err := k8sClient.Create(context.Background(), userHPA)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), userHPA)))
	})

	// Trigger a reconcile to make the controller detect the user HPA.
	triggerGatewayReconcile(t, k8sClient, gateway.Name, gateway.Namespace)

	// Verify the controller does NOT create its own HPA (name = <gateway>-hpa).
	controllerHPAName := fmt.Sprintf("%s-hpa", gateway.Name)
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      controllerHPAName,
			Namespace: namespace,
		}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.True(r, apierrors.IsNotFound(err),
			"controller must not create its own HPA when a user-managed HPA is detected; got: %v", err)
	})

	// Verify the user HPA was NOT deleted.
	var fetchedUserHPA autoscalingv2.HorizontalPodAutoscaler
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: userHPAName, Namespace: namespace}, &fetchedUserHPA)
	require.NoError(t, err, "user-managed HPA must not be deleted by the controller")
	require.Equal(t, userMaxReplicas, fetchedUserHPA.Spec.MaxReplicas)

	logger.Logf(t, "confirmed controller correctly deferred to user-managed HPA for gateway %s/%s", namespace, gateway.Name)
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledAnnotationTransitionStaticToHPA verifies
// that updating a gateway's annotation from static replicas to HPA mode:
//  1. Creates a controller-managed HPA
//  2. The deployment replica count is governed by the HPA
func TestAPIGateway_Scaling_EnterpriseGateEnabledAnnotationTransitionStaticToHPA(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	// Phase 1: start with static replicas.
	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, map[string]string{
		annotationDefaultReplicas: "5",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 5)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	// Phase 2: switch to HPA mode by updating the annotations.
	var liveGateway gwv1.Gateway
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: namespace}, &liveGateway)
	require.NoError(t, err)

	liveGateway.Annotations = map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "2",
		annotationHPAMaxReplicas: "20",
		annotationHPACPUTarget:   "65",
	}
	err = k8sClient.Update(context.Background(), &liveGateway)
	require.NoError(t, err)
	logger.Logf(t, "updated gateway %s/%s annotations to enable HPA mode", namespace, gateway.Name)

	// Verify controller-managed HPA is created with the specified configuration.
	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(20), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(65), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)

	logger.Logf(t, "confirmed transition from static to HPA mode for gateway %s/%s", namespace, gateway.Name)
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledAnnotationTransitionHPAToStatic verifies
// that removing the HPA annotations (switching back to static) from a gateway:
//  1. Deletes the controller-managed HPA
//  2. Sets the deployment to the specified static replica count
func TestAPIGateway_Scaling_EnterpriseGateEnabledAnnotationTransitionHPAToStatic(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	// Phase 1: start in HPA mode.
	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "3",
		annotationHPAMaxReplicas: "15",
		annotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.Equal(t, int32(15), hpa.Spec.MaxReplicas)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 3)

	// Phase 2: switch to static mode by replacing all annotations with a static replica count.
	var liveGateway gwv1.Gateway
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: namespace}, &liveGateway)
	require.NoError(t, err)

	liveGateway.Annotations = map[string]string{
		annotationDefaultReplicas: "7",
	}
	err = k8sClient.Update(context.Background(), &liveGateway)
	require.NoError(t, err)
	logger.Logf(t, "updated gateway %s/%s annotations to switch to static mode with 7 replicas", namespace, gateway.Name)

	// Verify the controller-managed HPA is removed.
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	// Verify the deployment is set to the new static replica count.
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 7)

	logger.Logf(t, "confirmed transition from HPA to static mode for gateway %s/%s", namespace, gateway.Name)
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledDanglingUserHPA covers the "dangling HPA"
// scenario from the PR: a user-managed HPA already exists targeting the gateway deployment
// at the time the controller first reconciles the gateway. The controller must detect it
// immediately on the first reconcile, enter hpa-user mode, and never overwrite or delete
// the pre-existing HPA.
func TestAPIGateway_Scaling_EnterpriseGateEnabledDanglingUserHPA(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(4)),
	})

	// Determine the gateway name ahead of time so the HPA can reference it
	// before the gateway is created (simulating a "dangling" pre-existing HPA).
	danglingGatewayName := helpers.RandomName()
	danglingHPAName := fmt.Sprintf("%s-dangling-hpa", danglingGatewayName)
	danglingMinReplicas := int32(5)
	danglingMaxReplicas := int32(20)

	// Step 1: create the user HPA BEFORE the gateway exists.
	danglingHPA := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      danglingHPAName,
			Namespace: namespace,
			// No OwnerReferences — pure user-managed resource.
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       danglingGatewayName,
			},
			MinReplicas: &danglingMinReplicas,
			MaxReplicas: danglingMaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptr.To(int32(55)),
						},
					},
				},
			},
		},
	}
	err := k8sClient.Create(context.Background(), danglingHPA)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), danglingHPA)))
	})
	logger.Logf(t, "created dangling HPA %s/%s before gateway exists", namespace, danglingHPAName)

	// Step 2: now create the gateway with the pre-determined name.
	gateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      danglingGatewayName,
			Namespace: namespace,
		},
		Spec: gwv1.GatewaySpec{
			GatewayClassName: gwv1.ObjectName(gatewayClassName),
			Listeners: []gwv1.Listener{
				{
					Name:     gwv1.SectionName("http"),
					Port:     8080,
					Protocol: gwv1.HTTPProtocolType,
				},
			},
		},
	}
	err = k8sClient.Create(context.Background(), gateway)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), gateway)))
	})
	logger.Logf(t, "created gateway %s/%s after dangling HPA already existed", namespace, danglingGatewayName)

	// Step 3: the controller must detect the dangling HPA on first reconcile and NOT
	// create its own controller-managed HPA (<gateway>-hpa).
	controllerHPAName := fmt.Sprintf("%s-hpa", danglingGatewayName)
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      controllerHPAName,
			Namespace: namespace,
		}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.True(r, apierrors.IsNotFound(err),
			"controller must not create its own HPA when a dangling user HPA already targets the deployment; got: %v", err)
	})

	// Step 4: verify the dangling HPA was NOT deleted or modified by the controller.
	var fetchedHPA autoscalingv2.HorizontalPodAutoscaler
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: danglingHPAName, Namespace: namespace}, &fetchedHPA)
	require.NoError(t, err, "dangling user HPA must not be deleted by the controller")
	require.Equal(t, danglingMaxReplicas, fetchedHPA.Spec.MaxReplicas,
		"dangling user HPA spec must not be modified by the controller")

	logger.Logf(t, "confirmed controller correctly deferred to dangling user HPA for gateway %s/%s", namespace, danglingGatewayName)
}

// TestAPIGateway_Scaling_EnterpriseGateEnabledFutureGatewayUpgrade covers the
// "upgrade pre-existing gateway" scenario from the PR: a gateway already exists and
// is running with the deprecated GatewayClassConfig-based replica bounds. After the
// operator upgrades the Helm chart with scaling.enabled=true and adds HPA annotations
// to the gateway, the controller must create the controller-managed HPA and respect
// the new annotation-driven configuration going forward.
func TestAPIGateway_Scaling_EnterpriseGateEnabledFutureGatewayUpgrade(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace

	// Simulate a pre-existing gateway that was created with the deprecated GCC deployment
	// spec (old behaviour before the scaling feature was available).
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(3)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(8)), // old hard limit still set in GCC
	})

	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	// Confirm it starts at 3 replicas as per GCC default.
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 3)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	// Simulate the operator "upgrading" the gateway by adding HPA annotations
	// (as they would after enabling the scaling feature and wanting auto-scaling).
	var liveGateway gwv1.Gateway
	err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: namespace}, &liveGateway)
	require.NoError(t, err)

	liveGateway.Annotations = map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "4",
		annotationHPAMaxReplicas: "16", // beyond the old GCC hard limit of 8
		annotationHPACPUTarget:   "70",
	}
	err = k8sClient.Update(context.Background(), &liveGateway)
	require.NoError(t, err)
	logger.Logf(t, "upgraded gateway %s/%s with HPA annotations (maxReplicas=16, beyond old GCC cap of 8)", namespace, gateway.Name)

	// Verify the controller-managed HPA is created with the new values —
	// specifically that maxReplicas=16 is honoured, not capped at the old GCC max of 8.
	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(4), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(16), hpa.Spec.MaxReplicas,
		"HPA maxReplicas must respect the annotation value, not the old GCC cap of 8")

	logger.Logf(t, "confirmed post-upgrade HPA for gateway %s/%s has maxReplicas=16 (old cap was 8)", namespace, gateway.Name)
}

