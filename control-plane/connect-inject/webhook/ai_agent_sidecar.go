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

// aiAgentSidecar builds and returns the consul-mcp-gateway sidecar container
// that runs alongside the standard consul-dataplane sidecar for AI agent pods.
// It expects the ConfigMap volume (aiAgentConfigVolumeName) to already have been
// appended to pod.Spec.Volumes by Handle().
func (w *MeshWebhook) aiAgentSidecar(pod corev1.Pod) (corev1.Container, error) {
	gatewayBinary := w.GatewayBinary
	if gatewayBinary == "" {
		gatewayBinary = constants.DefaultGatewayBinary
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
