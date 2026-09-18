// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"strconv"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

func TestValidateMultiportRegistration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		disabled    bool
		annotations map[string]string
		containers  []corev1.Container
		wantError   string
	}{
		"gate enabled permits multi-port registration": {
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
		},
		"gate disabled rejects multiple values for a multi-port application": {
			disabled:    true,
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"one named value opts a multi-port application down": {
			disabled:    true,
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: "metrics"},
		},
		"one numeric value opts a multi-port application down": {
			disabled:    true,
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: "9090"},
		},
		"empty comma-separated values are ignored": {
			disabled:    true,
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: " , metrics, "},
		},
		"two non-empty values with whitespace are rejected": {
			disabled:    true,
			containers:  containersWithPortCounts(2),
			annotations: map[string]string{constants.AnnotationPort: " http, , metrics "},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"single-port application selecting two ports is gated": {
			disabled:    true,
			containers:  containersWithPortCounts(1),
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"selection is gated even without an application container": {
			disabled:    true,
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"port named on a later application container is gated": {
			disabled:    true,
			containers:  containersWithPortCounts(1, 2),
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"absent annotation with one declared port is not gated": {
			disabled:   true,
			containers: containersWithPortCounts(1),
		},
		"absent annotation with no declared port is not gated": {
			disabled:   true,
			containers: []corev1.Container{{Name: "app"}},
		},
		"absent annotation falls back to ports declared on any container": {
			disabled:   true,
			containers: append(containersWithPortCounts(0), containersWithPortCounts(2)...),
			wantError:  "multi-port Consul service registration is disabled",
		},
		"multi-port first application container is gated even if second is single-port": {
			disabled:    true,
			containers:  containersWithPortCounts(2, 1),
			annotations: map[string]string{constants.AnnotationPort: "http,metrics"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"unnamed numeric application ports are usable": {
			disabled: true,
			containers: []corev1.Container{{Name: "app", Ports: []corev1.ContainerPort{
				{ContainerPort: 8080}, {ContainerPort: 9090},
			}}},
			annotations: map[string]string{constants.AnnotationPort: "8080,9090"},
			wantError:   "multi-port Consul service registration is disabled",
		},
		"legacy multiple-service registration remains compatible": {
			disabled:   true,
			containers: containersWithPortCounts(2),
			annotations: map[string]string{
				constants.AnnotationService: "api,metrics",
				constants.AnnotationPort:    "http,metrics",
			},
		},
		"transparent proxy false does not bypass the gate": {
			disabled:   true,
			containers: containersWithPortCounts(2),
			annotations: map[string]string{
				constants.KeyTransparentProxy: "false",
				constants.AnnotationPort:      "http,metrics",
			},
			wantError: "multi-port Consul service registration is disabled",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       corev1.PodSpec{Containers: tt.containers},
			}
			webhook := MeshWebhook{DisableMultiportRegistration: tt.disabled}

			err := webhook.validateMultiportRegistration(pod)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

func TestMultiportRegistrationAdmissionOrdering(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(scheme)

	tests := map[string]struct {
		requireAnnotation bool
		annotations       map[string]string
		client            *corev1.Namespace
		wantAllowed       bool
		wantMessage       string
	}{
		"explicit injection false bypasses all later validation": {
			annotations: map[string]string{
				constants.AnnotationInject:    "false",
				constants.KeyTransparentProxy: "invalid",
				constants.AnnotationPort:      "http,metrics",
			},
			wantAllowed: true,
		},
		"injection default false bypasses all later validation": {
			requireAnnotation: true,
			annotations: map[string]string{
				constants.KeyTransparentProxy: "invalid",
				constants.AnnotationPort:      "http,metrics",
			},
			wantAllowed: true,
		},
		"transparent proxy is validated before service ports": {
			annotations: map[string]string{
				constants.AnnotationInject:    "true",
				constants.KeyTransparentProxy: "invalid",
				constants.AnnotationPort:      "http,metrics",
			},
			client:      &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			wantMessage: "couldn't check if transparent proxy is enabled",
		},
		"service ports are checked after valid transparent proxy configuration": {
			annotations: map[string]string{
				constants.AnnotationInject:    "true",
				constants.KeyTransparentProxy: "false",
				constants.AnnotationPort:      "http,metrics",
			},
			client:      &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"namespace transparent proxy configuration is validated before service ports": {
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "http,metrics",
			},
			client: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   "default",
				Labels: map[string]string{constants.KeyTransparentProxy: "invalid"},
			}},
			wantMessage: "couldn't check if transparent proxy is enabled",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var clientset *fake.Clientset
			if tt.client != nil {
				clientset = fake.NewSimpleClientset(tt.client)
			}
			webhook := MeshWebhook{
				Log:                          logrtest.New(t),
				AllowK8sNamespacesSet:        mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:         mapset.NewSet(),
				RequireAnnotation:            tt.requireAnnotation,
				DisableMultiportRegistration: true,
				Clientset:                    clientset,
				decoder:                      decoder,
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       corev1.PodSpec{Containers: containersWithPortCounts(2)},
			}
			resp := webhook.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Namespace: "default",
				Object:    encodeRaw(t, &pod),
			}})
			require.Equal(t, tt.wantAllowed, resp.Allowed)
			if tt.wantMessage != "" {
				require.Contains(t, resp.Result.Message, tt.wantMessage)
			}
		})
	}
}

