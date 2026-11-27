// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	mapset "github.com/deckarep/golang-set"
	jsonpatch "github.com/evanphx/json-patch"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

func TestHandle_AmbientMesh(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	etcResolv, err := os.CreateTemp("", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(etcResolv.Name()) })
	_, err = etcResolv.WriteString("nameserver 10.96.0.10\nsearch default.svc.cluster.local\n")
	require.NoError(t, err)

	appPod := func(annotations map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default", Annotations: annotations},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name:  "web",
				Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
				ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{Port: intstr.FromString("http")},
				}},
				LivenessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(9090)},
				}},
			}}},
		}
	}

	cases := map[string]struct {
		nsLabels      map[string]string
		pod           *corev1.Pod
		featureOn     bool
		expectAmbient bool
		expectErr     string
	}{
		"pod annotation opts in": {
			pod:           appPod(map[string]string{constants.KeyAmbientMesh: "true"}),
			featureOn:     true,
			expectAmbient: true,
		},
		"namespace label opts in without connect-inject annotation": {
			nsLabels:      map[string]string{constants.KeyAmbientMesh: "true"},
			pod:           appPod(nil),
			featureOn:     true,
			expectAmbient: true,
		},
		"pod annotation opts out of ambient namespace": {
			nsLabels:  map[string]string{constants.KeyAmbientMesh: "true"},
			pod:       appPod(map[string]string{constants.KeyAmbientMesh: "false"}),
			featureOn: true,
		},
		"feature disabled falls back to sidecar": {
			pod: appPod(map[string]string{constants.KeyAmbientMesh: "true"}),
		},
		"multi-port pods are rejected": {
			pod: appPod(map[string]string{
				constants.KeyAmbientMesh:    "true",
				constants.AnnotationService: "web,web-admin",
			}),
			featureOn: true,
			expectErr: "multi-port pods are not supported in the ambient mesh",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: c.nsLabels}}
			w := MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				RequireAnnotation:     true,
				EnableAmbientMesh:     c.featureOn,
				AmbientInboundPort:    15006,
				AmbientOutboundPort:   15001,
				AmbientProxyMark:      1337,
				EnableConsulDNS:       true,
				ReleaseNamespace:      "consul",
				etcResolvFile:         etcResolv.Name(),
				decoder:               decoder,
				Clientset:             fake.NewSimpleClientset(ns),
			}
			// A user must not be able to pre-set CNI-owned keys.
			c.pod.Annotations = withDefaults(c.pod.Annotations, map[string]string{
				constants.AnnotationAmbientNetNS: "/proc/1/ns/net",
			})
			raw, err := json.Marshal(c.pod)
			require.NoError(t, err)

			resp := w.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Namespace: "default",
				Object:    runtime.RawExtension{Raw: raw},
			}})
			if c.expectErr != "" {
				require.False(t, resp.Allowed)
				require.Contains(t, resp.Result.Message, c.expectErr)
				return
			}
			require.True(t, resp.Allowed, resp.Result)

			patchJSON, err := json.Marshal(resp.Patches)
			require.NoError(t, err)
			patch, err := jsonpatch.DecodePatch(patchJSON)
			require.NoError(t, err)
			patched, err := patch.Apply(raw)
			require.NoError(t, err)
			var out corev1.Pod
			require.NoError(t, json.Unmarshal(patched, &out))

			if !c.expectAmbient {
				require.NotContains(t, out.Annotations, constants.KeyAmbientMeshStatus)
				require.NotContains(t, out.Labels, constants.KeyAmbientMesh)
				return
			}

			require.NotContains(t, out.Annotations, constants.AnnotationAmbientNetNS)
			require.Len(t, out.Spec.Containers, 1, "ambient pods must not get a sidecar")
			require.Empty(t, out.Spec.InitContainers, "ambient pods must not get an init container")
			require.Empty(t, out.Spec.Volumes)
			require.Equal(t, constants.Injected, out.Annotations[constants.KeyInjectStatus])
			require.Equal(t, constants.AmbientMeshStatusWaiting, out.Annotations[constants.KeyAmbientMeshStatus])
			require.Equal(t, constants.LabelAmbientMeshEnabled, out.Labels[constants.KeyAmbientMesh])
			require.Equal(t, constants.Injected, out.Labels[constants.KeyInjectStatus])
			require.NotContains(t, out.Annotations, constants.KeyTransparentProxyStatus)
			require.NotContains(t, out.Annotations, constants.AnnotationRedirectTraffic)

			var cfg ambientRedirectConfig
			require.NoError(t, json.Unmarshal([]byte(out.Annotations[constants.AnnotationAmbientRedirectConfig]), &cfg))
			require.Equal(t, ambientRedirectConfig{
				InboundPort:   15006,
				OutboundPort:  15001,
				ProxyMark:     1337,
				ConsulDNSIP:   "127.0.0.1",
				ConsulDNSPort: 8600,
				ProbePorts:    []string{"8080", "9090"},
			}, cfg)

			require.Equal(t, corev1.DNSNone, out.Spec.DNSPolicy)
			require.Equal(t, []string{"127.0.0.1", "10.96.0.10"}, out.Spec.DNSConfig.Nameservers)
		})
	}
}

func TestHandle_StripsAmbientKeysFromSidecarPods(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	w := MeshWebhook{
		Log:                   logrtest.New(t),
		AllowK8sNamespacesSet: mapset.NewSetWith("*"),
		DenyK8sNamespacesSet:  mapset.NewSet(),
		decoder:               admission.NewDecoder(s),
		Clientset:             defaultTestClientWithNamespace(),
		ConsulConfig:          &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Annotations: map[string]string{
				constants.KeyAmbientMeshStatus:   constants.AmbientMeshStatusWaiting,
				constants.AnnotationAmbientNetNS: "/proc/1/ns/net",
			},
			Labels: map[string]string{constants.KeyAmbientMesh: constants.LabelAmbientMeshEnabled},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web"}}},
	}
	raw, err := json.Marshal(pod)
	require.NoError(t, err)
	resp := w.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
		Namespace: "default",
		Object:    runtime.RawExtension{Raw: raw},
	}})
	require.True(t, resp.Allowed, resp.Result)

	patchJSON, err := json.Marshal(resp.Patches)
	require.NoError(t, err)
	patch, err := jsonpatch.DecodePatch(patchJSON)
	require.NoError(t, err)
	patched, err := patch.Apply(raw)
	require.NoError(t, err)
	var out corev1.Pod
	require.NoError(t, json.Unmarshal(patched, &out))
	require.NotContains(t, out.Annotations, constants.KeyAmbientMeshStatus)
	require.NotContains(t, out.Annotations, constants.AnnotationAmbientNetNS)
	require.NotContains(t, out.Labels, constants.KeyAmbientMesh)
}

func withDefaults(m map[string]string, extra map[string]string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}
