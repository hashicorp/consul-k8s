// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway-custom/common"
)

func Test_compareDeployments(t *testing.T) {
	testCases := []struct {
		name          string
		a, b          *appsv1.Deployment
		shouldBeEqual bool
	}{
		{
			name:          "zero-state deployments",
			a:             &appsv1.Deployment{},
			b:             &appsv1.Deployment{},
			shouldBeEqual: true,
		},
		{
			name: "different replicas",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: common.PointerTo(int32(1)),
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: common.PointerTo(int32(2)),
				},
			},
			shouldBeEqual: false,
		},
		{
			name: "same replicas",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: common.PointerTo(int32(1)),
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Replicas: common.PointerTo(int32(1)),
				},
			},
			shouldBeEqual: true,
		},
		{
			name: "different init container resources",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu":    requireQuantity(t, "111m"),
											"memory": requireQuantity(t, "111Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu":    requireQuantity(t, "222m"),
											"memory": requireQuantity(t, "111Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			shouldBeEqual: false,
		},
		{
			name: "same init container resources",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu":    requireQuantity(t, "111m"),
											"memory": requireQuantity(t, "111Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											"cpu":    requireQuantity(t, "111m"),
											"memory": requireQuantity(t, "111Mi"),
										},
									},
								},
							},
						},
					},
				},
			},
			shouldBeEqual: true,
		},
		{
			name: "different container ports",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Ports: []corev1.ContainerPort{
										{ContainerPort: 7070},
										{ContainerPort: 9090},
									},
								},
							},
						},
					},
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Ports: []corev1.ContainerPort{
										{ContainerPort: 8080},
										{ContainerPort: 9090},
									},
								},
							},
						},
					},
				},
			},
			shouldBeEqual: false,
		},
		{
			name: "same container ports",
			a: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Ports: []corev1.ContainerPort{
										{ContainerPort: 8080},
										{ContainerPort: 9090},
									},
								},
							},
						},
					},
				},
			},
			b: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Ports: []corev1.ContainerPort{
										{ContainerPort: 8080},
										{ContainerPort: 9090},
									},
								},
							},
						},
					},
				},
			},
			shouldBeEqual: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.shouldBeEqual {
				assert.True(t, compareDeployments(testCase.a, testCase.b), "expected deployments to be equal but they were not")
			} else {
				assert.False(t, compareDeployments(testCase.a, testCase.b), "expected deployments to be different but they were not")
			}
		})
	}
}

func TestCompareDeployments_ContainerArgs(t *testing.T) {
	a := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "consul-dataplane", Args: []string{"-envoy-concurrency=4"}}}}}}}
	b := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "consul-dataplane", Args: []string{"-envoy-concurrency=1"}}}}}}}
	assert.False(t, compareDeployments(a, b), "different container args should not be equal")
	assert.True(t, compareDeployments(a, a.DeepCopy()), "same container args should be equal")
}

// A changed managed arg (e.g. -envoy-concurrency) must replace the existing template.
func TestMergeDeployments_ArgsUpdate(t *testing.T) {
	existing := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "consul-dataplane", Args: []string{"-envoy-concurrency=1"}}}}}}}
	desired := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "consul-dataplane", Args: []string{"-envoy-concurrency=4"}}}}}}}

	merged := mergeDeployments(desired, existing)
	assert.Equal(t, []string{"-envoy-concurrency=4"}, merged.Spec.Template.Spec.Containers[0].Args)
}
