// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"net"
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
	//   --socket     unix domain socket for ext_proc (shared with Envoy/consul-dataplane)
	//   --addr       TCP fallback address when --socket is not set (default: :21101)
	//   --log-level  maps to the webhook-wide log level
	//   --child      optional: path to child binary to co-supervise (from pod annotation)
	//   --child-args optional: args for the child binary (from pod annotation)
	//
	// Transport selection:
	//   The custom Consul build's mcp_ext_proc_interceptor cluster dials
	//   127.0.0.1:21101 (TCP). The binary's listen() opens either UDS or TCP —
	//   never both. When the ai-agent-addr annotation is set we use TCP only
	//   (no --socket) so the Consul ext_proc cluster can connect. Otherwise
	//   we use UDS (production default, shared with consul-dataplane Envoy).
	var args []string
	if addr, ok := pod.Annotations[constants.AnnotationAIAgentAddr]; ok && addr != "" {
		// TCP mode: Consul's mcp_ext_proc_interceptor cluster dials this address.
		// Do NOT pass --socket — listen() only opens one transport.
		args = []string{
			"--addr=" + addr,
			"--log-level=" + w.LogLevel,
		}
	} else {
		// UDS mode: default production transport shared with consul-dataplane.
		args = []string{
			"--socket=" + mcpGatewayUDSPath,
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
		// Explicit Command required: Args alone replace image CMD, and the
		// playground mcp-gateway image has CMD not ENTRYPOINT — without this,
		// kubelet tries to exec "--socket=..." as the binary (exit 128).
		Command: []string{constants.DefaultMCPGatewayBinary},
		Args:    args,
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

// mcpGatewayReadinessProbe returns the correct readiness probe depending on
// whether the mcp-gateway is running in TCP mode (ai-agent-addr annotation set)
// or UDS mode (default).
//
//   - TCP mode: TCPSocket probe on the configured addr port — the binary binds
//     TCP and no socket file is created.
//   - UDS mode: exec "test -S <socket>" — Kubernetes does not support UDS probes
//     natively so we stat the socket file instead.
func (w *MeshWebhook) mcpGatewayReadinessProbe(pod corev1.Pod, hitlPort int32) *corev1.Probe {
	if addr, ok := pod.Annotations[constants.AnnotationAIAgentAddr]; ok && addr != "" {
		// Parse the port from the addr string e.g. ":21101" → 21101.
		// Fall back to hitlPort if the addr is malformed.
		port := hitlPort
		if len(addr) > 1 {
			if p, err := strconv.ParseInt(addr[1:], 10, 32); err == nil {
				port = int32(p)
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
	// UDS mode: probe the socket file.
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

// oboInboundSidecar builds consul-obo-inbound for ai-agent pods.
// Listens on loopback :21102; envelope + broker UDS for split-knowledge credentials.
// Verify-only — no private key material in this process beyond envelope decrypt.
func (w *MeshWebhook) oboInboundSidecar(runAsUser, runAsGroup int64) (corev1.Container, error) {
	if w.ImageConsulOBOInbound == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOInbound must be set for ai-agent pods; " +
				"configure ai.obo.inbound.image (or -consul-obo-inbound-image)")
	}
	return w.oboSidecar(runAsUser, runAsGroup, constants.ConsulOBOInboundContainerName, w.ImageConsulOBOInbound, constants.DefaultOBOInboundBinary, constants.DefaultOBOInboundPort)
}

// oboOutboundSidecar builds consul-obo-outbound for ai-agent pods.
// Listens on loopback :21103; RFC 8693 exchange after envelope decrypt via broker.
// Not an SDS/xDS client — envelope UDS + Local Credential Broker only.
func (w *MeshWebhook) oboOutboundSidecar(runAsUser, runAsGroup int64) (corev1.Container, error) {
	if w.ImageConsulOBOOutbound == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOOutbound must be set for ai-agent pods; " +
				"configure ai.obo.outbound.image (or -consul-obo-outbound-image)")
	}
	return w.oboSidecar(runAsUser, runAsGroup, constants.ConsulOBOOutboundContainerName, w.ImageConsulOBOOutbound, constants.DefaultOBOOutboundBinary, constants.DefaultOBOOutboundPort)
}

func (w *MeshWebhook) oboSidecar(runAsUser, runAsGroup int64, name, image, binary string, port int) (corev1.Container, error) {
	// Listen address stays 127.0.0.1. xDS OBO clusters dial that address.
	// Only the Envoy admin readiness URL follows the dual-stack bind.
	readyHost := constants.Getv4orv6Str("127.0.0.1", "::1")
	readyURL := "http://" + net.JoinHostPort(readyHost, strconv.Itoa(constants.DefaultEnvoyAdminPort)) + "/ready"

	return corev1.Container{
		Name:            name,
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
		Command: []string{binary},
		Args: []string{
			"--addr",
			net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			"--log-level=info",
			"--envelope-uds=/consul/connect-inject/oauth-envelope.sock",
			"--broker-uds=/consul/connect-inject/credential-broker.sock",
			"--dataplane-ready-url=" + readyURL,
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(runAsUser),
			RunAsGroup:               ptr.To(runAsGroup),
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

// dataplaneContainerRunAs returns the user and group on the consul-dataplane
// container this webhook just built. OBO must use those IDs: the credential
// broker accepts a peer only when SO_PEERCRED matches the dataplane process.
func dataplaneContainerRunAs(c corev1.Container) (int64, int64, error) {
	if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil || c.SecurityContext.RunAsGroup == nil {
		return 0, 0, fmt.Errorf("consul-dataplane container %q is missing runAsUser/runAsGroup", c.Name)
	}
	return *c.SecurityContext.RunAsUser, *c.SecurityContext.RunAsGroup, nil
}
