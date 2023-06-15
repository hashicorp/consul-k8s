// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// GatewayClassConfig tests the creation of a gatewayclassconfig object and makes sure that its configuration
// is properly applied to any child gateway objects, namely that the number of gateway instances match the defined
// minInstances,maxInstances and defaultInstances parameters, and that changing the parent gateway does not affect
// the child gateways.
func TestAPIGateway_GatewayClassConfig(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()
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
	namespace := "gateway-namespace"

	//create clean namespace
	err = k8sClient.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	})
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting gateway namesapce")
		k8sClient.Delete(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		})
	})

	defaultInstances := pointer.Int32(2)
	maxInstances := pointer.Int32(8)
	minInstances := pointer.Int32(1)
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
	logger.Log(t, "creating gateway class config")
	err = k8sClient.Create(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateway class configs")
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	gatewayParametersRef := &gwv1beta1.ParametersReference{
		Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	}

	// create gateway class referencing gateway-class-config
	gatewayClassName := "gateway-class"
	logger.Log(t, "creating controlled gateway class")
	createGatewayClass(t, k8sClient, gatewayClassName, gatewayClassControllerName, gatewayParametersRef)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateway classes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.GatewayClass{})
	})

	// Create a certificate to reference in listeners
	certificateInfo := generateCertificate(t, nil, "certificate.consul.local")
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
			corev1.TLSCertKey:       certificateInfo.CertPEM,
			corev1.TLSPrivateKeyKey: certificateInfo.PrivateKeyPEM,
		},
	}
	logger.Log(t, "creating certificate")
	err = k8sClient.Create(context.Background(), certificate)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8sClient.Delete(context.Background(), certificate)
	})

	// Create gateway referencing gateway class config
	gatewayName := "gateway"
	logger.Log(t, "creating controlled gateway")
	gateway := createGateway(t, k8sClient, gatewayName, namespace, gatewayClassName, certificateName)
	// make sure it exists
	logger.Log(t, "checking that gateway one is synchronized to Consul")
	checkConsulExists(t, consulClient, api.APIGateway, gatewayName)

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateways")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.Gateway{}, client.InNamespace(namespace))
	})

	// Scenario: Gateway deployment should match the default instances defined on the gateway class config
	logger.Log(t, "checking that gateway instances match defined gateway class config")
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, defaultInstances, gateway)

	//Scenario: Updating the GatewayClassConfig should not affect gateways that have already been created
	logger.Log(t, "updating gatewayclassconfig values")
	err = k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayClassConfigName, Namespace: namespace}, gatewayClassConfig)
	require.NoError(t, err)
	gatewayClassConfig.Spec.DeploymentSpec.DefaultInstances = pointer.Int32(8)
	gatewayClassConfig.Spec.DeploymentSpec.MinInstances = pointer.Int32(5)
	err = k8sClient.Update(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, defaultInstances, gateway)

	//Scenario: gateways should be able to scale independently and not get overridden by the controller unless it's above the max
	scale(t, k8sClient, gateway.Name, gateway.Namespace, maxInstances)
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, maxInstances, gateway)
	scale(t, k8sClient, gateway.Name, gateway.Namespace, pointer.Int32(10))
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, maxInstances, gateway)
	scale(t, k8sClient, gateway.Name, gateway.Namespace, pointer.Int32(0))
	checkNumberOfInstances(t, k8sClient, consulClient, gateway.Name, gateway.Namespace, minInstances, gateway)

}

func scale(t *testing.T, client client.Client, name, namespace string, scaleTo *int32) {
	t.Helper()

	retryCheck(t, 30, func(r *retry.R) {
		var deployment appsv1.Deployment
		err := client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &deployment)
		require.NoError(r, err)

		deployment.Spec.Replicas = scaleTo
		err = client.Update(context.Background(), &deployment)
		require.NoError(r, err)
	})
}

func checkNumberOfInstances(t *testing.T, k8client client.Client, consulClient *api.Client, name, namespace string, wantNumber *int32, gateway *gwv1beta1.Gateway) {
	t.Helper()

	retryCheck(t, 30, func(r *retry.R) {
		//first check to make sure the number of replicas has been set properly
		var deployment appsv1.Deployment
		err := k8client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &deployment)
		require.NoError(r, err)
		require.EqualValues(r, *wantNumber, *deployment.Spec.Replicas)

		//then check to make sure the number of gateway pods matches the replicas generated
		podList := corev1.PodList{}
		labels := common.LabelsForGateway(gateway)
		err = k8client.List(context.Background(), &podList, client.InNamespace(namespace), client.MatchingLabels(labels))
		require.NoError(r, err)
		require.EqualValues(r, *wantNumber, len(podList.Items))

		services, _, err := consulClient.Catalog().Service(name, "", nil)
		require.NoError(r, err)
		require.EqualValues(r, *wantNumber, len(services))
	})
}
