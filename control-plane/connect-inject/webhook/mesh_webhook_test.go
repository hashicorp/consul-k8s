// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul-k8s/version"
)

func TestHandlerHandle(t *testing.T) {
	t.Parallel()
	basicSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name: "web",
			},
		},
	}
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	cases := []struct {
		Name    string
		Webhook MeshWebhook
		Req     admission.Request
		Err     string // expected error string, not exact
		Patches []jsonpatch.Operation
	}{
		{
			"kube-system namespace",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: metav1.NamespaceSystem,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
				},
			},
			"",
			nil,
		},

		{
			"already injected",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.KeyInjectStatus: constants.Injected,
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			nil,
		},

		{
			"empty pod basic",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
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
					Path:      "/spec/containers/1",
				},
			},
		},
		{
			"empty pod basic with lifecycle",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
				LifecycleConfig:       lifecycle.Config{DefaultEnableProxyLifecycle: true},
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
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
					Path:      "/spec/containers/1",
				},

				{
					Operation: "add",
					Path:      "/spec/containers/0/readinessProbe",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/securityContext",
				},
				{
					Operation: "replace",
					Path:      "/spec/containers/0/name",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/args",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/volumeMounts",
				},
			},
		},

		{
			"pod with upstreams specified",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationUpstreams: "echo:1234,db:1234",
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env",
				},
			},
		},

		{
			"empty pod with injection disabled",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationInject: "false",
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			nil,
		},

		{
			"empty pod with injection truthy",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationInject: "t",
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"pod with empty volume mount annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationInjectMountVolumes: "",
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"pod with volume mount annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationInjectMountVolumes: "web,unknown,web_three_point_oh",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "web",
								},
								{
									Name: "web_two_point_oh",
								},
								{
									Name: "web_three_point_oh",
								},
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/volumeMounts",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/2/volumeMounts",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/3",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"pod with sidecar volume mount annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationConsulSidecarUserVolume: "[{\"name\":\"bbb\",\"csi\":{\"driver\":\"bob\"}}]",
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
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"pod with sidecar invalid volume mount annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationConsulSidecarUserVolume: "[a]",
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
				},
			},
			"error unmarshalling sidecar user volumes: invalid character 'a' looking for beginning of value",
			nil,
		},
		{
			"pod with service annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationService: "foo",
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},

		{
			"pod with existing label",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"testLabel": "123",
							},
						},
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations",
				},
				{
					Operation: "add",
					Path:      "/metadata/labels/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels/" + escapeJSONPointer(constants.KeyManagedBy),
				},
			},
		},
		{
			"tproxy with overwriteProbes is enabled",
			MeshWebhook{
				Log:                    logrtest.New(t),
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSet(),
				EnableTransparentProxy: true,
				TProxyOverwriteProbes:  true,
				decoder:                decoder,
				Clientset:              defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
							// We're setting an existing annotation so that we can assert on the
							// specific annotations that are set as a result of probes being overwritten.
							Annotations: map[string]string{"foo": "bar"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "web",
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Port: intstr.FromInt(8080),
											},
										},
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Port: intstr.FromInt(8081),
											},
										},
									},
								},
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyTransparentProxyStatus),
				},

				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "replace",
					Path:      "/spec/containers/0/livenessProbe/httpGet/port",
				},
				{
					Operation: "replace",
					Path:      "/spec/containers/0/readinessProbe/httpGet/port",
				},
			},
		},
		{
			"multiport pod kube < 1.24 with AuthMethod, serviceaccount has secret ref",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             testClientWithServiceAccountAndSecretRefs(),
				AuthMethod:            "k8s",
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationService: "web,web-admin",
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/2",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"multiport pod kube 1.24 with AuthMethod, serviceaccount does not have secret ref",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             testClientWithServiceAccountAndSecrets(),
				AuthMethod:            "k8s",
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationService: "web,web-admin",
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/2",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"multiport pod kube < 1.24 with AuthMethod, serviceaccount has secret ref, lifecycle enabled",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             testClientWithServiceAccountAndSecretRefs(),
				AuthMethod:            "k8s",
				LifecycleConfig:       lifecycle.Config{DefaultEnableProxyLifecycle: true},
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								constants.AnnotationService: "web,web-admin",
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/spec/containers/0/env",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/volumeMounts",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/readinessProbe",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/securityContext",
				},
				{
					Operation: "replace",
					Path:      "/spec/containers/0/name",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/args",
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/2",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
			},
		},
		{
			"dns redirection enabled",
			MeshWebhook{
				Log:                    logrtest.New(t),
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSet(),
				EnableTransparentProxy: true,
				TProxyOverwriteProbes:  true,
				decoder:                decoder,
				Clientset:              defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels:      map[string]string{},
							Annotations: map[string]string{constants.KeyConsulDNS: "true"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "web",
								},
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyTransparentProxyStatus),
				},

				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/spec/dnsPolicy",
				},
				{
					Operation: "add",
					Path:      "/spec/dnsConfig",
				},
			},
		},
		{
			"dns redirection only enabled if tproxy enabled",
			MeshWebhook{
				Log:                    logrtest.New(t),
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSet(),
				EnableTransparentProxy: true,
				TProxyOverwriteProbes:  true,
				decoder:                decoder,
				Clientset:              defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
							Annotations: map[string]string{
								constants.KeyConsulDNS:        "true",
								constants.KeyTransparentProxy: "false",
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
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				// Note: no DNS policy/config additions.
			},
		},

		{
			"empty pod basic with native sidecars",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
				NativeSidecarsEnabled: true,
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						Spec: basicSpec,
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
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
			},
		},

		{
			"empty pod basic with native sidecars annotation",
			MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				Clientset:             defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
							Annotations: map[string]string{
								constants.AnnotationNativeSidecarsEnabled: "true",
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
				},
			},
			"",
			[]jsonpatch.Operation{
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/spec/volumes",
				},
				{
					Operation: "add",
					Path:      "/spec/initContainers",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			tt.Webhook.ConsulConfig = &consul.Config{HTTPPort: 8500}
			ctx := context.Background()
			resp := tt.Webhook.Handle(ctx, tt.Req)
			if (tt.Err == "") != resp.Allowed {
				t.Fatalf("allowed: %v, expected err: %v", resp.Allowed, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(t, resp.Result.Message, tt.Err)
				return
			}

			actual := resp.Patches
			if len(actual) > 0 {
				for i := range actual {
					actual[i].Value = nil
				}
			}
			require.ElementsMatch(t, tt.Patches, actual)
		})
	}
}

