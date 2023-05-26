// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbac "k8s.io/api/rbac/v1"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/cache"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
)

const (
	TestGatewayClassConfigName = "test-gateway-class-config"
	TestAnnotationConfigKey    = "api-gateway.consul.hashicorp.com/config"
	TestGatewayClassName       = "test-gateway-class"
	TestGatewayName            = "test-gateway"
	TestNamespace              = "test-namespace"
)

func stubConsulCache(t *testing.T) *cache.Cache {
	t.Helper()

	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/acl/policies":
			fmt.Fprintln(w, `[]`)
		case "/v1/acl/tokens":
			fmt.Fprintln(w, `[]`)
		case "/v1/config":
			fmt.Fprintln(w, `[]`)
		case "/v1/catalog/services":
			fmt.Fprintln(w, `{}`)
		default:
			w.WriteHeader(500)
			fmt.Fprintln(w, "Mock Server not configured for this route: "+r.URL.Path)
		}
	}))
	t.Cleanup(consulServer.Close)

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)

	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(t, err)

	return cache.New(cache.Config{
		ConsulClientConfig: &consul.Config{
			APIClientConfig: &api.Config{},
			HTTPPort:        port,
			GRPCPort:        port,
			APITimeout:      0,
		},
		ConsulServerConnMgr: test.MockConnMgrForIPAndPort(serverURL.Hostname(), port),
		NamespacesEnabled:   false,
		Logger:              logrtest.New(t),
	})
}

func TestGatewayReconcileGatekeeperUpdates(t *testing.T) {
	t.Parallel()

	namespace := "test-namespace"
	name := "test-gateway"

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}

	basicGatewayClass, basicGatewayClassConfig := getBasicGatewayClassAndConfig()

	cases := map[string]struct {
		gateway       *gwv1beta1.Gateway
		k8sObjects    []runtime.Object
		expectedError error
	}{
		"successful update of gateway": {
			gateway: &gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayFinalizer},
					Annotations: map[string]string{
						TestAnnotationConfigKey: `{"serviceType":"serviceType"}`,
					},
				},
				Spec: gwv1beta1.GatewaySpec{
					GatewayClassName: TestGatewayClassName,
				},
			},
			k8sObjects: []runtime.Object{
				&basicGatewayClass,
				&basicGatewayClassConfig,
			},
			expectedError: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))
			require.NoError(t, gwv1alpha2.AddToScheme(s))
			require.NoError(t, rbac.AddToScheme(s))
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, appsv1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gateway != nil {
				objs = append(objs, tc.gateway)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()

			r := &GatewayController{
				cache:  stubConsulCache(t),
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			_, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedError, err)
			deployment := appsv1.Deployment{}
			r.Client.Get(context.TODO(), types.NamespacedName{
				Namespace: TestNamespace,
				Name:      TestGatewayName,
			}, &deployment)
			require.NotEmpty(t, deployment)
			require.Equal(t, TestGatewayName, deployment.ObjectMeta.Name)
		})
	}
}

func TestGatewayReconcileGatekeeperDeletes(t *testing.T) {
	t.Parallel()

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: TestNamespace,
			Name:      TestGatewayName,
		},
	}

	basicGatewayClass, basicGatewayClassConfig := getBasicGatewayClassAndConfig()
	cases := map[string]struct {
		gateway       *gwv1beta1.Gateway
		k8sObjects    []runtime.Object
		expectedError error
	}{
		"successful change of gatewayclass on gateway": {
			gateway: &gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  TestNamespace,
					Name:       TestGatewayName,
					Finalizers: []string{gatewayFinalizer},
					Annotations: map[string]string{
						TestAnnotationConfigKey: `{"serviceType":"serviceType"}`,
					},
				},
				Spec: gwv1beta1.GatewaySpec{
					GatewayClassName: TestGatewayClassName,
				},
			},
			k8sObjects: []runtime.Object{
				&basicGatewayClass,
				&basicGatewayClassConfig,
			},
			expectedError: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))
			require.NoError(t, gwv1alpha2.AddToScheme(s))
			require.NoError(t, rbac.AddToScheme(s))
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, appsv1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gateway != nil {
				objs = append(objs, tc.gateway)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()

			r := &GatewayController{
				cache:  stubConsulCache(t),
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			_, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedError, err)
			deployment := appsv1.Deployment{}
			r.Client.Get(context.TODO(), types.NamespacedName{
				Namespace: TestNamespace,
				Name:      TestGatewayName,
			}, &deployment)
			require.NotEmpty(t, deployment)
			require.Equal(t, TestGatewayName, deployment.ObjectMeta.Name)
		})
	}
}

