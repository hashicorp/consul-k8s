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
	// The consul-mcp-gateway container runs from the dedicated
	// consul-ai-mcp-interceptor image which ships the standalone
	// consul-mcp-gateway binary at /app/consul-mcp-gateway.
	// It is invoked directly — there is no `consul connect mcp-gateway` wrapper.
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
		// Invoke the standalone binary directly.
		Command: []string{constants.DefaultGatewayBinary},
		Args: []string{
			"--addr",
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

// oboInboundSidecar builds and returns the consul-obo-inbound sidecar container
// that handles INBOUND OBO for AI agent pods (ai-role=ai-agent).
//
// It listens on loopback :21102 (DefaultOBOInboundPort) and is wired by the
// Consul xDS generator as an ext_proc filter on the inbound Envoy listener.
// Its responsibilities are:
//
//  1. Verify the inbound OBO JWT against the expected audience carried in the
//     trusted x-consul-obo-expected-hop stream metadata (INV-7).
//  2. Validate the projected x-user-* / x-claim-* headers against the verified
//     claims, rejecting forged or mismatched values (INV-8).
//
// Inbound never exchanges or re-projects a token; that is the outbound
// sidecar's job. It therefore holds no OAuth credentials at all.
//
// The expected audience, destination, and IAM issuer all arrive as ext_proc
// stream metadata from the Consul xDS generator, so no identity flags are
// strictly required. The flags set below are an independent cross-check.
//
// This sidecar is injected only for AI agent pods (AnnotationAIRole=ai-agent).
func (w *MeshWebhook) oboInboundSidecar(namespace corev1.Namespace, pod corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOInbound
	if image == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOInbound must be set when OBO is enabled; " +
				"the consul-k8s image does not contain the consul-obo-inbound binary")
	}

	container := corev1.Container{
		Name:            constants.ConsulOBOInboundContainerName,
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
				// Shared data volume written by consul-connect-inject-init;
				// the obo-inbound sidecar reads the Consul HTTP address and
				// token files from here.
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
				ReadOnly:  true,
			},
		},
		// The consul-obo-inbound binary is invoked directly — it does not need
		// the consul connect mcp-gateway wrapper.
		Command: []string{constants.DefaultOBOInboundBinary},
		Args: []string{
			"--addr",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultOBOInboundPort)),
			"--log-level=info",
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

	// Bind this sidecar to its own Consul identity so it can reject an
	// expected-hop whose destination names a different service. Unlike the
	// outbound sidecar the service annotation is not mandatory here — it is
	// only used for the cross-check, not to derive an SDS resource name — so a
	// pod without it still gets a working verifier driven purely by xDS.
	if svcName := pod.Annotations[constants.AnnotationService]; svcName != "" {
		container.Args = append(container.Args, "--service-name="+svcName)
	}
	if w.EnableNamespaces {
		container.Args = append(container.Args,
			"--namespace="+w.consulNamespace(namespace.Name))
	}
	if w.ConsulPartition != "" {
		container.Args = append(container.Args,
			"--partition="+w.ConsulPartition)
	}

	return container, nil
}

// oboOutboundSidecar builds and returns the consul-obo-outbound sidecar
// container that handles OUTBOUND OBO for ALL outbound calls from AI agent
// pods (ai-role=ai-agent). It covers A2A, A2REST, A2MCP, and A2LLM paths.
//
// It listens on loopback :21103 (DefaultOBOOutboundPort) and is wired by the
// Consul xDS generator as an ext_proc filter on the outbound Envoy listener
// for every ai-agent service. Its responsibilities are:
//
//  1. Detect that the outbound request carries a user-context bearer token.
//  2. Perform RFC 8693 OBO exchange for the target service audience.
//  3. Replace the Authorization header with the audience-bound JWT before the
//     request leaves the agent's Envoy sidecar over mTLS.
//
// The OAuth private key is delivered via xDS (x-consul-oauth-config gRPC
// stream metadata) — never via file, env var, or Kubernetes Secret.
func (w *MeshWebhook) oboOutboundSidecar(namespace corev1.Namespace, pod corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOOutbound
	if image == "" {
		return corev1.Container{}, fmt.Errorf(
			"ImageConsulOBOOutbound must be set when OBO is enabled; " +
				"the consul-k8s image does not contain the consul-obo-outbound binary")
	}

	// Derive the Consul service name from the connect-service annotation.
	// This is the value written as cfgSnap.Service in proxycfg, and therefore
	// the suffix of the SDS resource name "oauth/<svcName>" emitted by
	// secretsFromSnapshotConnectProxy in consul-enterprise/agent/xds/secrets.go.
	svcName := pod.Annotations[constants.AnnotationService]
	if svcName == "" {
		// Without the service name we cannot construct the SDS resource name.
		// Fail loudly rather than subscribing to "oauth/" (empty suffix) which
		// would watch a resource that never exists.
		return corev1.Container{}, fmt.Errorf(
			"annotation %q must be set on the pod when OBO is enabled; "+
				"it is required to derive the SDS resource name (oauth/<svcName>)",
			constants.AnnotationService)
	}

	container := corev1.Container{
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
			// RunAsUser MUST match sidecarUserAndGroupID (5995) — the same UID
			// that consul-connect-inject-init adds to the iptables RETURN rule
			// (CONSUL_PROXY_OUTPUT chain, "owner UID match 5995").
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
	}

	return container, nil
}
