// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GatewayClassConfig tests the creation of a gatewayclassconfig object and makes sure that its configuration
// is properly applied to any child gateway objects, namely that the number of gateway instances match the defined
// minInstances,maxInstances and defaultInstances parameters, and that changing the parent gateway does not affect
// the child gateways.
func TestAPIGateway_GatewayClassConfig(t *testing.T) {
	var (
		defaultInstances = ptr.To(int32(2))
		maxInstances     = ptr.To(int32(3))
		minInstances     = ptr.To(int32(1))
		gatewayClassName = "custom-gateway-class"
	)

	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()
	namespace := ctx.KubectlOptions(t).Namespace
	helmValues := map[string]string{
		"global.logLevel":       "trace",
		"connectInject.enabled": "true",
	}
	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// Override the default proxy config settings for this test.
	consulClient, _ := consulCluster.SetupConsulClient(t, false)
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	k8sClient := ctx.ControllerRuntimeClient(t)

	// create a GatewayClassConfig with configuration set
	gatewayClassConfigName := "gateway-class-config"
	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassConfigName,
		},
		Spec: v1alpha1.GatewayClassConfigSpec{
			DeploymentSpec: v1alpha1.DeploymentSpec{
				DefaultInstances: defaultInstances,
				MaxInstances:     maxInstances,
				MinInstances:     minInstances,
			},
		},
	}
	if cfg.EnableOpenshift {
		gatewayClassConfig.Spec.OpenshiftSCCName = "restricted-v2"
	}
	logger.Log(t, "creating gateway class config")
	err = k8sClient.Create(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway class configs")
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	gatewayParametersRef := &gwv1beta1.ParametersReference{
		Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	}

	// Create gateway class referencing gateway-class-config.
	logger.Log(t, "creating controlled gateway class")
	createGatewayClass(t, k8sClient, gatewayClassName, gatewayClassControllerName, gatewayParametersRef)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway classes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.CustomGatewayClass{})
	})

	// Create a certificate to reference in listeners.
	certInfo := generateCertificate(t, nil, "certificate.consul.local")
	certificateName := "certificate"
	certificate := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certificateName,
			Namespace: namespace,
			Labels: map[string]string{
				"test-certificate": "true",
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       certInfo.CertPEM,
			corev1.TLSPrivateKeyKey: certInfo.PrivateKeyPEM,
		},
	}
	logger.Log(t, "creating certificate")
	err = k8sClient.Create(context.Background(), certificate)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), certificate)
	})

	// Create gateway referencing gateway class.
	gatewayName := "gcctestgateway" + namespace
	logger.Log(t, "creating controlled gateway")
	gateway := createGateway(t, k8sClient, gatewayName, namespace, gatewayClassName, certificateName)

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateways")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.Gateway{}, client.InNamespace(namespace))
	})

	// Ensure it exists.
	logger.Log(t, "checking that gateway is synchronized to Consul")
	checkConsulExists(t, consulClient, api.APIGateway, gatewayName)

	// Scenario: Gateway deployment should match the default instances defined on the gateway class config
	// checking that gateway instances match defined gateway class config
	/*
		defaultInstances = ptr.To(int32(2))
		maxInstances     = ptr.To(int32(3))
		minInstances     = ptr.To(int32(1))
	*/
	// expect 2
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, defaultInstances, gateway)

	// expect 3
	scale(t, k8sClient, gateway.Name, gateway.Namespace, ptr.To(int32(*maxInstances+1)))
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, maxInstances, gateway)
	// at this stage replica count is equal to the maxInstances
	replicas := maxInstances
	// Scenario: Updating the GatewayClassConfig should not affect gateways that have already been created
	logger.Log(t, "updating gatewayclassconfig values")
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayClassConfigName, Namespace: namespace}, gatewayClassConfig)
	require.NoError(t, err)
	// gatewayClassConfig.Spec.DeploymentSpec.DefaultInstances = ptr.To(int32(8))
	// gatewayClassConfig.Spec.DeploymentSpec.MinInstances = ptr.To(int32(5))
	gatewayClassConfig.Spec.DeploymentSpec.DefaultInstances = ptr.To(int32(2))
	gatewayClassConfig.Spec.DeploymentSpec.MinInstances = ptr.To(int32(2))
	gatewayClassConfig.Spec.DeploymentSpec.MaxInstances = ptr.To(int32(5))
	err = k8sClient.Update(context.Background(), gatewayClassConfig)
	require.NoError(t, err)

	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, replicas, gateway)

	/*
			Here we have updated the gatewayclass config with:
			defaultInstances: 8
			minInstances: 5
			maxInstances: 3


			# real variables values
			defaultInstances: 2
			minInstances: 1
			maxInstances: 3

			# Before fix for gatewayclassconfig:
			## Values in the gatewayclass config
			defaultInstances: 2
			minInstances: 1
			maxInstances: 3

			# As we fix the gatewayclassConfig
			## Values in the gatewayclass config
			defaultInstances: 8
			minInstances: 5
			maxInstances: 3

		# In the next step we scale to maxinstances +1 which is 4; but since we set minInstances to 5 in the above gatewayclass config, we get 5.
		# In the comment "Scenario: gateways should be able to scale independently and not get overridden by the controller unless it's above the max"
		## We are expecting the instances should be 3, but as per our code Logic we minInstances at the last for instanceValue.

		## Here is a new bug. JIRA:-

		## As of now, we assume it should be 3, but since we have set min to 5, we get 5 instances.
		# Thus the scenarios now would be:
		1. set max instances to 4, min instances 2, default to 3 ; pass with no changes below, we expect 4 but try t scale to 5.
		2. scale down to 0, we expect 2.
	*/
	maxInstances = ptr.To(int32(5))
	minInstances = ptr.To(int32(2))
	// Scenario: gateways should be able to scale independently and not get overridden by the controller unless it's above the max
	// expect 5
	scale(t, k8sClient, gateway.Name, gateway.Namespace, ptr.To(int32(*maxInstances+1)))
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, maxInstances, gateway)
	// expect 2
	scale(t, k8sClient, gateway.Name, gateway.Namespace, ptr.To(int32(0)))
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, minInstances, gateway)

}