// This test validates that overwrite probes match the iptables configuration fromiptablesConfigJSON()
// Because they happen at different points in the injection, the port numbers can get out of sync.
func TestHandlerHandle_ValidateOverwriteProbes(t *testing.T) {
	t.Parallel()
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	cases := []struct {
		Name    string
		Webhook MeshWebhook
		Req     admission.Request
		Err     string // expected error string, not exact
		Patches []jsonpatch.Operation
	}{
		{
			"tproxy with overwriteProbes is enabled",
			MeshWebhook{
				Log:                    logrtest.New(t),
				AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:   mapset.NewSet(),
				EnableTransparentProxy: true,
				TProxyOverwriteProbes:  true,
				LifecycleConfig:        lifecycle.Config{DefaultEnableProxyLifecycle: true},
				decoder:                decoder,
				Clientset:              defaultTestClientWithNamespace(),
			},
			admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Namespace: namespaces.DefaultNamespace,
					Object: encodeRaw(t, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{},
							// We're setting an existing annotation so that we can assert on the
							// specific annotations that are set as a result of probes being overwritten.
							Annotations: map[string]string{"foo": "bar"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "web",
									LivenessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Port: intstr.FromInt(8080),
											},
										},
									},
									ReadinessProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Port: intstr.FromInt(8081),
											},
										},
									},
									StartupProbe: &corev1.Probe{
										ProbeHandler: corev1.ProbeHandler{
											HTTPGet: &corev1.HTTPGetAction{
												Port: intstr.FromInt(8082),
											},
										},
									},
								},
							},
						},
					}),
				},
			},
			"",
			[]jsonpatch.Operation{
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
					Path:      "/spec/containers/1",
				},
				{
					Operation: "replace",
					Path:      "/spec/containers/0/name",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/args",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/env",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/volumeMounts",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/readinessProbe/tcpSocket",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/readinessProbe/initialDelaySeconds",
				},
				{
					Operation: "remove",
					Path:      "/spec/containers/0/readinessProbe/httpGet",
				},
				{
					Operation: "add",
					Path:      "/spec/containers/0/securityContext",
				},
				{
					Operation: "remove",
					Path:      "/spec/containers/0/startupProbe",
				},
				{
					Operation: "remove",
					Path:      "/spec/containers/0/livenessProbe",
				},
				{
					Operation: "add",
					Path:      "/metadata/labels",
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyInjectStatus),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.KeyTransparentProxyStatus),
				},

				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationOriginalPod),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.LegacyAnnotationConsulK8sVersion),
				},
				{
					Operation: "add",
					Path:      "/metadata/annotations/" + escapeJSONPointer(constants.AnnotationConsulK8sVersion),
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			tt.Webhook.ConsulConfig = &consul.Config{HTTPPort: 8500}
			ctx := context.Background()
			resp := tt.Webhook.Handle(ctx, tt.Req)
			if (tt.Err == "") != resp.Allowed {
				t.Fatalf("allowed: %v, expected err: %v", resp.Allowed, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(t, resp.Result.Message, tt.Err)
				return
			}

			var iptablesCfg iptables.Config
			var overwritePorts []string
			actual := resp.Patches
			if len(actual) > 0 {
				for i := range actual {

					// We want to grab the iptables configuration from the connect-init container's
					// environment.
					if actual[i].Path == "/spec/initContainers" {
						value := actual[i].Value.([]any)
						valueMap := value[0].(map[string]any)
						envs := valueMap["env"].([]any)
						redirectEnv := envs[8].(map[string]any)
						require.Equal(t, redirectEnv["name"].(string), "CONSUL_REDIRECT_TRAFFIC_CONFIG")
						iptablesJson := redirectEnv["value"].(string)

						err := json.Unmarshal([]byte(iptablesJson), &iptablesCfg)
						require.NoError(t, err)
					}

					// We want to accumulate the httpGet Probes from the application container to
					// compare them to the iptables rules. This is now the second container in the spec
					if strings.Contains(actual[i].Path, "/spec/containers/1") {
						valueMap, ok := actual[i].Value.(map[string]any)
						require.True(t, ok)

						for k, v := range valueMap {
							if strings.Contains(k, "Probe") {
								probe := v.(map[string]any)
								httpProbe := probe["httpGet"]
								httpProbeMap := httpProbe.(map[string]any)
								port := httpProbeMap["port"]
								portNum := port.(float64)

								overwritePorts = append(overwritePorts, strconv.Itoa(int(portNum)))
							}
						}
					}

					// nil out all the patch values to just compare the keys changing.
					actual[i].Value = nil
				}
			}
			// Make sure the iptables excluded ports match the ports on the container
			require.ElementsMatch(t, iptablesCfg.ExcludeInboundPorts, overwritePorts)
			require.ElementsMatch(t, tt.Patches, actual)
		})
	}
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
			map[string]string{
				constants.AnnotationOriginalPod:            "{\"metadata\":{\"creationTimestamp\":null},\"spec\":{\"containers\":null},\"status\":{}}",
				constants.LegacyAnnotationConsulK8sVersion: version.GetHumanVersion(),
				constants.AnnotationConsulK8sVersion:       version.GetHumanVersion(),
			},
			"",
		},

		{
			"basic pod, no ports",
			&corev1.Pod{
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
			},
			map[string]string{
				constants.AnnotationOriginalPod:            "{\"metadata\":{\"creationTimestamp\":null},\"spec\":{\"containers\":[{\"name\":\"web\",\"resources\":{}},{\"name\":\"web-side\",\"resources\":{}}]},\"status\":{}}",
				constants.LegacyAnnotationConsulK8sVersion: version.GetHumanVersion(),
				constants.AnnotationConsulK8sVersion:       version.GetHumanVersion(),
			},
			"",
		},

		{
			"basic pod, name annotated",
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
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
			},
			map[string]string{
				"consul.hashicorp.com/connect-service":     "foo",
				constants.AnnotationOriginalPod:            "{\"metadata\":{\"creationTimestamp\":null,\"annotations\":{\"consul.hashicorp.com/connect-service\":\"foo\"}},\"spec\":{\"containers\":[{\"name\":\"web\",\"resources\":{}},{\"name\":\"web-side\",\"resources\":{}}]},\"status\":{}}",
				constants.LegacyAnnotationConsulK8sVersion: version.GetHumanVersion(),
				constants.AnnotationConsulK8sVersion:       version.GetHumanVersion(),
			},

			"",
		},

		{
			"basic pod, with ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
								},
							},
						},
						{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				constants.AnnotationPort:                   "http",
				constants.AnnotationOriginalPod:            "{\"metadata\":{\"creationTimestamp\":null},\"spec\":{\"containers\":[{\"name\":\"web\",\"ports\":[{\"name\":\"http\",\"containerPort\":8080}],\"resources\":{}},{\"name\":\"web-side\",\"resources\":{}}]},\"status\":{}}",
				constants.LegacyAnnotationConsulK8sVersion: version.GetHumanVersion(),
				constants.AnnotationConsulK8sVersion:       version.GetHumanVersion(),
			},
			"",
		},

		{
			"basic pod, with unnamed ports",
			&corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
								},
							},
						},
						{
							Name: "web-side",
						},
					},
				},
			},
			map[string]string{
				constants.AnnotationPort:                   "8080",
				constants.AnnotationOriginalPod:            "{\"metadata\":{\"creationTimestamp\":null},\"spec\":{\"containers\":[{\"name\":\"web\",\"ports\":[{\"containerPort\":8080}],\"resources\":{}},{\"name\":\"web-side\",\"resources\":{}}]},\"status\":{}}",
				constants.LegacyAnnotationConsulK8sVersion: version.GetHumanVersion(),
				constants.AnnotationConsulK8sVersion:       version.GetHumanVersion(),
			},
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			podJson, err := json.Marshal(tt.Pod)
			require.NoError(t, err)

			var w MeshWebhook
			err = w.defaultAnnotations(tt.Pod, string(podJson))
			if (tt.Err != "") != (err != nil) {
				t.Fatalf("actual: %v, expected err: %v", err, tt.Err)
			}
			if tt.Err != "" {
				require.Contains(t, err.Error(), tt.Err)
				return
			}

			actual := tt.Pod.Annotations
			if len(actual) == 0 {
				actual = nil
			}
			require.Equal(t, tt.Expected, actual)
		})
	}
}

