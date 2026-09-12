// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
)

// baseAIWebhook returns a MeshWebhook with only the fields needed to build an
// AI agent sidecar container.
func baseAIWebhook(t *testing.T) *MeshWebhook {
	t.Helper()
	return &MeshWebhook{
		ImageConsulK8S:              "hashicorp/consul-k8s:test",
		ImageConsulAIMCPInterceptor: "hashicorp/consul-ai-mcp-interceptor:test",
		GlobalImagePullPolicy:       "IfNotPresent",
		GatewayBinary:               "/usr/local/bin/consul-mcp-gateway",
		LogLevel:                    "info",
		DefaultConsulSidecarResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		LifecycleConfig: lifecycle.Config{},
		MetricsConfig:   metrics.Config{},
	}
}

// aiPod returns a Pod annotated as an AI agent for use in tests.
func aiPod(serviceName, cmName string) corev1.Pod {
	annotations := map[string]string{
		constants.AnnotationService:         serviceName,
		constants.AnnotationInject:          "true",
		constants.AnnotationAIRole:          constants.AIAgentRole,
		constants.AnnotationAIAgentMCPConfig: cmName,
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   "default",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app", Image: "myapp:latest"},
			},
		},
	}
}

// TestIsAIAgent verifies isAIAgent detects the annotation correctly.
func TestIsAIAgent(t *testing.T) {
	cases := map[string]struct {
		annotations map[string]string
		expected    bool
	}{
		"ai-agent annotation present": {
			annotations: map[string]string{constants.AnnotationAIRole: "ai-agent"},
			expected:    true,
		},
		"ai-agent annotation absent": {
			annotations: map[string]string{},
			expected:    false,
		},
		"ai-agent annotation wrong value": {
			annotations: map[string]string{constants.AnnotationAIRole: "something-else"},
			expected:    false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations},
			}
			require.Equal(t, tc.expected, common.IsAIAgent(pod))
		})
	}
}

// TestAIAgentMCPConfigName verifies aiAgentMCPConfigName returns the correct ConfigMap name.
func TestAIAgentMCPConfigName(t *testing.T) {
	cases := map[string]struct {
		annotations map[string]string
		expected    string
	}{
		"annotation present": {
			annotations: map[string]string{constants.AnnotationAIAgentMCPConfig: "my-mcp-config"},
			expected:    "my-mcp-config",
		},
		"annotation absent": {
			annotations: map[string]string{},
			expected:    "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: tc.annotations},
			}
			require.Equal(t, tc.expected, common.AIAgentMCPConfigName(pod))
		})
	}
}

// TestAIAgentSidecar verifies the container built by aiAgentSidecar has the
// correct name, image, volume mounts, and command.
func TestAIAgentSidecar(t *testing.T) {
	w := baseAIWebhook(t)
	pod := aiPod("my-ai-app", "my-mcp-config")

	container, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)

	// Name and image — must use the dedicated interceptor image, not consul-k8s.
	require.Equal(t, constants.AIContainerName, container.Name)
	require.Equal(t, "hashicorp/consul-ai-mcp-interceptor:test", container.Image)
	require.Equal(t, corev1.PullPolicy("IfNotPresent"), container.ImagePullPolicy)

	// Volume mounts: shared data + config map.
	var mountNames []string
	for _, vm := range container.VolumeMounts {
		mountNames = append(mountNames, vm.Name)
	}
	require.Contains(t, mountNames, volumeName)
	require.Contains(t, mountNames, aiAgentConfigVolumeName)

	// Config map mount is read-only.
	for _, vm := range container.VolumeMounts {
		if vm.Name == aiAgentConfigVolumeName {
			require.True(t, vm.ReadOnly)
		}
	}

	// Command is [consulBinary]; sub-command and flags live in Args.
	require.Len(t, container.Command, 1)
	require.Equal(t, constants.ConsulBinarypath, container.Command[0])

	// Args must contain the sub-command and all expected flags.
	args := container.Args
	require.Contains(t, args, "connect")
	require.Contains(t, args, "mcp-gateway")
	require.Contains(t, args, "-gateway-binary")
	require.Contains(t, args, w.GatewayBinary)
	require.Contains(t, args, "-addr")

	// Interceptor port must appear in the -addr value.
	addrIdx := -1
	for i, v := range args {
		if v == "-addr" {
			addrIdx = i
			break
		}
	}
	require.Greater(t, addrIdx, -1)
	require.Contains(t, args[addrIdx+1], "21101")

	// Security context: non-root, no privilege escalation, read-only filesystem.
	require.NotNil(t, container.SecurityContext)
	require.True(t, *container.SecurityContext.RunAsNonRoot)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
}

