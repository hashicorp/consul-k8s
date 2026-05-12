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

func TestAPIGateway_Scaling_AnnotatedGatewayBeforeUpgradeReconcilesAfterUpgrade(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, false)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{})

	gateway := createScalingGateway(t, k8sClient, ctx.KubectlOptions(t).Namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "2",
		annotationHPAMaxReplicas: "8",
		annotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 1)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	logger.Logf(t, "TEST ACTION: upgrading control plane with pre-existing annotated Gateway %s/%s", gateway.Namespace, gateway.Name)
	consulCluster.Upgrade(t, map[string]string{
		"connectInject.apiGateway.managedGatewayClass.scaling.enabled": "true",
	})

	// Restart the API Gateway controller to ensure it detects the enterprise license
	// after the upgrade enables scaling.
	restartAPIGatewayController(t, ctx)

	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(8), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(70), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
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
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 5)
}

func TestAPIGateway_Scaling_UserManagedHPATakesPrecedenceOverAnnotations(t *testing.T) {
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

	// Start with controller-managed HPA via annotations.
	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "2",
		annotationHPAMaxReplicas: "8",
		annotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)

	// User now creates their own HPA targeting the gateway deployment.
	createUserManagedHPA(t, k8sClient, gateway.Name, gateway.Namespace, 4, 12, 60, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	// Controller-managed HPA must be removed; user HPA must remain untouched.
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAUnchanged(t, k8sClient, gateway.Name, gateway.Namespace, 4, 12, 60)
}

func TestAPIGateway_Scaling_UserManagedHPAPreservesManualScale(t *testing.T) {
	skipUnlessEnterpriseLicenseConfigured(t)

	ctx := suite.Environment().DefaultContext(t)
	consulCluster := installScalingCluster(t, true)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	requireEnterpriseLicenseValid(t, consulClient)

	restartAPIGatewayController(t, ctx)

	cfg := suite.Config()
	k8sClient := ctx.ControllerRuntimeClient(t)
	namespace := ctx.KubectlOptions(t).Namespace

	// GatewayClassConfig still sets deprecated default replicas to confirm the
	// user HPA wins over the deprecated source as well.
	gatewayClassName := createScalingGatewayClassResources(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(5)),
	})

	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 2)

	// Add a user-managed HPA, then manually scale the deployment.
	createUserManagedHPA(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	scaleGatewayDeployment(t, k8sClient, gateway.Name, gateway.Namespace, 6)

	// Controller must not overwrite replicas while a user-managed HPA exists.
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 6)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAUnchanged(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80)
}