func TestHandlerPrometheusAnnotations(t *testing.T) {
	cases := []struct {
		Name     string
		Webhook  MeshWebhook
		Expected map[string]string
	}{
		{
			Name: "Sets the correct prometheus annotations on the pod if metrics are enabled",
			Webhook: MeshWebhook{
				MetricsConfig: metrics.Config{
					DefaultEnableMetrics:        true,
					DefaultPrometheusScrapePort: "20200",
					DefaultPrometheusScrapePath: "/metrics",
				},
			},
			Expected: map[string]string{
				constants.AnnotationPrometheusScrape: "true",
				constants.AnnotationPrometheusPort:   "20200",
				constants.AnnotationPrometheusPath:   "/metrics",
			},
		},
		{
			Name: "Does not set annotations if metrics are not enabled",
			Webhook: MeshWebhook{
				MetricsConfig: metrics.Config{
					DefaultEnableMetrics:        false,
					DefaultPrometheusScrapePort: "20200",
					DefaultPrometheusScrapePath: "/metrics",
				},
			},
			Expected: map[string]string{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)
			h := tt.Webhook
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

			err := h.prometheusAnnotations(pod)
			require.NoError(err)

			require.Equal(pod.Annotations, tt.Expected)
		})
	}
}

// Test consulNamespace function.
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

			w := MeshWebhook{
				EnableNamespaces:           tt.EnableNamespaces,
				ConsulDestinationNamespace: tt.ConsulDestinationNamespace,
				EnableK8SNSMirroring:       tt.EnableK8SNSMirroring,
				K8SNSMirroringPrefix:       tt.K8SNSMirroringPrefix,
			}

			ns := w.consulNamespace(tt.K8sNamespace)

			require.Equal(tt.Expected, ns)
		})
	}
}