// TestAIAgentSidecarUsesAIMCPInterceptorImage verifies that the container uses
// ImageConsulAIMCPInterceptor when set, and falls back to ImageConsulK8S when not.
func TestAIAgentSidecarUsesAIMCPInterceptorImage(t *testing.T) {
	t.Run("uses dedicated interceptor image", func(t *testing.T) {
		w := baseAIWebhook(t)
		pod := aiPod("svc", "cm")
		container, err := w.aiAgentSidecar(pod)
		require.NoError(t, err)
		require.Equal(t, "hashicorp/consul-ai-mcp-interceptor:test", container.Image)
	})

	t.Run("falls back to consul-k8s image when interceptor image empty", func(t *testing.T) {
		w := baseAIWebhook(t)
		w.ImageConsulAIMCPInterceptor = "" // cleared
		pod := aiPod("svc", "cm")
		container, err := w.aiAgentSidecar(pod)
		require.NoError(t, err)
		require.Equal(t, "hashicorp/consul-k8s:test", container.Image)
	})
}

// TestAIAgentSidecarDefaultGatewayBinary verifies that if GatewayBinary is
// empty the default path is used.
func TestAIAgentSidecarDefaultGatewayBinary(t *testing.T) {
	w := baseAIWebhook(t)
	w.GatewayBinary = "" // clear to force default
	pod := aiPod("my-ai-app", "my-mcp-config")

	container, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)

	require.Contains(t, container.Args, constants.DefaultGatewayBinary)
}

// TestAIAgentSidecarServiceNameFallback verifies that the sidecar is built
// successfully when AnnotationService is absent.
func TestAIAgentSidecarServiceNameFallback(t *testing.T) {
	w := baseAIWebhook(t)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				constants.AnnotationAIRole:           constants.AIAgentRole,
				constants.AnnotationAIAgentMCPConfig: "my-config",
				// AnnotationService intentionally absent
			},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "my-ai-service",
		},
	}

	_, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)
}

// TestAIAgentSidecarEnvVars verifies all required environment variables are present.
func TestAIAgentSidecarEnvVars(t *testing.T) {
	w := baseAIWebhook(t)
	pod := aiPod("my-ai-app", "my-mcp-config")

	container, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)

	envNames := make(map[string]string)
	for _, e := range container.Env {
		envNames[e.Name] = e.Value
	}

	require.Contains(t, envNames, "POD_NAME")
	require.Contains(t, envNames, "POD_NAMESPACE")
}

// TestOBOInboundSidecar verifies the container built by oboInboundSidecar has
// the correct name, image, volume mount, command, and args.
func TestOBOInboundSidecar(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOInbound = "hashicorp/consul-obo-inbound:test"
	pod := aiPod("my-ai-app", "my-mcp-config")

	container, err := w.oboInboundSidecar(pod)
	require.NoError(t, err)

	// Container name must match the OBO inbound constant.
	require.Equal(t, constants.ConsulOBOInboundContainerName, container.Name)
	require.Equal(t, "hashicorp/consul-obo-inbound:test", container.Image)
	require.Equal(t, corev1.PullPolicy("IfNotPresent"), container.ImagePullPolicy)

	// Volume mount: shared data only (no ConfigMap mount).
	require.Len(t, container.VolumeMounts, 1)
	require.Equal(t, volumeName, container.VolumeMounts[0].Name)
	require.True(t, container.VolumeMounts[0].ReadOnly)

	// Command is the obo-inbound binary directly.
	require.Len(t, container.Command, 1)
	require.Equal(t, constants.DefaultOBOInboundBinary, container.Command[0])

	// Args must contain --addr with the OBO inbound port.
	args := container.Args
	require.Contains(t, args, "--addr")
	addrIdx := -1
	for i, v := range args {
		if v == "--addr" {
			addrIdx = i
			break
		}
	}
	require.Greater(t, addrIdx, -1)
	require.Contains(t, args[addrIdx+1], "21102")
	require.Contains(t, args, "--log-level=info")
	require.NotContains(t, args, "--obo=true",
		"--obo flag removed: consul-obo-inbound is always inbound-only; no flag needed")

	// Security context: non-root, no privilege escalation, read-only filesystem.
	require.NotNil(t, container.SecurityContext)
	require.True(t, *container.SecurityContext.RunAsNonRoot)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
}

