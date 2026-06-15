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
	scalingAnnotationHPAEnabled      = "consul.hashicorp.com/hpa-enabled"
	scalingAnnotationHPAMinReplicas  = "consul.hashicorp.com/hpa-minimum-replicas"
	scalingAnnotationHPAMaxReplicas  = "consul.hashicorp.com/hpa-maximum-replicas"
	scalingAnnotationHPACPUTarget    = "consul.hashicorp.com/hpa-cpu-utilisation-target"
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
		"global.logLevel": "debug",
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
		"global.logLevel": "debug",
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
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 5)
}

func TestOpenshift_APIGateway_Scaling_UserManagedHPATakesPrecedenceOverAnnotations(t *testing.T) {
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
		scalingAnnotationHPAMinReplicas: "2",
		scalingAnnotationHPAMaxReplicas: "8",
		scalingAnnotationHPACPUTarget:   "70",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace)

	createUserManagedHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace, 4, 12, 60, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAUnchangedOCP(t, k8sClient, gateway.Name, gateway.Namespace, 4, 12, 60)
}

func TestOpenshift_APIGateway_Scaling_UserManagedHPAPreservesManualScale(t *testing.T) {
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
	gatewayClassName := createScalingGatewayClassResourcesOCP(t, k8sClient, cfg.NoCleanupOnFailure, cfg.NoCleanup, v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(int32(2)),
		MinInstances:     ptr.To(int32(1)),
		MaxInstances:     ptr.To(int32(5)),
	})

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 2)

	createUserManagedHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	scaleGatewayDeploymentOCP(t, k8sClient, gateway.Name, gateway.Namespace, 6)

	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 6)
	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAUnchangedOCP(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80)
}

func TestOpenshift_APIGateway_Scaling_UserManagedHPARemovedRestoresControllerHPA(t *testing.T) {
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
		scalingAnnotationHPAMinReplicas: "2",
		scalingAnnotationHPAMaxReplicas: "6",
		scalingAnnotationHPACPUTarget:   "75",
	}, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace)

	userHPAName := createUserManagedHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace, 3, 9, 65, cfg.NoCleanupOnFailure, cfg.NoCleanup)
	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)

	deleteUserManagedHPAOCP(t, k8sClient, userHPAName, gateway.Namespace)

	hpa := waitForGatewayHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	require.NotNil(t, hpa.Spec.MinReplicas)
	require.Equal(t, int32(2), *hpa.Spec.MinReplicas)
	require.Equal(t, int32(6), hpa.Spec.MaxReplicas)
	require.Len(t, hpa.Spec.Metrics, 1)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource)
	require.NotNil(t, hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	require.Equal(t, int32(75), *hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
}

func TestOpenshift_APIGateway_Scaling_UserManagedHPAUpdatesDriveReplicaChanges(t *testing.T) {
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

	gateway := createScalingGatewayOCP(t, k8sClient, gatewayNamespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	hpaName := createUserManagedHPAOCP(t, k8sClient, gateway.Name, gateway.Namespace, 1, 10, 80, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 1)

	updateUserManagedHPAReplicaBoundsOCP(t, k8sClient, hpaName, gateway.Namespace, 4, 10)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 4)

	updateUserManagedHPAReplicaBoundsOCP(t, k8sClient, hpaName, gateway.Namespace, 6, 10)
	waitForGatewayDeploymentReplicasOCP(t, k8sClient, gateway.Name, gateway.Namespace, 6)

	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
}

func TestOpenshift_APIGateway_Scaling_DanglingUserHPABeforeGatewayTakesPrecedence(t *testing.T) {
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
	gatewayName := helpers.RandomName()

	hpaName := createUserManagedHPAForTargetOCP(t, k8sClient, "dangling-user-hpa", gatewayNamespace, gatewayName, 3, 9, 65, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	gateway := createScalingGatewayWithNameOCP(t, k8sClient, gatewayName, gatewayNamespace, gatewayClassName, nil, cfg.NoCleanupOnFailure, cfg.NoCleanup)

	waitForGatewayPodsReadyOCP(t, k8sClient, gateway.Name, gateway.Namespace, 1)
	waitForGatewayHPAAbsentOCP(t, k8sClient, gateway.Name, gateway.Namespace)
	requireUserHPAForTargetUnchangedOCP(t, k8sClient, hpaName, gateway.Namespace, gateway.Name, 3, 9, 65)
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

	return createScalingGatewayWithNameOCP(t, k8sClient, helpers.RandomName(), namespace, gatewayClassName, annotations, noCleanupOnFailure, noCleanup)
}

func createScalingGatewayWithNameOCP(
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
	logger.Logf(t, "TEST INPUT: Gateway %s/%s spec gatewayClass=%s listeners=%s annotations=%s", gateway.Namespace, gateway.Name, gateway.Spec.GatewayClassName, gatewayListenersOCP(gateway.Spec.Listeners), gatewayAnnotationsOCP(gateway.Annotations))
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

func waitForGatewayPodsReadyOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string, minReady int32) {
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

func waitForGatewayHPAOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string) *autoscalingv2.HorizontalPodAutoscaler {
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

	logger.Logf(t, "BEHAVIOR RESULT: controller-managed HPA %s/%s exists spec=%s owners=%s", namespace, controllerHPAName, hpaSpecOCP(hpa), hpaOwnerReferencesOCP(hpa.OwnerReferences))
	return hpa
}

func waitForGatewayHPAAbsentOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string) {
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

// createUserManagedHPAOCP creates an HPA owned by the user (no Gateway owner
// reference) targeting the gateway's Deployment. Returns the HPA name.
func createUserManagedHPAOCP(
	t *testing.T,
	k8sClient client.Client,
	gatewayName, namespace string,
	minReplicas, maxReplicas, cpuTarget int32,
	noCleanupOnFailure bool,
	noCleanup bool,
) string {
	t.Helper()

	hpaName := fmt.Sprintf("%s-user-hpa", gatewayName)
	return createUserManagedHPAForTargetOCP(t, k8sClient, hpaName, namespace, gatewayName, minReplicas, maxReplicas, cpuTarget, noCleanupOnFailure, noCleanup)
}

func createUserManagedHPAForTargetOCP(
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

	logger.Logf(t, "TEST INPUT: user-managed HPA %s/%s spec=%s owners=%s", namespace, hpaName, hpaSpecOCP(hpa), hpaOwnerReferencesOCP(hpa.OwnerReferences))
	return hpaName
}

// updateUserManagedHPAReplicaBoundsOCP updates the min/max replicas on an
// existing user-managed HPA, retrying on conflict.
func updateUserManagedHPAReplicaBoundsOCP(t *testing.T, k8sClient client.Client, hpaName, namespace string, minReplicas, maxReplicas int32) {
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

// deleteUserManagedHPAOCP deletes a user-managed HPA by name.
func deleteUserManagedHPAOCP(t *testing.T, k8sClient client.Client, hpaName, namespace string) {
	t.Helper()

	logger.Logf(t, "TEST ACTION: deleting user-managed HPA %s/%s so controller-managed HPA can be restored", namespace, hpaName)

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: hpaName, Namespace: namespace},
	}
	err := k8sClient.Delete(context.Background(), hpa)
	require.NoError(t, client.IgnoreNotFound(err))

	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: hpaName, Namespace: namespace}, &autoscalingv2.HorizontalPodAutoscaler{})
		require.True(r, apierrors.IsNotFound(err), "expected user HPA to be deleted, got %v", err)
	})

	logger.Logf(t, "BEHAVIOR RESULT: user-managed HPA %s/%s was deleted by the test action", namespace, hpaName)
}

