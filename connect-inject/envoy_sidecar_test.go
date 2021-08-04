package connectinject

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandlerEnvoySidecar(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationService: "foo",
			},
		},

		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	}
	container, err := h.envoySidecar(testNS, pod)
	require.NoError(err)
	require.Equal(container.Command, []string{
		"envoy",
		"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
	})

	require.Equal(container.VolumeMounts, []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	})
}

func TestHandlerEnvoySidecar_withSecurityContext(t *testing.T) {
	cases := map[string]struct {
		tproxyEnabled      bool
		openShiftEnabled   bool
		expSecurityContext *corev1.SecurityContext
	}{
		"tproxy disabled; openshift disabled": {
			tproxyEnabled:    false,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointerToInt64(envoyUserAndGroupID),
				RunAsGroup:             pointerToInt64(envoyUserAndGroupID),
				RunAsNonRoot:           pointerToBool(true),
				ReadOnlyRootFilesystem: pointerToBool(true),
			},
		},
		"tproxy enabled; openshift disabled": {
			tproxyEnabled:    true,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointerToInt64(envoyUserAndGroupID),
				RunAsGroup:             pointerToInt64(envoyUserAndGroupID),
				RunAsNonRoot:           pointerToBool(true),
				ReadOnlyRootFilesystem: pointerToBool(true),
			},
		},
		"tproxy disabled; openshift enabled": {
			tproxyEnabled:      false,
			openShiftEnabled:   true,
			expSecurityContext: nil,
		},
		"tproxy enabled; openshift enabled": {
			tproxyEnabled:    true,
			openShiftEnabled: true,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointerToInt64(envoyUserAndGroupID),
				RunAsGroup:             pointerToInt64(envoyUserAndGroupID),
				RunAsNonRoot:           pointerToBool(true),
				ReadOnlyRootFilesystem: pointerToBool(true),
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := Handler{
				EnableTransparentProxy: c.tproxyEnabled,
				EnableOpenShift:        c.openShiftEnabled,
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			ec, err := h.envoySidecar(testNS, pod)
			require.NoError(t, err)
			require.Equal(t, c.expSecurityContext, ec.SecurityContext)
		})
	}
}

// Test that if the user specifies a pod security context with the same uid as `envoyUserAndGroupID` that we return
// an error to the handler.
func TestHandlerEnvoySidecar_FailsWithDuplicatePodSecurityContextUID(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser: pointerToInt64(envoyUserAndGroupID),
			},
		},
	}
	_, err := h.envoySidecar(testNS, pod)
	require.Error(err, fmt.Sprintf("pod security context cannot have the same uid as envoy: %v", envoyUserAndGroupID))
}

// Test that if the user specifies a container with security context with the same uid as `envoyUserAndGroupID`
// that we return an error to the handler.
func TestHandlerEnvoySidecar_FailsWithDuplicateContainerSecurityContextUID(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
					// Setting RunAsUser: 1 should succeed.
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: pointerToInt64(1),
					},
				},
				{
					Name: "app",
					// Setting RunAsUser: 5995 should fail.
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: pointerToInt64(envoyUserAndGroupID),
					},
				},
			},
		},
	}
	_, err := h.envoySidecar(testNS, pod)
	require.Error(err, fmt.Sprintf("container %q has runAsUser set to the same uid %q as envoy which is not allowed", pod.Spec.Containers[1].Name, envoyUserAndGroupID))
}

// Test that we can pass extra args to envoy via the extraEnvoyArgs flag
// or via pod annotations. When arguments are passed in both ways, the
// arguments set via pod annotations are used.
func TestHandlerEnvoySidecar_EnvoyExtraArgs(t *testing.T) {
	cases := []struct {
		name                     string
		envoyExtraArgs           string
		pod                      *corev1.Pod
		expectedContainerCommand []string
	}{
		{
			name:           "no extra options provided",
			envoyExtraArgs: "",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
			},
		},
		{
			name:           "via flag: extra log-level option",
			envoyExtraArgs: "--log-level debug",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
			},
		},
		{
			name:           "via flag: multiple arguments with quotes",
			envoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
			pod:            &corev1.Pod{},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
		{
			name:           "via annotation: multiple arguments with quotes",
			envoyExtraArgs: "",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
		{
			name:           "via flag and annotation: should prefer setting via the annotation",
			envoyExtraArgs: "this should be overwritten",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedContainerCommand: []string{
				"envoy",
				"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
				"--log-level", "debug",
				"--admin-address-path", "\"/tmp/consul/foo bar\"",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler{
				ImageConsul:    "hashicorp/consul:latest",
				ImageEnvoy:     "hashicorp/consul-k8s:latest",
				EnvoyExtraArgs: tc.envoyExtraArgs,
			}

			c, err := h.envoySidecar(testNS, *tc.pod)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContainerCommand, c.Command)
		})
	}
}

func TestHandlerEnvoySidecar_Resources(t *testing.T) {
	mem1 := resource.MustParse("100Mi")
	mem2 := resource.MustParse("200Mi")
	cpu1 := resource.MustParse("100m")
	cpu2 := resource.MustParse("200m")
	zero := resource.MustParse("0")

	cases := map[string]struct {
		handler      Handler
		annotations  map[string]string
		expResources corev1.ResourceRequirements
		expErr       string
	}{
		"no defaults, no annotations": {
			handler:     Handler{},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		},
		"all defaults, no annotations": {
			handler: Handler{
				DefaultProxyCPURequest:    cpu1,
				DefaultProxyCPULimit:      cpu2,
				DefaultProxyMemoryRequest: mem1,
				DefaultProxyMemoryLimit:   mem2,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"no defaults, all annotations": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "100m",
				annotationSidecarProxyMemoryRequest: "100Mi",
				annotationSidecarProxyCPULimit:      "200m",
				annotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"annotations override defaults": {
			handler: Handler{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "100m",
				annotationSidecarProxyMemoryRequest: "100Mi",
				annotationSidecarProxyCPULimit:      "200m",
				annotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"defaults set to zero, no annotations": {
			handler: Handler{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"annotations set to 0": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest:    "0",
				annotationSidecarProxyMemoryRequest: "0",
				annotationSidecarProxyCPULimit:      "0",
				annotationSidecarProxyMemoryLimit:   "0",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"invalid cpu request": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid cpu limit": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyCPULimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-limit:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory request": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyMemoryRequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory limit": {
			handler: Handler{},
			annotations: map[string]string{
				annotationSidecarProxyMemoryLimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-limit:\"invalid\": quantities must match the regular expression",
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			require := require.New(tt)
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := c.handler.envoySidecar(testNS, pod)
			if c.expErr != "" {
				require.NotNil(err)
				require.Contains(err.Error(), c.expErr)
			} else {
				require.NoError(err)
				require.Equal(c.expResources, container.Resources)
			}
		})
	}
}
