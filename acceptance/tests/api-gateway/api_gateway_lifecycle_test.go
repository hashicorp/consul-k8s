// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestAPIGateway_Lifecycle(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()
	helmValues := map[string]string{
		"global.logLevel":       "trace",
		"connectInject.enabled": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	k8sClient := ctx.ControllerRuntimeClient(t)
	consulClient, _ := consulCluster.SetupConsulClient(t, false)

	defaultNamespace := "default"

	// create a service to target
	targetName := "static-server"
	logger.Log(t, "creating target server")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	// create a basic GatewayClassConfig
	gatewayClassConfigName := "controlled-gateway-class-config"
	gatewayClassConfig := &v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: gatewayClassConfigName,
		},
	}
	logger.Log(t, "creating gateway class config")
	err := k8sClient.Create(context.Background(), gatewayClassConfig)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateway class configs")
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	// create three gateway classes, two we control, one we don't
	controlledGatewayClassOneName := "controlled-gateway-class-one"
	controlledGatewayClassOne := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: controlledGatewayClassOneName,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gatewayClassControllerName,
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
				Name:  gatewayClassConfigName,
			},
		},
	}
	controlledGatewayClassTwoName := "controlled-gateway-class-two"
	controlledGatewayClassTwo := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: controlledGatewayClassTwoName,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gatewayClassControllerName,
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
				Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
				Name:  gatewayClassConfigName,
			},
		},
	}
	uncontrolledGatewayClassName := "uncontrolled-gateway-class"
	uncontrolledGatewayClass := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: uncontrolledGatewayClassName,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: "example.com/some-controller",
		},
	}

	logger.Log(t, "creating controlled gateway class one")
	err = k8sClient.Create(context.Background(), controlledGatewayClassOne)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateway classes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.GatewayClass{})
	})

	logger.Log(t, "creating controlled gateway class two")
	err = k8sClient.Create(context.Background(), controlledGatewayClassTwo)
	require.NoError(t, err)

	logger.Log(t, "creating an uncontrolled gateway class")
	err = k8sClient.Create(context.Background(), uncontrolledGatewayClass)
	require.NoError(t, err)

	// Create a certificate to reference in listeners
	certificateInfo := generateCertificate(t, nil, "certificate.consul.local")
	certificateName := "certificate"
	certificate := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certificateName,
			Namespace: defaultNamespace,
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

	// Create three gateways with a basic HTTPS listener to correspond to the three classes
	controlledGatewayOneName := "controlled-gateway-one"
	controlledGatewayOne := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controlledGatewayOneName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(controlledGatewayClassOneName),
			Listeners: []gwv1beta1.Listener{{
				Name:     gwv1beta1.SectionName("listener"),
				Protocol: gwv1beta1.HTTPSProtocolType,
				Port:     8443,
				TLS: &gwv1beta1.GatewayTLSConfig{
					CertificateRefs: []gwv1beta1.SecretObjectReference{{
						Name: gwv1beta1.ObjectName(certificateName),
					}},
				},
			}},
		},
	}
	controlledGatewayTwoName := "controlled-gateway-two"
	controlledGatewayTwo := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controlledGatewayTwoName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(controlledGatewayClassTwoName),
			Listeners: []gwv1beta1.Listener{{
				Name:     gwv1beta1.SectionName("listener"),
				Protocol: gwv1beta1.HTTPSProtocolType,
				Port:     8443,
				TLS: &gwv1beta1.GatewayTLSConfig{
					CertificateRefs: []gwv1beta1.SecretObjectReference{{
						Name: gwv1beta1.ObjectName(certificateName),
					}},
				},
			}},
		},
	}
	uncontrolledGatewayName := "uncontrolled-gateway"
	uncontrolledGateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uncontrolledGatewayName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(uncontrolledGatewayClassName),
			Listeners: []gwv1beta1.Listener{{
				Name:     gwv1beta1.SectionName("listener"),
				Protocol: gwv1beta1.HTTPSProtocolType,
				Port:     8443,
				TLS: &gwv1beta1.GatewayTLSConfig{
					CertificateRefs: []gwv1beta1.SecretObjectReference{{
						Name: gwv1beta1.ObjectName(certificateName),
					}},
				},
			}},
		},
	}

	logger.Log(t, "creating controlled gateway one")
	err = k8sClient.Create(context.Background(), controlledGatewayOne)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all gateways")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.Gateway{}, client.InNamespace(defaultNamespace))
	})

	logger.Log(t, "creating controlled gateway two")
	err = k8sClient.Create(context.Background(), controlledGatewayTwo)
	require.NoError(t, err)

	logger.Log(t, "creating an uncontrolled gateway")
	err = k8sClient.Create(context.Background(), uncontrolledGateway)
	require.NoError(t, err)

	// create two http routes associated with the first controlled gateway
	routeOneName := "route-one"
	routeOne := &gwv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeOneName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{Name: gwv1beta1.ObjectName(controlledGatewayOneName)},
				},
			},
			Rules: []gwv1beta1.HTTPRouteRule{
				{BackendRefs: []gwv1beta1.HTTPBackendRef{
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: gwv1beta1.ObjectName(targetName)},
					}},
				}},
			},
		},
	}

	routeTwoName := "route-two"
	routeTwo := &gwv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeTwoName,
			Namespace: defaultNamespace,
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{Name: gwv1beta1.ObjectName(controlledGatewayOneName)},
				},
			},
			Rules: []gwv1beta1.HTTPRouteRule{
				{BackendRefs: []gwv1beta1.HTTPBackendRef{
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: gwv1beta1.ObjectName(targetName)},
					}},
				}},
			},
		},
	}

	logger.Log(t, "creating route one")
	err = k8sClient.Create(context.Background(), routeOne)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		logger.Log(t, "deleting all http routes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.HTTPRoute{}, client.InNamespace(defaultNamespace))
	})

	logger.Log(t, "creating route two")
	err = k8sClient.Create(context.Background(), routeTwo)
	require.NoError(t, err)

	// Scenarios: Swapping a route to another controlled gateway should clean up the old parent statuses and references on Consul resources

	// check that the route is bound properly and objects are reflected in Consul
	logger.Log(t, "checking that http route one is bound to gateway one")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, controlledGatewayOneName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Synced", "Synced"))
	})

	logger.Log(t, "checking that http route one is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 1)
		require.Equal(r, controlledGatewayOneName, route.Parents[0].Name)
	})

	// update the route to point to the other controlled gateway
	logger.Log(t, "updating route one to be bound to gateway two")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(routeOne), routeOne)
	require.NoError(t, err)
	routeOne.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(controlledGatewayTwoName)
	err = k8sClient.Update(context.Background(), routeOne)
	require.NoError(t, err)

	// check that the route is bound properly and objects are reflected in Consul
	logger.Log(t, "checking that http route one is bound to gateway two")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, controlledGatewayTwoName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Synced", "Synced"))
	})

	logger.Log(t, "checking that http route one is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 1)
		require.Equal(r, controlledGatewayTwoName, route.Parents[0].Name)
	})

	// Scenarios: Binding a route to a controlled gateway and then associating it with another gateway we don’t control should clean up Consul resources, route statuses, and finalizers
	// check that the route is bound properly and objects are reflected in Consul

	// check that our second http route is bound properly
	logger.Log(t, "checking that http route two is bound to gateway two")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeTwoName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, controlledGatewayTwoName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Synced", "Synced"))
	})

	logger.Log(t, "checking that http route two is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeTwoName, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 1)
		require.Equal(r, controlledGatewayTwoName, route.Parents[0].Name)
	})

	// update the route to point to the uncontrolled gateway
	logger.Log(t, "updating route two to be bound to an uncontrolled gateway")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(routeTwo), routeTwo)
	require.NoError(t, err)
	routeTwo.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(uncontrolledGatewayName)
	err = k8sClient.Update(context.Background(), routeTwo)
	require.NoError(t, err)

	// check that the route is unbound and all Consul objects and Kubernetes statuses are cleaned up
	logger.Log(t, "checking that http route two is cleaned up because we no longer control it")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeTwoName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 0)
		require.Len(r, route.Finalizers, 0)
	})

	logger.Log(t, "checking that http route two is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeTwoName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "http-route" / "route-two")`)
	})

	// Scenarios: Switching a controlled gateway’s protocol that causes a route to unbind should cause the route to drop the parent ref in Consul and result in proper statuses set in Kubernetes

	// swap the gateway's protocol and see the route unbind
	logger.Log(t, "marking gateway two as using TCP")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(controlledGatewayTwo), controlledGatewayTwo)
	require.NoError(t, err)
	controlledGatewayTwo.Spec.Listeners[0].Protocol = gwv1beta1.TCPProtocolType
	err = k8sClient.Update(context.Background(), controlledGatewayTwo)
	require.NoError(t, err)

	// check that the route is unbound and all Consul objects and Kubernetes statuses are cleaned up
	logger.Log(t, "checking that http route one is not bound to gateway two")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, controlledGatewayTwoName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, falseCondition("Accepted", "NotAllowedByListeners"))
	})

	logger.Log(t, "checking the route one does not have a reference to gateway one in Consul")
	retryCheck(t, 10, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 0)
	})

	// Scenarios: Deleting a gateway should result in routes only referencing it to get cleaned up from Consul and their statuses/finalizers cleared, but routes referencing another controlled gateway should still exist in Consul and only have their statuses cleaned up from referencing the gateway we previously controlled. Any referenced certificates should also get cleaned up.

	// delete gateway two
	logger.Log(t, "deleting gateway two in Kubernetes")
	err = k8sClient.Delete(context.Background(), controlledGatewayTwo)
	require.NoError(t, err)

	// check that the gateway is deleted from Consul
	logger.Log(t, "checking that gateway two is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.APIGateway, controlledGatewayTwoName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "api-gateway" / "controlled-gateway-two")`)
	})

	// check that the Kubernetes route is cleaned up and the entries deleted from Consul
	logger.Log(t, "checking that http route one is cleaned up in Kubernetes")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status, 0)
		require.Len(r, route.Finalizers, 0)
	})

	logger.Log(t, "checking that http route one is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "http-route" / "route-one")`)
	})

	// Scenarios: Changing a gateway class name on a gateway to something we don’t control should have the same affect as deleting it with the addition of cleaning up our finalizer from the gateway.

	// reset route one to point to our first gateway and check that it's bound properly
	logger.Log(t, "remarking route one as bound to gateway one")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(routeOne), routeOne)
	require.NoError(t, err)
	routeOne.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(controlledGatewayOneName)
	err = k8sClient.Update(context.Background(), routeOne)
	require.NoError(t, err)

	logger.Log(t, "checking that http route one is bound to gateway one")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, controlledGatewayOneName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Synced", "Synced"))
	})

	logger.Log(t, "checking that http route one is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 1)
		require.Equal(r, controlledGatewayOneName, route.Parents[0].Name)
	})

	// make the gateway uncontrolled by pointing to a non-existent gateway class
	logger.Log(t, "marking gateway one as not controlled by our controller")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(controlledGatewayOne), controlledGatewayOne)
	require.NoError(t, err)
	controlledGatewayOne.Spec.GatewayClassName = "non-existent"
	err = k8sClient.Update(context.Background(), controlledGatewayOne)
	require.NoError(t, err)

	// check that the Kubernetes gateway is cleaned up
	logger.Log(t, "checking that gateway one is cleaned up in Kubernetes")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: controlledGatewayOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Finalizers, 0)
	})

	// check that the gateway is deleted from Consul
	logger.Log(t, "checking that gateway one is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.APIGateway, controlledGatewayOneName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "api-gateway" / "controlled-gateway-one")`)
	})

	// check that the Kubernetes route is cleaned up and the entries deleted from Consul
	logger.Log(t, "checking that http route one is cleaned up in Kubernetes")
	retryCheck(t, 10, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status, 0)
		require.Len(r, route.Finalizers, 0)
	})

	logger.Log(t, "checking that http route one is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, routeOneName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "http-route" / "route-one")`)
	})

	// Scenarios: Deleting a certificate referenced by a gateway’s listener should make the listener invalid and drop it from Consul.

	// reset the gateway
	logger.Log(t, "remarking gateway one as controlled by our controller")
	err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(controlledGatewayOne), controlledGatewayOne)
	require.NoError(t, err)
	controlledGatewayOne.Spec.GatewayClassName = gwv1beta1.ObjectName(controlledGatewayClassOneName)
	err = k8sClient.Update(context.Background(), controlledGatewayOne)
	require.NoError(t, err)

	// make sure it exists
	logger.Log(t, "checking that gateway one is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.APIGateway, controlledGatewayOneName, nil)
		require.NoError(r, err)
	})

	// make sure our certificate exists
	logger.Log(t, "checking that the certificate is synchronized to Consul")
	retryCheck(t, 10, func(r *retry.R) {
		_, _, err := consulClient.ConfigEntries().Get(api.InlineCertificate, certificateName, nil)
		require.NoError(r, err)
	})

	// delete the certificate in Kubernetes
	logger.Log(t, "deleting the certificate in Kubernetes")
	err = k8sClient.Delete(context.Background(), certificate)
	require.NoError(t, err)

	// make sure the certificate no longer exists in Consul
	logger.Log(t, "checking that the certificate is deleted from Consul")
	retryCheck(t, 10, func(r *retry.R) {
		// we only sync validly referenced certificates over, so check to make sure it is not created.
		_, _, err := consulClient.ConfigEntries().Get(api.InlineCertificate, certificateName, nil)
		require.Error(r, err)
		require.EqualError(r, err, `Unexpected response code: 404 (Config entry not found for "inline-certificate" / "certificate")`)
	})
}
