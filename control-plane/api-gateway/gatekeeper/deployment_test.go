// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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
func TestAdditionalAccessLogVolumeMount(t *testing.T) {
	t.Parallel()

	type expectedResponse struct {
		hasProxyDefaults   bool
		errOnProxyDefaults bool
		accessLogEnabled   bool
		fileLogSinkType    bool
		fileLogPath        string
	}

	type testCase struct {
		expectedResponse     expectedResponse
		proxyDefaultResource *v1alpha1.ProxyDefaults
	}

	cases := map[string]testCase{
		"no proxy-defaults configured": {
			proxyDefaultResource: nil,
			expectedResponse: expectedResponse{
				hasProxyDefaults: false,
			},
		},
		"error fetching proxy-defaults": {
			proxyDefaultResource: nil,
			expectedResponse: expectedResponse{
				errOnProxyDefaults: true,
			},
		},
		"access-logs disabled in proxy-defaults": {
			proxyDefaultResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
					AccessLogs: &v1alpha1.AccessLogs{
						Enabled: false,
					},
				},
			},
			expectedResponse: expectedResponse{
				hasProxyDefaults: true,
				accessLogEnabled: false,
			},
		},
		"access-logs enabled but sink is not file type": {
			proxyDefaultResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
					AccessLogs: &v1alpha1.AccessLogs{
						Enabled: true,
						Type:    v1alpha1.DefaultLogSinkType,
					},
				},
			},
			expectedResponse: expectedResponse{
				hasProxyDefaults: true,
				accessLogEnabled: true,
				fileLogSinkType:  false,
			},
		},
		"file-type access-log enabled with path": {
			proxyDefaultResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
					AccessLogs: &v1alpha1.AccessLogs{
						Enabled: true,
						Type:    v1alpha1.FileLogSinkType,
						Path:    "/var/log/envoy/access.log",
					},
				},
			},
			expectedResponse: expectedResponse{
				hasProxyDefaults: true,
				accessLogEnabled: true,
				fileLogSinkType:  true,
				fileLogPath:      "/var/log/envoy/access.log",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			var fakeClient client.WithWatch
			s := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(s))
			if tc.proxyDefaultResource != nil {
				fakeClient = fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tc.proxyDefaultResource).Build()
			} else if tc.expectedResponse.errOnProxyDefaults {
				fakeClient = fake.NewClientBuilder().Build()
			} else {
				fakeClient = fake.NewClientBuilder().WithScheme(s).Build()
			}

			g := &Gatekeeper{
				Log:    logr.Discard(),
				Client: fakeClient,
			}

			initialvolume := []corev1.Volume{}
			initialmount := []corev1.VolumeMount{}

			updatedVolumes, updatedMounts, err := g.additionalAccessLogVolumeMount(ctx, initialvolume, initialmount)
			if tc.expectedResponse.errOnProxyDefaults {
				require.Error(t, err)
				require.ErrorContains(t, err, "error fetching proxy-defaults")
				return
			}
			require.NoError(t, err)
			if tc.expectedResponse.hasProxyDefaults && tc.expectedResponse.accessLogEnabled && tc.expectedResponse.fileLogSinkType {
				// Expect volume and mount to be added
				require.Len(t, updatedVolumes, 1)
				require.Len(t, updatedMounts, 1)
				require.Equal(t, accessLogVolumeName, updatedVolumes[0].Name)
				require.Equal(t, accessLogVolumeName, updatedMounts[0].Name)
				require.Equal(t, filepath.Dir(tc.expectedResponse.fileLogPath), updatedMounts[0].MountPath)
			} else {
				// Expect no additional volume or mount to be added
				require.Len(t, updatedVolumes, 0)
				require.Len(t, updatedMounts, 0)
			}
		})
	}
}
