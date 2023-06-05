// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	common "github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

var (
	createdAtLabelKey   = "gateway.consul.hashicorp.com/created"
	createdAtLabelValue = "101010"
	name                = "test"
	namespace           = "default"
	labels              = map[string]string{
		"gateway.consul.hashicorp.com/name":      name,
		"gateway.consul.hashicorp.com/namespace": namespace,
		createdAtLabelKey:                        createdAtLabelValue,
		"gateway.consul.hashicorp.com/managed":   "true",
	}
	listeners = []gwv1beta1.Listener{
		{
			Name:     "Listener 1",
			Port:     8080,
			Protocol: "TCP",
		},
		{
			Name:     "Listener 2",
			Port:     8081,
			Protocol: "TCP",
		},
	}
)

type testCase struct {
	gateway            gwv1beta1.Gateway
	gatewayClassConfig v1alpha1.GatewayClassConfig
	helmConfig         common.HelmConfig

	initialResources resources
	finalResources   resources
}

type resources struct {
	deployments     []*appsv1.Deployment
	roles           []*rbac.Role
	roleBindings    []*rbac.RoleBinding
	services        []*corev1.Service
	serviceAccounts []*corev1.ServiceAccount
}

func TestUpsert(t *testing.T) {
	t.Parallel()

	cases := map[string]testCase{
		"create a new gateway deployment with only Deployment": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig:       common.HelmConfig{},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway deployment with managed Service": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig:       common.HelmConfig{},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway deployment with managed Service and ACLs": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				AuthMethod: "method",
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
		},
		"create a new gateway where the GatewayClassConfig has a default number of instances greater than the max on the GatewayClassConfig": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(8)),
						MaxInstances:     common.PointerTo(int32(5)),
						MinInstances:     common.PointerTo(int32(2)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig:       common.HelmConfig{},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 5, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway where the GatewayClassConfig has a default number of instances lesser than the min on the GatewayClassConfig": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(1)),
						MaxInstances:     common.PointerTo(int32(5)),
						MinInstances:     common.PointerTo(int32(2)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig:       common.HelmConfig{},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 2, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"update a gateway, adding a listener to a service": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				AuthMethod: "method",
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "2"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "2"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
		},
		"update a gateway, removing a listener from a service": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						listeners[0],
					},
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				AuthMethod: "method",
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "2"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
					}, "2"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
		},
		"updating a gateway deployment respects the number of replicas a user has set": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(5)),
						MaxInstances:     common.PointerTo(int32(7)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 5, nil, nil, "", "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 5, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"update a gateway deployment by scaling it when no min or max number of instances is defined on the GatewayClassConfig": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     nil,
						MinInstances:     nil,
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 8, nil, nil, "", "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 8, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"update a gateway deployment by scaling it lower than the min number of instances on the GatewayClassConfig": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(5)),
						MinInstances:     common.PointerTo(int32(2)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 1, nil, nil, "", "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 2, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"update a gateway deployment by scaling it higher than the max number of instances on the GatewayClassConfig": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(5)),
						MinInstances:     common.PointerTo(int32(2)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 10, nil, nil, "", "1"),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 5, nil, nil, "", "1"),
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

			gatekeeper := New(log, client)

			err := gatekeeper.Upsert(context.Background(), tc.gateway, tc.gatewayClassConfig, tc.helmConfig)
			require.NoError(t, err)
			require.NoError(t, validateResourcesExist(t, client, tc.finalResources))
		})
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	cases := map[string]testCase{
		"delete a gateway deployment with only Deployment": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
			},
			finalResources: resources{
				deployments:     []*appsv1.Deployment{},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"delete a gateway deployment with a managed Service": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
			finalResources: resources{
				deployments:     []*appsv1.Deployment{},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"delete a gateway deployment with managed Service and ACLs": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: listeners,
				},
			},
			gatewayClassConfig: v1alpha1.GatewayClassConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "consul-gatewayclassconfig",
				},
				Spec: v1alpha1.GatewayClassConfigSpec{
					DeploymentSpec: v1alpha1.DeploymentSpec{
						DefaultInstances: common.PointerTo(int32(3)),
						MaxInstances:     common.PointerTo(int32(3)),
						MinInstances:     common.PointerTo(int32(1)),
					},
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				AuthMethod: "method",
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1"),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
						{
							Name:     "Listener 2",
							Protocol: "TCP",
							Port:     8081,
						},
					}, "1"),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1"),
				},
			},
			finalResources: resources{
				deployments:     []*appsv1.Deployment{},
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

			gatekeeper := New(log, client)

			err := gatekeeper.Delete(context.Background(), types.NamespacedName{
				Namespace: tc.gateway.Namespace,
				Name:      tc.gateway.Name,
			})
			require.NoError(t, err)
			require.NoError(t, validateResourcesExist(t, client, tc.finalResources))
			require.NoError(t, validateResourcesAreDeleted(t, client, tc.initialResources))
		})
	}
}

