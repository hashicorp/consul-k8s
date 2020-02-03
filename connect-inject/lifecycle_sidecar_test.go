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
		ImageConsul:    "hashicorp/consul:3.2.1",
	}

	container, err := handler.lifecycleSidecar(&corev1.Pod{
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
		Image: "hashicorp/consul:3.2.1",
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
			"/bin/sh", "-ec",
			`while true;
do /bin/consul services register \
  /consul/connect-inject/service.hcl;
sleep 10;
done;`,
		},
	}, container)
	require.NoError(t, err)
}

// Test that if there's an auth method we set the -token-file flag
// and if there isn't we don't.
func TestLifecycleSidecar_AuthMethod(t *testing.T) {
	for _, authMethod := range []string{"", "auth-method"} {
		t.Run("authmethod: "+authMethod, func(t *testing.T) {
			handler := Handler{
				Log:        hclog.Default().Named("handler"),
				AuthMethod: authMethod,
			}
			container, err := handler.lifecycleSidecar(&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			})

			if authMethod == "" {
				require.NotContains(t, container.Command[2], `-token-file="/consul/connect-inject/acl-token"`)
			} else {
				require.Contains(t,
					container.Command[2],
					`-token-file="/consul/connect-inject/acl-token"`,
				)
			}
			require.NoError(t, err)
		})
	}
}

// Test that if there's an annotation on the original pod that changes the sync
// period we use that value.
func TestLifecycleSidecar_SyncPeriodAnnotation(t *testing.T) {
	handler := Handler{
		Log: hclog.Default().Named("handler"),
	}

	container, err := handler.lifecycleSidecar(&corev1.Pod{
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

	require.Contains(t, container.Command[2], "sleep 55")
	require.NoError(t, err)
}

// Test that the Consul address uses HTTPS
// and that the CA is provided
func TestLifecycleSidecar_TLS(t *testing.T) {
	handler := Handler{
		Log:            hclog.Default().Named("handler"),
		ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
		ImageConsul:    "hashicorp/consul:3.2.1",
		ConsulCACert:   "consul-ca-cert",
	}

	container, err := handler.lifecycleSidecar(&corev1.Pod{
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
		Image: "hashicorp/consul:3.2.1",
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
			"/bin/sh", "-ec",
			`while true;
do /bin/consul services register \
  /consul/connect-inject/service.hcl;
sleep 10;
done;`,
		},
	}, container)
	require.NoError(t, err)
}
