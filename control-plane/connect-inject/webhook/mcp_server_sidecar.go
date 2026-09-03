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
	mcpServerContainer     = "mcp-server"
	mcpServerContainerPort = "mcp-interceptor"
)

// mcpServerSidecar builds the mcp-server sidecar container that is injected
// alongside consul-dataplane when a pod has the annotation
// consul.hashicorp.com/ai-role: "mcp-server".
//
// Per-pod annotations take precedence over the McpServerDefaults from the CRD.
func (w *MeshWebhook) mcpServerSidecar(pod corev1.Pod, defaults v1alpha1.McpServerDefaults) corev1.Container {
	// Resolve config: pod annotations override CRD defaults.
	interceptorPort := defaults.InterceptorPort
	if interceptorPort == 0 {
		interceptorPort = 21102
	}
	if v, ok := pod.Annotations[constants.AnnotationAIMCPServerPort]; ok {
		if p, err := strconv.ParseInt(v, 10, 32); err == nil {
			interceptorPort = int32(p)
		}
	}

	transport := defaults.Transport
	if transport == "" {
		transport = "streamable-http"
	}
	if v, ok := pod.Annotations[constants.AnnotationAIMCPServerTransport]; ok && v != "" {
		transport = v
	}

	path := defaults.Path
	if path == "" {
		path = "/mcp"
	}
	if v, ok := pod.Annotations[constants.AnnotationAIMCPServerPath]; ok && v != "" {
		path = v
	}

	protocolVersion := defaults.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = "2025-03-26"
	}
	if v, ok := pod.Annotations[constants.AnnotationAIMCPServerProtocolVersion]; ok && v != "" {
		protocolVersion = v
	}

	args := []string{
		"-transport=" + transport,
		"-path=" + path,
		"-protocol-version=" + protocolVersion,
		"-port=" + strconv.Itoa(int(interceptorPort)),
		"-log-level=" + w.LogLevel,
		"-log-json=" + strconv.FormatBool(w.LogJSON),
	}

	container := corev1.Container{
		Name:            mcpServerContainer,
		Image:           w.ImageMCPServer,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       defaults.Resources,
		Command:         []string{"/bin/mcp-server"},
		Args:            args,
		Ports: []corev1.ContainerPort{
			{
				Name:          mcpServerContainerPort,
				ContainerPort: interceptorPort,
			},
		},
		Env: []corev1.EnvVar{
			{Name: "MCP_PORT", Value: strconv.Itoa(int(interceptorPort))},
			{Name: "MCP_TRANSPORT", Value: transport},
			{Name: "MCP_PATH", Value: path},
			{Name: "MCP_PROTOCOL_VERSION", Value: protocolVersion},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(int(interceptorPort)),
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
