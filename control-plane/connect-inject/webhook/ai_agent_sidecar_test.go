// Copyright IBM Corp. 2018, 2026
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
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
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
		require.Contains(t, c.Args, "--obo-inbound-addr=127.0.0.1:21102")
		require.Contains(t, c.Args, "--obo-outbound-addr=127.0.0.1:21103")
		require.Contains(t, c.Args, "--mcp-socket="+mcpGatewayUDSPath)
		require.Equal(t, []string{defaultMCPScBinary}, c.Command)
		require.NotNil(t, c.StartupProbe)
		require.NotNil(t, c.StartupProbe.Exec)
		require.Equal(t, []string{defaultMCPScBinary, "probe", "--mode=mcp",
			"--mcp-socket=" + mcpGatewayUDSPath}, c.StartupProbe.Exec.Command)
		require.NotNil(t, c.ReadinessProbe)
		require.NotNil(t, c.ReadinessProbe.Exec)
		require.Equal(t, []string{defaultMCPScBinary, "probe", "--mode=agent",
			"--obo-inbound-addr=127.0.0.1:21102",
			"--obo-outbound-addr=127.0.0.1:21103",
			"--mcp-socket=" + mcpGatewayUDSPath}, c.ReadinessProbe.Exec.Command)
	})

	t.Run("tcp mcp binds loopback and probes it", func(t *testing.T) {
		for _, addr := range []string{":21101", "127.0.0.1:21101", "[::1]:21101"} {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				constants.AnnotationAIAgentAddr: addr,
			}}}
			c, err := w.aiAgentSidecar(pod, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
			require.NoError(t, err, addr)
			require.Contains(t, c.Args, "--mcp-addr=127.0.0.1:21101", addr)
			require.NotContains(t, c.Args, "--mcp-socket="+mcpGatewayUDSPath, addr)
			require.Equal(t, []string{defaultMCPScBinary, "probe", "--mode=mcp",
				"--mcp-addr=127.0.0.1:21101"}, c.StartupProbe.Exec.Command, addr)
			require.Contains(t, c.ReadinessProbe.Exec.Command, "--mcp-addr=127.0.0.1:21101", addr)
		}
	})

	t.Run("invalid listener is returned to the caller", func(t *testing.T) {
		pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.AnnotationAIAgentAddr: "10.0.0.1:21101",
		}}}
		_, err := w.aiAgentSidecar(pod, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
		require.ErrorContains(t, err, "host must be empty or loopback")
	})

	t.Run("forwards mcp child binary and args", func(t *testing.T) {
		pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			constants.AnnotationAIAgentChildBinary: "/app/server",
			constants.AnnotationAIAgentChildArgs:   "--port=9",
		}}}
		c, err := w.aiAgentSidecar(pod, v1alpha1.AgentDefaults{}, sidecarUserAndGroupID, sidecarUserAndGroupID)
		require.NoError(t, err)
		require.Contains(t, c.Args, "--mcp-child=/app/server")
		require.Contains(t, c.Args, "--mcp-child-args=--port=9")
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
	// Envoy dials the ext_proc clusters on 127.0.0.1 in every IP family.
	require.Contains(t, c.Args, "--obo-inbound-addr=127.0.0.1:21102")
	require.Contains(t, c.Args, "--obo-outbound-addr=127.0.0.1:21103")
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
		clientset := fake.NewSimpleClientset(namespaceWithOpenShift(false))
		w := baseWebhook(clientset)
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

		namespaceGets := 0
		for _, action := range clientset.Actions() {
			if action.GetVerb() == "get" && action.GetResource().Resource == "namespaces" {
				namespaceGets++
			}
		}
		require.Equal(t, 1, namespaceGets)
	})

	t.Run("missing image fails admission", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		w.ImageAIAgent = ""
		resp := w.Handle(context.Background(), admissionRequest(t, aiPod()))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, "AI sidecar image must be set")
	})

	t.Run("invalid AI port annotation fails admission", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		pod.Annotations[constants.AnnotationAIAgentHITLPort] = "80"
		resp := w.Handle(context.Background(), admissionRequest(t, pod))
		require.False(t, resp.Allowed)
		require.Contains(t, resp.Result.Message, constants.AnnotationAIAgentHITLPort)
	})

	t.Run("non-loopback MCP listener fails admission", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		pod.Annotations[constants.AnnotationAIAgentAddr] = "0.0.0.0:21101"
		resp := w.Handle(context.Background(), admissionRequest(t, pod))
		require.False(t, resp.Allowed)
		require.Equal(t, int32(400), resp.Result.Code)
		require.Contains(t, resp.Result.Message, "host must be empty or loopback")
	})

	t.Run("non ai-agent pod has no ai sidecar", func(t *testing.T) {
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		pod := aiPod()
		delete(pod.Annotations, constants.AnnotationAIRole)
		containers := injectedContainers(t, w, pod)
		require.NotContains(t, containers, mcpGatewayContainer)
	})

	t.Run("AgentConfig interceptor port is bound on loopback", func(t *testing.T) {
		scheme := runtime.NewScheme()
		require.NoError(t, v1alpha1.AddToScheme(scheme))
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		w.Client = ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(&v1alpha1.AgentConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "my-agent"},
			Spec:       v1alpha1.AgentConfigSpec{Defaults: v1alpha1.AgentDefaults{InterceptorPort: 17101}},
		}).Build()
		pod := aiPod()
		pod.Annotations[constants.AnnotationAIAgentConfig] = "my-agent"
		pod.Annotations[constants.AnnotationAIAgentAddr] = "127.0.0.1:17101"
		ai := injectedContainers(t, w, pod)[mcpGatewayContainer]
		require.Contains(t, ai.Args, "--mcp-addr=127.0.0.1:17101")
		require.Contains(t, ai.Args, "--obo-inbound-addr=127.0.0.1:21102")
		require.Contains(t, ai.Args, "--obo-outbound-addr=127.0.0.1:21103")
		require.Equal(t, []string{defaultMCPScBinary, "probe", "--mode=mcp",
			"--mcp-addr=127.0.0.1:17101"}, ai.StartupProbe.Exec.Command)
	})

	t.Run("missing AgentConfig still binds the loopback listeners", func(t *testing.T) {
		scheme := runtime.NewScheme()
		require.NoError(t, v1alpha1.AddToScheme(scheme))
		w := baseWebhook(fake.NewSimpleClientset(namespaceWithOpenShift(false)))
		w.Client = ctrlfake.NewClientBuilder().WithScheme(scheme).Build()
		ai := injectedContainers(t, w, aiPod())[mcpGatewayContainer]
		require.Contains(t, ai.Args, "--mode=agent")
		require.Contains(t, ai.Args, "--obo-inbound-addr=127.0.0.1:21102")
		require.Contains(t, ai.Args, "--obo-outbound-addr=127.0.0.1:21103")
		require.Contains(t, ai.Args, "--mcp-socket="+mcpGatewayUDSPath)
		require.Equal(t, []string{defaultMCPScBinary, "probe", "--mode=agent",
			"--obo-inbound-addr=127.0.0.1:21102",
			"--obo-outbound-addr=127.0.0.1:21103",
			"--mcp-socket=" + mcpGatewayUDSPath}, ai.ReadinessProbe.Exec.Command)
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
