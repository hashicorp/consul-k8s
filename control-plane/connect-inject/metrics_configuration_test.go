package connectinject

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricsConfigEnableMetrics(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig MetricsConfig
		Expected      bool
		Err           string
	}{
		{
			Name: "Metrics enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationEnableMetrics] = "true"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationEnableMetrics] = "not-a-bool"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-metrics annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			actual, err := mc.enableMetrics(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestMetricsConfigEnableMetricsMerging(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig MetricsConfig
		Expected      bool
		Err           string
	}{
		{
			Name: "Metrics merging enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetricsMerging: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics merging enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationEnableMetricsMerging] = "true"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetricsMerging: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics merging configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationEnableMetricsMerging] = "not-a-bool"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetricsMerging: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-metrics-merging annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			actual, err := mc.enableMetricsMerging(*tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestMetricsConfigServiceMetricsPort(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Expected string
	}{
		{
			Name: "Prefers annotationServiceMetricsPort",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[annotationServiceMetricsPort] = "9000"
				return pod
			},
			Expected: "9000",
		},
		{
			Name: "Uses annotationPort of annotationServiceMetricsPort is not set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			Expected: "1234",
		},
		{
			Name: "Is set to 0 if neither annotationPort nor annotationServiceMetricsPort is set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Expected: "0",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := MetricsConfig{}

			actual, err := mc.serviceMetricsPort(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

func TestMetricsConfigServiceMetricsPath(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Expected string
	}{
		{
			Name: "Defaults to /metrics",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Expected: "/metrics",
		},
		{
			Name: "Uses annotationServiceMetricsPath when set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationServiceMetricsPath] = "/custom-metrics-path"
				return pod
			},
			Expected: "/custom-metrics-path",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := MetricsConfig{}

			actual := mc.serviceMetricsPath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

func TestMetricsConfigPrometheusScrapePath(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig MetricsConfig
		Expected      string
	}{
		{
			Name: "Defaults to the meshWebhook's value",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultPrometheusScrapePath: "/default-prometheus-scrape-path",
			},
			Expected: "/default-prometheus-scrape-path",
		},
		{
			Name: "Uses annotationPrometheusScrapePath when set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPrometheusScrapePath] = "/custom-scrape-path"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultPrometheusScrapePath: "/default-prometheus-scrape-path",
			},
			Expected: "/custom-scrape-path",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			actual := mc.prometheusScrapePath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

// This test only needs unique cases not already handled in tests for
// h.enableMetrics, h.enableMetricsMerging, and h.serviceMetricsPort.
func TestMetricsConfigShouldRunMergedMetricsServer(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig MetricsConfig
		Expected      bool
	}{
		{
			Name: "Returns true when metrics and metrics merging are enabled, and the service metrics port is greater than 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: true,
			},
			Expected: true,
		},
		{
			Name: "Returns false when service metrics port is 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "0"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: true,
			},
			Expected: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			actual, err := mc.shouldRunMergedMetricsServer(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

// Tests determineAndValidatePort, which in turn tests the
// prometheusScrapePort() and mergedMetricsPort() functions because their logic
// is just to call out to determineAndValidatePort().
func TestMetricsConfigDetermineAndValidatePort(t *testing.T) {
	cases := []struct {
		Name        string
		Pod         func(*corev1.Pod) *corev1.Pod
		Annotation  string
		Privileged  bool
		DefaultPort string
		Expected    string
		Err         string
	}{
		{
			Name: "Valid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "1234"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "1234",
			Err:        "",
		},
		{
			Name: "Uses default when there's no annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "4321",
			Expected:    "4321",
			Err:         "",
		},
		{
			Name: "Gets the value of the named default port when there's no annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          "web-port",
						ContainerPort: 2222,
					},
				}
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "web-port",
			Expected:    "2222",
			Err:         "",
		},
		{
			Name: "Errors if the named default port doesn't exist on the pod",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "web-port",
			Expected:    "",
			Err:         "web-port is not a valid port on the pod minimal",
		},
		{
			Name: "Gets the value of the named port",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "web-port"
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{
						Name:          "web-port",
						ContainerPort: 2222,
					},
				}
				return pod
			},
			Annotation:  "consul.hashicorp.com/test-annotation-port",
			Privileged:  false,
			DefaultPort: "4321",
			Expected:    "2222",
			Err:         "",
		},
		{
			Name: "Invalid annotation (not an integer)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "not-an-int"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of not-an-int is not a valid integer",
		},
		{
			Name: "Invalid annotation (integer not in port range)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "100000"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: true,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of 100000 is not in the valid port range 1-65535",
		},
		{
			Name: "Invalid annotation (integer not in unprivileged port range)",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "22"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: false,
			Expected:   "",
			Err:        "consul.hashicorp.com/test-annotation-port annotation value of 22 is not in the unprivileged port range 1024-65535",
		},
		{
			Name: "Privileged ports allowed",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations["consul.hashicorp.com/test-annotation-port"] = "22"
				return pod
			},
			Annotation: "consul.hashicorp.com/test-annotation-port",
			Privileged: true,
			Expected:   "22",
			Err:        "",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			actual, err := determineAndValidatePort(*tt.Pod(minimal()), tt.Annotation, tt.DefaultPort, tt.Privileged)

			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.Expected, actual)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

// Tests mergedMetricsServerConfiguration happy path and error case not covered by other MetricsConfig tests.
func TestMetricsConfigMergedMetricsServerConfiguration(t *testing.T) {
	cases := []struct {
		Name                       string
		Pod                        func(*corev1.Pod) *corev1.Pod
		MetricsConfig              MetricsConfig
		ExpectedMergedMetricsPort  string
		ExpectedServiceMetricsPort string
		ExpectedServiceMetricsPath string
		ExpErr                     string
	}{
		{
			Name: "Returns merged metrics server configuration correctly",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: true,
				DefaultMergedMetricsPort:    "12345",
			},
			ExpectedMergedMetricsPort:  "12345",
			ExpectedServiceMetricsPort: "1234",
			ExpectedServiceMetricsPath: "/metrics",
		},
		{
			Name: "Returns an error when merged metrics server shouldn't run",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "0"
				return pod
			},
			MetricsConfig: MetricsConfig{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: false,
			},
			ExpErr: "metrics merging should be enabled in order to return the metrics server configuration",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			metricsPorts, err := mc.mergedMetricsServerConfiguration(*tt.Pod(minimal()))

			if tt.ExpErr != "" {
				require.Equal(tt.ExpErr, err.Error())
			} else {
				require.NoError(err)
				require.Equal(tt.ExpectedMergedMetricsPort, metricsPorts.mergedPort)
				require.Equal(tt.ExpectedServiceMetricsPort, metricsPorts.servicePort)
				require.Equal(tt.ExpectedServiceMetricsPath, metricsPorts.servicePath)
			}
		})
	}
}

func minimal() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaces.DefaultNamespace,
			Name:      "minimal",
			Annotations: map[string]string{
				annotationService: "foo",
			},
		},

		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
				{
					Name: "web-side",
				},
			},
		},
	}
}