func TestAPIGateway_Scaling_UserManagedHPARemovedRestoresControllerHPA(t *testing.T) {
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

	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, map[string]string{
		annotationHPAEnabled:     "true",
		annotationHPAMinReplicas: "2",
		annotationHPAMaxReplicas: "6",
		annotationHPACPUTarget:   "75",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	// Controller-managed HPA exists initially.
	waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)

	// User adds their own HPA; controller HPA must disappear.
	userHPAName := createUserManagedHPA(t, k8sClient, gateway.Name, gateway.Namespace, 3, 9, 65, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)

	// User deletes their HPA; controller-managed HPA must come back from annotations.
	deleteUserManagedHPA(t, k8sClient, userHPAName, gateway.Namespace)

	hpa := waitForGatewayHPA(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(6), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(75), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
}

func TestAPIGateway_Scaling_UserManagedHPAUpdatesDriveReplicaChanges(t *testing.T) {
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

	gateway := createScalingGateway(t, k8sClient, namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	// Create the user-managed HPA with a low minReplicas so the deployment
	// initially settles at that floor.
	hpaName := createUserManagedHPA(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	// Controller must yield the deployment to the user HPA.
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 1)

	// User raises minReplicas. The Kubernetes HPA controller must scale the
	// deployment up to the new floor, and the consul controller must not
	// interfere.
	updateUserManagedHPAReplicaBounds(t, k8sClient, hpaName, gateway.Namespace, 4, 10)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 4)

	// User raises minReplicas further; deployment must follow.
	updateUserManagedHPAReplicaBounds(t, k8sClient, hpaName, gateway.Namespace, 6, 10)
	waitForGatewayDeploymentReplicas(t, k8sClient, gateway.Name, gateway.Namespace, 6)

	// Controller-managed HPA must remain absent throughout.
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
}

func TestAPIGateway_Scaling_DanglingUserHPABeforeGatewayTakesPrecedence(t *testing.T) {
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
	gatewayName := helpers.RandomName()

	hpaName := createUserManagedHPAForTarget(t, k8sClient, "dangling-user-hpa", namespace, gatewayName, 3, 9, 65, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	gateway := createScalingGatewayWithName(t, k8sClient, gatewayName, namespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayPodsReady(t, k8sClient, gateway.Name, gateway.Namespace, 1)
	waitForGatewayHPAAbsent(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAForTargetUnchanged(t, k8sClient, hpaName, gateway.Namespace, gateway.Name, 3, 9, 65)
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

	return createScalingGatewayWithName(t, k8sClient, helpers.RandomName(), namespace, gatewayClassName, annotations, noCleanupOnFailure, noCleanup)
}

func createScalingGatewayWithName(
	t *testing.T,
	k8sClient client.Client,
	gatewayName string,
	namespace string,
	gatewayClassName string,
	annotations map[string]string,
	noCleanupOnFailure bool,
	noCleanup bool,
) *gwv1.Gateway {
	t.Helper()

	gateway := &gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        gatewayName,
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
	logger.Logf(t, "TEST INPUT: Gateway %s/%s spec gatewayClass=%s listeners=%s annotations=%s", gateway.Namespace, gateway.Name, gateway.Spec.GatewayClassName, gatewayListeners(gateway.Spec.Listeners), gatewayAnnotations(gateway.Annotations))
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

func waitForGatewayPodsReady(t *testing.T, k8sClient client.Client, gatewayName, namespace string, minReady int32) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: namespace}, &deployment)
		require.NoError(r, err)
		require.NotNil(r, deployment.Spec.Selector)
		require.NotEmpty(r, deployment.Spec.Selector.MatchLabels)

		var pods corev1.PodList
		err = k8sClient.List(context.Background(), &pods, client.InNamespace(namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels))
		require.NoError(r, err)
		require.NotEmpty(r, pods.Items)

		var readyPods int32
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					readyPods++
					break
				}
			}
		}

		require.GreaterOrEqual(r, readyPods, minReady)
	})

	logger.Logf(t, "BEHAVIOR RESULT: gateway deployment %s/%s has at least %d ready pod(s)", namespace, gatewayName, minReady)
}

func waitForGatewayHPA(t *testing.T, k8sClient client.Client, gatewayName, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
	t.Helper()

	controllerHPAName := fmt.Sprintf("%s-hpa", gatewayName)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      controllerHPAName,
			Namespace: namespace,
		}, hpa)
		require.NoError(r, err)
	})

	logger.Logf(t, "BEHAVIOR RESULT: controller-managed HPA %s/%s exists spec=%s owners=%s", namespace, controllerHPAName, hpaSpec(hpa), hpaOwnerReferences(hpa.OwnerReferences))
	return hpa
}

func waitForGatewayHPAAbsent(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
	t.Helper()

	controllerHPAName := fmt.Sprintf("%s-hpa", gatewayName)

	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      controllerHPAName,
			Namespace: namespace,
		}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.Error(r, err)
		require.True(r, apierrors.IsNotFound(err), "expected HPA to be absent, got %v", err)
	})

	logger.Logf(t, "BEHAVIOR RESULT: controller-managed HPA %s/%s is absent", namespace, controllerHPAName)
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

