// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"testing"
	"time"

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
		"global.logLevel":              "trace",
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

	consulCluster.Create(t)

	k8sClient := ctx.ControllerRuntimeClient(t)
	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	defaultNamespace := "default"

	// create a service to target
	targetName := "static-server"
	logger.Log(t, "creating target server")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

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
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway class configs")
		k8sClient.DeleteAllOf(context.Background(), &v1alpha1.GatewayClassConfig{})
	})

	gatewayParametersRef := &gwv1beta1.ParametersReference{
		Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
		Kind:  gwv1beta1.Kind(v1alpha1.GatewayClassConfigKind),
		Name:  gatewayClassConfigName,
	}

	// create three gateway classes, two we control, one we don't
	controlledGatewayClassOneName := "controlled-gateway-class-one"
	logger.Log(t, "creating controlled gateway class one")
	createGatewayClass(t, k8sClient, controlledGatewayClassOneName, gatewayClassControllerName, gatewayParametersRef)

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateway classes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.GatewayClass{})
	})

	controlledGatewayClassTwoName := "controlled-gateway-class-two"
	logger.Log(t, "creating controlled gateway class two")
	createGatewayClass(t, k8sClient, controlledGatewayClassTwoName, gatewayClassControllerName, gatewayParametersRef)

	uncontrolledGatewayClassName := "uncontrolled-gateway-class"
	logger.Log(t, "creating uncontrolled gateway class")
	createGatewayClass(t, k8sClient, uncontrolledGatewayClassName, "example.com/some-controller", nil)

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
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8sClient.Delete(context.Background(), certificate)
	})

	// Create three gateways with a basic HTTPS listener to correspond to the three classes
	controlledGatewayOneName := "controlled-gateway-one"
	logger.Log(t, "creating controlled gateway one")
	controlledGatewayOne := createGateway(t, k8sClient, controlledGatewayOneName, defaultNamespace, controlledGatewayClassOneName, certificateName)

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all gateways")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.Gateway{}, client.InNamespace(defaultNamespace))
	})

	controlledGatewayTwoName := "controlled-gateway-two"
	logger.Log(t, "creating controlled gateway two")
	controlledGatewayTwo := createGateway(t, k8sClient, controlledGatewayTwoName, defaultNamespace, controlledGatewayClassTwoName, certificateName)

	uncontrolledGatewayName := "uncontrolled-gateway"
	logger.Log(t, "creating uncontrolled gateway")
	_ = createGateway(t, k8sClient, uncontrolledGatewayName, defaultNamespace, uncontrolledGatewayClassName, certificateName)

	// create two http routes associated with the first controlled gateway
	routeOneName := "route-one"
	logger.Log(t, "creating route one")
	routeOne := createRoute(t, k8sClient, routeOneName, defaultNamespace, controlledGatewayOneName, targetName)

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		logger.Log(t, "deleting all http routes")
		k8sClient.DeleteAllOf(context.Background(), &gwv1beta1.HTTPRoute{}, client.InNamespace(defaultNamespace))
	})

	routeTwoName := "route-two"
	logger.Log(t, "creating route two")
	routeTwo := createRoute(t, k8sClient, routeTwoName, defaultNamespace, controlledGatewayTwoName, targetName)

	// Scenario: Ensure ACL roles/policies are set correctly
	logger.Log(t, "checking that ACL roles/policies are set correctly for controlled gateway one")
	checkACLRolesPolicies(t, consulClient, controlledGatewayOneName)

	logger.Log(t, "checking that ACL roles/policies are set correctly for controlled gateway two")
	checkACLRolesPolicies(t, consulClient, controlledGatewayTwoName)

	// Scenario: Swapping a route to another controlled gateway should clean up the old parent statuses and references on Consul resources

	// check that the route is bound properly and objects are reflected in Consul
	logger.Log(t, "checking that http route one is bound to gateway one")
	checkRouteBound(t, k8sClient, routeOneName, defaultNamespace, controlledGatewayOneName)

	logger.Log(t, "checking that http route one is synchronized to Consul")
	checkConsulRouteParent(t, consulClient, routeOneName, controlledGatewayOneName)

	// update the route to point to the other controlled gateway
	logger.Log(t, "updating route one to be bound to gateway two")
	updateKubernetes(t, k8sClient, routeOne, func(r *gwv1beta1.HTTPRoute) {
		r.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(controlledGatewayTwoName)
	})

	// check that the route is bound properly and objects are reflected in Consul
	logger.Log(t, "checking that http route one is bound to gateway two")
	checkRouteBound(t, k8sClient, routeOneName, defaultNamespace, controlledGatewayTwoName)

	logger.Log(t, "checking that http route one is synchronized to Consul")
	checkConsulRouteParent(t, consulClient, routeOneName, controlledGatewayTwoName)

	// Scenario: Binding a route to a controlled gateway and then associating it with another gateway we don’t control should clean up Consul resources, route statuses, and finalizers
	// check that the route is bound properly and objects are reflected in Consul

	// check that our second http route is bound properly
	logger.Log(t, "checking that http route two is bound to gateway two")
	checkRouteBound(t, k8sClient, routeTwoName, defaultNamespace, controlledGatewayTwoName)

	logger.Log(t, "checking that http route two is synchronized to Consul")
	checkConsulRouteParent(t, consulClient, routeTwoName, controlledGatewayTwoName)

	// update the route to point to the uncontrolled gateway
	logger.Log(t, "updating route two to be bound to an uncontrolled gateway")
	updateKubernetes(t, k8sClient, routeTwo, func(r *gwv1beta1.HTTPRoute) {
		r.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(uncontrolledGatewayName)
	})

	// check that the route is unbound and all Consul objects and Kubernetes statuses are cleaned up
	logger.Log(t, "checking that http route two is cleaned up because we no longer control it")
	checkEmptyRoute(t, k8sClient, routeTwoName, defaultNamespace)

	logger.Log(t, "checking that http route two is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.HTTPRoute, routeTwoName)

	// Scenario: Switching a controlled gateway’s protocol that causes a route to unbind should cause the route to drop the parent ref in Consul and result in proper statuses set in Kubernetes

	// swap the gateway's protocol and see the route unbind
	logger.Log(t, "marking gateway two as using TCP")
	updateKubernetes(t, k8sClient, controlledGatewayTwo, func(g *gwv1beta1.Gateway) {
		g.Spec.Listeners[0].Protocol = gwv1beta1.TCPProtocolType
	})

	// check that the route is unbound and all Consul objects and Kubernetes statuses are cleaned up
	logger.Log(t, "checking that http route one is not bound to gateway two")
	retryCheck(t, 60, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: routeOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, controlledGatewayTwoName, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, falseCondition("Accepted", "NotAllowedByListeners"))
	})

	logger.Log(t, "checking that route one is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.HTTPRoute, routeOneName)

	// Scenario: Deleting a gateway should result in routes only referencing it to get cleaned up from Consul and their statuses/finalizers cleared, but routes referencing another controlled gateway should still exist in Consul and only have their statuses cleaned up from referencing the gateway we previously controlled. Any referenced certificates should also get cleaned up.
	// and acl roles and policies for that gateway should be cleaned up

	// delete gateway two
	logger.Log(t, "deleting gateway two in Kubernetes")
	err = k8sClient.Delete(context.Background(), controlledGatewayTwo)
	require.NoError(t, err)

	// check that the gateway is deleted from Consul
	logger.Log(t, "checking that gateway two is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.APIGateway, controlledGatewayTwoName)

	// check that the Kubernetes route is cleaned up and the entries deleted from Consul
	logger.Log(t, "checking that http route one is cleaned up in Kubernetes")
	checkEmptyRoute(t, k8sClient, routeOneName, defaultNamespace)

	logger.Log(t, "checking that ACL roles/policies are set correctly for controlled gateway one")
	checkACLRolesPolicies(t, consulClient, controlledGatewayOneName)

	logger.Log(t, "checking that ACL roles/policies are removed for controlled gateway two")
	checkACLRolesPoliciesDontExist(t, consulClient, controlledGatewayTwoName)

	// Scenario: Changing a gateway class name on a gateway to something we don’t control should have the same affect as deleting it with the addition of cleaning up our finalizer from the gateway.

	// reset route one to point to our first gateway and check that it's bound properly
	logger.Log(t, "remarking route one as bound to gateway one")
	updateKubernetes(t, k8sClient, routeOne, func(r *gwv1beta1.HTTPRoute) {
		r.Spec.ParentRefs[0].Name = gwv1beta1.ObjectName(controlledGatewayOneName)
	})

	logger.Log(t, "checking that http route one is bound to gateway one")
	checkRouteBound(t, k8sClient, routeOneName, defaultNamespace, controlledGatewayOneName)

	logger.Log(t, "checking that http route one is synchronized to Consul")
	checkConsulRouteParent(t, consulClient, routeOneName, controlledGatewayOneName)

	// make the gateway uncontrolled by pointing to a non-existent gateway class
	logger.Log(t, "marking gateway one as not controlled by our controller")
	updateKubernetes(t, k8sClient, controlledGatewayOne, func(g *gwv1beta1.Gateway) {
		g.Spec.GatewayClassName = "non-existent"
	})

	// check that the Kubernetes gateway is cleaned up
	logger.Log(t, "checking that gateway one is cleaned up in Kubernetes")
	retryCheck(t, 60, func(r *retry.R) {
		var route gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: controlledGatewayOneName, Namespace: defaultNamespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Finalizers, 0)
	})

	// check that the gateway is deleted from Consul
	logger.Log(t, "checking that gateway one is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.APIGateway, controlledGatewayOneName)

	// check that the Kubernetes route is cleaned up and the entries deleted from Consul
	logger.Log(t, "checking that http route one is cleaned up in Kubernetes")
	checkEmptyRoute(t, k8sClient, routeOneName, defaultNamespace)

	logger.Log(t, "checking that http route one is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.HTTPRoute, routeOneName)

	// Scenario: Deleting a certificate referenced by a gateway’s listener should make the listener invalid and drop it from Consul.

	// reset the gateway
	logger.Log(t, "remarking gateway one as controlled by our controller")
	updateKubernetes(t, k8sClient, controlledGatewayOne, func(g *gwv1beta1.Gateway) {
		g.Spec.GatewayClassName = gwv1beta1.ObjectName(controlledGatewayClassOneName)
	})

	// make sure it exists
	logger.Log(t, "checking that gateway one is synchronized to Consul")
	checkConsulExists(t, consulClient, api.APIGateway, controlledGatewayOneName)

	// make sure our certificate exists
	logger.Log(t, "checking that the certificate is synchronized to Consul")
	checkConsulExists(t, consulClient, api.FileSystemCertificate, certificateName)

	// delete the certificate in Kubernetes
	logger.Log(t, "deleting the certificate in Kubernetes")
	err = k8sClient.Delete(context.Background(), certificate)
	require.NoError(t, err)

	// make sure the certificate no longer exists in Consul
	logger.Log(t, "checking that the certificate is deleted from Consul")
	checkConsulNotExists(t, consulClient, api.FileSystemCertificate, certificateName)
}

func checkConsulNotExists(t *testing.T, client *api.Client, kind, name string, namespace ...string) {
	t.Helper()

	opts := &api.QueryOptions{}
	if len(namespace) != 0 {
		opts.Namespace = namespace[0]
	}

	retryCheck(t, 60, func(r *retry.R) {
		_, _, err := client.ConfigEntries().Get(kind, name, opts)
		require.Error(r, err)
		require.EqualError(r, err, fmt.Sprintf("Unexpected response code: 404 (Config entry not found for %q / %q)", kind, name))
	})
}

func checkConsulExists(t *testing.T, client *api.Client, kind, name string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		_, _, err := client.ConfigEntries().Get(kind, name, nil)
		require.NoError(r, err)
	})
}

