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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
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
