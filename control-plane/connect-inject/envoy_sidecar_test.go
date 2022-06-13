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
	w := MeshWebhook{}
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
	container, err := w.envoySidecar(testNS, pod, multiPortInfo{})
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

func TestHandlerEnvoySidecar_Multiport(t *testing.T) {
	require := require.New(t)
	w := MeshWebhook{}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationService: "web,web-admin",
			},
		},

		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
				{
					Name: "web-admin",
				},
			},
		},
	}
	multiPortInfos := []multiPortInfo{
		{
			serviceIndex: 0,
			serviceName:  "web",
		},
		{
			serviceIndex: 1,
			serviceName:  "web-admin",
		},
	}
	expCommand := map[int][]string{
		0: {"envoy", "--config-path", "/consul/connect-inject/envoy-bootstrap-web.yaml", "--base-id", "0"},
		1: {"envoy", "--config-path", "/consul/connect-inject/envoy-bootstrap-web-admin.yaml", "--base-id", "1"},
	}
	for i := 0; i < 2; i++ {
		container, err := w.envoySidecar(testNS, pod, multiPortInfos[i])
		require.NoError(err)
		require.Equal(expCommand[i], container.Command)

		require.Equal(container.VolumeMounts, []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		})
	}
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
			w := MeshWebhook{
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
			ec, err := w.envoySidecar(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, c.expSecurityContext, ec.SecurityContext)
		})
	}
}

// Test that if the user specifies a pod security context with the same uid as `envoyUserAndGroupID` that we return
// an error to the meshWebhook.
func TestHandlerEnvoySidecar_FailsWithDuplicatePodSecurityContextUID(t *testing.T) {
	require := require.New(t)
	w := MeshWebhook{}
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
	_, err := w.envoySidecar(testNS, pod, multiPortInfo{})
	require.Error(err, fmt.Sprintf("pod security context cannot have the same uid as envoy: %v", envoyUserAndGroupID))
}

// Test that if the user specifies a container with security context with the same uid as `envoyUserAndGroupID` that we
// return an error to the meshWebhook. If a container using the envoy image has the same uid, we don't return an error
// because in multiport pod there can be multiple envoy sidecars.
func TestHandlerEnvoySidecar_FailsWithDuplicateContainerSecurityContextUID(t *testing.T) {
	cases := []struct {
		name          string
		pod           corev1.Pod
		webhook       MeshWebhook
		expErr        bool
		expErrMessage error
	}{
		{
			name: "fails with non envoy image",
			pod: corev1.Pod{
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
							Image: "not-envoy",
						},
					},
				},
			},
			webhook:       MeshWebhook{},
			expErr:        true,
			expErrMessage: fmt.Errorf("container app has runAsUser set to the same uid %q as envoy which is not allowed", envoyUserAndGroupID),
		},
		{
			name: "doesn't fail with envoy image",
			pod: corev1.Pod{
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
							Name: "sidecar",
							// Setting RunAsUser: 5995 should succeed if the image matches h.ImageEnvoy.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointerToInt64(envoyUserAndGroupID),
							},
							Image: "envoy",
						},
					},
				},
			},
			webhook: MeshWebhook{
				ImageEnvoy: "envoy",
			},
			expErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.webhook.envoySidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expErr {
				require.Error(t, err, tc.expErrMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
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
			h := MeshWebhook{
				ImageConsul:    "hashicorp/consul:latest",
				ImageEnvoy:     "hashicorp/consul-k8s:latest",
				EnvoyExtraArgs: tc.envoyExtraArgs,
			}

			c, err := h.envoySidecar(testNS, *tc.pod, multiPortInfo{})
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
		webhook      MeshWebhook
		annotations  map[string]string
		expResources corev1.ResourceRequirements
		expErr       string
	}{
		"no defaults, no annotations": {
			webhook:     MeshWebhook{},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		},
		"all defaults, no annotations": {
			webhook: MeshWebhook{
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
			webhook: MeshWebhook{},
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
			webhook: MeshWebhook{
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
			webhook: MeshWebhook{
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
			webhook: MeshWebhook{},
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
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPURequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid cpu limit": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyCPULimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-limit:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory request": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				annotationSidecarProxyMemoryRequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory limit": {
			webhook: MeshWebhook{},
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
			container, err := c.webhook.envoySidecar(testNS, pod, multiPortInfo{})
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