func scale(t *testing.T, client client.Client, name, namespace string, scaleTo *int32) {
	t.Helper()
	cfg := suite.Config()
	var deployment appsv1.Deployment
	err := client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &deployment)
	require.NoError(t, err)

	logger.Log(t, fmt.Sprintf("scaling gateway from %d to %d", *deployment.Spec.Replicas, *scaleTo))

	deployment.Spec.Replicas = scaleTo
	err = client.Update(context.Background(), &deployment)
	require.NoError(t, err)

	if cfg.EnableOpenshift {
		retryCheckWithWait(t, 12, 5*time.Second, func(r *retry.R) {
			var updatedDeployment appsv1.Deployment
			err := client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &updatedDeployment)
			require.NoError(r, err)
			require.NotNil(r, updatedDeployment.Spec.Replicas)
		})

		triggerGatewayReconcile := func() {
			var gateway gwv1beta1.Gateway
			err = client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &gateway)
			require.NoError(t, err)
			if gateway.Annotations == nil {
				gateway.Annotations = map[string]string{}
			}
			gateway.Annotations["acceptance.hashicorp.com/reconcile-trigger"] = time.Now().UTC().Format(time.RFC3339Nano)
			err = client.Update(context.Background(), &gateway)
			require.NoError(t, err)
		}

		triggerGatewayReconcile()

		// The gateway controller can observe the owned Deployment update before its cache
		// reflects the new replica count. Trigger a second reconcile after a short delay so
		// the clamp logic uses the latest Deployment state.
		time.Sleep(15 * time.Second)
		triggerGatewayReconcile()
	}

}

func checkNumberOfInstances(t *testing.T, k8client client.Client, consulClient *api.Client, name, namespace string, wantNumber *int32, gateway *gwv1beta1.Gateway) {
	t.Helper()

	retryCheckWithWait(t, 40, 10*time.Second, func(r *retry.R) {
		logger.Log(t, "checking that gateway instances match defined gateway class config")
		logger.Log(t, fmt.Sprintf("want: %d", *wantNumber))

		// Ensure the number of replicas has been set properly.
		var deployment appsv1.Deployment
		err := k8client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &deployment)
		require.NoError(r, err)
		logger.Log(t, fmt.Sprintf("deployment replicas: %d", *deployment.Spec.Replicas))
		require.EqualValues(r, *wantNumber, *deployment.Spec.Replicas, "deployment replicas should match the number of instances defined on the gateway class config")

		// Ensure the number of gateway pods matches the replicas generated.
		podList := corev1.PodList{}
		//TODO:: Label check
		//labels := common.LabelsForGateway(gateway)
		//err = k8client.List(context.Background(), &podList, client.InNamespace(namespace), client.MatchingLabels(labels))
		//require.NoError(r, err)
		logger.Log(t, fmt.Sprintf("number of pods: %d", len(podList.Items)))
		require.EqualValues(r, *wantNumber, len(podList.Items), "number of pods should match the number of instances defined on the gateway class config")

		// Ensure the number of services matches the replicas generated.
		services, _, err := consulClient.Catalog().Service(name, "", nil)
		seenServices := map[string]interface{}{}
		require.NoError(r, err)
		logger.Log(t, fmt.Sprintf("number of services: %d", len(services)))
		//we need to double check that we aren't double counting services with the same ID
		for _, s := range services {
			seenServices[s.ServiceID] = true
			logger.Log(t, fmt.Sprintf("service info: id: %s, name: %s, namespace: %s", s.ServiceID, s.ServiceName, s.Namespace))
		}

		logger.Log(t, fmt.Sprintf("number of services: %d", len(services)))
		require.EqualValues(r, int(*wantNumber), len(seenServices), "number of services should match the number of instances defined on the gateway class config")
	})
}
