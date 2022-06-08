package connectinject

import (
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that if the conditions for running a merged metrics server are true,
// that we pass the metrics flags to consul sidecar.
func TestConsulSidecar_MetricsFlags(t *testing.T) {
	handler := ConnectWebhook{
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

func TestHandlerConsulSidecar_Resources(t *testing.T) {
	mem1 := resource.MustParse("100Mi")
	mem2 := resource.MustParse("200Mi")
	cpu1 := resource.MustParse("100m")
	cpu2 := resource.MustParse("200m")
	zero := resource.MustParse("0")

	cases := map[string]struct {
		handler      ConnectWebhook
		annotations  map[string]string
		expResources corev1.ResourceRequirements
		expErr       string
	}{
		"no defaults, no annotations": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:  "20100",
				annotationServiceMetricsPort: "8080",
				annotationServiceMetricsPath: "/metrics",
			},
			expResources: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		},
		"all defaults, no annotations": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
				DefaultConsulSidecarResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    cpu1,
						corev1.ResourceMemory: mem1,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    cpu2,
						corev1.ResourceMemory: mem2,
					},
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:  "20100",
				annotationServiceMetricsPort: "8080",
				annotationServiceMetricsPath: "/metrics",
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
		"no defaults, all annotations": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:          "20100",
				annotationServiceMetricsPort:         "8080",
				annotationServiceMetricsPath:         "/metrics",
				annotationConsulSidecarCPURequest:    "100m",
				annotationConsulSidecarMemoryRequest: "100Mi",
				annotationConsulSidecarCPULimit:      "200m",
				annotationConsulSidecarMemoryLimit:   "200Mi",
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
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
				DefaultConsulSidecarResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    zero,
						corev1.ResourceMemory: zero,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    zero,
						corev1.ResourceMemory: zero,
					},
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:          "20100",
				annotationServiceMetricsPort:         "8080",
				annotationServiceMetricsPath:         "/metrics",
				annotationConsulSidecarCPURequest:    "100m",
				annotationConsulSidecarMemoryRequest: "100Mi",
				annotationConsulSidecarCPULimit:      "200m",
				annotationConsulSidecarMemoryLimit:   "200Mi",
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
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
				DefaultConsulSidecarResources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    zero,
						corev1.ResourceMemory: zero,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    zero,
						corev1.ResourceMemory: zero,
					},
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:  "20100",
				annotationServiceMetricsPort: "8080",
				annotationServiceMetricsPath: "/metrics",
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
		"annotations set to 0": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:          "20100",
				annotationServiceMetricsPort:         "8080",
				annotationServiceMetricsPath:         "/metrics",
				annotationConsulSidecarCPURequest:    "0",
				annotationConsulSidecarMemoryRequest: "0",
				annotationConsulSidecarCPULimit:      "0",
				annotationConsulSidecarMemoryLimit:   "0",
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
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:       "20100",
				annotationServiceMetricsPort:      "8080",
				annotationServiceMetricsPath:      "/metrics",
				annotationConsulSidecarCPURequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/consul-sidecar-cpu-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid cpu limit": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:     "20100",
				annotationServiceMetricsPort:    "8080",
				annotationServiceMetricsPath:    "/metrics",
				annotationConsulSidecarCPULimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/consul-sidecar-cpu-limit:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory request": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:          "20100",
				annotationServiceMetricsPort:         "8080",
				annotationServiceMetricsPath:         "/metrics",
				annotationConsulSidecarMemoryRequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/consul-sidecar-memory-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory limit": {
			handler: ConnectWebhook{
				Log:            logrtest.TestLogger{T: t},
				ImageConsulK8S: "hashicorp/consul-k8s:9.9.9",
				MetricsConfig: MetricsConfig{
					DefaultEnableMetrics:        true,
					DefaultEnableMetricsMerging: true,
				},
			},
			annotations: map[string]string{
				annotationMergedMetricsPort:        "20100",
				annotationServiceMetricsPort:       "8080",
				annotationServiceMetricsPath:       "/metrics",
				annotationConsulSidecarMemoryLimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/consul-sidecar-memory-limit:\"invalid\": quantities must match the regular expression",
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
			container, err := c.handler.consulSidecar(pod)
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