func checkConsulRouteParent(t *testing.T, client *api.Client, name, parent string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		entry, _, err := client.ConfigEntries().Get(api.HTTPRoute, name, nil)
		require.NoError(r, err)
		route := entry.(*api.HTTPRouteConfigEntry)

		require.Len(r, route.Parents, 1)
		require.Equal(r, parent, route.Parents[0].Name)
	})
}

func checkEmptyRoute(t *testing.T, client client.Client, name, namespace string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 0)
		require.Len(r, route.Finalizers, 0)
	})
}

func checkRouteBound(t *testing.T, client client.Client, name, namespace, parent string) {
	t.Helper()

	retryCheck(t, 60, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := client.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &route)
		require.NoError(r, err)

		require.Len(r, route.Status.Parents, 1)
		require.EqualValues(r, gatewayClassControllerName, route.Status.Parents[0].ControllerName)
		require.EqualValues(r, parent, route.Status.Parents[0].ParentRef.Name)
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Synced", "Synced"))
	})
}

func updateKubernetes[T client.Object](t *testing.T, k8sClient client.Client, o T, fn func(o T)) {
	t.Helper()
	maxRetries := 20
	retryCount := 0
	sleepTime := 1 * time.Minute
	for {
		if retryCount > maxRetries {
			require.NoError(t, fmt.Errorf("max retries exceeded"))
		}
		retryCount++

		logger.Log(t, fmt.Sprintf("updateKubernetes loop executing for %d time", retryCount))

		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(o), o)
		if err != nil {
			logger.Log(t, "k8s client get failed with error: %v", err)
			time.Sleep(sleepTime)
			continue
		}

		fn(o)

		err = k8sClient.Update(context.Background(), o)
		if err != nil {
			logger.Log(t, "k8s client update failed with error: %v", err)
			time.Sleep(sleepTime)
			continue
		}

		break
	}
}

