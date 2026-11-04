// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulgwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
)

// TestOpenshift_ConsulAPIGateway_GatewayClassConfigReplicas is the OpenShift /
// consul.hashicorp.com counterpart of TestAPIGateway_GatewayClassConfig. It covers
// the replica behaviour that 1.8.x supports without HPA autoscaling:
//
//   - a new gateway deployment starts at GatewayClassConfig defaultInstances;
//   - manual scaling above maxInstances is capped at maxInstances;
//   - manual scaling within [minInstances, maxInstances] is preserved;
//   - updated GatewayClassConfig bounds apply to the existing gateway, and
//     manual scaling below minInstances is raised to minInstances.
//
// Each step checks the deployment spec, the ready gateway pods and the gateway
// instances registered in the Consul catalog.
func TestOpenshift_ConsulAPIGateway_GatewayClassConfigReplicas(t *testing.T) {
	cfg := suite.Config()
	newOpenshiftClusterWithHelmValues(t, cfg, false, false, consulAPIGatewayHelmValues)
	logf.SetLogger(logr.New(nil))

	ctx := suite.Environment().DefaultContext(t)
	k8sClient := consulGatewayRuntimeClient(t, ctx)

	consulCluster := consul.NewHelmCluster(t, map[string]string{}, ctx, cfg, "")
	consulCluster.SetNamespace("consul")
	consulClient, _ := consulCluster.SetupConsulClient(t, false)

	gatewayNamespace, _ := createNamespace(t, ctx, cfg)

	// GatewayClassConfig -> CustomGatewayClass -> Gateway, all owned by the new controller.
	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName()},
		Spec: v1alpha1.GatewayClassConfigSpec{
			OpenshiftSCCName: "restricted-v2",
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: ptr.To(int32(2)),
				MinInstances:     ptr.To(int32(1)),
				MaxInstances:     ptr.To(int32(3)),
			},
		},
	}
	require.NoError(t, k8sClient.Create(context.Background(), gatewayClassConfig))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(), gatewayClassConfig))
	})

	gatewayClass := &consulgwv1beta1.CustomGatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName()},
		Spec: consulgwv1beta1.GatewayClassSpec{
			ControllerName: consulGatewayClassControllerName,
			ParametersRef: &consulgwv1beta1.ParametersReference{
				Group: consulgwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  consulgwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
				Name:  gatewayClassConfig.Name,
			},
		},
	}
	require.NoError(t, k8sClient.Create(context.Background(), gatewayClass))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(), gatewayClass))
	})
	requireConsulGatewayClassAccepted(t, k8sClient, gatewayClass.Name)

	gateway := &consulgwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      helpers.RandomName(),
			Namespace: gatewayNamespace,
			Labels:    map[string]string{"component": "api-gateway-consul"},
		},
		Spec: consulgwv1beta1.GatewaySpec{
			GatewayClassName: consulgwv1beta1.ObjectName(gatewayClass.Name),
			Listeners: []consulgwv1beta1.Listener{{
				Name:     "http",
				Port:     8080,
				Protocol: consulgwv1beta1.HTTPProtocolType,
			}},
		},
	}
	require.NoError(t, k8sClient.Create(context.Background(), gateway))
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_ = client.IgnoreNotFound(k8sClient.Delete(context.Background(), gateway))
	})

	checkConsulExists(t, consulClient, api.APIGateway, gateway.Name)

	logger.Log(t, "deployment starts at defaultInstances")
	checkConsulGatewayInstances(t, k8sClient, consulClient, gateway, 2)

	logger.Log(t, "scaling above maxInstances is capped at maxInstances")
	scaleConsulGateway(t, k8sClient, gateway, 4)
	checkConsulGatewayInstances(t, k8sClient, consulClient, gateway, 3)

	logger.Log(t, "scaling within bounds is preserved")
	scaleConsulGateway(t, k8sClient, gateway, 1)
	checkConsulGatewayInstances(t, k8sClient, consulClient, gateway, 1)
	requireConsulGatewayReplicasStable(t, k8sClient, gateway, 1, time.Minute)

	logger.Log(t, "updating GatewayClassConfig bounds to min=2, max=5")
	retry.RunWith(&retry.Counter{Count: 10, Wait: 2 * time.Second}, t, func(r *retry.R) {
		var current v1alpha1.GatewayClassConfig
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayClassConfig.Name}, &current))
		current.Spec.DeploymentSpec.MinInstances = ptr.To(int32(2))
		current.Spec.DeploymentSpec.MaxInstances = ptr.To(int32(5))
		require.NoError(r, k8sClient.Update(context.Background(), &current))
	})

	logger.Log(t, "new maxInstances caps manual scaling")
	scaleConsulGateway(t, k8sClient, gateway, 6)
	checkConsulGatewayInstances(t, k8sClient, consulClient, gateway, 5)

	logger.Log(t, "new minInstances raises manual scaling below the minimum")
	scaleConsulGateway(t, k8sClient, gateway, 0)
	checkConsulGatewayInstances(t, k8sClient, consulClient, gateway, 2)
}

