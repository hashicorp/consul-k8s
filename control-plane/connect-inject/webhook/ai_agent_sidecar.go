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
// that handles INBOUND OBO for AI agent pods (and any pod with oauth_client=true).
//
// It listens on loopback :21102 (DefaultOBOInboundPort) and is wired by the
// Consul xDS generator as an ext_proc filter on the inbound Envoy listener.
// Its responsibilities are:
//
//  1. Strip any forged x-user-* / x-claim-* headers from the caller (INV-8).
//  2. Perform RFC 8693 OBO token exchange: swap the inbound bearer token for
//     an audience-bound JWT signed with the service's EC P-256 key.
//  3. Project x-user-role (pipe-separated UPPER CASE groups), x-user-aud,
//     x-claim-sub, x-claim-email so that the downstream jwt_authn / RBAC
//     filters can evaluate role-based intentions.
//
// The OAuth private key is delivered via xDS (x-consul-oauth-config gRPC
// stream metadata) — never via file, env var, or Kubernetes Secret.
//
// This sidecar is injected for all pods that have opted into the OBO identity
// plane: AI agent pods (AnnotationAIRole=ai-agent) and any pod annotated with
// consul.hashicorp.com/oauth-client: "true".
func (w *MeshWebhook) oboInboundSidecar(_ corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOInbound
	if image == "" {
		// Fall back to the consul-k8s image so the webhook still works in
		// environments where the dedicated image has not been configured yet.
		image = w.ImageConsulK8S
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

	return container, nil
}

// oboOutboundSidecar builds and returns the consul-obo-outbound sidecar
// container that handles OUTBOUND OBO for ALL outbound calls from any service
// with oauth_client=true.  It is not specific to MCP — it covers A2A, A2REST,
// A2MCP, and A2LLM outbound paths.
//
// It listens on loopback :21103 (DefaultOBOOutboundPort) and is wired by the
// Consul xDS generator as an ext_proc filter on the outbound Envoy listener
// for every oauth_client=true service.  Its responsibilities are:
//
//  1. Detect that the outbound request carries a user-context bearer token.
//  2. Perform RFC 8693 OBO exchange for the target service audience.
//  3. Replace the Authorization header with the audience-bound JWT before the
//     request leaves the agent's Envoy sidecar over mTLS.
//
// The OAuth private key is delivered via xDS (x-consul-oauth-config gRPC
// stream metadata) — never via file, env var, or Kubernetes Secret.
func (w *MeshWebhook) oboOutboundSidecar(pod corev1.Pod) (corev1.Container, error) {
	image := w.ImageConsulOBOOutbound
	if image == "" {
		// Fall back to the consul-k8s image so the webhook still works in
		// environments where the dedicated image has not been configured yet.
		image = w.ImageConsulK8S
	}

	// Derive the Consul service name from the connect-service annotation.
	// This is the value written as cfgSnap.Service in proxycfg, and therefore
	// the suffix of the SDS resource name "oauth/<svcName>" emitted by
	// secretsFromSnapshotConnectProxy in consul-enterprise/agent/xds/secrets.go.
	svcName := pod.Annotations[constants.AnnotationService]

	xdsAddr := net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultDataplaneXDSPort))
	sdsResource := "oauth/" + svcName

	container := corev1.Container{
		Name:            constants.ConsulOBOOutboundContainerName,
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
				// the obo-outbound sidecar reads the Consul HTTP address and
				// token files from here.
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
				ReadOnly:  true,
			},
		},
		// The consul-obo-outbound binary is invoked directly — it does not need
		// the consul connect mcp-gateway wrapper.
		Command: []string{constants.DefaultOBOOutboundBinary},
		Args: []string{
			"--addr",
			net.JoinHostPort("127.0.0.1", fmt.Sprint(constants.DefaultOBOOutboundPort)),
			"--log-level=info",
			// SDS client: subscribe to the GenericSecret carrying the
			// OAuthClientConfig (EC private key + client-id + token endpoint).
			// consul-dataplane binds xDS on a fixed port (DefaultDataplaneXDSPort)
			// so this address is stable across pod restarts.
			"--xds-addr=" + xdsAddr,
			"--sds-resource=" + sdsResource,
			// consul-connect-inject-init writes the sidecar proxy service-ID to
			// this file.  The SDS client sends it as node.Id in the first
			// DeltaDiscoveryRequest so the Consul server can locate the correct
			// proxy snapshot and push the GenericSecret (oauth/<svcName>).
			"--proxy-id-file=/consul/connect-inject/proxyid",
			// consul-connect-inject-init also writes the Consul node name to this
			// file (value: $(NODE_NAME)-virtual, same source as DP_SERVICE_NODE_NAME).
			// The SDS client sends it as node.metadata.node_name so the Consul server
			// resolves the same proxycfg identity that Envoy uses.  Without it the
			// server falls back to s.NodeName which may differ, causing the SDS stream
			// to watch the wrong snapshot and never push the oauth/<svcName> secret.
			"--node-name-file=/consul/connect-inject/nodename",
			// Wait for Envoy to be fully ready before opening the SDS subscription.
			// consul-obo-outbound starts at pod init, before consul-dataplane has
			// established its ADS stream to the Consul server.  If the SDS stream
			// opens while consul-dataplane is still bootstrapping, the Consul server
			// has not yet built the proxy snapshot and will never push the GenericSecret.
			//
			// We poll Envoy admin :19000/ready (returns "LIVE" + HTTP 200 only after
			// all clusters are initialised and the ADS stream is active) — NOT the
			// graceful_startup endpoint (:20600) which with startupGracePeriodSeconds=0
			// returns HTTP 200 immediately, before consul-dataplane has set up its ADS
			// stream or the Consul server has built the proxy snapshot.
			fmt.Sprintf("--dataplane-ready-url=http://127.0.0.1:%d/ready",
				constants.DefaultEnvoyAdminPort),
		},
		SecurityContext: &corev1.SecurityContext{
			// RunAsUser MUST match sidecarUserAndGroupID (5995) — the same UID
			// that consul-connect-inject-init adds to the iptables RETURN rule
			// (CONSUL_PROXY_OUTPUT chain, "owner UID match 5995"). Without this,
			// the obo-outbound container's outbound HTTPS calls to IBM Verify
			// are captured by transparent proxy → redirected to :15001 → Envoy's
			// original-destination cluster returns zero bytes (SO_ORIGINAL_DST
			// loop on loopback-redirected connections in kind+Podman), causing
			// SSL_EOF on every token exchange attempt.
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