func createRoute(t *testing.T, client client.Client, name, namespace, parent, target string) *gwv1beta1.HTTPRoute {
	t.Helper()

	route := &gwv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{Name: gwv1beta1.ObjectName(parent)},
				},
			},
			Rules: []gwv1beta1.HTTPRouteRule{
				{BackendRefs: []gwv1beta1.HTTPBackendRef{
					{BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{Name: gwv1beta1.ObjectName(target)},
					}},
				}},
			},
		},
	}

	err := client.Create(context.Background(), route)
	require.NoError(t, err)
	return route
}

func createGateway(t *testing.T, client client.Client, name, namespace, gatewayClass, certificate string) *gwv1beta1.Gateway {
	t.Helper()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(gatewayClass),
			Listeners: []gwv1beta1.Listener{{
				Name:     gwv1beta1.SectionName("listener"),
				Protocol: gwv1beta1.HTTPSProtocolType,
				Port:     8443,
				TLS: &gwv1beta1.GatewayTLSConfig{
					CertificateRefs: []gwv1beta1.SecretObjectReference{{
						Name: gwv1beta1.ObjectName(certificate),
					}},
				},
			}},
		},
	}

	err := client.Create(context.Background(), gateway)
	require.NoError(t, err)

	return gateway
}

func createGatewayClass(t *testing.T, client client.Client, name, controllerName string, parameters *gwv1beta1.ParametersReference) {
	t.Helper()

	gatewayClass := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gwv1beta1.GatewayController(controllerName),
			ParametersRef:  parameters,
		},
	}

	err := client.Create(context.Background(), gatewayClass)
	require.NoError(t, err)
}

func checkACLRolesPolicies(t *testing.T, client *api.Client, gatewayName string) {
	t.Helper()
	retryCheck(t, 60, func(r *retry.R) {
		role, _, err := client.ACL().RoleReadByName(fmt.Sprint("managed-gateway-acl-role-", gatewayName), nil)
		require.NoError(r, err)
		require.NotNil(r, role)
		policy, _, err := client.ACL().PolicyReadByName(fmt.Sprint("api-gateway-policy-for-", gatewayName), nil)
		require.NoError(r, err)
		require.NotNil(r, policy)
	})
}

func checkACLRolesPoliciesDontExist(t *testing.T, client *api.Client, gatewayName string) {
	t.Helper()
	retryCheck(t, 60, func(r *retry.R) {
		role, _, err := client.ACL().RoleReadByName(fmt.Sprint("managed-gateway-acl-role-", gatewayName), nil)
		require.NoError(r, err)
		require.Nil(r, role)
		policy, _, err := client.ACL().PolicyReadByName(fmt.Sprint("api-gateway-policy-for-", gatewayName), nil)
		require.NoError(r, err)
		require.Nil(r, policy)
	})
}
