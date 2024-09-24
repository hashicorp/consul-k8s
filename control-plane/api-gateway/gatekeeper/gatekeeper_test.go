// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"fmt"
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	designatedOpenShiftUIDRange       = "1000700000/100000"
	designatedOpenShiftGIDRange       = "1000700000/100000"
	expectedOpenShiftInitContainerUID = 1000799999
	expectedOpenShiftInitContainerGID = 1000799999
)

var (
	createdAtLabelKey   = "gateway.consul.hashicorp.com/created"
	createdAtLabelValue = "101010"
	dataplaneImage      = "hashicorp/consul-dataplane"
	name                = "test"
	namespace           = "default"

	labels = map[string]string{
		"component":                              "api-gateway",
		"gateway.consul.hashicorp.com/name":      name,
		"gateway.consul.hashicorp.com/namespace": namespace,
		createdAtLabelKey:                        createdAtLabelValue,
		"gateway.consul.hashicorp.com/managed":   "true",
	}

	// These annotations are used for testing that annotations stay on the service after reconcile.
	copyAnnotationKey = "copy-this-annotation"
	copyAnnotations   = map[string]string{
		copyAnnotationKey: "copy-this-annotation-value",
	}
	externalAnnotations = map[string]string{
		"external-annotation": "external-annotation-value",
	}
	externalAndCopyAnnotations = map[string]string{
		"external-annotation": "external-annotation-value",
		copyAnnotationKey:     "copy-this-annotation-value",
	}

	listeners = []gwv1beta1.Listener{
		{
			Name:     "Listener 1",
			Port:     8080,
			Protocol: "TCP",
			Hostname: common.PointerTo(gwv1beta1.Hostname("example.com")),
		},
		{
			Name:     "Listener 2",
			Port:     8081,
			Protocol: "TCP",
		},
		{
			Name:     "Listener 3",
			Port:     8080,
			Protocol: "TCP",
			Hostname: common.PointerTo(gwv1beta1.Hostname("example.net")),
		},
	}
)

type testCase struct {
	gateway            gwv1beta1.Gateway
	gatewayClassConfig v1alpha1.GatewayClassConfig
	helmConfig         common.HelmConfig

	initialResources resources
	finalResources   resources

	// This is used to ignore the timestamp on the service when comparing the final resources
	// This is useful for testing an update on a service
	ignoreTimestampOnService bool
}