// createUserManagedHPA creates an HPA owned by the user (no Gateway owner reference)
// targeting the gateway's Deployment. Returns the HPA name.
func createUserManagedHPA(
	t *testing.T,
	k8sClient client.Client,
	gatewayName, namespace string,
	minReplicas, maxReplicas, cpuTarget int32,
	noCleanupOnFailure bool,
	noCleanup bool,
) string {
	t.Helper()

	// Use a name distinct from the controller-managed "<gateway>-hpa" so the
	// two can coexist if the controller has not yet reconciled.
	hpaName := fmt.Sprintf("%s-user-hpa", gatewayName)
	return createUserManagedHPAForTarget(t, k8sClient, hpaName, namespace, gatewayName, minReplicas, maxReplicas, cpuTarget, noCleanupOnFailure, noCleanup)
}

func createUserManagedHPAForTarget(
	t *testing.T,
	k8sClient client.Client,
	hpaName, namespace, targetName string,
	minReplicas, maxReplicas, cpuTarget int32,
	noCleanupOnFailure bool,
	noCleanup bool,
) string {
	t.Helper()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: namespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetName,
			},
			MinReplicas: ptr.To(minReplicas),
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: ptr.To(cpuTarget),
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(context.Background(), hpa)
	require.NoError(t, err)
	helpers.Cleanup(t, noCleanupOnFailure, noCleanup, func() {
		logger.Logf(t, "TEST CLEANUP: deleting user-managed HPA %s/%s", namespace, hpaName)
		require.NoError(t, client.IgnoreNotFound(k8sClient.Delete(context.Background(), hpa)))
	})

	logger.Logf(t, "TEST INPUT: user-managed HPA %s/%s spec=%s owners=%s", namespace, hpaName, hpaSpec(hpa), hpaOwnerReferences(hpa.OwnerReferences))
	return hpaName
}

func retargetUserManagedHPA(t *testing.T, k8sClient client.Client, hpaName, namespace, targetName string) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *retry.R) {
		var hpa autoscalingv2.HorizontalPodAutoscaler
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: namespace}, &hpa)
		require.NoError(r, err)

		hpa.Spec.ScaleTargetRef = autoscalingv2.CrossVersionObjectReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       targetName,
		}

		err = k8sClient.Update(context.Background(), &hpa)
		require.NoError(r, err)
	})

	logger.Logf(t, "TEST ACTION: retargeted existing user-managed HPA %s/%s to Deployment/%s", namespace, hpaName, targetName)
}

// updateUserManagedHPAReplicaBounds updates the min/max replicas on an existing
// user-managed HPA. Retries on conflict to tolerate concurrent updates by the
// kubernetes HPA controller writing status.
func updateUserManagedHPAReplicaBounds(t *testing.T, k8sClient client.Client, hpaName, namespace string, minReplicas, maxReplicas int32) {
	t.Helper()

	retry.RunWith(&retry.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *retry.R) {
		var hpa autoscalingv2.HorizontalPodAutoscaler
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: namespace}, &hpa)
		require.NoError(r, err)

		hpa.Spec.MinReplicas = ptr.To(minReplicas)
		hpa.Spec.MaxReplicas = maxReplicas

		err = k8sClient.Update(context.Background(), &hpa)
		require.NoError(r, err)
	})

	logger.Logf(t, "TEST ACTION: updated user-managed HPA %s/%s to min=%d max=%d", namespace, hpaName, minReplicas, maxReplicas)
}

// deleteUserManagedHPA deletes a user-managed HPA by name.
func deleteUserManagedHPA(t *testing.T, k8sClient client.Client, hpaName, namespace string) {
	t.Helper()

	logger.Logf(t, "TEST ACTION: deleting user-managed HPA %s/%s so controller-managed HPA can be restored", namespace, hpaName)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: namespace},
	}
	err := k8sClient.Delete(context.Background(), hpa)
	require.NoError(t, client.IgnoreNotFound(err))

	// Confirm it's actually gone before returning.
	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: namespace}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.True(r, apierrors.IsNotFound(err), "expected user HPA to be deleted, got %v", err)
	})

	logger.Logf(t, "BEHAVIOR RESULT: user-managed HPA %s/%s was deleted by the test action", namespace, hpaName)
}

