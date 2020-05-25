package connectinject

import (
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

// NOTE: This is tested here rather than in handler_test because doing it there
// would require a lot of boilerplate to get at the underlying patches that would
// complicate understanding the tests (which are simple).

// Test that the lifecycle sidecar is as expected.
func TestLifecycleSidecar_Default(t *testing.T) {
	handler := Handler{
		Log:            hclog.Default().Named("handler"),
		ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
	}
	container := handler.lifecycleSidecar(&corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	})
	require.Equal(t, corev1.Container{
		Name:  "consul-connect-lifecycle-sidecar",
		Image: "hashicorp/consul-k8s:9.9.9",
		Env: []corev1.EnvVar{
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
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{
			"consul-k8s", "lifecycle-sidecar",
			"-service-config", "/consul/connect-inject/service.hcl",
			"-consul-binary", "/consul/connect-inject/consul",
		},
	}, container)
}

// Test that if there's an auth method we set the -token-file flag
// and if there isn't we don't.
func TestLifecycleSidecar_AuthMethod(t *testing.T) {
	for _, authMethod := range []string{"", "auth-method"} {
		t.Run("authmethod: "+authMethod, func(t *testing.T) {
			handler := Handler{
				Log:            hclog.Default().Named("handler"),
				AuthMethod:     authMethod,
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
			}
			container := handler.lifecycleSidecar(&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			})

			if authMethod == "" {
				require.NotContains(t, container.Command, "-token-file=/consul/connect-inject/acl-token")
			} else {
				require.Contains(t,
					container.Command,
					"-token-file=/consul/connect-inject/acl-token",
				)
			}
		})
	}
}

// Test that if there's an annotation on the original pod that changes the sync
// period we use that value.
func TestLifecycleSidecar_SyncPeriodAnnotation(t *testing.T) {
	handler := Handler{
		Log:            hclog.Default().Named("handler"),
		ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
	}
	container := handler.lifecycleSidecar(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"consul.hashicorp.com/connect-sync-period": "55s",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	})

	require.Contains(t, container.Command, "-sync-period=55s")
}

// Test that the Consul address uses HTTPS
// and that the CA is provided
func TestLifecycleSidecar_TLS(t *testing.T) {
	handler := Handler{
		Log:            hclog.Default().Named("handler"),
		ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
		ConsulCACert:   "consul-ca-cert",
	}
	container := handler.lifecycleSidecar(&corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	})
	require.Equal(t, corev1.Container{
		Name:  "consul-connect-lifecycle-sidecar",
		Image: "hashicorp/consul-k8s:9.9.9",
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
			{
				Name:  "CONSUL_HTTP_ADDR",
				Value: "https://$(HOST_IP):8501",
			},
			{
				Name:  "CONSUL_CACERT",
				Value: "/consul/connect-inject/consul-ca.pem",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{
			"consul-k8s", "lifecycle-sidecar",
			"-service-config", "/consul/connect-inject/service.hcl",
			"-consul-binary", "/consul/connect-inject/consul",
		},
	}, container)
}