// Test shouldInject function.
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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
						constants.AnnotationService: "testing",
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

			w := MeshWebhook{
				RequireAnnotation:     false,
				EnableNamespaces:      tt.EnableNamespaces,
				AllowK8sNamespacesSet: tt.AllowK8sNamespacesSet,
				DenyK8sNamespacesSet:  tt.DenyK8sNamespacesSet,
			}

			injected, err := w.shouldInject(*tt.Pod, tt.K8sNamespace)

			require.Equal(nil, err)
			require.Equal(tt.Expected, injected)
		})
	}
}

func TestOverwriteProbes(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		tproxyEnabled         bool
		overwriteProbes       bool
		podContainers         []corev1.Container
		expLivenessPort       []int
		expReadinessPort      []int
		expStartupPort        []int
		additionalAnnotations map[string]string
	}{
		"transparent proxy disabled; overwrites probes disabled": {
			tproxyEnabled: false,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			expReadinessPort: []int{8080},
		},
		"transparent proxy enabled; overwrite probes disabled": {
			tproxyEnabled: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			expReadinessPort: []int{8080},
		},
		"transparent proxy disabled; overwrite probes enabled": {
			tproxyEnabled:   false,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			expReadinessPort: []int{8080},
		},
		"just the readiness probe": {
			tproxyEnabled:   true,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
			},
			expReadinessPort: []int{exposedPathsReadinessPortsRangeStart},
		},
		"just the liveness probe": {
			tproxyEnabled:   true,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8081),
							},
						},
					},
				},
			},
			expLivenessPort: []int{exposedPathsLivenessPortsRangeStart},
		},
		"skips envoy sidecar": {
			tproxyEnabled:   true,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: sidecarContainer,
				},
			},
		},
		"readiness, liveness and startup probes": {
			tproxyEnabled:   true,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8081),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8082),
							},
						},
					},
				},
			},
			expLivenessPort:  []int{exposedPathsLivenessPortsRangeStart},
			expReadinessPort: []int{exposedPathsReadinessPortsRangeStart},
			expStartupPort:   []int{exposedPathsStartupPortsRangeStart},
		},
		"readiness, liveness and startup probes multiple containers": {
			tproxyEnabled:   true,
			overwriteProbes: true,
			podContainers: []corev1.Container{
				{
					Name: "test",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8081,
						},
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8081),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8080),
							},
						},
					},
				},
				{
					Name: "test-2",
					Ports: []corev1.ContainerPort{
						{
							Name:          "tcp",
							ContainerPort: 8083,
						},
						{
							Name:          "http",
							ContainerPort: 8082,
						},
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8083),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8082),
							},
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Port: intstr.FromInt(8082),
							},
						},
					},
				},
			},
			expLivenessPort:  []int{exposedPathsLivenessPortsRangeStart, exposedPathsLivenessPortsRangeStart + 1},
			expReadinessPort: []int{exposedPathsReadinessPortsRangeStart, exposedPathsReadinessPortsRangeStart + 1},
			expStartupPort:   []int{exposedPathsStartupPortsRangeStart, exposedPathsStartupPortsRangeStart + 1},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{},
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: c.podContainers,
				},
			}
			if c.additionalAnnotations != nil {
				pod.ObjectMeta.Annotations = c.additionalAnnotations
			}

			w := MeshWebhook{
				EnableTransparentProxy: c.tproxyEnabled,
				TProxyOverwriteProbes:  c.overwriteProbes,
			}
			err := w.overwriteProbes(corev1.Namespace{}, pod)
			require.NoError(t, err)
			for i, container := range pod.Spec.Containers {
				if container.ReadinessProbe != nil {
					require.Equal(t, c.expReadinessPort[i], container.ReadinessProbe.HTTPGet.Port.IntValue())
				}
				if container.LivenessProbe != nil {
					require.Equal(t, c.expLivenessPort[i], container.LivenessProbe.HTTPGet.Port.IntValue())
				}
				if container.StartupProbe != nil {
					require.Equal(t, c.expStartupPort[i], container.StartupProbe.HTTPGet.Port.IntValue())
				}
			}
		})
	}
}

