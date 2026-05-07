// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

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
	scalingAnnotationDefaultReplicas = "consul.hashicorp.com/default-replicas"
	scalingAnnotationHPAEnabled     = "consul.hashicorp.com/hpa-enabled"
	scalingAnnotationHPAMinReplicas = "consul.hashicorp.com/hpa-minimum-replicas"
	scalingAnnotationHPAMaxReplicas = "consul.hashicorp.com/hpa-maximum-replicas"
	scalingAnnotationHPACPUTarget   = "consul.hashicorp.com/hpa-cpu-utilisation-target"

	scalingTestReconcileAnnotation = "test.hashicorp.com/reconcile-nonce"
)

func TestOpenshift_APIGateway_Scaling_EnterpriseGateEnabledControllerManagedHPA(t *testing.T) {
	skipUnlessEnterpriseLicenseConfiguredOCP(t)

	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	newOpenshiftClusterWithHelmValues(t, cfg, false, false, map[string]string{
		"connectInject.apiGateway.enabled":                             "true",
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": "true",
		"global.logLevel": "debug",
	})

	consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
	consulCluster.SetNamespace("consul")

	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValidOCP(t, consulClient)
	restartAPIGatewayControllerOCP(t, ctx)

	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayNamespace, _ := createNamespace(t, ctx, cfg)
	gatewayClassName := createScalingGatewayClassResourcesOCP(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, map[string]string{
		scalingAnnotationHPAEnabled:     "true",
		scalingAnnotationHPAMinReplicas: "3",
		scalingAnnotationHPAMaxReplicas: "25",
		scalingAnnotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	hpa := waitForGatewayHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(3), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(25), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(70), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)

	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 3)
}

func TestOpenshift_APIGateway_Scaling_EnterpriseGateDisabledIgnoresGatewayAnnotations(t *testing.T) {
	skipUnlessEnterpriseLicenseConfiguredOCP(t)

	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	newOpenshiftClusterWithHelmValues(t, cfg, false, false, map[string]string{
		"connectInject.apiGateway.enabled":                             "true",
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": "false",
		"global.logLevel":                                              "debug",
	})

	consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
	consulCluster.SetNamespace("consul")

	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValidOCP(t, consulClient)

	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayNamespace, _ := createNamespace(t, ctx, cfg)
	gatewayClassName := createScalingGatewayClassResourcesOCP(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(3)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(5)),
	})

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, map[string]string{
		scalingAnnotationHPAEnabled:     "true",
		scalingAnnotationHPAMinReplicas: "2",
		scalingAnnotationHPAMaxReplicas: "10",
		scalingAnnotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 3)
	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
}

func TestOpenshift_APIGateway_Scaling_EnterpriseGateEnabledStaticReplicas(t *testing.T) {
	skipUnlessEnterpriseLicenseConfiguredOCP(t)

	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	newOpenshiftClusterWithHelmValues(t, cfg, false, false, map[string]string{
		"connectInject.apiGateway.enabled":                             "true",
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": "true",
		"global.logLevel":                                              "debug",
	})

	consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
	consulCluster.SetNamespace("consul")

	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValidOCP(t, consulClient)
	restartAPIGatewayControllerOCP(t, ctx)

	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayNamespace, _ := createNamespace(t, ctx, cfg)
	gatewayClassName := createScalingGatewayClassResourcesOCP(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, map[string]string{
		scalingAnnotationDefaultReplicas: "4",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 4)
}

func TestOpenshift_APIGateway_Scaling_EnterpriseGateEnabledPreservesManualScale(t *testing.T) {
	skipUnlessEnterpriseLicenseConfiguredOCP(t)

	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	newOpenshiftClusterWithHelmValues(t, cfg, false, false, map[string]string{
		"connectInject.apiGateway.enabled":                             "true",
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": "true",
		"global.logLevel":                                              "debug",
	})

	consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
	consulCluster.SetNamespace("consul")

	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValidOCP(t, consulClient)
	restartAPIGatewayControllerOCP(t, ctx)

	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayNamespace, _ := createNamespace(t, ctx, cfg)
	gatewayClassName := createScalingGatewayClassResourcesOCP(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(3)),
	})

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 2)
	scaleGatewayDeploymentOCP(t, k8sClient, gateway.Name, gateway.Namespace, 5)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 5)
	triggerGatewayReconcileOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 5)
}

