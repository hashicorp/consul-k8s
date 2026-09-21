// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"net"
	"strconv"
	"strings"

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
	//   --socket     unix domain socket for ext_proc (shared with Envoy/consul-dataplane)
	//   --addr       TCP fallback address when --socket is not set (default: :21101)
	//   --log-level  maps to the webhook-wide log level
	//   --child      optional: path to child binary to co-supervise (from pod annotation)
	//   --child-args optional: args for the child binary (from pod annotation)
	//
	// Transport selection:
	//   Enterprise xDS mcp_ext_proc_interceptor dials 127.0.0.1:21101 (TCP).
	//   Default to TCP so CAMP stacks work without an annotation. Opt into UDS
	//   with consul.hashicorp.com/ai-agent-transport: uds (shared volume socket).
	//   When ai-agent-addr is set, that TCP address wins.
	var args []string
	if addr, ok := pod.Annotations[constants.AnnotationAIAgentAddr]; ok && addr != "" {
		args = []string{
			"--addr=" + addr,
			"--log-level=" + w.LogLevel,
		}
	} else if pod.Annotations[constants.AnnotationAIAgentTransport] == "uds" {
		args = []string{
			"--socket=" + mcpGatewayUDSPath,
			"--log-level=" + w.LogLevel,
		}
	} else {
		args = []string{
			"--addr=127.0.0.1:" + strconv.Itoa(constants.DefaultAIInterceptorPort),
			"--log-level=" + w.LogLevel,
		}
	}

	childBin, hasChild := pod.Annotations[constants.AnnotationAIAgentChildBinary]
	if hasChild && childBin != "" {
		args = append(args, "--child="+childBin)
		if childArgs, ok := pod.Annotations[constants.AnnotationAIAgentChildArgs]; ok && childArgs != "" {
			args = append(args, "--child-args="+childArgs)
		}
	}

	// ReadOnlyRootFilesystem is safe when consul-mcp-gateway runs alone.
	// When --child is set the child binary runs in the same container and may
	// need to write temp files, so we allow a writable root in that case.
	readOnlyRootFS := ptr.To(!hasChild || childBin == "")

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
		ReadinessProbe: w.mcpGatewayReadinessProbe(pod, hitlPort),
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
			ReadOnlyRootFilesystem: readOnlyRootFS,
		},
	}

	return container
}

// mcpGatewayReadinessProbe returns the readiness probe for TCP (default) or UDS mode.
func (w *MeshWebhook) mcpGatewayReadinessProbe(pod corev1.Pod, hitlPort int32) *corev1.Probe {
	if pod.Annotations[constants.AnnotationAIAgentTransport] == "uds" &&
		pod.Annotations[constants.AnnotationAIAgentAddr] == "" {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"test", "-S", mcpGatewayUDSPath},
				},
			},
			InitialDelaySeconds: 1,
			PeriodSeconds:       5,
			FailureThreshold:    12,
		}
	}

	port := int32(constants.DefaultAIInterceptorPort)
	if addr, ok := pod.Annotations[constants.AnnotationAIAgentAddr]; ok && addr != "" {
		// Parse ":21101" or "127.0.0.1:21101".
		if idx := strings.LastIndex(addr, ":"); idx >= 0 && idx+1 < len(addr) {
			if p, err := strconv.ParseInt(addr[idx+1:], 10, 32); err == nil {
				port = int32(p)
			}
		} else if hitlPort != 0 {
			port = hitlPort
		}
	}
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(int(port)),
			},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       5,
		FailureThreshold:    12,
	}
}

// oboInboundSidecar builds consul-obo-inbound for ai-agent pods.
// Listens on loopback :21102; envelope + broker UDS for split-knowledge credentials.
// Verify-only — no private key material in this process beyond envelope decrypt.
func (w *MeshWebhook) oboInboundSidecar(_ corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOInbound
	if image == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOInbound must be set for ai-agent pods; " +
				"configure ai.obo.inbound.image (or -consul-obo-inbound-image)")
	}

	return corev1.Container{
		Name:            constants.ConsulOBOInboundContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       w.DefaultConsulSidecarResources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
				ReadOnly:  true,
			},
		},
		Command: []string{constants.DefaultOBOInboundBinary},
		Args: []string{
			"--addr",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultOBOInboundPort)),
			"--log-level=info",
			"--envelope-uds=/consul/connect-inject/oauth-envelope.sock",
			"--broker-uds=/consul/connect-inject/credential-broker.sock",
			fmt.Sprintf("--dataplane-ready-url=http://127.0.0.1:%d/ready",
				constants.DefaultEnvoyAdminPort),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(sidecarUserAndGroupID)),
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
	}, nil
}

// oboOutboundSidecar builds consul-obo-outbound for ai-agent pods.
// Listens on loopback :21103; RFC 8693 exchange after envelope decrypt via broker.
// Not an SDS/xDS client — envelope UDS + Local Credential Broker only.
func (w *MeshWebhook) oboOutboundSidecar(_ corev1.Namespace, _ corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOOutbound
	if image == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOOutbound must be set for ai-agent pods; " +
				"configure ai.obo.outbound.image (or -consul-obo-outbound-image)")
	}

	return corev1.Container{
		Name:            constants.ConsulOBOOutboundContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       w.DefaultConsulSidecarResources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
				ReadOnly:  true,
			},
		},
		Command: []string{constants.DefaultOBOOutboundBinary},
		Args: []string{
			"--addr",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultOBOOutboundPort)),
			"--log-level=info",
			"--envelope-uds=/consul/connect-inject/oauth-envelope.sock",
			"--broker-uds=/consul/connect-inject/credential-broker.sock",
			fmt.Sprintf("--dataplane-ready-url=http://127.0.0.1:%d/ready",
				constants.DefaultEnvoyAdminPort),
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(sidecarUserAndGroupID)),
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
	}, nil
}
