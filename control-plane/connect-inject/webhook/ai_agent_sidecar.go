// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"net"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	// aiAgentConfigVolumeName is the name of the volume that mounts the MCP
	// agent ConfigMap into the consul-mcp-gateway container.
	aiAgentConfigVolumeName = "consul-ai-agent-config"

	// aiAgentConfigMountPath is where the MCP ConfigMap is mounted inside the
	// consul-mcp-gateway container.
	aiAgentConfigMountPath = "/consul/ai-agent-config"
)

// isAIAgent returns true when the pod carries the AI agent role annotation with
// the expected "ai-agent" value.
func isAIAgent(pod corev1.Pod) bool {
	return pod.Annotations[constants.AnnotationAIRole] == constants.AIAgentRole
}

// aiAgentMCPConfigName returns the ConfigMap name from the MCP config annotation,
// or an empty string if the annotation is absent.
func aiAgentMCPConfigName(pod corev1.Pod) string {
	return pod.Annotations[constants.AnnotationAIAgentMCPConfig]
}

// aiAgentSidecar builds and returns the consul-mcp-gateway sidecar container
// that runs alongside the standard consul-dataplane sidecar for AI agent pods.
// It expects the ConfigMap volume (aiAgentConfigVolumeName) to already have been
// appended to pod.Spec.Volumes by Handle().
func (w *MeshWebhook) aiAgentSidecar(pod corev1.Pod) (corev1.Container, error) {
	gatewayBinary := w.GatewayBinary
	if gatewayBinary == "" {
		gatewayBinary = constants.DefaultGatewayBinary
	}

	// Resolve the service name the same way the rest of the webhook does:
	// prefer the explicit annotation, fall back to the pod's ServiceAccountName
	// (which by convention matches the Consul service name for connect-injected
	// pods), and finally fall back to the pod's GenerateName prefix if both are
	// absent (e.g. during early webhook admission before a name is assigned).
	serviceName := pod.Annotations[constants.AnnotationService]
	if serviceName == "" {
		serviceName = pod.Spec.ServiceAccountName
	}

	// The consul-mcp-gateway container runs from the dedicated
	// consul-ai-mcp-interceptor image which ships the consul binary with the
	// `consul connect mcp-gateway` subcommand.
	image := w.ImageConsulAIMCPInterceptor
	if image == "" {
		// Fall back to the consul-k8s image so that the webhook still works in
		// environments where the AI interceptor image has not been configured yet.
		image = w.ImageConsulK8S
	}

	container := corev1.Container{
		Name:            constants.AIContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       w.DefaultConsulSidecarResources,
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			{
				Name:  "AI_AGENT_SERVICE",
				Value: serviceName,
			},
			{
				Name:  "AI_AGENT_LOG_LEVEL",
				Value: w.LogLevel,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				// Shared data volume written by consul-connect-inject-init and
				// read by consul-dataplane; the mcp-gateway reads the Consul HTTP
				// address and token files from here.
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
				ReadOnly:  true,
			},
			{
				// The MCP agent ConfigMap mounted by Handle().
				Name:      aiAgentConfigVolumeName,
				MountPath: aiAgentConfigMountPath,
				ReadOnly:  true,
			},
		},
		Command: []string{constants.ConsulBinarypath},
		Args: []string{
		"connect",
		"mcp-gateway",
		"-gateway-binary",
		gatewayBinary,
  		"-addr",
		net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultAIInterceptorPort)),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	return container, nil
}