// TestOBOInboundSidecarImageFallback verifies that the container falls back to
// ImageConsulK8S when ImageConsulOBOInbound is empty.
func TestOBOInboundSidecarImageFallback(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOInbound = "" // not configured
	pod := aiPod("svc", "cm")

	container, err := w.oboInboundSidecar(pod)
	require.NoError(t, err)
	require.Equal(t, "hashicorp/consul-k8s:test", container.Image)
}


// TestOBOOutboundSidecar verifies the container built by oboOutboundSidecar has
// the correct name, image, volume mount, command, and args.
func TestOBOOutboundSidecar(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOOutbound = "hashicorp/consul-obo-outbound:test"
	pod := aiPod("my-ai-app", "my-mcp-config")

	container, err := w.oboOutboundSidecar(pod)
	require.NoError(t, err)

	require.Equal(t, constants.ConsulOBOOutboundContainerName, container.Name)
	require.Equal(t, "hashicorp/consul-obo-outbound:test", container.Image)
	require.Equal(t, corev1.PullPolicy("IfNotPresent"), container.ImagePullPolicy)

	// Volume mount: shared data only.
	require.Len(t, container.VolumeMounts, 1)
	require.Equal(t, volumeName, container.VolumeMounts[0].Name)
	require.True(t, container.VolumeMounts[0].ReadOnly)

	// Command is the obo-outbound binary directly.
	require.Len(t, container.Command, 1)
	require.Equal(t, constants.DefaultOBOOutboundBinary, container.Command[0])

	// Args must contain --addr with the OBO outbound port plus the SDS flags.
	args := container.Args
	require.Contains(t, args, "--addr")
	addrIdx := -1
	for i, v := range args {
		if v == "--addr" {
			addrIdx = i
			break
		}
	}
	require.Greater(t, addrIdx, -1)
	require.Contains(t, args[addrIdx+1], "21103")
	// SDS client flags must be present so consul-obo-outbound subscribes to the
	// GenericSecret for this service's OAuthClientConfig.
	require.Contains(t, args, "--xds-addr=127.0.0.1:19500")
	require.Contains(t, args, "--sds-resource=oauth/my-ai-app")
	// proxy-id-file tells the SDS client which node.Id to send in the first
	// DeltaDiscoveryRequest so the Consul server starts watching the right proxy.
	require.Contains(t, args, "--proxy-id-file=/consul/connect-inject/proxyid")
	// node-name-file supplies node.metadata.node_name so the Consul server resolves
	// the same proxycfg identity that Envoy uses.  Without it the server falls back
	// to s.NodeName which may differ, silently watching the wrong snapshot and never
	// pushing the oauth/<svcName> GenericSecret.
	require.Contains(t, args, "--node-name-file=/consul/connect-inject/nodename")
	// dataplane-ready-url must be set to the Envoy admin /ready endpoint so the
	// SDS subscription waits for Envoy to fully initialise (ADS stream active)
	// before opening the stream.  Do NOT use graceful_startup (:20600) — with
	// startupGracePeriodSeconds=0 it returns 200 immediately, before the Consul
	// server has built the proxy snapshot.
	dataplaneReadyFound := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--dataplane-ready-url=") {
			dataplaneReadyFound = true
			require.Contains(t, arg, ":19000/ready",
				"--dataplane-ready-url must point to the Envoy admin /ready endpoint (:19000/ready), not graceful_startup")
			break
		}
	}
	require.True(t, dataplaneReadyFound,
		"--dataplane-ready-url must be present so SDS subscription waits for Envoy to be ready")

	// Security context.
	require.NotNil(t, container.SecurityContext)
	// RunAsUser MUST be sidecarUserAndGroupID (5995) so iptables transparent proxy
	// excludes the container's outbound traffic (CONSUL_PROXY_OUTPUT RETURN rule
	// "owner UID match 5995"). Without this the HTTPS calls to IBM Verify are
	// captured by TP, Envoy's original-destination cluster returns zero bytes, and
	// every token exchange fails with SSL_EOF.
	require.NotNil(t, container.SecurityContext.RunAsUser,
		"RunAsUser must be set to sidecarUserAndGroupID to bypass TP iptables capture")
	require.Equal(t, int64(sidecarUserAndGroupID), *container.SecurityContext.RunAsUser)
	require.True(t, *container.SecurityContext.RunAsNonRoot)
	require.False(t, *container.SecurityContext.AllowPrivilegeEscalation)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
}

