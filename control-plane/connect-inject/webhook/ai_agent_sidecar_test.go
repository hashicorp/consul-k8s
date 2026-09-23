// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"encoding/json"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

func TestOBOSidecarRequiresImage(t *testing.T) {
	w := &MeshWebhook{}

	_, err := w.oboInboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.ErrorContains(t, err, "ImageConsulOBOInbound must be set")

	_, err = w.oboOutboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.ErrorContains(t, err, "ImageConsulOBOOutbound must be set")
}

func TestOBOSidecarUsesDataplaneRunAs(t *testing.T) {
	w := &MeshWebhook{
		ImageConsulOBOInbound:  "obo-inbound:test",
		ImageConsulOBOOutbound: "obo-outbound:test",
	}

	t.Run("stock kubernetes", func(t *testing.T) {
		inbound, err := w.oboInboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
		require.NoError(t, err)
		outbound, err := w.oboOutboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
		require.NoError(t, err)

		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), inbound.SecurityContext.RunAsUser)
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), inbound.SecurityContext.RunAsGroup)
		require.Equal(t, inbound.SecurityContext.RunAsUser, outbound.SecurityContext.RunAsUser)
		require.Equal(t, inbound.SecurityContext.RunAsGroup, outbound.SecurityContext.RunAsGroup)
		require.Contains(t, inbound.Args, "--dataplane-ready-url=http://127.0.0.1:19000/ready")
		require.Contains(t, inbound.Args, "127.0.0.1:21102")
	})

	t.Run("openshift uid", func(t *testing.T) {
		const openShiftID int64 = 1000799998
		inbound, err := w.oboInboundSidecar(openShiftID, openShiftID)
		require.NoError(t, err)
		require.Equal(t, ptr.To(openShiftID), inbound.SecurityContext.RunAsUser)
		require.Equal(t, ptr.To(openShiftID), inbound.SecurityContext.RunAsGroup)
	})
}

func TestOBOSidecarReadyURLDualStack(t *testing.T) {
	t.Setenv(constants.ConsulDualStackEnvVar, "true")

	w := &MeshWebhook{
		ImageConsulOBOInbound:  "obo-inbound:test",
		ImageConsulOBOOutbound: "obo-outbound:test",
	}
	inbound, err := w.oboInboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.NoError(t, err)
	outbound, err := w.oboOutboundSidecar(sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.NoError(t, err)

	require.Contains(t, inbound.Args, "--dataplane-ready-url=http://[::1]:19000/ready")
	require.Contains(t, outbound.Args, "--dataplane-ready-url=http://[::1]:19000/ready")
	// xDS OBO clusters stay on IPv4 loopback.
	require.Contains(t, inbound.Args, "127.0.0.1:21102")
	require.Contains(t, outbound.Args, "127.0.0.1:21103")
}

func TestHandleAIAgentOBOIndependentOfMCPImage(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)

	aiPod := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					constants.AnnotationAIRole: constants.AIAgentRole,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "web",
					Image: "app:test",
				}},
			},
		}
	}

	baseWebhook := func(clientset *fake.Clientset) MeshWebhook {
		return MeshWebhook{
			Log:                    logrtest.New(t),
			AllowK8sNamespacesSet:  mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:   mapset.NewSet(),
			decoder:                decoder,
			Clientset:              clientset,
			ConsulConfig:           &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			ImageConsulDataplane:   "dataplane:test",
			ImageConsulOBOInbound:  "obo-inbound:test",
			ImageConsulOBOOutbound: "obo-outbound:test",
		}
	}

	t.Run("obo injected when mcp gateway image is unset", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		containers := injectedContainers(t, w, aiPod())
		require.NotContains(t, containers, mcpGatewayContainer)
		require.Contains(t, containers, constants.ConsulOBOInboundContainerName)
		require.Contains(t, containers, constants.ConsulOBOOutboundContainerName)

		dp := containers[sidecarContainer]
		inbound := containers[constants.ConsulOBOInboundContainerName]
		require.Equal(t, dp.SecurityContext.RunAsUser, inbound.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, inbound.SecurityContext.RunAsGroup)
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), inbound.SecurityContext.RunAsUser)
	})

	t.Run("missing obo image fails admission", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		w.ImageConsulOBOInbound = ""
		resp := w.Handle(context.Background(), admissionRequest(t, aiPod()))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "ImageConsulOBOInbound must be set")
	})

	t.Run("non ai-agent pod has no obo", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		delete(pod.Annotations, constants.AnnotationAIRole)
		containers := injectedContainers(t, w, pod)
		require.NotContains(t, containers, constants.ConsulOBOInboundContainerName)
		require.NotContains(t, containers, constants.ConsulOBOOutboundContainerName)
	})

	t.Run("application container named consul-dataplane-metrics is ignored", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "consul-dataplane-metrics",
				Image: "app:test",
				SecurityContext: &corev1.SecurityContext{
					RunAsUser:  ptr.To(int64(1000)),
					RunAsGroup: ptr.To(int64(1000)),
				},
			},
			{Name: "web", Image: "app:test"},
		}
		containers := injectedContainers(t, w, pod)
		dp := containers[sidecarContainer]
		inbound := containers[constants.ConsulOBOInboundContainerName]
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), dp.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsUser, inbound.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, inbound.SecurityContext.RunAsGroup)
		require.NotEqual(t, ptr.To(int64(1000)), inbound.SecurityContext.RunAsUser)
	})

	t.Run("openshift obo uid matches dataplane", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(true)))
		w.EnableOpenShift = true
		w.ImageAIAgent = "mcp-gateway:test"
		containers := injectedContainers(t, w, aiPod())
		require.Contains(t, containers, mcpGatewayContainer)

		dp := containers[sidecarContainer]
		inbound := containers[constants.ConsulOBOInboundContainerName]
		outbound := containers[constants.ConsulOBOOutboundContainerName]
		require.NotNil(t, dp.SecurityContext.RunAsUser)
		require.NotEqual(t, int64(sidecarUserAndGroupID), *dp.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsUser, inbound.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, inbound.SecurityContext.RunAsGroup)
		require.Equal(t, dp.SecurityContext.RunAsUser, outbound.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, outbound.SecurityContext.RunAsGroup)
	})
}

func namespaceWithOpenShift(openShift bool) *corev1.Namespace {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaces.DefaultNamespace}}
	if openShift {
		ns.Annotations = map[string]string{
			constants.AnnotationOpenShiftUIDRange: "1000700000/100000",
			constants.AnnotationOpenShiftGroups:   "1000700000/100000",
		}
	}
	return ns
}

func admissionRequest(t *testing.T, pod *corev1.Pod) admission.Request {
	t.Helper()
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: namespaces.DefaultNamespace,
			Object:    encodeRaw(t, pod),
		},
	}
}

func injectedContainers(t *testing.T, w MeshWebhook, pod *corev1.Pod) map[string]corev1.Container {
	t.Helper()
	resp := w.Handle(context.Background(), admissionRequest(t, pod))
	require.True(t, resp.Allowed, resp.Result.Message)

	found := map[string]corev1.Container{}
	for _, p := range resp.Patches {
		raw, err := json.Marshal(p.Value)
		require.NoError(t, err)
		var c corev1.Container
		if err := json.Unmarshal(raw, &c); err != nil || c.Name == "" {
			continue
		}
		found[c.Name] = c
	}
	return found
}