type resources struct {
	deployments     []*appsv1.Deployment
	namespaces      []*corev1.Namespace
	roles           []*rbac.Role
	roleBindings    []*rbac.RoleBinding
	secrets         []*corev1.Secret
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
				InitContainerResources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    requireQuantity(t, "100m"),
						corev1.ResourceMemory: requireQuantity(t, "2Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    requireQuantity(t, "100m"),
						corev1.ResourceMemory: requireQuantity(t, "2Gi"),
					},
				},
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				secrets:         []*corev1.Secret{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway with service and map privileged ports correctly": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "Listener 1",
							Port:     80,
							Protocol: "TCP",
						},
						{
							Name:     "Listener 2",
							Port:     8080,
							Protocol: "TCP",
						},
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
					CopyAnnotations:             v1alpha1.CopyAnnotationsSpec{},
					ServiceType:                 (*corev1.ServiceType)(common.PointerTo("NodePort")),
					MapPrivilegedContainerPorts: 2000,
				},
			},
			helmConfig: common.HelmConfig{
				ImageDataplane:   dataplaneImage,
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "my-secret"}},
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       80,
							TargetPort: intstr.FromInt(2080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					}, "1", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", []corev1.LocalObjectReference{{Name: "my-secret"}}),
				},
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "1", false, false),
				},
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
				AuthMethod:       "method",
				ImageDataplane:   dataplaneImage,
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "my-secret"}},
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "1", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", []corev1.LocalObjectReference{{Name: "my-secret"}}),
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 5, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				secrets:         []*corev1.Secret{},
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 2, nil, nil, "", "1"),
				},
				roles:           []*rbac.Role{},
				secrets:         []*corev1.Secret{},
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
				AuthMethod:     "method",
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:     "Listener 1",
							Protocol: "TCP",
							Port:     8080,
						},
					}, "1", true, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "2"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "2", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			ignoreTimestampOnService: true,
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
				AuthMethod:     "method",
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
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
					}, "1", true, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "2"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
				},
				services: []*corev1.Service{
					configureService(name, namespace, labels, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
					}, "2", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			ignoreTimestampOnService: true,
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
				secrets:         []*corev1.Secret{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"updating a gateway deployment respects the labels and annotations a user has set": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Namespace:   namespace,
					Annotations: copyAnnotations,
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
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{Service: []string{copyAnnotationKey}},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				services: []*corev1.Service{
					configureService(name, namespace, labels, externalAnnotations, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "1", true, false),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{},
				roles:       []*rbac.Role{},
				secrets:     []*corev1.Secret{},
				services: []*corev1.Service{
					configureService(name, namespace, labels, externalAndCopyAnnotations, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "2", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
			ignoreTimestampOnService: true,
		},
		"updating a gateway that has copy-annotations and labels doesn't panic if another controller has removed them all": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        name,
					Namespace:   namespace,
					Annotations: copyAnnotations,
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
					CopyAnnotations: v1alpha1.CopyAnnotationsSpec{Service: []string{copyAnnotationKey}},
					ServiceType:     (*corev1.ServiceType)(common.PointerTo("NodePort")),
				},
			},
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				services: []*corev1.Service{
					configureService(name, namespace, nil, nil, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "1", true, false),
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{},
				roles:       []*rbac.Role{},
				secrets:     []*corev1.Secret{},
				services: []*corev1.Service{
					configureService(name, namespace, labels, copyAnnotations, (corev1.ServiceType)("NodePort"), []corev1.ServicePort{
						{
							Name:       "Listener 1",
							Protocol:   "TCP",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "Listener 2",
							Protocol:   "TCP",
							Port:       8081,
							TargetPort: intstr.FromInt(8081),
						},
					}, "2", false, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
			ignoreTimestampOnService: true,
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
				secrets:         []*corev1.Secret{},
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
				secrets:         []*corev1.Secret{},
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
				secrets:         []*corev1.Secret{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway with openshift enabled": {
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
					CopyAnnotations:  v1alpha1.CopyAnnotationsSpec{},
					OpenshiftSCCName: "test-api-gateway",
				},
			},
			helmConfig: common.HelmConfig{
				EnableOpenShift: true,
				ImageDataplane:  "hashicorp/consul-dataplane",
			},
			initialResources: resources{
				namespaces: []*corev1.Namespace{
					{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Namespace",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "default",
							Annotations: map[string]string{
								constants.AnnotationOpenShiftUIDRange: designatedOpenShiftUIDRange,
								constants.AnnotationOpenShiftGroups:   designatedOpenShiftGIDRange,
							},
						},
					},
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", true),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets:  []*corev1.Secret{},
				services: []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
		},
		"create a new gateway with TLS certificate reference in the same namespace": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "Listener 1",
							Port:     443,
							Protocol: "TCP",
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{
										Namespace: common.PointerTo(gwv1beta1.Namespace(namespace)),
										Name:      "tls-cert",
									},
								},
							},
						},
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
					CopyAnnotations:  v1alpha1.CopyAnnotationsSpec{},
					OpenshiftSCCName: "test-api-gateway",
				},
			},
			helmConfig: common.HelmConfig{
				EnableOpenShift: false,
				ImageDataplane:  "hashicorp/consul-dataplane",
			},
			initialResources: resources{
				secrets: []*corev1.Secret{
					{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Secret",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tls-cert",
							Namespace: namespace,
						},
						Data: map[string][]byte{
							corev1.TLSCertKey:       []byte("cert"),
							corev1.TLSPrivateKeyKey: []byte("key"),
						},
						Type: corev1.SecretTypeTLS,
					},
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles:        []*rbac.Role{},
				roleBindings: []*rbac.RoleBinding{},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", map[string][]byte{
						"default_tls-cert_tls.crt": []byte("cert"),
						"default_tls-cert_tls.key": []byte("key"),
					}),
				},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"create a new gateway with TLS certificate reference in a different namespace": {
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "Listener 1",
							Port:     443,
							Protocol: "TCP",
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{
										Namespace: common.PointerTo(gwv1beta1.Namespace("non-default")),
										Name:      "tls-cert",
									},
								},
							},
						},
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
					CopyAnnotations:  v1alpha1.CopyAnnotationsSpec{},
					OpenshiftSCCName: "test-api-gateway",
				},
			},
			helmConfig: common.HelmConfig{
				EnableOpenShift: false,
				ImageDataplane:  "hashicorp/consul-dataplane",
			},
			initialResources: resources{
				namespaces: []*corev1.Namespace{
					{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Namespace",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "non-default",
						},
					},
				},
				secrets: []*corev1.Secret{
					{
						TypeMeta: metav1.TypeMeta{
							APIVersion: "v1",
							Kind:       "Secret",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "tls-cert",
							Namespace: "non-default",
						},
						Data: map[string][]byte{
							corev1.TLSCertKey:       []byte("cert"),
							corev1.TLSPrivateKeyKey: []byte("key"),
						},
						Type: corev1.SecretTypeTLS,
					},
				},
			},
			finalResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles:        []*rbac.Role{},
				roleBindings: []*rbac.RoleBinding{},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", map[string][]byte{
						"non-default_tls-cert_tls.crt": []byte("cert"),
						"non-default_tls-cert_tls.key": []byte("key"),
					}),
				},
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
			require.NoError(t, validateResourcesExist(t, client, tc.helmConfig, tc.finalResources, tc.ignoreTimestampOnService))
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
			helmConfig: common.HelmConfig{
				ImageDataplane: dataplaneImage,
			},
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
					}, "1", true, false),
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
				AuthMethod:     "method",
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
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
					}, "1", true, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			finalResources: resources{
				deployments:     []*appsv1.Deployment{},
				roles:           []*rbac.Role{},
				services:        []*corev1.Service{},
				serviceAccounts: []*corev1.ServiceAccount{},
			},
		},
		"delete a gateway deployment with a Secret": {
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
				AuthMethod:     "method",
				ImageDataplane: dataplaneImage,
			},
			initialResources: resources{
				deployments: []*appsv1.Deployment{
					configureDeployment(name, namespace, labels, 3, nil, nil, "", "1"),
				},
				roles: []*rbac.Role{
					configureRole(name, namespace, labels, "1", false),
				},
				roleBindings: []*rbac.RoleBinding{
					configureRoleBinding(name, namespace, labels, "1"),
				},
				secrets: []*corev1.Secret{
					configureSecret(name, namespace, labels, "1", nil),
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
					}, "1", true, false),
				},
				serviceAccounts: []*corev1.ServiceAccount{
					configureServiceAccount(name, namespace, labels, "1", nil),
				},
			},
			finalResources: resources{
				deployments:     []*appsv1.Deployment{},
				roles:           []*rbac.Role{},
				secrets:         []*corev1.Secret{},
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

			err := gatekeeper.Delete(context.Background(), tc.gateway)
			require.NoError(t, err)
			require.NoError(t, validateResourcesExist(t, client, tc.helmConfig, tc.finalResources, false))
			require.NoError(t, validateResourcesAreDeleted(t, client, tc.initialResources))
		})
	}
}

