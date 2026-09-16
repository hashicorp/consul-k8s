// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMetricsConfigEnableMetrics(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig Config
		Expected      bool
		Err           string
	}{
		{
			Name: "Metrics enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: Config{
				DefaultEnableMetrics: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				return pod
			},
			MetricsConfig: Config{
				DefaultEnableMetrics: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetrics] = "not-a-bool"
				return pod
			},
			MetricsConfig: Config{
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

			actual, err := mc.EnableMetrics(*tt.Pod(minimal()))

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
		MetricsConfig Config
		Expected      bool
		Err           string
	}{
		{
			Name: "Metrics merging enabled via meshWebhook",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: Config{
				DefaultEnableMetricsMerging: true,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics merging enabled via annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetricsMerging] = "true"
				return pod
			},
			MetricsConfig: Config{
				DefaultEnableMetricsMerging: false,
			},
			Expected: true,
			Err:      "",
		},
		{
			Name: "Metrics merging configured via invalid annotation",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetricsMerging] = "not-a-bool"
				return pod
			},
			MetricsConfig: Config{
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

			actual, err := mc.EnableMetricsMerging(*tt.Pod(minimal()))

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
				pod.Annotations[constants.AnnotationPort] = "1234"
				pod.Annotations[constants.AnnotationServiceMetricsPort] = "9000"
				return pod
			},
			Expected: "9000",
		},
		{
			Name: "Uses annotationPort of annotationServiceMetricsPort is not set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPort] = "1234"
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
			mc := Config{}

			actual, err := mc.ServiceMetricsPort(*tt.Pod(minimal()))

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
				pod.Annotations[constants.AnnotationServiceMetricsPath] = "/custom-metrics-path"
				return pod
			},
			Expected: "/custom-metrics-path",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := Config{}

			actual := mc.ServiceMetricsPath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

