// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	// mcpGatewayContainer is the name of the mcp-gateway sidecar injected
	// alongside an ai-agent pod. It serves the Envoy ext_proc gRPC interface
	// that intercepts MCP JSON-RPC traffic and stamps routing/HITL headers.
	mcpGatewayContainer = "mcp-gateway"

	// mcpGatewayUDSPath is the Unix domain socket the mcp-gateway listens on.
	// It is placed inside the shared consul-connect-inject-data volume so both
	// the mcp-gateway container and Envoy (consul-dataplane) can reach it.
	mcpGatewayUDSPath = "/consul/connect-inject/mcp-extproc.sock"
)

// aiAgentSidecar builds the mcp-gateway sidecar container that is injected
// alongside consul-dataplane when a pod has the annotation
// consul.hashicorp.com/ai-role: "ai-agent".
//
// The mcp-gateway binary (consul-mcp-gateway) serves the Envoy ext_proc gRPC
// server on a Unix domain socket shared with consul-dataplane. It intercepts
// MCP JSON-RPC requests and stamps x-mcp-* routing/HITL decision headers.
//
// Per-pod annotations take precedence over the AgentDefaults from the CRD.
func (w *MeshWebhook) aiAgentSidecar(pod corev1.Pod, defaults v1alpha1.AgentDefaults) corev1.Container {
	// Resolve HITL port: used only as a named container port for observability.
	hitlPort := defaults.HITL.Port
	if hitlPort == 0 {
		hitlPort = 16101
	}
	if v, ok := pod.Annotations[constants.AnnotationAIAgentHITLPort]; ok {
		if p, err := strconv.ParseInt(v, 10, 32); err == nil {
			hitlPort = int32(p)
		}
	}

	// consul-mcp-gateway flags:
	//   --socket   unix domain socket for ext_proc (shared with Envoy/consul-dataplane)
	//   --log-level  maps to the webhook-wide log level
	args := []string{
		"--socket=" + mcpGatewayUDSPath,
		"--log-level=" + w.LogLevel,
	}

	container := corev1.Container{
		Name:            mcpGatewayContainer,
		Image:           w.ImageAIAgent,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       defaults.Resources,
		Args:            args,
		Ports: []corev1.ContainerPort{
			{
				Name:          "hitl",
				ContainerPort: hitlPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				// Shared with consul-dataplane and Envoy so the UDS is accessible
				// to all three containers.
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				// The socket file appears once mcp-gateway is ready to accept
				// connections. Use a TCP socket probe on the HITL port as a
				// lightweight liveness signal until UDS probes are supported.
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(int(hitlPort)),
				},
			},
			InitialDelaySeconds: 1,
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(sidecarUserAndGroupID)),
			RunAsGroup:               ptr.To(int64(sidecarUserAndGroupID)),
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: ptr.To(true),
		},
	}

	return container
}