func joinResources(resources resources) (objs []client.Object) {
	for _, deployment := range resources.deployments {
		objs = append(objs, deployment)
	}

	for _, namespace := range resources.namespaces {
		objs = append(objs, namespace)
	}

	for _, role := range resources.roles {
		objs = append(objs, role)
	}

	for _, roleBinding := range resources.roleBindings {
		objs = append(objs, roleBinding)
	}

	for _, secret := range resources.secrets {
		objs = append(objs, secret)
	}

	for _, service := range resources.services {
		objs = append(objs, service)
	}

	for _, serviceAccount := range resources.serviceAccounts {
		objs = append(objs, serviceAccount)
	}

	return objs
}

func validateResourcesExist(t *testing.T, client client.Client, helmConfig common.HelmConfig, resources resources, ignoreTimestampOnService bool) error {
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
		require.Equal(t, expected.Spec.Template.ObjectMeta.Annotations, actual.Spec.Template.ObjectMeta.Annotations)
		require.Equal(t, expected.Spec.Template.ObjectMeta.Labels, actual.Spec.Template.Labels)

		// Ensure there is an init container
		hasInitContainer := false
		for _, container := range actual.Spec.Template.Spec.InitContainers {
			if container.Name == injectInitContainerName {
				hasInitContainer = true

				// If the Helm config specifies init container resources, verify they are set
				if helmConfig.InitContainerResources != nil {
					assert.Equal(t, helmConfig.InitContainerResources.Limits, container.Resources.Limits)
					assert.Equal(t, helmConfig.InitContainerResources.Requests, container.Resources.Requests)
				}

				require.NotNil(t, container.SecurityContext.RunAsUser)
				require.NotNil(t, container.SecurityContext.RunAsGroup)
				if helmConfig.EnableOpenShift {
					assert.EqualValues(t, *container.SecurityContext.RunAsUser, expectedOpenShiftInitContainerUID)
					assert.EqualValues(t, *container.SecurityContext.RunAsGroup, expectedOpenShiftInitContainerGID)
				} else {
					assert.EqualValues(t, *container.SecurityContext.RunAsUser, initContainersUserAndGroupID)
					assert.EqualValues(t, *container.SecurityContext.RunAsGroup, initContainersUserAndGroupID)
				}
			}
		}
		assert.True(t, hasInitContainer)

		// Ensure there is a consul-dataplane container dropping ALL capabilities, adding
		// back the NET_BIND_SERVICE capability, and establishing a read-only root filesystem
		hasDataplaneContainer := false
		for _, container := range actual.Spec.Template.Spec.Containers {
			if container.Image == dataplaneImage {
				hasDataplaneContainer = true
				require.NotNil(t, container.SecurityContext)
				require.NotNil(t, container.SecurityContext.Capabilities)
				require.NotNil(t, container.SecurityContext.ReadOnlyRootFilesystem)
				assert.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
				assert.Equal(t, []corev1.Capability{netBindCapability}, container.SecurityContext.Capabilities.Add)
				assert.Equal(t, []corev1.Capability{allCapabilities}, container.SecurityContext.Capabilities.Drop)
			}
		}
		assert.True(t, hasDataplaneContainer)
	}

	for _, namespace := range resources.namespaces {
		actual := &corev1.Namespace{}
		err := client.Get(context.Background(), types.NamespacedName{Name: namespace.Name}, actual)
		if err != nil {
			return err
		}

		// Patch the createdAt label
		actual.Labels[createdAtLabelKey] = createdAtLabelValue

		require.Equal(t, namespace, actual)
	}

	for _, expected := range resources.secrets {
		actual := &corev1.Secret{}
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

		if ignoreTimestampOnService {
			expected.CreationTimestamp = actual.CreationTimestamp
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
						constants.AnnotationInject:                   "false",
						constants.AnnotationGatewayConsulServiceName: name,
						constants.AnnotationGatewayKind:              "api-gateway",
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

func configureSecret(name, namespace string, labels map[string]string, resourceVersion string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
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
		Data: data,
	}
}

func configureRole(name, namespace string, labels map[string]string, resourceVersion string, openshiftEnabled bool) *rbac.Role {
	rules := []rbac.PolicyRule{}

	if openshiftEnabled {
		rules = []rbac.PolicyRule{
			{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{name + "-api-gateway"},
				Verbs:         []string{"use"},
			},
		}
	}
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
		Rules: rules,
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

func configureService(name, namespace string, labels, annotations map[string]string, serviceType corev1.ServiceType, ports []corev1.ServicePort, resourceVersion string, isInitialResource, addExternalLabel bool) *corev1.Service {

	// This is used only to test that any external labels added to the service
	// are not removed on reconcile
	combinedLabels := labels
	if addExternalLabel {
		combinedLabels["extra-label"] = "extra-label-value"
	}

	service := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       namespace,
			Labels:          combinedLabels,
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

	if isInitialResource {
		service.ObjectMeta.CreationTimestamp = metav1.Now()
	}

	return &service
}

func configureServiceAccount(name, namespace string, labels map[string]string, resourceVersion string, pullSecrets []corev1.LocalObjectReference) *corev1.ServiceAccount {
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
		ImagePullSecrets: pullSecrets,
	}
}

func requireQuantity(t *testing.T, v string) resource.Quantity {
	quantity, err := resource.ParseQuantity(v)
	require.NoError(t, err)
	return quantity
}
