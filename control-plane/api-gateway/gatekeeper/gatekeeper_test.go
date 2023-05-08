package gatekeeper

import (
	"context"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type testCase struct {
	gateway            gwv1beta1.Gateway
	gatewayClassConfig v1alpha1.GatewayClassConfig
	helmConfig         apigateway.HelmConfig

	initialResources resources
	finalResources   resources
}

type resources struct {
	deployments     []*appsv1.Deployment
	roles           []*rbac.Role
	services        []*corev1.Service
	serviceAccounts []*corev1.ServiceAccount
}

func TestUpsert(t *testing.T) {
	t.Parallel()

	var (
		name      = "test"
		namespace = "default"
		labels    = map[string]string{}
	)

	cases := map[string]testCase{
		"create a new gateway deployment": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
			},
			helmConfig: apigateway.HelmConfig{},

			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))
			require.NoError(t, rbac.AddToScheme(s))
			require.NoError(t, corev1.AddToScheme(s))
			require.NoError(t, appsv1.AddToScheme(s))

			log := logrtest.New(t)

			objs := append(joinResources(tc.initialResources), &tc.gateway, &tc.gatewayClassConfig)
			client := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

			gatekeeper := New(Config{
				Log:                log,
				Client:             client,
				Gateway:            tc.gateway,
				GatewayClassConfig: tc.gatewayClassConfig,
				HelmConfig:         tc.helmConfig,
			})

			err := gatekeeper.Upsert(context.Background())
			require.NoError(t, err)
			require.NoError(t, validateResourcesExist(t, client, tc.finalResources))
		})
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	cases := map[string]testCase{}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, gwv1beta1.Install(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			log := logrtest.New(t)

			objs := append(joinResources(tc.initialResources), &tc.gateway, &tc.gatewayClassConfig)
			client := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

			gatekeeper := New(Config{
				Log:                log,
				Client:             client,
				Gateway:            tc.gateway,
				GatewayClassConfig: tc.gatewayClassConfig,
				HelmConfig:         tc.helmConfig,
			})

			err := gatekeeper.Delete(context.Background())
			require.NoError(t, err)
			require.NoError(t, validateResourcesExist(t, client, tc.finalResources))
		})
	}
}

func joinResources(resources resources) (objs []client.Object) {
	return objs
}

func validateResourcesExist(t *testing.T, client client.Client, resources resources) error {
	for _, expected := range resources.deployments {
		fmt.Println(expected)

		actual := &appsv1.Deployment{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		fmt.Println(actual)
		fmt.Println(err)
		if err != nil {
			return err
		}
		require.Equal(t, expected, actual)
	}

	for _, expected := range resources.roles {
		actual := &rbac.Role{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if err != nil {
			return err
		}
		require.Equal(t, expected, actual)
	}

	for _, expected := range resources.services {
		actual := &corev1.Service{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if err != nil {
			return err
		}
		require.Equal(t, expected, actual)
	}

	for _, expected := range resources.serviceAccounts {
		actual := &corev1.ServiceAccount{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if err != nil {
			return err
		}
		require.Equal(t, expected, actual)
	}

	return nil
}

func configureDeployment(name, namespace string, labels map[string]string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{}
}
