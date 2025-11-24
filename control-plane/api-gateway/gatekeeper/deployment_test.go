// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	capi "github.com/hashicorp/consul/api"
)

type consulServerRespCfg struct {
	hasProxyDefaults   bool
	errOnProxyDefaults bool
	accessLogEnabled   bool
	fileLogSinkType    bool
	fileLogPath        string
}

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

func TestMergeDeployments_ProbePropagation(t *testing.T) {
	t.Parallel()

	log := logr.Discard()

	gcc := v1alpha1.GatewayClassConfig{}

	gateway := gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"httpGet": {
						"path": "/health",
						"port": 8080
					}
				}`,
			},
		},
	}

	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "consul-dataplane",
						},
					},
				},
			},
		},
	}

	merged := mergeDeployments(log, gcc, gateway, deployment, &appsv1.Deployment{})
	assert.NotNil(t, merged)

	// Verify probe was applied to the container
	assert.Len(t, merged.Spec.Template.Spec.Containers, 1)
	container := merged.Spec.Template.Spec.Containers[0]
	assert.NotNil(t, container.LivenessProbe)
	assert.NotNil(t, container.LivenessProbe.HTTPGet)
	assert.Equal(t, "/health", container.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, int32(8080), container.LivenessProbe.HTTPGet.Port.IntVal)

	// Now update Gateway with different probe (TCPSocket instead of HTTPGet)
	gatewayUpdated := gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"tcpSocket": {
						"port": 9090
					}
				}`,
			},
		},
	}

	mergedUpdated := mergeDeployments(log, gcc, gatewayUpdated, merged, &appsv1.Deployment{})
	assert.NotNil(t, mergedUpdated)

	// Verify the probe handler was replaced (TCPSocket instead of HTTPGet)
	containerUpdated := mergedUpdated.Spec.Template.Spec.Containers[0]
	assert.NotNil(t, containerUpdated.LivenessProbe)
	assert.NotNil(t, containerUpdated.LivenessProbe.TCPSocket)
	assert.Nil(t, containerUpdated.LivenessProbe.HTTPGet)
	assert.Equal(t, int32(9090), containerUpdated.LivenessProbe.TCPSocket.Port.IntVal)
}

func TestAdditionalAccessLogVolumeMount(t *testing.T) {
	t.Parallel()

	type testCase struct {
		srvResponseConfig consulServerRespCfg
	}

	cases := map[string]testCase{
		"no proxy-defaults configured": {
			srvResponseConfig: consulServerRespCfg{
				hasProxyDefaults: false,
			},
		},
		"error fetching proxy-defaults": {
			srvResponseConfig: consulServerRespCfg{
				errOnProxyDefaults: true,
			},
		},
		"access-logs disabled in proxy-defaults": {
			srvResponseConfig: consulServerRespCfg{
				hasProxyDefaults: true,
				accessLogEnabled: false,
			},
		},
		"access-logs enabled but sink is not file type": {
			srvResponseConfig: consulServerRespCfg{
				hasProxyDefaults: true,
				accessLogEnabled: true,
				fileLogSinkType:  false,
			},
		},
		"file-type access-log enabled with path": {
			srvResponseConfig: consulServerRespCfg{
				hasProxyDefaults: true,
				accessLogEnabled: true,
				fileLogSinkType:  true,
				fileLogPath:      "/var/log/envoy/access.log",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := fake.NewClientBuilder().Build()
			server, testClient := fakeConsulServer(t, tc.srvResponseConfig)
			defer server.Close()

			g := &Gatekeeper{
				Log:          logr.Discard(),
				Client:       client,
				ConsulConfig: testClient.Cfg,
			}

			initialvolume := []corev1.Volume{}
			initialmount := []corev1.VolumeMount{}

			updatedVolumes, updatedMounts, err := g.additionalAccessLogVolumeMount(initialvolume, initialmount)
			if tc.srvResponseConfig.errOnProxyDefaults {
				require.Error(t, err)
				require.ErrorContains(t, err, "error fetching global proxy-defaults")
				return
			}
			require.NoError(t, err)
			if tc.srvResponseConfig.hasProxyDefaults && tc.srvResponseConfig.accessLogEnabled && tc.srvResponseConfig.fileLogSinkType {
				// Expect volume and mount to be added
				require.Len(t, updatedVolumes, 1)
				require.Len(t, updatedMounts, 1)
				require.Equal(t, accessLogVolumeName, updatedVolumes[0].Name)
				require.Equal(t, accessLogVolumeName, updatedMounts[0].Name)
				require.Equal(t, filepath.Dir(tc.srvResponseConfig.fileLogPath), updatedMounts[0].MountPath)
			} else {
				// Expect no additional volume or mount to be added
				require.Len(t, updatedVolumes, 0)
				require.Len(t, updatedMounts, 0)
			}
		})
	}
}

func fakeConsulServer(t *testing.T, serverResponseConfig consulServerRespCfg) (*httptest.Server, *test.TestServerClient) {
	t.Helper()
	mux := buildMux(t, serverResponseConfig)
	consulServer := httptest.NewServer(mux)

	parsedURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	host := strings.Split(parsedURL.Host, ":")[0]

	port, err := strconv.Atoi(parsedURL.Port())
	require.NoError(t, err)

	cfg := &consul.Config{APIClientConfig: &capi.Config{Address: host}, HTTPPort: port}
	cfg.APIClientConfig.Address = consulServer.URL

	testClient := &test.TestServerClient{
		Cfg:     cfg,
		Watcher: test.MockConnMgrForIPAndPort(t, host, port, false),
	}

	return consulServer, testClient
}

func buildMux(t *testing.T, cfg consulServerRespCfg) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/config/proxy-defaults/"+capi.ProxyConfigGlobal, func(w http.ResponseWriter, r *http.Request) {
		if cfg.errOnProxyDefaults {
			w.WriteHeader(500)
			return
		}
		if !cfg.hasProxyDefaults {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
		accessLogType := capi.DefaultLogSinkType
		if cfg.fileLogSinkType {
			accessLogType = capi.FileLogSinkType
		}
		proxyDefaults := capi.ProxyConfigEntry{
			AccessLogs: &capi.AccessLogsConfig{
				Enabled: cfg.accessLogEnabled,
				Type:    accessLogType,
				Path:    cfg.fileLogPath,
			},
		}
		val, err := json.Marshal(proxyDefaults)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.Write(val)
	})

	return mux
}
