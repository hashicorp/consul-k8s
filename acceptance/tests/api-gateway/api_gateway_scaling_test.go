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

	// Wait for the controller to accept the GatewayClass before returning.
	// Creating a Gateway before the GatewayClass is accepted causes the controller
	// to skip reconciliation, so the Deployment is never created.
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		var gc gwv1.GatewayClass
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayClassName}, &gc)
		require.NoError(r, err)
		for _, cond := range gc.Status.Conditions {
			if cond.Type == string(gwv1.GatewayClassConditionStatusAccepted) {
				require.Equal(r, metav1.ConditionTrue, cond.Status, "GatewayClass %q not yet accepted: %s", gatewayClassName, cond.Message)
				return
			}
		}
		r.Fatalf("GatewayClass %q: accepted condition not yet set", gatewayClassName)
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
