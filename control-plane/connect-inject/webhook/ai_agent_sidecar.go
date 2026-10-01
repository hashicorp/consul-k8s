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
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	// mcpGatewayContainer is the injected AI ext_proc sidecar name (historical).
	// One container runs consul-mcp-sc, which supervises MCP and/or OBO.
	mcpGatewayContainer = "mcp-gateway"

	// mcpGatewayUDSPath is the Unix domain socket MCP may listen on when not
	// using TCP. Shared volume with consul-dataplane / Envoy.
	mcpGatewayUDSPath = "/consul/connect-inject/mcp-extproc.sock"

	// defaultMCPScBinary is the supervisor ENTRYPOINT in consul-mcp-sc images.
	defaultMCPScBinary = "/usr/local/bin/consul-mcp-sc"
)

// aiAgentSidecar builds the single AI ext_proc sidecar for ai-agent pods.
// It runs consul-mcp-sc --mode=agent (MCP + OBO inbound + OBO outbound).
//
// runAsUser/runAsGroup must match consul-dataplane: Local Credential Broker
// FetchKey accepts the peer only when SO_PEERCRED UID matches.
func (w *MeshWebhook) aiAgentSidecar(pod corev1.Pod, defaults v1alpha1.AgentDefaults, runAsUser, runAsGroup int64) (corev1.Container, error) {
	image := w.mcpScImage()
	if image == "" {
		return corev1.Container{}, fmt.Errorf(
			"AI sidecar image must be set for ai-agent pods; " +
				"configure ai.agent.image to the consul-mcp-sc multi-binary image")
	}
	mcpPort, useMCPAddr, err := common.ValidateAIAgentAddress(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	// HITL / interceptor / MCP ports come from ResolveAIAgentDefaults (caller).
	hitlPort := defaults.HITL.Port
	if hitlPort == 0 {
		hitlPort = 16101
	}

	readyHost := constants.Getv4orv6Str("127.0.0.1", "::1")
	readyURL := "http://" + net.JoinHostPort(readyHost, strconv.Itoa(constants.DefaultEnvoyAdminPort)) + "/ready"

	args := []string{
		"--mode=agent",
		"--log-level=" + w.LogLevel,
		"--envelope-uds=/consul/connect-inject/oauth-envelope.sock",
		"--broker-uds=/consul/connect-inject/credential-broker.sock",
		"--dataplane-ready-url=" + readyURL,
		// Bind all interfaces (":port"), not 127.0.0.1 — kubelet TCP readiness
		// probes the pod IP. Envoy still dials 127.0.0.1 from the same pod.
		"--obo-inbound-addr=:" + strconv.Itoa(constants.DefaultOBOInboundPort),
		"--obo-outbound-addr=:" + strconv.Itoa(constants.DefaultOBOOutboundPort),
	}

	// MCP transport: TCP when ai-agent-addr is set (xDS dials that addr);
	// otherwise UDS shared with dataplane Envoy.
	if useMCPAddr {
		args = append(args, "--mcp-addr="+pod.Annotations[constants.AnnotationAIAgentAddr])
	} else {
		args = append(args, "--mcp-socket="+mcpGatewayUDSPath)
	}

	childBin, hasChild := pod.Annotations[constants.AnnotationAIAgentChildBinary]
	if hasChild && childBin != "" {
		args = append(args, "--mcp-child="+childBin)
		if childArgs, ok := pod.Annotations[constants.AnnotationAIAgentChildArgs]; ok && childArgs != "" {
			args = append(args, "--mcp-child-args="+childArgs)
		}
	}

	// Supervisor + optional app --child may need a writable root.
	readOnlyRootFS := ptr.To(!hasChild || childBin == "")

	return corev1.Container{
		Name:            mcpGatewayContainer,
		Image:           image,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       defaults.Resources,
		Command:         []string{defaultMCPScBinary},
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
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		// StartupProbe: MCP transport (UDS or TCP), matching pre-combined inject.
		// ReadinessProbe: OBO outbound TCP — kubelet can probe the pod IP on :21103.
		// Both must pass before the pod is Ready (startup gates readiness).
		StartupProbe:   w.mcpScStartupProbe(useMCPAddr, mcpPort),
		ReadinessProbe: w.mcpScReadinessProbe(),
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(runAsUser),
			RunAsGroup:               ptr.To(runAsGroup),
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
	}, nil
}

// mcpScImage returns ai.agent.image (consul-mcp-sc multi-binary: MCP + OBO).
func (w *MeshWebhook) mcpScImage() string {
	return w.ImageAIAgent
}

// mcpScStartupProbe waits for the MCP ext_proc transport used by Envoy:
//   - TCP on the validated wildcard listener when ai-agent-addr is set
//   - UDS socket file otherwise (Kubernetes has no native UDS probe)
func (w *MeshWebhook) mcpScStartupProbe(useTCP bool, port int32) *corev1.Probe {
	if useTCP {
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

// mcpScReadinessProbe checks that OBO outbound is listening (TCP :21103) so
// Envoy can dial the identity-plane filter. Bind is ":port" (all interfaces)
// so kubelet can probe the pod IP.
func (w *MeshWebhook) mcpScReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(constants.DefaultOBOOutboundPort),
			},
		},
		InitialDelaySeconds: 1,
		PeriodSeconds:       5,
		FailureThreshold:    12,
	}
}

// dataplaneContainerRunAs returns the user and group on the consul-dataplane
// container this webhook just built. The AI sidecar must use those IDs: the
// credential broker accepts a peer only when SO_PEERCRED matches dataplane.
func dataplaneContainerRunAs(c corev1.Container) (int64, int64, error) {
	if c.SecurityContext == nil || c.SecurityContext.RunAsUser == nil || c.SecurityContext.RunAsGroup == nil {
		return 0, 0, fmt.Errorf("consul-dataplane container %q is missing runAsUser/runAsGroup", c.Name)
	}
	return *c.SecurityContext.RunAsUser, *c.SecurityContext.RunAsGroup, nil
}
