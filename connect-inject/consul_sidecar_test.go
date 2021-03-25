package connectinject

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	consulSidecarResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("25Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("20m"),
			corev1.ResourceMemory: resource.MustParse("50Mi"),
		},
	}
)

// NOTE: This is tested here rather than in handler_test because doing it there
// would require a lot of boilerplate to get at the underlying patches that would
// complicate understanding the tests (which are simple).

// Test that if the conditions for running a merged metrics server are true,
// that we pass the metrics flags to consul sidecar.
func TestConsulSidecar_MetricsFlags(t *testing.T) {
	handler := Handler{
		Log:                         hclog.Default().Named("handler"),
		ImageConsulK8S:              "hashicorp/consul-k8s:9.9.9",
		DefaultEnableMetrics:        true,
		DefaultEnableMetricsMerging: true,
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

// Test that the Consul sidecar errors when metrics merging is disabled.
func TestConsulSidecar_ErrorsWhenMetricsMergingIsDisabled(t *testing.T) {
	handler := Handler{
		Log:                    hclog.Default().Named("handler"),
		ImageConsulK8S:         "hashicorp/consul-k8s:9.9.9",
		ConsulSidecarResources: consulSidecarResources,
	}
	_, err := handler.consulSidecar(corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
		},
	})
	require.EqualError(t, err, "metrics merging should be enabled in order to inject the consul-sidecar")
}
