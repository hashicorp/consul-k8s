package connectinject

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestContainerEnvVars(t *testing.T) {

	cases := []struct {
		Name     string
		Upstream string
	}{
		{
			"Upstream with datacenter",
			"static-server:7890:dc1",
		},
		{
			"Upstream without datacenter",
			"static-server:7890",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var w MeshWebhook
			envVars := w.containerEnvVars(corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService:   "foo",
						annotationUpstreams: tt.Upstream,
					},
				},
			})

			require.ElementsMatch(envVars, []corev1.EnvVar{
				{
					Name:  "STATIC_SERVER_CONNECT_SERVICE_HOST",
					Value: "127.0.0.1",
				}, {
					Name:  "STATIC_SERVER_CONNECT_SERVICE_PORT",
					Value: "7890",
				},
			})
		})
	}
}
