// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"context"
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	gw "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const controllerName = "consul.hashicorp.com/gateway-controller-consul"

type gatewayHarness struct {
	ctx       environment.TestContext
	k8s       client.Client
	consul    *api.Client
	namespace string
}

func newGatewayHarness(t *testing.T, secure bool) *gatewayHarness {
	t.Helper()
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": strconv.FormatBool(secure),
		"global.tls.enabled":           strconv.FormatBool(secure),
	}
	// On OpenShift the chart installs the consul.hashicorp.com Gateway CRDs and
	// enables the custom gateway controller from the OpenShift-specific value.
	if cfg.EnableOpenshift {
		helmValues["global.openshift.crds.consulapi.enabled"] = "true"
	} else {
		helmValues["global.crds.consulapi.enabled"] = "true"
	}
	cluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, helpers.RandomName())
	cluster.Create(t)
	consulClient, _ := cluster.SetupConsulClient(t, secure)
	// The shared client does not register the custom Gateway API types.
	options := ctx.KubectlOptions(t)
	configPath, err := options.GetConfigPath(t)
	require.NoError(t, err)
	restConfig, err := terratestk8s.LoadApiClientConfigE(configPath, options.ContextName)
	require.NoError(t, err)
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, gw.Install(scheme))
	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	require.NoError(t, err)
	return &gatewayHarness{ctx: ctx, k8s: k8sClient, consul: consulClient, namespace: options.Namespace}
}

func retryGateway(t *testing.T, f func(*retry.R)) {
	t.Helper()
	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, f)
}

func (h *gatewayHarness) create(t *testing.T, obj client.Object) {
	t.Helper()
	require.NoError(t, h.k8s.Create(context.Background(), obj))
	cfg := suite.Config()
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		require.NoError(t, client.IgnoreNotFound(h.k8s.Delete(context.Background(), obj)))
		// Finish finalizers before deleting parent classes or uninstalling Consul.
		retryGateway(t, func(r *retry.R) {
			copy := obj.DeepCopyObject().(client.Object)
			err := h.k8s.Get(context.Background(), client.ObjectKeyFromObject(obj), copy)
			require.True(r, apierrors.IsNotFound(err), "waiting for %T %s deletion: %v", obj, obj.GetName(), err)
		})
	})
}

func (h *gatewayHarness) createClass(t *testing.T, deployment v1alpha1.DeploymentSpec) (*v1alpha1.GatewayClassConfig, *gw.CustomGatewayClass) {
	t.Helper()
	gcc := &v1alpha1.GatewayClassConfig{ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName()}, Spec: v1alpha1.GatewayClassConfigSpec{ServiceType: ptr.To(corev1.ServiceTypeClusterIP), DeploymentSpec: deployment}}
	h.create(t, gcc)
	class := &gw.CustomGatewayClass{ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName()}, Spec: gw.GatewayClassSpec{ControllerName: controllerName, ParametersRef: &gw.ParametersReference{Group: gw.Group(v1alpha1.ConsulHashicorpGroup), Kind: gw.Kind(v1alpha1.GatewayClassConfigKind), Name: gcc.Name}}}
	h.create(t, class)
	retryGateway(t, func(r *retry.R) {
		require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(class), class))
		requireCondition(r, class.Status.Conditions, "Accepted", class.Generation)
	})
	return gcc, class
}

func (h *gatewayHarness) createGateway(t *testing.T, class *gw.CustomGatewayClass) *gw.Gateway {
	t.Helper()
	gateway := &gw.Gateway{ObjectMeta: metav1.ObjectMeta{Name: helpers.RandomName(), Namespace: h.namespace, Labels: map[string]string{"component": "api-gateway-consul"}}, Spec: gw.GatewaySpec{GatewayClassName: gw.ObjectName(class.Name), Listeners: []gw.Listener{{Name: "http", Port: 8080, Protocol: gw.HTTPProtocolType}}}}
	h.create(t, gateway)
	retryGateway(t, func(r *retry.R) {
		require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(gateway), gateway))
		requireCondition(r, gateway.Status.Conditions, "Accepted", gateway.Generation)
		requireCondition(r, gateway.Status.Conditions, "Programmed", gateway.Generation)
		_, _, err := h.consul.ConfigEntries().Get(api.APIGateway, gateway.Name, nil)
		require.NoError(r, err)
	})
	return gateway
}

func requireCondition(t require.TestingT, conditions []metav1.Condition, name string, generation int64) {
	for _, condition := range conditions {
		if condition.Type == name {
			require.Equal(t, metav1.ConditionTrue, condition.Status, "%s: %s", name, condition.Message)
			require.Equal(t, generation, condition.ObservedGeneration, "%s is stale", name)
			return
		}
	}
	require.Fail(t, "missing status condition", "%s", name)
}

func (h *gatewayHarness) checkInstances(t *testing.T, gateway *gw.Gateway, want int32) {
	t.Helper()
	retryGateway(t, func(r *retry.R) {
		var deployment appsv1.Deployment
		require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(gateway), &deployment))
		require.NotNil(r, deployment.Spec.Replicas)
		require.Equal(r, want, *deployment.Spec.Replicas)
		require.NotNil(r, deployment.Spec.Selector)
		require.NotEmpty(r, deployment.Spec.Selector.MatchLabels)
		var pods corev1.PodList
		require.NoError(r, h.k8s.List(context.Background(), &pods, client.InNamespace(gateway.Namespace), client.MatchingLabels(deployment.Spec.Selector.MatchLabels)))
		var ready int32
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					ready++
				}
			}
		}
		require.Equal(r, want, ready, "ready gateway pods")
		services, _, err := h.consul.Catalog().Service(gateway.Name, "", nil)
		require.NoError(r, err)
		ids := map[string]struct{}{}
		for _, service := range services {
			ids[service.ServiceID] = struct{}{}
		}
		require.Len(r, ids, int(want), "gateway registrations in Consul")
	})
}

func (h *gatewayHarness) scale(t *testing.T, gateway *gw.Gateway, replicas int32) {
	t.Helper()
	retryGateway(t, func(r *retry.R) {
		var deployment appsv1.Deployment
		require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(gateway), &deployment))
		deployment.Spec.Replicas = ptr.To(replicas)
		require.NoError(r, h.k8s.Update(context.Background(), &deployment))
	})
}

func (h *gatewayHarness) requireStableReplicas(t *testing.T, gateway *gw.Gateway, want int32) {
	t.Helper()
	// A single GET could pass before the controller reconciles.
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(5 * time.Second) {
		var deployment appsv1.Deployment
		require.NoError(t, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(gateway), &deployment))
		require.NotNil(t, deployment.Spec.Replicas)
		require.Equal(t, want, *deployment.Spec.Replicas, "controller changed manually set replicas")
	}
}