// requireUserHPAUnchangedOCP asserts the user-managed HPA still exists with its
// original spec and has not acquired a Gateway owner reference.
func requireUserHPAUnchangedOCP(t *testing.T, k8sClient client.Client, gatewayName, namespace string, minReplicas, maxReplicas, cpuTarget int32) {
	t.Helper()

	hpaName := fmt.Sprintf("%s-user-hpa", gatewayName)
	requireUserHPAForTargetUnchangedOCP(t, k8sClient, hpaName, namespace, gatewayName, minReplicas, maxReplicas, cpuTarget)
}

func requireUserHPAForTargetUnchangedOCP(t *testing.T, k8sClient client.Client, hpaName, namespace, targetName string, minReplicas, maxReplicas, cpuTarget int32) {
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

	logger.Logf(t, "BEHAVIOR RESULT: user-managed HPA %s/%s still exists unchanged spec=%s owners=%s", namespace, hpaName, hpaSpecOCP(hpa), hpaOwnerReferencesOCP(hpa.OwnerReferences))
}

func gatewayListenersOCP(listeners []gwv1.Listener) string {
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

func gatewayAnnotationsOCP(annotations map[string]string) string {
	if len(annotations) == 0 {
		return "none"
	}

	keys := []string{
		scalingAnnotationDefaultReplicas,
		scalingAnnotationHPAEnabled,
		scalingAnnotationHPAMinReplicas,
		scalingAnnotationHPAMaxReplicas,
		scalingAnnotationHPACPUTarget,
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

func hpaSpecOCP(hpa *autoscalingv2.HorizontalPodAutoscaler) string {
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

func hpaOwnerReferencesOCP(ownerReferences []metav1.OwnerReference) string {
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