func joinResources(resources resources) (objs []client.Object) {
	for _, deployment := range resources.deployments {
		objs = append(objs, deployment)
	}

	for _, role := range resources.roles {
		objs = append(objs, role)
	}

	for _, roleBinding := range resources.roleBindings {
		objs = append(objs, roleBinding)
	}

	for _, service := range resources.services {
		objs = append(objs, service)
	}

	for _, serviceAccount := range resources.serviceAccounts {
		objs = append(objs, serviceAccount)
	}

	return objs
}

func validateResourcesExist(t *testing.T, client client.Client, resources resources) error {
	t.Helper()

	for _, expected := range resources.deployments {
		actual := &appsv1.Deployment{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if err != nil {
			return err
		}

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue
		actual.Spec.Selector.MatchLabels[createdAtLabelKey] = createdAtLabelValue
		actual.Spec.Template.ObjectMeta.Labels[createdAtLabelKey] = createdAtLabelValue

		require.Equal(t, expected.Name, actual.Name)
		require.Equal(t, expected.Namespace, actual.Namespace)
		require.Equal(t, expected.APIVersion, actual.APIVersion)
		require.Equal(t, expected.Labels, actual.Labels)
		if expected.Spec.Replicas != nil {
			require.NotNil(t, actual.Spec.Replicas)
			require.EqualValues(t, *expected.Spec.Replicas, *actual.Spec.Replicas)
		}
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

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue

		require.Equal(t, expected, actual)
	}

	for _, expected := range resources.roleBindings {
		actual := &rbac.RoleBinding{}
		err := client.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if err != nil {
			return err
		}

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue

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

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue
		actual.Spec.Selector[createdAtLabelKey] = createdAtLabelValue

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

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue

		require.Equal(t, expected, actual)
	}

	return nil
}

func validateResourcesAreDeleted(t *testing.T, k8sClient client.Client, resources resources) error {
	t.Helper()

	for _, expected := range resources.deployments {
		actual := &appsv1.Deployment{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("expected deployment %s to be deleted", expected.Name)
		}
		require.Error(t, err)
	}

	for _, expected := range resources.roles {
		actual := &rbac.Role{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("expected role %s to be deleted", expected.Name)
		}
		require.Error(t, err)
	}

	for _, expected := range resources.roleBindings {
		actual := &rbac.RoleBinding{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("expected rolebinding %s to be deleted", expected.Name)
		}
		require.Error(t, err)
	}

	for _, expected := range resources.services {
		actual := &corev1.Service{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("expected service %s to be deleted", expected.Name)
		}
		require.Error(t, err)
	}

	for _, expected := range resources.serviceAccounts {
		actual := &corev1.ServiceAccount{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		}, actual)
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("expected service account %s to be deleted", expected.Name)
		}
		require.Error(t, err)
	}

	return nil
}

func configureDeployment(name, namespace string, labels map[string]string, replicas int32, nodeSelector map[string]string, tolerations []corev1.Toleration, serviceAccoutName, resourceVersion string) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			ResourceVersion: resourceVersion,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "gateway.networking.k8s.io/v1beta1",
					Kind:               "Gateway",
					Name:               name,
					Controller:         common.PointerTo(true),
					BlockOwnerDeletion: common.PointerTo(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"consul.hashicorp.com/connect-inject": "false",
					},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 1,
									PodAffinityTerm: corev1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: labels,
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
					NodeSelector:       nodeSelector,
					Tolerations:        tolerations,
					ServiceAccountName: serviceAccoutName,
				},
			},
		},
	}
}

func configureRole(name, namespace string, labels map[string]string, resourceVersion string) *rbac.Role {
	return &rbac.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			ResourceVersion: resourceVersion,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "gateway.networking.k8s.io/v1beta1",
					Kind:               "Gateway",
					Name:               name,
					Controller:         common.PointerTo(true),
					BlockOwnerDeletion: common.PointerTo(true),
				},
			},
		},
		Rules: []rbac.PolicyRule{},
	}
}

func configureRoleBinding(name, namespace string, labels map[string]string, resourceVersion string) *rbac.RoleBinding {
	return &rbac.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			ResourceVersion: resourceVersion,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "gateway.networking.k8s.io/v1beta1",
					Kind:               "Gateway",
					Name:               name,
					Controller:         common.PointerTo(true),
					BlockOwnerDeletion: common.PointerTo(true),
				},
			},
		},
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: namespace,
			},
		},
	}
}

func configureService(name, namespace string, labels, annotations map[string]string, serviceType corev1.ServiceType, ports []corev1.ServicePort, resourceVersion string) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			Annotations:     annotations,
			ResourceVersion: resourceVersion,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "gateway.networking.k8s.io/v1beta1",
					Kind:               "Gateway",
					Name:               name,
					Controller:         common.PointerTo(true),
					BlockOwnerDeletion: common.PointerTo(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Type:     serviceType,
			Ports:    ports,
		},
	}
}

func configureServiceAccount(name, namespace string, labels map[string]string, resourceVersion string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          labels,
			ResourceVersion: resourceVersion,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "gateway.networking.k8s.io/v1beta1",
					Kind:               "Gateway",
					Name:               name,
					Controller:         common.PointerTo(true),
					BlockOwnerDeletion: common.PointerTo(true),
				},
			},
		},
	}
}
