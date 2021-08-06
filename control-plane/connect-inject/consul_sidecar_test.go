package connectinject

import (
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that if the conditions for running a merged metrics server are true,
// that we pass the metrics flags to consul sidecar.
func TestConsulSidecar_MetricsFlags(t *testing.T) {
	handler := Handler{
		Log:            logrtest.TestLogger{T: t},
		ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
		MetricsConfig: MetricsConfig{
			DefaultEnableMetrics:        true,
			DefaultEnableMetricsMerging: true,
		},
	}
	container, err := handler.consulSidecar(corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationMergedMetricsPort:  "20100",
				annotationServiceMetricsPort: "8080",
				annotationServiceMetricsPath: "/metrics",
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

	require.NoError(t, err)
	require.Contains(t, container.Command, "-enable-metrics-merging=true")
	require.Contains(t, container.Command, "-merged-metrics-port=20100")
	require.Contains(t, container.Command, "-service-metrics-port=8080")
	require.Contains(t, container.Command, "-service-metrics-path=/metrics")
}
