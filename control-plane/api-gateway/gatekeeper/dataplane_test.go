// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestConsulDataplaneContainer_PrivilegedPorts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                        string
		gateway                     gwv1beta1.Gateway
		gcc                         v1alpha1.GatewayClassConfig
		expectedCommand             []string
		expectedHasEnvoyArg         bool
		expectedCapabilities        []corev1.Capability
		expectedPrivilegeEscalation bool
	}{
		{
			name: "privileged port with mapping disabled",
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name: "https",
							Port: 443, // Privileged port
						},
					},
				},
			},
			gcc: v1alpha1.GatewayClassConfig{
				Spec: v1alpha1.GatewayClassConfigSpec{
					MapPrivilegedContainerPorts: 0, // Mapping disabled
				},
			},
			expectedCommand:             []string{"privileged-consul-dataplane"},
			expectedHasEnvoyArg:         true,
			expectedCapabilities:        []corev1.Capability{"NET_BIND_SERVICE"},
			expectedPrivilegeEscalation: true,
		},
		{
			name: "privileged port with mapping enabled",
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name: "https",
							Port: 443, // Privileged port
						},
					},
				},
			},
			gcc: v1alpha1.GatewayClassConfig{
				Spec: v1alpha1.GatewayClassConfigSpec{
					MapPrivilegedContainerPorts: 8443, // Mapping enabled
				},
			},
			expectedCommand:             nil, // No custom command
			expectedHasEnvoyArg:         false,
			expectedCapabilities:        []corev1.Capability{},
			expectedPrivilegeEscalation: false,
		},
		{
			name: "non-privileged port",
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name: "http",
							Port: 8080, // Non-privileged port
						},
					},
				},
			},
			gcc: v1alpha1.GatewayClassConfig{
				Spec: v1alpha1.GatewayClassConfigSpec{
					MapPrivilegedContainerPorts: 0, // Mapping disabled
				},
			},
			expectedCommand:             nil, // No custom command
			expectedHasEnvoyArg:         false,
			expectedCapabilities:        []corev1.Capability{},
			expectedPrivilegeEscalation: false,
		},
		{
			name: "multiple listeners with one privileged",
			gateway: gwv1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: "default",
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name: "http",
							Port: 8080, // Non-privileged port
						},
						{
							Name: "https",
							Port: 443, // Privileged port
						},
					},
				},
			},
			gcc: v1alpha1.GatewayClassConfig{
				Spec: v1alpha1.GatewayClassConfigSpec{
					MapPrivilegedContainerPorts: 0, // Mapping disabled
				},
			},
			expectedCommand:             []string{"privileged-consul-dataplane"},
			expectedHasEnvoyArg:         true,
			expectedCapabilities:        []corev1.Capability{"NET_BIND_SERVICE"},
			expectedPrivilegeEscalation: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := common.MetricsConfig{}
			config := common.HelmConfig{
				ImageDataplane:        "consul-dataplane:test",
				GlobalImagePullPolicy: "IfNotPresent",
				LogLevel:              "INFO",
				LogJSON:               false,
				ConsulConfig: common.ConsulConfig{
					Address:  "consul.default.svc.cluster.local",
					GRPCPort: 8502,
				},
			}

			container, err := consulDataplaneContainer(metrics, config, tc.gcc, tc.gateway, []corev1.VolumeMount{})
			require.NoError(t, err)

			// Check command
			if tc.expectedCommand != nil {
				require.Equal(t, tc.expectedCommand, container.Command)
			} else {
				require.Nil(t, container.Command)
			}

			// Check for envoy executable path argument
			hasEnvoyArg := false
			for _, arg := range container.Args {
				if arg == "-envoy-executable-path=/usr/local/bin/privileged-envoy" {
					hasEnvoyArg = true
					break
				}
			}
			require.Equal(t, tc.expectedHasEnvoyArg, hasEnvoyArg)

			// Check security context
			require.NotNil(t, container.SecurityContext)
			require.Equal(t, ptr.To(true), container.SecurityContext.ReadOnlyRootFilesystem)
			require.Equal(t, ptr.To(tc.expectedPrivilegeEscalation), container.SecurityContext.AllowPrivilegeEscalation)

			// Check capabilities
			if len(tc.expectedCapabilities) > 0 {
				require.NotNil(t, container.SecurityContext.Capabilities)
				require.Equal(t, tc.expectedCapabilities, container.SecurityContext.Capabilities.Add)
				require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
			}
		})
	}
}

