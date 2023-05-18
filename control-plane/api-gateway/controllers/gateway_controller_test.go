package controllers

import (
	"context"
	"testing"

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
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestGatewayReconciler(t *testing.T) {
	t.Parallel()

	namespace := "test-namespace"
	name := "test-gateway"
	gatewayClassName := gwv1beta1.ObjectName("test-gateway-class")

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}

	cases := map[string]struct {
		gateway            *gwv1beta1.Gateway
		k8sObjects         []runtime.Object
		expectedResult     ctrl.Result
		expectedError      error
		expectedFinalizers []string
		expectedIsDeleted  bool
		expectedConditions []metav1.Condition
	}{
		"successful reconcile with no change simple gateway": {
			gateway: &gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace,
					Name:       name,
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.GatewaySpec{
					GatewayClassName: gatewayClassName,
				},
			},
			k8sObjects: []runtime.Object{
				&gwv1beta1.GatewayClass{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "",
						Name:      string(gatewayClassName),
						Finalizers: []string{
							gatewayClassFinalizer,
						},
					},
					Spec: gwv1beta1.GatewayClassSpec{
						ControllerName: GatewayClassControllerName,
					},
				},
			},
			expectedResult:     ctrl.Result{},
			expectedError:      nil,
			expectedFinalizers: []string{gatewayFinalizer},
			expectedIsDeleted:  false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha2.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			objs := tc.k8sObjects
			if tc.gateway != nil {
				objs = append(objs, tc.gateway)
			}

			fakeClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...)).Build()

			r := &GatewayController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}
			result, err := r.Reconcile(context.Background(), req)

			require.Equal(t, tc.expectedResult, result)
			require.Equal(t, tc.expectedError, err)

			// TODO: Add back with real implementation of reconcile
			//// Check the GatewayClass after reconciliation.
			//g := &gwv1beta1.Gateway{}
			//err = r.Client.Get(context.Background(), req.NamespacedName, g)
			//
			//require.NoError(t, client.IgnoreNotFound(err))
			//require.Equal(t, tc.expectedFinalizers, g.ObjectMeta.Finalizers)
			//require.Equal(t, len(tc.expectedConditions), len(g.Status.Conditions), "expected %+v, got %+v", tc.expectedConditions, g.Status.Conditions)
			//for i, expectedCondition := range tc.expectedConditions {
			//	require.True(t, equalConditions(expectedCondition, g.Status.Conditions[i]), "expected %+v, got %+v", expectedCondition, g.Status.Conditions[i])
			//}
		})
	}
}

func TestObjectsToRequests(t *testing.T) {
	t.Parallel()

	namespace := "test-namespace"
	name := "test-gatewayclass"

	namespacedName := types.NamespacedName{
		Namespace: namespace,
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
						Namespace: namespace,
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
	require.NoError(t, v1alpha2.Install(s))
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

	tcpRoute := &v1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp route",
		},
		Spec: v1alpha2.TCPRouteSpec{
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
