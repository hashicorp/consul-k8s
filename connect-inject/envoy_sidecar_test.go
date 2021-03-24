package connectinject

import (
	"strings"
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
	container, err := h.envoySidecar(pod, k8sNamespace)
	require.NoError(err)
	require.Equal(container.Command, []string{
		"envoy",
		"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
	})

	preStopCommand := strings.Join(container.Lifecycle.PreStop.Exec.Command, " ")
	require.Equal(preStopCommand, `/bin/sh -ec /consul/connect-inject/consul services deregister \
  /consul/connect-inject/service.hcl`)

	require.Equal(container.VolumeMounts, []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	})

	require.Equal(container.Env, []corev1.EnvVar{
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
			},
		},
		{
			Name:  "CONSUL_HTTP_ADDR",
			Value: "$(HOST_IP):8500",
		},
	})
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

			c, err := h.envoySidecar(*tc.pod, k8sNamespace)
			require.NoError(t, err)
			require.Equal(t, tc.expectedContainerCommand, c.Command)
		})
	}
}

// Test that if AuthMethod is set
// the preStop command includes a token
func TestHandlerEnvoySidecar_AuthMethod(t *testing.T) {
	require := require.New(t)
	h := Handler{
		AuthMethod: "test-auth-method",
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
	container, err := h.envoySidecar(pod, k8sNamespace)
	require.NoError(err)

	preStopCommand := strings.Join(container.Lifecycle.PreStop.Exec.Command, " ")
	require.Equal(preStopCommand, `/bin/sh -ec /consul/connect-inject/consul services deregister \
  -token-file="/consul/connect-inject/acl-token" \
  /consul/connect-inject/service.hcl
/consul/connect-inject/consul logout \
  -token-file="/consul/connect-inject/acl-token"`)
}

// If Consul CA cert is set,
// Consul addresses should use HTTPS
// and CA cert should be set as env variable
func TestHandlerEnvoySidecar_WithTLS(t *testing.T) {
	require := require.New(t)
	h := Handler{
		ConsulCACert: "consul-ca-cert",
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
	container, err := h.envoySidecar(pod, k8sNamespace)
	require.NoError(err)
	require.Equal(container.Env, []corev1.EnvVar{
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
			},
		},
		{
			Name:  "CONSUL_CACERT",
			Value: "/consul/connect-inject/consul-ca.pem",
		},
		{
			Name:  "CONSUL_HTTP_ADDR",
			Value: "https://$(HOST_IP):8501",
		},
	})
}

// Test that the pre-stop command is modified when namespaces
// are enabled. A single test is enough here, since the exclusion
// cases are tested in the other cases above and there are numerous
// tests specifically for `h.consulNamespace` in handler_test.go
func TestHandlerEnvoySidecar_Namespaces(t *testing.T) {
	require := require.New(t)
	h := Handler{
		EnableNamespaces:           true,
		ConsulDestinationNamespace: k8sNamespace,
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
	container, err := h.envoySidecar(pod, k8sNamespace)
	require.NoError(err)

	preStopCommand := strings.Join(container.Lifecycle.PreStop.Exec.Command, " ")
	require.Equal(preStopCommand, `/bin/sh -ec /consul/connect-inject/consul services deregister \
  -namespace="k8snamespace" \
  /consul/connect-inject/service.hcl`)
}

func TestHandlerEnvoySidecar_NamespacesAndAuthMethod(t *testing.T) {
	require := require.New(t)
	h := Handler{
		EnableNamespaces:           true,
		ConsulDestinationNamespace: k8sNamespace,
		AuthMethod:                 "test-auth-method",
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
	container, err := h.envoySidecar(pod, k8sNamespace)
	require.NoError(err)

	preStopCommand := strings.Join(container.Lifecycle.PreStop.Exec.Command, " ")
	require.Equal(preStopCommand, `/bin/sh -ec /consul/connect-inject/consul services deregister \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  /consul/connect-inject/service.hcl
/consul/connect-inject/consul logout \
  -token-file="/consul/connect-inject/acl-token"`)
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
			container, err := c.handler.envoySidecar(pod, k8sNamespace)
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