func TestObjectsToRequests(t *testing.T) {
	t.Parallel()

	name := "test-gatewayclass"

	namespacedName := types.NamespacedName{
		Namespace: TestNamespace,
		Name:      name,
	}

	cases := map[string]struct {
		objects        []metav1.Object
		expectedResult []reconcile.Request
	}{
		"successful conversion of gateway to request": {
			objects: []metav1.Object{
				&gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: TestNamespace,
						Name:      name,
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: GatewayClassControllerName,
					},
				},
			},
			expectedResult: []reconcile.Request{
				{
					NamespacedName: namespacedName,
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requests := objectsToRequests(tc.objects)

			require.Equal(t, tc.expectedResult, requests)
		})
	}
}

func TestGatewayController_getAllRefsForGateway(t *testing.T) {
	t.Parallel()
	s := runtime.NewScheme()
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, gwv1alpha2.Install(s))
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "secret squirrel",
		},
	}
	gw := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-gw",
			Annotations: map[string]string{},
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: "",
			Listeners: []gwv1beta1.Listener{
				{
					Name: "l1",
					TLS: &gwv1beta1.GatewayTLSConfig{
						CertificateRefs: []gwv1beta1.SecretObjectReference{
							{
								Kind: pointerTo(gwv1beta1.Kind("Secret")),
								Name: "secret squirrel",
							},
						},
						Options: map[gwv1beta1.AnnotationKey]gwv1beta1.AnnotationValue{},
					},
					AllowedRoutes: &gwv1beta1.AllowedRoutes{},
				},
			},
			Addresses: []gwv1beta1.GatewayAddress{},
		},
		Status: gwv1beta1.GatewayStatus{},
	}
	gwc := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-gw-class",
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: "",
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: Group,
				Kind:  v1alpha1.GatewayClassConfigKind,
				Name:  "the config",
			},
			Description: new(string),
		},
	}
	gwcConfig := &v1alpha1.GatewayClassConfig{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "the config",
		},
		Spec: v1alpha1.GatewayClassConfigSpec{
			ServiceType: pointerTo(corev1.ServiceType("serviceType")),
			NodeSelector: map[string]string{
				"selector": "of node",
			},
			Tolerations: []v1.Toleration{
				{
					Key:               "key",
					Operator:          "op",
					Value:             "120",
					Effect:            "to the moon",
					TolerationSeconds: new(int64),
				},
			},
			CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
				Service: []string{"service"},
			},
		},
	}

	httpRouteOnGateway := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "route 1",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Name: gwv1beta1.ObjectName(gw.Name),
					},
				},
			},
		},
	}

	httpRouteNotOnGateway := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "route not on gateway",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Name: gwv1beta1.ObjectName("not on the gateway"),
					},
				},
			},
		},
		Status: gwv1beta1.HTTPRouteStatus{},
	}

	tcpRoute := &gwv1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp route",
		},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Name: gwv1beta1.ObjectName(gw.Name),
					},
				},
			},
		},
	}

	objs := []runtime.Object{gw, gwc, gwcConfig, httpRouteOnGateway, httpRouteNotOnGateway, tcpRoute, secret}

	fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()
	controller := GatewayController{
		Client: fakeClient,
	}

	ctx := context.Background()

	actual, err := controller.getAllRefsForGateway(ctx, gw)

	require.NoError(t, err)
	expectedEntries := []metav1.Object{httpRouteOnGateway, tcpRoute, secret}

	require.ElementsMatch(t, expectedEntries, actual)
}

func getBasicGatewayClassAndConfig() (gwv1beta1.GatewayClass, v1alpha1.GatewayClassConfig) {
	serviceType := corev1.ServiceType("NodePort")
	basicGatewayClass := gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "",
			Name:      TestGatewayClassName,
			Finalizers: []string{
				gatewayClassFinalizer,
			},
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: GatewayClassControllerName,
			ParametersRef: &gwv1beta1.ParametersReference{
				Group:     "consul.hashicorp.com",
				Kind:      "GatewayClassConfig",
				Name:      TestGatewayClassConfigName,
				Namespace: nil,
			},
		},
	}

	basicGatewayClassConfig := v1alpha1.GatewayClassConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestGatewayClassConfigName,
		},
		Spec: v1alpha1.GatewayClassConfigSpec{
			ServiceType: &serviceType,
			CopyAnnotations: v1alpha1.CopyAnnotationsSpec{
				Service: []string{"serviceType"},
			},
		},
	}

	return basicGatewayClass, basicGatewayClassConfig
}