func skipUnlessEnterpriseLicenseConfiguredOCP(t *testing.T) {
	t.Helper()

	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}
	if cfg.EnterpriseLicense == "" {
		t.Skipf("skipping this test because no enterprise license is configured")
	}
}

func requireEnterpriseLicenseValidOCP(t *testing.T, consulClient *api.Client) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		license, err := consulClient.Operator().LicenseGet(nil)
		require.NoError(r, err)
		require.NotNil(r, license)
		require.True(r, license.Valid)
	})
}

func createScalingGatewayClassResourcesOCP(
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

	gatewayClass := &gwv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassName,
		},
		Spec: gwv1.GatewayClassSpec{
			ControllerName: gwv1.GatewayController(gatewayClassControllerName),
			ParametersRef: &gwv1.ParametersReference{
				Group: gwv1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  gwv1.Kind(v1alpha1.GatewayClassConfigKind),
				Name:  gatewayClassConfigName,
			},
		},
	}

	err = k8sClient.Create(context.Background(), gatewayClass)
	require.NoError(t, err)
	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), gatewayClass)))
	})

	return gatewayClassName
}

func createScalingGatewayOCP(
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
			Labels: map[string]string{
				"component": "api-gateway",
			},
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

func waitForGatewayDeploymentReplicasOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string, want int32) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &deployment)
		require.NoError(r, err)
		require.NotNil(r, deployment.Spec.Replicas)
		require.Equal(r, want, *deployment.Spec.Replicas)
	})
}

func waitForGatewayHPAOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
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

func waitForGatewayHPAAbsentOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
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

func scaleGatewayDeploymentOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string, replicas int32) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &deployment)
		require.NoError(r, err)

		deployment.Spec.Replicas = ptr.To(replicas)
		err = k8sClient.Update(context.Background(), &deployment)
		require.NoError(r, err)
	})

	logger.Logf(t, "manually scaled deployment %s/%s to %d replicas", namespace, gatewayName, replicas)
}

func triggerGatewayReconcileOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		var gateway gwv1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &gateway)
		require.NoError(r, err)

		if gateway.Annotations == nil {
			gateway.Annotations = map[string]string{}
		}
		gateway.Annotations[scalingTestReconcileAnnotation] = fmt.Sprintf("%d", time.Now().UnixNano())

		err = k8sClient.Update(context.Background(), &gateway)
		require.NoError(r, err)
	})

	logger.Logf(t, "triggered reconcile for gateway %s/%s", namespace, gatewayName)
}

func restartAPIGatewayControllerOCP(t *testing.T, ctx environment.TestContext) {
	t.Helper()

	k8sClient := ctx.KubernetesClient(t)

	pods, err := k8sClient.CoreV1().Pods("consul").List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=consul,component=connect-injector",
	})
	require.NoError(t, err)
	require.NotEmpty(t, pods.Items, "no connect-injector pods found")

	for _, pod := range pods.Items {
		err = k8sClient.CoreV1().Pods("consul").Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
		require.NoError(t, err)
		logger.Logf(t, "deleted pod consul/%s to restart API Gateway controller", pod.Name)
	}

	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		pods, err := k8sClient.CoreV1().Pods("consul").List(context.Background(), metav1.ListOptions{
			LabelSelector: "app=consul,component=connect-injector",
		})
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items, "no connect-injector pods found after restart")

		for _, pod := range pods.Items {
			require.Equal(r, corev1.PodRunning, pod.Status.Phase, "pod %s not running", pod.Name)
			ready := false
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady {
					require.Equal(r, corev1.ConditionTrue, condition.Status, "pod %s not ready", pod.Name)
					ready = true
				}
			}
			require.True(r, ready, "pod %s missing ready condition", pod.Name)
		}
	})

	logger.Logf(t, "API Gateway controller restarted and ready")
}
