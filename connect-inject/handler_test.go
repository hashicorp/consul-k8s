package connectinject

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deckarep/golang-set"
	"github.com/hashicorp/go-hclog"
	"github.com/mattbaird/jsonpatch"
	"github.com/stretchr/testify/require"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestHandlerHandle(t *testing.T) {
	basicSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			corev1.Container{
				Name: "web",
			},
		},
	}

	cases := []struct {
		Name    string
		Handler Handler
		Req     v1beta1.AdmissionRequest
		Err     string // expected error string, not exact
		Patches []jsonpatch.JsonPatchOperation
	}{
		{
			"kube-system namespace",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Namespace: metav1.NamespaceSystem,
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
			},
			"",
			nil,
		},

		{
			"already injected",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationStatus: injected,
						},
					},

					Spec: basicSpec,
				}),
			},
			"",
			nil,
		},

		{
			"empty pod basic",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
				}),
			},
			"",
			[]jsonpatch.JsonPatchOperation{
				{
					Operation: "add",
					Path:      "/metadata/annotations",
				},
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"pod with upstreams specified",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationUpstreams: "echo:1234,db:1234",
						},
					},

					Spec: basicSpec,
				}),
			},
			"",
			[]jsonpatch.JsonPatchOperation{
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationService),
				},
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env/-",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"empty pod with injection disabled",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationInject: "false",
						},
					},

					Spec: basicSpec,
				}),
			},
			"",
			nil,
		},

		{
			"empty pod with injection truthy",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationInject: "t",
						},
					},

					Spec: basicSpec,
				}),
			},
			"",
			[]jsonpatch.JsonPatchOperation{
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationService),
				},
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"empty pod basic",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationService: "foo",
						},
					},
				}),
			},
			"",
			[]jsonpatch.JsonPatchOperation{
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"pod with existing label",
			Handler{
				Log:                   hclog.Default().Named("handler"),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
			},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"testLabel": "123",
						},
					},

					Spec: basicSpec,
				}),
			},
			"",
			[]jsonpatch.JsonPatchOperation{
				{
					Operation: "add",
					Path:      "/metadata/annotations",
				},
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/-",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels/" + escapeJSONPointer(labelInject),
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			resp := tt.Handler.Mutate(&tt.Req)
			if (tt.Err == "") != resp.Allowed {
				t.Fatalf("allowed: %v, expected err: %v", resp.Allowed, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(resp.Result.Message, tt.Err)
				return
			}

			var actual []jsonpatch.JsonPatchOperation
			if len(resp.Patch) > 0 {
				require.NoError(json.Unmarshal(resp.Patch, &actual))
				for i, _ := range actual {
					actual[i].Value = nil
				}
			}
			require.Equal(tt.Patches, actual)
		})
	}
}

// Test that we error out if the protocol annotation is set.
func TestHandler_ErrorsOnProtocolAnnotations(t *testing.T) {
	require := require.New(t)
	handler := Handler{
		Log:                   hclog.Default().Named("handler"),
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSet(),
	}

	request := v1beta1.AdmissionRequest{
		Namespace: "default",
		Object: encodeRaw(t, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotationProtocol: "http",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "web",
					},
				},
			},
		}),
	}

	response := handler.Mutate(&request)
	require.False(response.Allowed)
	require.Equal(response.Result.Message, "Error validating pod: the \"consul.hashicorp.com/connect-service-protocol\" annotation is no longer supported. Instead, create a ServiceDefaults resource (see www.consul.io/docs/k8s/crds/upgrade-to-crds)")
}

// Test that an incorrect content type results in an error.
func TestHandlerHandle_badContentType(t *testing.T) {
	req, err := http.NewRequest("POST", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")

	h := Handler{
		Log:                   hclog.Default().Named("handler"),
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSet(),
	}
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "content-type")
}

// Test that no body results in an error
func TestHandlerHandle_noBody(t *testing.T) {
	req, err := http.NewRequest("POST", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	h := Handler{
		Log:                   hclog.Default().Named("handler"),
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSet(),
	}
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "body")
}

func TestHandlerDefaultAnnotations(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      *corev1.Pod
		Expected map[string]string
		Err      string
	}{
		{
			"empty",
			&corev1.Pod{},
			nil,
			"",
		},

		{
			"basic pod, no ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				annotationService: "web",
			},
			"",
		},

		{
			"basic pod, name annotated",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				annotationService: "foo",
			},
			"",
		},

		{
			"basic pod, with ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
							Ports: []corev1.ContainerPort{
								corev1.ContainerPort{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				annotationService: "web",
				annotationPort:    "http",
			},
			"",
		},

		{
			"basic pod, with unnamed ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
							Ports: []corev1.ContainerPort{
								corev1.ContainerPort{
									ContainerPort: 8080,
								},
							},
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				annotationService: "web",
				annotationPort:    "8080",
			},
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var h Handler
			var patches []jsonpatch.JsonPatchOperation
			err := h.defaultAnnotations(tt.Pod, &patches)
			if (tt.Err != "") != (err != nil) {
				t.Fatalf("actual: %v, expected err: %v", err, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(err.Error(), tt.Err)
				return
			}

			actual := tt.Pod.Annotations
			if len(actual) == 0 {
				actual = nil
			}
			require.Equal(tt.Expected, actual)
		})
	}
}

