package connectinject

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandlerEnvoySidecar(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	pod := &corev1.Pod{
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
		"--max-obj-name-len", "256",
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

// Test that if AuthMethod is set
// the preStop command includes a token
func TestHandlerEnvoySidecar_AuthMethod(t *testing.T) {
	require := require.New(t)
	h := Handler{
		AuthMethod: "test-auth-method",
	}
	pod := &corev1.Pod{
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
&& /consul/connect-inject/consul logout \
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
	pod := &corev1.Pod{
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
	pod := &corev1.Pod{
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
	pod := &corev1.Pod{
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
&& /consul/connect-inject/consul logout \
  -token-file="/consul/connect-inject/acl-token"`)
}