// checkConsulGatewayInstances waits until the gateway deployment, its ready
// pods and its Consul catalog registrations all equal want.
func checkConsulGatewayInstances(t *testing.T, k8sClient client.Client, consulClient *api.Client, gateway *consulgwv1beta1.Gateway, want int32) {
	t.Helper()

	retryCheckWithWait(t, 40, 10*time.Second, func(r *retry.R) {
		var deployment appsv1.Deployment
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}, &deployment))
		require.NotNil(r, deployment.Spec.Replicas)
		require.EqualValues(r, want, *deployment.Spec.Replicas, "deployment replicas")

		var pods corev1.PodList
		require.NoError(r, k8sClient.List(context.Background(), &pods, client.InNamespace(gateway.Namespace), client.MatchingLabels{
			"gateway.consul.hashicorp.com/name":      gateway.Name,
			"gateway.consul.hashicorp.com/namespace": gateway.Namespace,
		}))
		var ready int32
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					ready++
				}
			}
		}
		require.EqualValues(r, want, ready, "ready gateway pods")

		services, _, err := consulClient.Catalog().Service(gateway.Name, "", nil)
		require.NoError(r, err)
		ids := map[string]struct{}{}
		for _, s := range services {
			ids[s.ServiceID] = struct{}{}
		}
		require.EqualValues(r, want, len(ids), "gateway instances registered in Consul")
	})
	logger.Logf(t, "gateway %s/%s has %d instances", gateway.Namespace, gateway.Name, want)
}

func scaleConsulGateway(t *testing.T, k8sClient client.Client, gateway *consulgwv1beta1.Gateway, replicas int32) {
	t.Helper()

	retry.RunWith(&retry.Counter{Count: 10, Wait: 2 * time.Second}, t, func(r *retry.R) {
		var deployment appsv1.Deployment
		require.NoError(r, k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}, &deployment))
		deployment.Spec.Replicas = ptr.To(replicas)
		require.NoError(r, k8sClient.Update(context.Background(), &deployment))
	})
	logger.Logf(t, "scaled gateway deployment %s/%s to %d", gateway.Namespace, gateway.Name, replicas)
}

// requireConsulGatewayReplicasStable asserts the controller does not change
// the deployment's replica count for the given duration.
func requireConsulGatewayReplicasStable(t *testing.T, k8sClient client.Client, gateway *consulgwv1beta1.Gateway, want int32, duration time.Duration) {
	t.Helper()

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		var deployment appsv1.Deployment
		require.NoError(t, k8sClient.Get(context.Background(), types.NamespacedName{Name: gateway.Name, Namespace: gateway.Namespace}, &deployment))
		require.NotNil(t, deployment.Spec.Replicas)
		require.EqualValues(t, want, *deployment.Spec.Replicas, fmt.Sprintf("controller changed manually set replicas of %s/%s", gateway.Namespace, gateway.Name))
		time.Sleep(5 * time.Second)
	}
}

func checkConsulExists(t *testing.T, consulClient *api.Client, kind, name string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(kind, name, nil)
		require.NoError(r, err)
	})
}