func TestMetricsConfigPrometheusScrapePath(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig Config
		Expected      string
	}{
		{
			Name: "Defaults to the meshWebhook's value",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MetricsConfig: Config{
				DefaultPrometheusScrapePath: "/default-prometheus-scrape-path",
			},
			Expected: "/default-prometheus-scrape-path",
		},
		{
			Name: "Uses annotationPrometheusScrapePath when set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPrometheusScrapePath] = "/custom-scrape-path"
				return pod
			},
			MetricsConfig: Config{
				DefaultPrometheusScrapePath: "/default-prometheus-scrape-path",
			},
			Expected: "/custom-scrape-path",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			mc := tt.MetricsConfig

			actual := mc.PrometheusScrapePath(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

// This test only needs unique cases not already handled in tests for
// h.EnableMetrics, h.EnableMetricsMerging, and h.ServiceMetricsPort.
func TestMetricsConfigShouldRunMergedMetricsServer(t *testing.T) {
	cases := []struct {
		Name          string
		Pod           func(*corev1.Pod) *corev1.Pod
		MetricsConfig Config
		Expected      bool
	}{
		{
			Name: "Returns true when metrics and metrics merging are enabled, and the service metrics port is greater than 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPort] = "1234"
				return pod
			},
			MetricsConfig: Config{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: true,
			},
			Expected: true,
		},
		{
			Name: "Returns false when service metrics port is 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPort] = "0"
				return pod
			},
			MetricsConfig: Config{
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

			actual, err := mc.ShouldRunMergedMetricsServer(*tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

// Tests MergedMetricsServerConfiguration happy path and error case not covered by other Config tests.
func TestMetricsConfigMergedMetricsServerConfiguration(t *testing.T) {
	cases := []struct {
		Name                       string
		Pod                        func(*corev1.Pod) *corev1.Pod
		MetricsConfig              Config
		ExpectedMergedMetricsPort  string
		ExpectedServiceMetricsPort string
		ExpectedServiceMetricsPath string
		ExpErr                     string
	}{
		{
			Name: "Returns merged metrics server configuration correctly",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPort] = "1234"
				return pod
			},
			MetricsConfig: Config{
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
				pod.Annotations[constants.AnnotationPort] = "0"
				return pod
			},
			MetricsConfig: Config{
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

			metricsPorts, err := mc.MergedMetricsServerConfiguration(*tt.Pod(minimal()))

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

func TestMetricsConfigServiceMetricsEndpoints(t *testing.T) {
	cases := []struct {
		Name                string
		Pod                 func(*corev1.Pod) *corev1.Pod
		Expected            []ServiceMetricsEndpoint
		ExpErr              string
		CheckLegacyBehavior bool
		ExpectedLegacyPort  string
		ExpectedLegacyPath  string
	}{
		{
			Name: "Returns nil when the annotation is not set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsPort] = "9000"
				pod.Annotations[constants.AnnotationServiceMetricsPath] = "/custom"
				return pod
			},
			Expected: nil,
		},
		{
			Name: "Returns nil when the annotation is empty",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = ""
				return pod
			},
			Expected: nil,
		},
		{
			Name: "Parses a single port with a path",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{{Port: "8080", Path: "/metrics"}},
		},
		{
			Name: "Parses multiple ports with paths",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,9090:/admin/metrics,7070:/q/metrics"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/admin/metrics"},
				{Port: "7070", Path: "/q/metrics"},
			},
		},
		{
			Name: "Path is optional and defaults to /metrics",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080,9090:/admin"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/admin"},
			},
		},
		{
			Name: "Whitespace and empty entries are tolerated",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = " 8080 : /metrics , , 9090 "
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/metrics"},
			},
		},
		{
			Name: "Named container ports are resolved",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Spec.Containers[0].Ports = []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{Name: "admin", ContainerPort: 9090},
				}
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "http,admin:/admin/metrics"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/admin/metrics"},
			},
		},
		{
			Name: "Privileged ports are allowed",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "80:/metrics"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{{Port: "80", Path: "/metrics"}},
		},
		{
			Name: "Same port with different paths is kept",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,8080:/other"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "8080", Path: "/other"},
			},
		},
		{
			Name: "Exact duplicates are de-duplicated",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,8080:/metrics,9090"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/metrics"},
			},
		},
		{
			Name: "Takes precedence over service-metrics-port and service-metrics-path",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationPort] = "1234"
				pod.Annotations[constants.AnnotationServiceMetricsPort] = "9000"
				pod.Annotations[constants.AnnotationServiceMetricsPath] = "/ignored"
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,9090"
				return pod
			},
			Expected: []ServiceMetricsEndpoint{
				{Port: "8080", Path: "/metrics"},
				{Port: "9090", Path: "/metrics"},
			},
		},
		{
			Name: "Does not disturb ServiceMetricsPort and ServiceMetricsPath",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsPort] = "9000"
				pod.Annotations[constants.AnnotationServiceMetricsPath] = "/legacy"
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080"
				return pod
			},
			Expected:            []ServiceMetricsEndpoint{{Port: "8080", Path: "/metrics"}},
			ExpectedLegacyPort:  "9000",
			ExpectedLegacyPath:  "/legacy",
			CheckLegacyBehavior: true,
		},
		{
			Name: "Errors on a non-numeric, non-named port",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,nope:/metrics"
				return pod
			},
			ExpErr: "does not have a valid port",
		},
		{
			Name: "Errors on an out of range port",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "70000:/metrics"
				return pod
			},
			ExpErr: "is not in the valid port range 1-65535",
		},
		{
			Name: "Errors on a port of 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "0:/metrics"
				return pod
			},
			ExpErr: "is not in the valid port range 1-65535",
		},
		{
			Name: "Errors on a missing port",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = ":/metrics"
				return pod
			},
			ExpErr: "is missing a port",
		},
		{
			Name: "Errors on a path without a leading slash",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:metrics"
				return pod
			},
			ExpErr: "must begin with '/'",
		},
		{
			Name: "Errors when the value contains no endpoints",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = ",,"
				return pod
			},
			ExpErr: "did not contain any endpoints",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			mc := Config{}
			pod := *tt.Pod(minimal())

			actual, err := mc.ServiceMetricsEndpoints(pod)

			if tt.ExpErr != "" {
				require.ErrorContains(t, err, tt.ExpErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)

			if tt.CheckLegacyBehavior {
				legacyPort, err := mc.ServiceMetricsPort(pod)
				require.NoError(t, err)
				require.Equal(t, tt.ExpectedLegacyPort, legacyPort)
				require.Equal(t, tt.ExpectedLegacyPath, mc.ServiceMetricsPath(pod))
			}
		})
	}
}

func TestMetricsConfigShouldRunMergedMetricsServerWithEndpoints(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Expected bool
	}{
		{
			Name: "Runs when service-metrics-endpoints is set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod.Annotations[constants.AnnotationEnableMetricsMerging] = "true"
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics,9090"
				return pod
			},
			Expected: true,
		},
		{
			Name: "Does not run when merging is disabled even with endpoints set",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod.Annotations[constants.AnnotationEnableMetricsMerging] = "false"
				pod.Annotations[constants.AnnotationServiceMetricsEndpoints] = "8080:/metrics"
				return pod
			},
			Expected: false,
		},
		{
			Name: "Does not run when no endpoints are configured",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[constants.AnnotationEnableMetrics] = "true"
				pod.Annotations[constants.AnnotationEnableMetricsMerging] = "true"
				return pod
			},
			Expected: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			mc := Config{}

			actual, err := mc.ShouldRunMergedMetricsServer(*tt.Pod(minimal()))

			require.NoError(t, err)
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func minimal() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespaces.DefaultNamespace,
			Name:      "minimal",
			Annotations: map[string]string{
				constants.AnnotationService: "foo",
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