func TestHandler_checkUnsupportedMultiPortCases(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expErr      string
	}{
		{
			name:        "tproxy",
			annotations: map[string]string{constants.KeyTransparentProxy: "true"},
			expErr:      "multi port services are not compatible with transparent proxy",
		},
		{
			name:        "metrics",
			annotations: map[string]string{constants.AnnotationEnableMetrics: "true"},
			expErr:      "multi port services are not compatible with metrics",
		},
		{
			name:        "metrics merging",
			annotations: map[string]string{constants.AnnotationEnableMetricsMerging: "true"},
			expErr:      "multi port services are not compatible with metrics merging",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := MeshWebhook{}
			pod := minimal()
			pod.Annotations = tt.annotations
			err := w.checkUnsupportedMultiPortCases(corev1.Namespace{}, *pod)
			require.Error(t, err)
			require.Equal(t, tt.expErr, err.Error())
		})
	}
}

// encodeRaw is a helper to encode some data into a RawExtension.
func encodeRaw(t *testing.T, input interface{}) runtime.RawExtension {
	data, err := json.Marshal(input)
	require.NoError(t, err)
	return runtime.RawExtension{Raw: data}
}

// https://tools.ietf.org/html/rfc6901
func escapeJSONPointer(s string) string {
	s = strings.Replace(s, "~", "~0", -1)
	s = strings.Replace(s, "/", "~1", -1)
	return s
}

func defaultTestClientWithNamespace() kubernetes.Interface {
	return clientWithNamespace("default")
}

func testClientWithServiceAccountAndSecretRefs() kubernetes.Interface {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}
	sa1 := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-admin",
			Namespace: "default",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "web-admin",
			},
		},
	}
	sa2 := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "default",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name: "web",
			},
		},
	}
	return fake.NewSimpleClientset(&ns, &sa1, &sa2)
}

func testClientWithServiceAccountAndSecrets() kubernetes.Interface {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	}
	sa1 := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-admin",
			Namespace: "default",
		},
	}
	secret1 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "web-admin",
			Namespace:   "default",
			Annotations: map[string]string{corev1.ServiceAccountNameKey: "web-admin"},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	sa2 := corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web",
			Namespace: "default",
		},
	}
	secret2 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "web",
			Annotations: map[string]string{corev1.ServiceAccountNameKey: "web"},
			Namespace:   "default",
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	return fake.NewSimpleClientset(&ns, &sa1, &sa2, &secret1, &secret2)
}

func clientWithNamespace(name string) kubernetes.Interface {
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	return fake.NewSimpleClientset(&ns)
}