func minimal() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "minimal",
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

func TestHandlerEnableMetrics(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Handler  Handler
		Expected bool
		Err      string
	}{
		{
			Name: "Metrics enabled via handler",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Handler: Handler{
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
			Handler: Handler{
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
			Handler: Handler{
				DefaultEnableMetrics: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-metrics annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Handler

			actual, err := h.enableMetrics(tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestHandlerEnableMetricsMerging(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Handler  Handler
		Expected bool
		Err      string
	}{
		{
			Name: "Metrics merging enabled via handler",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Handler: Handler{
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
			Handler: Handler{
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
			Handler: Handler{
				DefaultEnableMetricsMerging: false,
			},
			Expected: false,
			Err:      "consul.hashicorp.com/enable-metrics-merging annotation value of not-a-bool was invalid: strconv.ParseBool: parsing \"not-a-bool\": invalid syntax",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Handler

			actual, err := h.enableMetricsMerging(tt.Pod(minimal()))

			if tt.Err == "" {
				require.Equal(tt.Expected, actual)
				require.NoError(err)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

func TestHandlerServiceMetricsPort(t *testing.T) {
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
			h := Handler{}

			actual, err := h.serviceMetricsPort(tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

func TestHandlerServiceMetricsPath(t *testing.T) {
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
			h := Handler{}

			actual := h.serviceMetricsPath(tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

func TestHandlerPrometheusScrapePath(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Handler  Handler
		Expected string
	}{
		{
			Name: "Defaults to the handler's value",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Handler: Handler{
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
			Handler: Handler{
				DefaultPrometheusScrapePath: "/default-prometheus-scrape-path",
			},
			Expected: "/custom-scrape-path",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Handler

			actual := h.prometheusScrapePath(tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
		})
	}
}

func TestHandlerPrometheusAnnotations(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Handler  Handler
		Expected map[string]string
	}{
		{
			Name: "Returns the correct prometheus annotations",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Handler: Handler{
				DefaultEnableMetrics:        true,
				DefaultPrometheusScrapePort: "20200",
				DefaultPrometheusScrapePath: "/metrics",
			},
			Expected: map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "20200",
				"prometheus.io/path":   "/metrics",
			},
		},
		{
			Name: "Returns nil if metrics are not enabled",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			Handler: Handler{
				DefaultEnableMetrics:        false,
				DefaultPrometheusScrapePort: "20200",
				DefaultPrometheusScrapePath: "/metrics",
			},
			Expected: nil,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Handler

			actual, err := h.prometheusAnnotations(tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

// This test only needs unique cases not already handled in tests for
// h.enableMetrics, h.enableMetricsMerging, and h.serviceMetricsPort.
func TestHandlerShouldRunMergedMetricsServer(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      func(*corev1.Pod) *corev1.Pod
		Handler  Handler
		Expected bool
	}{
		{
			Name: "Returns true when metrics and metrics merging are enabled, and the service metrics port is greater than 0",
			Pod: func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			Handler: Handler{
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
			Handler: Handler{
				DefaultEnableMetrics:        true,
				DefaultEnableMetricsMerging: true,
			},
			Expected: false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Handler

			actual, err := h.shouldRunMergedMetricsServer(tt.Pod(minimal()))

			require.Equal(tt.Expected, actual)
			require.NoError(err)
		})
	}
}

// Tests determineAndValidatePort, which in turn tests the
// prometheusScrapePort() and mergedMetricsPort() functions because their logic
// is just to call out to determineAndValidatePort().
func TestHandlerDetermineAndValidatePort(t *testing.T) {
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

			actual, err := determineAndValidatePort(tt.Pod(minimal()), tt.Annotation, tt.DefaultPort, tt.Privileged)

			if tt.Err == "" {
				require.NoError(err)
				require.Equal(tt.Expected, actual)
			} else {
				require.EqualError(err, tt.Err)
			}
		})
	}
}

// Test portValue function
func TestHandlerPortValue(t *testing.T) {
	cases := []struct {
		Name     string
		Pod      *corev1.Pod
		Value    string
		Expected int32
		Err      string
	}{
		{
			"empty",
			&corev1.Pod{},
			"",
			0,
			"strconv.ParseInt: parsing \"\": invalid syntax",
		},

		{
			"basic pod, with ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
							Ports: []corev1.ContainerPort{
								corev1.ContainerPort{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			"http",
			int32(8080),
			"",
		},

		{
			"basic pod, with unnamed ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						corev1.Container{
							Name: "web",
							Ports: []corev1.ContainerPort{
								corev1.ContainerPort{
									ContainerPort: 8080,
								},
							},
						},

						corev1.Container{
							Name: "web-side",
						},
					},
				},
			},
			"8080",
			int32(8080),
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			port, err := portValue(tt.Pod, tt.Value)
			if (tt.Err != "") != (err != nil) {
				t.Fatalf("actual: %v, expected err: %v", err, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(err.Error(), tt.Err)
				return
			}

			require.Equal(tt.Expected, port)
		})
	}
}

// Test consulNamespace function
func TestConsulNamespace(t *testing.T) {
	cases := []struct {
		Name                       string
		EnableNamespaces           bool
		ConsulDestinationNamespace string
		EnableK8SNSMirroring       bool
		K8SNSMirroringPrefix       string
		K8sNamespace               string
		Expected                   string
	}{
		{
			"namespaces disabled",
			false,
			"default",
			false,
			"",
			"namespace",
			"",
		},

		{
			"namespaces disabled, mirroring enabled",
			false,
			"default",
			true,
			"",
			"namespace",
			"",
		},

		{
			"namespaces disabled, mirroring enabled, prefix defined",
			false,
			"default",
			true,
			"test-",
			"namespace",
			"",
		},

		{
			"namespaces enabled, mirroring disabled",
			true,
			"default",
			false,
			"",
			"namespace",
			"default",
		},

		{
			"namespaces enabled, mirroring disabled, prefix defined",
			true,
			"default",
			false,
			"test-",
			"namespace",
			"default",
		},

		{
			"namespaces enabled, mirroring enabled",
			true,
			"default",
			true,
			"",
			"namespace",
			"namespace",
		},

		{
			"namespaces enabled, mirroring enabled, prefix defined",
			true,
			"default",
			true,
			"test-",
			"namespace",
			"test-namespace",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := Handler{
				EnableNamespaces:           tt.EnableNamespaces,
				ConsulDestinationNamespace: tt.ConsulDestinationNamespace,
				EnableK8SNSMirroring:       tt.EnableK8SNSMirroring,
				K8SNSMirroringPrefix:       tt.K8SNSMirroringPrefix,
			}

			ns := h.consulNamespace(tt.K8sNamespace)

			require.Equal(tt.Expected, ns)
		})
	}
}

// Test shouldInject function
func TestShouldInject(t *testing.T) {
	cases := []struct {
		Name                  string
		Pod                   *corev1.Pod
		K8sNamespace          string
		EnableNamespaces      bool
		AllowK8sNamespacesSet mapset.Set
		DenyK8sNamespacesSet  mapset.Set
		Expected              bool
	}{
		{
			"kube-system not injected",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						// Service annotation is required for injection
						annotationService: "testing",
					},
				},
			},
			"kube-system",
			false,
			mapset.NewSet(),
			mapset.NewSet(),
			false,
		},
		{
			"kube-public not injected",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"kube-public",
			false,
			mapset.NewSet(),
			mapset.NewSet(),
			false,
		},
		{
			"namespaces disabled, empty allow/deny lists",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSet(),
			mapset.NewSet(),
			false,
		},
		{
			"namespaces disabled, allow *",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("*"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces disabled, allow default",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("default"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces disabled, allow * and default",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("*", "default"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces disabled, allow only ns1 and ns2",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("ns1", "ns2"),
			mapset.NewSet(),
			false,
		},
		{
			"namespaces disabled, deny default ns",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSet(),
			mapset.NewSetWith("default"),
			false,
		},
		{
			"namespaces disabled, allow *, deny default ns",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("*"),
			mapset.NewSetWith("default"),
			false,
		},
		{
			"namespaces disabled, default ns in both allow and deny lists",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			false,
			mapset.NewSetWith("default"),
			mapset.NewSetWith("default"),
			false,
		},
		{
			"namespaces enabled, empty allow/deny lists",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSet(),
			mapset.NewSet(),
			false,
		},
		{
			"namespaces enabled, allow *",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("*"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces enabled, allow default",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("default"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces enabled, allow * and default",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("*", "default"),
			mapset.NewSet(),
			true,
		},
		{
			"namespaces enabled, allow only ns1 and ns2",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("ns1", "ns2"),
			mapset.NewSet(),
			false,
		},
		{
			"namespaces enabled, deny default ns",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSet(),
			mapset.NewSetWith("default"),
			false,
		},
		{
			"namespaces enabled, allow *, deny default ns",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("*"),
			mapset.NewSetWith("default"),
			false,
		},
		{
			"namespaces enabled, default ns in both allow and deny lists",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "testing",
					},
				},
			},
			"default",
			true,
			mapset.NewSetWith("default"),
			mapset.NewSetWith("default"),
			false,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := Handler{
				RequireAnnotation:     false,
				EnableNamespaces:      tt.EnableNamespaces,
				AllowK8sNamespacesSet: tt.AllowK8sNamespacesSet,
				DenyK8sNamespacesSet:  tt.DenyK8sNamespacesSet,
			}

			injected, err := h.shouldInject(tt.Pod, tt.K8sNamespace)

			require.Equal(nil, err)
			require.Equal(tt.Expected, injected)
		})
	}
}

// encodeRaw is a helper to encode some data into a RawExtension.
func encodeRaw(t *testing.T, input interface{}) runtime.RawExtension {
	data, err := json.Marshal(input)
	require.NoError(t, err)
	return runtime.RawExtension{Raw: data}
}
