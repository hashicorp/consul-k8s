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

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

func TestAIAgentSidecarRequiresImage(t *testing.T) {
	w := &MeshWebhook{}
	_, err := w.aiAgentSidecar(corev1.Pod{}, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.ErrorContains(t, err, "AI sidecar image must be set")
}

func TestAIAgentSidecarUsesDataplaneRunAs(t *testing.T) {
	w := &MeshWebhook{
		ImageAIAgent: "consul-mcp-sc:test",
		LogLevel:     "info",
	}

	t.Run("stock uid", func(t *testing.T) {
		c, err := w.aiAgentSidecar(corev1.Pod{}, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
		require.NoError(t, err)
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), c.SecurityContext.RunAsUser)
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), c.SecurityContext.RunAsGroup)
		require.Contains(t, c.Args, "--mode=agent")
		require.Contains(t, c.Args, "--envelope-uds=/consul/connect-inject/oauth-envelope.sock")
		require.Contains(t, c.Args, "--broker-uds=/consul/connect-inject/credential-broker.sock")
		require.Contains(t, c.Args, "--dataplane-ready-url=http://127.0.0.1:19000/ready")
		require.Contains(t, c.Args, "--obo-inbound-addr=:21102")
		require.Contains(t, c.Args, "--obo-outbound-addr=:21103")
		require.Contains(t, c.Args, "--mcp-socket="+mcpGatewayUDSPath)
		require.Equal(t, []string{defaultMCPScBinary}, c.Command)
	})

	t.Run("openshift uid", func(t *testing.T) {
		const openShiftID int64 = 1000799998
		c, err := w.aiAgentSidecar(corev1.Pod{}, v1alpha1.AgentDefaults{}, openShiftID, openShiftID)
		require.NoError(t, err)
		require.Equal(t, ptr.To(openShiftID), c.SecurityContext.RunAsUser)
		require.Equal(t, ptr.To(openShiftID), c.SecurityContext.RunAsGroup)
	})
}

func TestAIAgentSidecarReadyURLDualStack(t *testing.T) {
	t.Setenv(constants.ConsulDualStackEnvVar, "true")

	w := &MeshWebhook{ImageAIAgent: "consul-mcp-sc:test", LogLevel: "info"}
	c, err := w.aiAgentSidecar(corev1.Pod{}, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
	require.NoError(t, err)
	require.Contains(t, c.Args, "--dataplane-ready-url=http://[::1]:19000/ready")
	require.Contains(t, c.Args, "--obo-inbound-addr=:21102")
	require.Contains(t, c.Args, "--obo-outbound-addr=:21103")
}

func TestHandleAIAgentCombinedSidecar(t *testing.T) {
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
			Log:                   logrtest.New(t),
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSet(),
			decoder:               decoder,
			Clientset:             clientset,
			ConsulConfig:          &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			ImageConsulDataplane:  "dataplane:test",
			ImageAIAgent:          "consul-mcp-sc:test",
			LogLevel:              "info",
		}
	}

	t.Run("one combined sidecar uses dataplane uid", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		containers := injectedContainers(t, w, aiPod())
		require.Contains(t, containers, mcpGatewayContainer)
		require.NotContains(t, containers, constants.ConsulOBOInboundContainerName)
		require.NotContains(t, containers, constants.ConsulOBOOutboundContainerName)

		dp := containers[sidecarContainer]
		ai := containers[mcpGatewayContainer]
		require.Equal(t, dp.SecurityContext.RunAsUser, ai.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, ai.SecurityContext.RunAsGroup)
		require.Equal(t, ptr.To(int64(sidecarUserAndGroupID)), ai.SecurityContext.RunAsUser)
		require.Contains(t, ai.Args, "--mode=agent")
	})

	t.Run("missing image fails admission", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		w.ImageAIAgent = ""
		resp := w.Handle(context.Background(), admissionRequest(t, aiPod()))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "AI sidecar image must be set")
	})

	t.Run("non ai-agent pod has no ai sidecar", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		delete(pod.Annotations, constants.AnnotationAIRole)
		containers := injectedContainers(t, w, pod)
		require.NotContains(t, containers, mcpGatewayContainer)
	})

	t.Run("openshift ai sidecar uid matches dataplane", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(true)))
		w.EnableOpenShift = true
		containers := injectedContainers(t, w, aiPod())
		require.Contains(t, containers, mcpGatewayContainer)

		dp := containers[sidecarContainer]
		ai := containers[mcpGatewayContainer]
		require.NotNil(t, dp.SecurityContext.RunAsUser)
		require.NotEqual(t, int64(sidecarUserAndGroupID), *dp.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsUser, ai.SecurityContext.RunAsUser)
		require.Equal(t, dp.SecurityContext.RunAsGroup, ai.SecurityContext.RunAsGroup)
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
