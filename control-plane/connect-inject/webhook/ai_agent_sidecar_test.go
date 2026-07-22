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
			require.Equal(t, tc.expected, isAIAgent(pod))
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
			require.Equal(t, tc.expected, aiAgentMCPConfigName(pod))
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

	// Command contains expected flags.
	require.Len(t, container.Command, 3)
	cmdStr := container.Command[2]
	require.Contains(t, cmdStr, "consul connect mcp-gateway")
	require.Contains(t, cmdStr, "-service")
	require.Contains(t, cmdStr, "-gateway-binary")
	require.Contains(t, cmdStr, w.GatewayBinary)
	require.Contains(t, cmdStr, "-addr")
	require.Contains(t, cmdStr, "-http-addr")
	require.Contains(t, cmdStr, "-token")
	require.Contains(t, cmdStr, "-log-level")

	// Interceptor port in -addr flag.
	require.Contains(t, cmdStr, "21101")

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

	require.True(t, strings.Contains(container.Command[2], constants.DefaultGatewayBinary))
}

// TestAIAgentSidecarServiceNameFallback verifies that when AnnotationService is
// absent the service name falls back to pod.Spec.ServiceAccountName.
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

	container, err := w.aiAgentSidecar(pod)
	require.NoError(t, err)

	// The service name should appear in the container env vars.
	envMap := make(map[string]string)
	for _, e := range container.Env {
		envMap[e.Name] = e.Value
	}
	require.Equal(t, "my-ai-service", envMap["AI_AGENT_SERVICE"])
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

	require.Equal(t, "my-ai-app", envNames["AI_AGENT_SERVICE"])
	require.Equal(t, "info", envNames["AI_AGENT_LOG_LEVEL"])
	require.Contains(t, envNames, "POD_NAME")
	require.Contains(t, envNames, "POD_NAMESPACE")
}