func TestConsulDataplaneContainer_SecurityContext(t *testing.T) {
	t.Parallel()

	metrics := common.MetricsConfig{}
	config := common.HelmConfig{
		ImageDataplane:        "consul-dataplane:test",
		GlobalImagePullPolicy: "IfNotPresent",
		LogLevel:              "INFO",
		LogJSON:               false,
		ConsulConfig: common.ConsulConfig{
			Address:  "consul.default.svc.cluster.local",
			GRPCPort: 8502,
		},
	}

	gateway := gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gwv1beta1.GatewaySpec{
			Listeners: []gwv1beta1.Listener{
				{
					Name: "http",
					Port: 8080,
				},
			},
		},
	}

	gcc := v1alpha1.GatewayClassConfig{
		Spec: v1alpha1.GatewayClassConfigSpec{
			MapPrivilegedContainerPorts: 0,
		},
	}

	container, err := consulDataplaneContainer(metrics, config, gcc, gateway, []corev1.VolumeMount{})
	require.NoError(t, err)

	// Verify all security context fields are set correctly
	require.NotNil(t, container.SecurityContext)
	require.Equal(t, ptr.To(true), container.SecurityContext.ReadOnlyRootFilesystem)
	require.Equal(t, ptr.To(false), container.SecurityContext.AllowPrivilegeEscalation)
	require.Equal(t, ptr.To(true), container.SecurityContext.RunAsNonRoot)

	// Check seccomp profile
	require.NotNil(t, container.SecurityContext.SeccompProfile)
	require.Equal(t, corev1.SeccompProfileTypeRuntimeDefault, container.SecurityContext.SeccompProfile.Type)

	// Check capabilities for non-privileged case
	require.NotNil(t, container.SecurityContext.Capabilities)
	require.Equal(t, []corev1.Capability{"NET_BIND_SERVICE"}, container.SecurityContext.Capabilities.Add)
	require.Equal(t, []corev1.Capability{"ALL"}, container.SecurityContext.Capabilities.Drop)
}

func TestConsulDataplaneContainer_BasicFunctionality(t *testing.T) {
	t.Parallel()

	metrics := common.MetricsConfig{
		Enabled: true,
		Port:    9090,
	}
	config := common.HelmConfig{
		ImageDataplane:        "consul-dataplane:test",
		GlobalImagePullPolicy: "IfNotPresent",
		LogLevel:              "DEBUG",
		LogJSON:               true,
		ConsulConfig: common.ConsulConfig{
			Address:  "consul.default.svc.cluster.local",
			GRPCPort: 8502,
		},
	}

	gateway := gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gateway",
			Namespace: "default",
		},
		Spec: gwv1beta1.GatewaySpec{
			Listeners: []gwv1beta1.Listener{
				{
					Name: "http",
					Port: 8080,
				},
			},
		},
	}

	gcc := v1alpha1.GatewayClassConfig{
		Spec: v1alpha1.GatewayClassConfigSpec{
			MapPrivilegedContainerPorts: 0,
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "test-volume",
			MountPath: "/test",
		},
	}

	container, err := consulDataplaneContainer(metrics, config, gcc, gateway, volumeMounts)
	require.NoError(t, err)

	// Basic container properties
	require.Equal(t, gateway.Name, container.Name)
	require.Equal(t, config.ImageDataplane, container.Image)
	require.Equal(t, corev1.PullPolicy(config.GlobalImagePullPolicy), container.ImagePullPolicy)

	// Volume mounts
	require.Equal(t, volumeMounts, container.VolumeMounts)

	// Environment variables
	require.NotEmpty(t, container.Env)

	// Check for required environment variables
	envMap := make(map[string]string)
	for _, env := range container.Env {
		if env.Value != "" {
			envMap[env.Name] = env.Value
		}
	}
	require.Equal(t, "/consul/connect-inject", envMap["TMPDIR"])
	require.Equal(t, "$(NODE_NAME)-virtual", envMap["DP_SERVICE_NODE_NAME"])

	// Readiness probe
	require.NotNil(t, container.ReadinessProbe)
	require.NotNil(t, container.ReadinessProbe.HTTPGet)
	require.Equal(t, "/ready", container.ReadinessProbe.HTTPGet.Path)

	// Ports (should include prometheus port when metrics enabled)
	require.NotEmpty(t, container.Ports)

	var prometheusPortFound bool
	for _, port := range container.Ports {
		if port.Name == "prometheus" {
			prometheusPortFound = true
			require.Equal(t, int32(metrics.Port), port.ContainerPort)
		}
	}
	require.True(t, prometheusPortFound, "Prometheus port should be configured when metrics are enabled")

	// Args should contain expected dataplane arguments
	require.NotEmpty(t, container.Args)

	// Verify some key arguments are present
	argsStr := ""
	for _, arg := range container.Args {
		argsStr += arg + " "
	}
	require.Contains(t, argsStr, "-addresses")
	require.Contains(t, argsStr, "-grpc-port=8502")
	require.Contains(t, argsStr, "-log-level=DEBUG")
	require.Contains(t, argsStr, "-log-json=true")
}