// TestOBOOutboundSidecarImageFallback verifies fallback to ImageConsulK8S.
func TestOBOOutboundSidecarImageFallback(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOOutbound = "" // not configured
	pod := aiPod("svc", "cm")

	container, err := w.oboOutboundSidecar(pod)
	require.NoError(t, err)
	require.Equal(t, "hashicorp/consul-k8s:test", container.Image)
}

// oboPod returns a plain (non-AI) Pod annotated as an oauth-client for use in
// tests that exercise the OBO-only injection path.
func oboOnlyPod(serviceName string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				constants.AnnotationService:     serviceName,
				constants.AnnotationInject:      "true",
				constants.AnnotationOAuthClient: "true",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "myapp:latest"}},
		},
	}
}

// TestOBOSidecarsInjectedForNonAIPod verifies that a non-AI pod annotated with
// consul.hashicorp.com/oauth-client: "true" receives the consul-obo-inbound and
// consul-obo-outbound sidecars but NOT the consul-mcp-gateway sidecar.
func TestOBOSidecarsInjectedForNonAIPod(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOInbound = "hashicorp/consul-obo-inbound:test"
	w.ImageConsulOBOOutbound = "hashicorp/consul-obo-outbound:test"

	pod := oboOnlyPod("my-svc")

	inbound, err := w.oboInboundSidecar(pod)
	require.NoError(t, err)
	outbound, err := w.oboOutboundSidecar(pod)
	require.NoError(t, err)

	// Should have OBO containers.
	require.Equal(t, constants.ConsulOBOInboundContainerName, inbound.Name)
	require.Equal(t, constants.ConsulOBOOutboundContainerName, outbound.Name)

	// Verify IsOAuthClient recognises the pod.
	require.True(t, common.IsOAuthClient(pod))
	// Verify IsAIAgent does NOT recognise the pod (no mcp-gateway injection).
	require.False(t, common.IsAIAgent(pod))
}

// TestAIAgentPodGetsAllThreeSidecars verifies the injection contract for AI
// agent pods: they receive consul-mcp-gateway, consul-obo-inbound, and
// consul-obo-outbound.
func TestAIAgentPodGetsAllThreeSidecars(t *testing.T) {
	w := baseAIWebhook(t)
	w.ImageConsulOBOInbound = "hashicorp/consul-obo-inbound:test"
	w.ImageConsulOBOOutbound = "hashicorp/consul-obo-outbound:test"

	pod := aiPod("my-agent", "my-mcp-config")

	// All three sidecar builders must succeed.
	mcpGW, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)
	oboIn, err := w.oboInboundSidecar(pod)
	require.NoError(t, err)
	oboOut, err := w.oboOutboundSidecar(pod)
	require.NoError(t, err)

	require.Equal(t, constants.AIContainerName, mcpGW.Name)
	require.Equal(t, constants.ConsulOBOInboundContainerName, oboIn.Name)
	require.Equal(t, constants.ConsulOBOOutboundContainerName, oboOut.Name)

	// IsOAuthClient must be true for AI agent pods.
	require.True(t, common.IsOAuthClient(pod))
	require.True(t, common.IsAIAgent(pod))
}
