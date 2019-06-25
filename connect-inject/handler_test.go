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
			Handler{Log: hclog.Default().Named("handler")},
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
			Handler{Log: hclog.Default().Named("handler")},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationStatus: "injected",
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
			Handler{Log: hclog.Default().Named("handler")},
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
			},
		},

		{
			"pod with upstreams specified",
			Handler{Log: hclog.Default().Named("handler")},
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
			},
		},

		{
			"empty pod with injection disabled",
			Handler{Log: hclog.Default().Named("handler")},
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
			Handler{Log: hclog.Default().Named("handler")},
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
			},
		},

		{
			"empty pod basic, no default protocol",
			Handler{WriteServiceDefaults: true, DefaultProtocol: "", Log: hclog.Default().Named("handler")},
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
			},
		},

		{
			"empty pod basic, protocol in annotation",
			Handler{WriteServiceDefaults: true, Log: hclog.Default().Named("handler")},
			v1beta1.AdmissionRequest{
				Object: encodeRaw(t, &corev1.Pod{
					Spec: basicSpec,
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotationService:  "foo",
							annotationProtocol: "grpc",
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
			},
		},

		{
			"empty pod basic, default protocol specified",
			Handler{WriteServiceDefaults: true, DefaultProtocol: "http", Log: hclog.Default().Named("handler")},
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
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationProtocol),
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
			},
		},

		{
			"enable resources",
			Handler{Resources: true, CPULimit: "1", MemoryLimit: "100Mi", CPURequest: "1", MemoryRequest: "100Mi", Log: hclog.Default().Named("handler")},
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
					Path:      "/metadata/annotations/" + escapeJSONPointer(annotationStatus),
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

// Test that an incorrect content type results in an error.
func TestHandlerHandle_badContentType(t *testing.T) {
	req, err := http.NewRequest("POST", "/", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")

	h := Handler{Log: hclog.Default().Named("handler")}
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

	h := Handler{Log: hclog.Default().Named("handler")}
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

		{
			"basic pod, protocol annotated",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationProtocol: "http",
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
				annotationService:  "web",
				annotationProtocol: "http",
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
			"namespaces disabled",
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
			true,
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