// requireUserHPAUnchanged asserts the user-managed HPA still exists with its
// original spec and has not acquired a Gateway owner reference.
func requireUserHPAUnchanged(t *testing.T, k8sClient client.Client, gatewayName, namespace string, minReplicas, maxReplicas, cpuTarget int32) {
	t.Helper()

	hpaName := fmt.Sprintf("%s-user-hpa", gatewayName)
	requireUserHPAForTargetUnchanged(t, k8sClient, hpaName, namespace, gatewayName, minReplicas, maxReplicas, cpuTarget)
}

func requireUserHPAForTargetUnchanged(t *testing.T, k8sClient client.Client, hpaName, namespace, targetName string, minReplicas, maxReplicas, cpuTarget int32) {
	t.Helper()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{}

	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: namespace}, hpa)
		require.NoError(r, err)
		require.Equal(r, "Deployment", hpa.Spec.ScaleTargetRef.Kind)
		require.Equal(r, targetName, hpa.Spec.ScaleTargetRef.Name)
		require.NotNil(r, hpa.Spec.MinReplicas)
		require.Equal(r, minReplicas, *hpa.Spec.MinReplicas)
		require.Equal(r, maxReplicas, hpa.Spec.MaxReplicas)
		require.Len(r, hpa.Spec.Metrics, 1)
		require.NotNil(r, hpa.Spec.Metrics[0].Resource)
		require.NotNil(r, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
		require.Equal(r, cpuTarget, *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)

		for _, owner := range hpa.OwnerReferences {
			require.NotEqualf(r, "Gateway", owner.Kind, "user HPA unexpectedly owned by Gateway %s", owner.Name)
		}
	})

	logger.Logf(t, "BEHAVIOR RESULT: user-managed HPA %s/%s still exists unchanged spec=%s owners=%s", namespace, hpaName, hpaSpec(hpa), hpaOwnerReferences(hpa.OwnerReferences))
}

func gatewayListeners(listeners []gwv1.Listener) string {
	if len(listeners) == 0 {
		return "none"
	}

	result := ""
	for i, listener := range listeners {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s:%d/%s", listener.Name, listener.Port, listener.Protocol)
	}
	return result
}

func gatewayAnnotations(annotations map[string]string) string {
	if len(annotations) == 0 {
		return "none"
	}

	keys := []string{
		annotationDefaultReplicas,
		annotationHPAEnabled,
		annotationHPAMinReplicas,
		annotationHPAMaxReplicas,
		annotationHPACPUTarget,
	}

	result := ""
	for _, key := range keys {
		value, ok := annotations[key]
		if !ok {
			continue
		}
		if result != "" {
			result += ", "
		}
		result += fmt.Sprintf("%s=%s", key, value)
	}
	if result == "" {
		return "custom annotations present"
	}
	return result
}

func hpaSpec(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
	minReplicas := "nil"
	if hpa.Spec.MinReplicas != nil {
		minReplicas = fmt.Sprintf("%d", *hpa.Spec.MinReplicas)
	}

	cpuTarget := "nil"
	if len(hpa.Spec.Metrics) > 0 && hpa.Spec.Metrics[0].Resource != nil && hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != nil {
		cpuTarget = fmt.Sprintf("%d", *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}

	return fmt.Sprintf("target=%s/%s min=%s max=%d cpuAverageUtilization=%s", hpa.Spec.ScaleTargetRef.Kind, hpa.Spec.ScaleTargetRef.Name, minReplicas, hpa.Spec.MaxReplicas, cpuTarget)
}

func hpaOwnerReferences(ownerReferences []metav1.OwnerReference) string {
	if len(ownerReferences) == 0 {
		return "none"
	}

	owners := ""
	for i, owner := range ownerReferences {
		if i > 0 {
			owners += ", "
		}

		controller := "false"
		if owner.Controller != nil && *owner.Controller {
			controller = "true"
		}
		owners += fmt.Sprintf("%s/%s(controller=%s)", owner.Kind, owner.Name, controller)
	}

	return owners
}