func TestMultiportRegistrationFullAdmissionMatrix(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(scheme)

	tests := map[string]struct {
		disabled      bool
		annotations   map[string]string
		containers    []corev1.Container
		wantAllowed   bool
		wantMessage   string
		wantPortPatch bool
	}{
		"injection off is admitted without mutation": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "false",
				constants.AnnotationPort:   "http,metrics",
			},
			containers:  containersWithPortCounts(2),
			wantAllowed: true,
		},
		"single-port application remains injectable": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-0",
			},
			containers:    containersWithPortCounts(1),
			wantAllowed:   true,
			wantPortPatch: true,
		},
		"multi-port application with one named selection remains injectable": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-1",
			},
			containers:    containersWithPortCounts(2),
			wantAllowed:   true,
			wantPortPatch: true,
		},
		"multi-port application with one numeric selection remains injectable": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "8081",
			},
			containers:    containersWithPortCounts(2),
			wantAllowed:   true,
			wantPortPatch: true,
		},
		"multi-port registration is rejected": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-0,port-1",
			},
			containers:  containersWithPortCounts(2),
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"defaulted multi-port selection is rejected": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
			},
			containers:  containersWithPortCounts(2),
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"port named on a later container is gated": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-0,port-1",
			},
			containers:  containersWithPortCounts(1, 2),
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"first container without ports falls back to later containers": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
			},
			containers:  append(containersWithPortCounts(0), containersWithPortCounts(2)...),
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"multi-port first container triggers gate despite later container": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-0,port-1",
			},
			containers:  containersWithPortCounts(2, 1),
			wantMessage: "multi-port Consul service registration is disabled",
		},
		"enabled feature admits complete multi-port selection": {
			annotations: map[string]string{
				constants.AnnotationInject: "true",
				constants.AnnotationPort:   "port-0,port-1",
			},
			containers:    containersWithPortCounts(2),
			wantAllowed:   true,
			wantPortPatch: true,
		},
		"enabled feature admits defaulted complete multi-port selection": {
			annotations: map[string]string{
				constants.AnnotationInject: "true",
			},
			containers:    containersWithPortCounts(2),
			wantAllowed:   true,
			wantPortPatch: true,
		},
		"legacy multiple-service registration remains injectable while gate is disabled": {
			disabled: true,
			annotations: map[string]string{
				constants.AnnotationInject:  "true",
				constants.AnnotationService: "api,metrics",
				constants.AnnotationPort:    "port-0,port-1",
			},
			containers:    containersWithPortCounts(2),
			wantAllowed:   true,
			wantPortPatch: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			webhook := MeshWebhook{
				Log:                          logrtest.New(t),
				AllowK8sNamespacesSet:        mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:         mapset.NewSet(),
				DisableMultiportRegistration: tt.disabled,
				Clientset:                    defaultTestClientWithNamespace(),
				ConsulConfig:                 &consul.Config{HTTPPort: 8500},
				decoder:                      decoder,
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tt.annotations},
				Spec:       corev1.PodSpec{Containers: tt.containers},
			}
			resp := webhook.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Namespace: "default",
				Object:    encodeRaw(t, &pod),
			}})

			require.Equal(t, tt.wantAllowed, resp.Allowed, resp.Result.Message)
			if tt.wantMessage != "" {
				require.Contains(t, resp.Result.Message, tt.wantMessage)
			}
			if tt.wantPortPatch {
				require.NotEmpty(t, resp.Patches)
			}
		})
	}
}

func TestDefaultAnnotationsRetainsFullPortSelectionForAdmissionValidation(t *testing.T) {
	t.Parallel()
	pod := corev1.Pod{Spec: corev1.PodSpec{Containers: containersWithPortCounts(2)}}
	webhook := MeshWebhook{DisableMultiportRegistration: true}

	require.NoError(t, webhook.defaultAnnotations(&pod, "{}"))
	require.Equal(t, "port-0,port-1", pod.Annotations[constants.AnnotationPort])
	require.Error(t, webhook.validateMultiportRegistration(pod))
}

func containersWithPortCounts(counts ...int) []corev1.Container {
	containers := make([]corev1.Container, 0, len(counts))
	nextPort := int32(8080)
	for containerIndex, count := range counts {
		container := corev1.Container{Name: "container-" + strconv.Itoa(containerIndex)}
		for portIndex := 0; portIndex < count; portIndex++ {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				Name:          "port-" + strconv.Itoa(portIndex),
				ContainerPort: nextPort,
			})
			nextPort++
		}
		containers = append(containers, container)
	}
	return containers
}
