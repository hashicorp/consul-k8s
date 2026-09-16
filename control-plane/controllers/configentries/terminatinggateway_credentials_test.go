// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configentries

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// TestTerminatingGatewayCredentialPod asserts the controller's credential
// injection projection: the same normalized security and volume/mount facts the
// Helm chart's terminating-gateways-deployment.bats asserts, so both deployment
// paths agree. It is the controller half of the "controller and Helm output
// agree" requirement.
func TestTerminatingGatewayCredentialPod(t *testing.T) {
	ci := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:                true,
		ProcessorImage:         "camp-auth-processor:test",
		VaultAgentImage:        "hashicorp/vault:test",
		ProcessorConfigMap:     "camp-proc",
		VaultAgentConfigMap:    "camp-agent",
		VaultAddress:           "https://vault.default.svc:8200",
		VaultCAConfigMap:       "camp-ca",
		TokenAudience:          "vault",
		TokenExpirationSeconds: ptr.To(int64(600)),
		DrainSeconds:           ptr.To(int64(45)),
	}
	podSpec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "terminating-gateway-init"}},
		Containers:     []corev1.Container{{Name: "terminating-gateway"}},
	}

	applyTerminatingGatewayCredentialInjection(&podSpec, ci, "consul-k8s-control-plane:test", corev1.PullIfNotPresent, "info")

	// Envoy (main container) gets only the socket mount, never credentials/token.
	envoy := containerByName(t, podSpec.Containers, "terminating-gateway")
	require.True(t, hasMount(envoy, campAuthSocketVolume))
	for _, v := range []string{campVaultRenderedVolume, campVaultTokenVolume, campVaultAgentPrivateVolume, campVaultAgentConfigVolume, campAuthProcessorConfigVolume, campVaultCAVolume} {
		require.Falsef(t, hasMount(envoy, v), "Envoy must not mount %q", v)
	}

	// Narrowest-capability socket init container, socket-only mount.
	si := containerByName(t, podSpec.InitContainers, "camp-auth-socket-init")
	require.Len(t, si.VolumeMounts, 1)
	require.Equal(t, campAuthSocketVolume, si.VolumeMounts[0].Name)
	require.Contains(t, si.SecurityContext.Capabilities.Drop, corev1.Capability("ALL"))
	require.Contains(t, si.SecurityContext.Capabilities.Add, corev1.Capability("CHOWN"))
	require.Equal(t, ptr.To(false), si.SecurityContext.AllowPrivilegeEscalation)

	// Vault Agent init: hardened 10001, exits after auth, read-only token mount.
	ai := containerByName(t, podSpec.InitContainers, "camp-vault-agent-init")
	require.Contains(t, ai.Args, "-exit-after-auth")
	require.Equal(t, campCredentialUID, *ai.SecurityContext.RunAsUser)
	require.Equal(t, campCredentialUID, *ai.SecurityContext.RunAsGroup)
	require.True(t, mountReadOnly(ai, campVaultTokenVolume))

	// Vault Agent sidecar runs continuously (no exit-after-auth).
	as := containerByName(t, podSpec.Containers, "camp-vault-agent")
	require.NotContains(t, as.Args, "-exit-after-auth")
	require.Equal(t, campCredentialUID, *as.SecurityContext.RunAsUser)

	// Processor: 10001, read-only creds, writable socket, no token/token-sink,
	// health + drain contract.
	proc := containerByName(t, podSpec.Containers, "camp-auth-processor")
	require.Equal(t, campCredentialUID, *proc.SecurityContext.RunAsUser)
	require.True(t, mountReadOnly(proc, campVaultRenderedVolume))
	require.True(t, hasMount(proc, campAuthSocketVolume))
	require.False(t, mountReadOnly(proc, campAuthSocketVolume), "socket must be writable")
	require.False(t, hasMount(proc, campVaultTokenVolume), "processor must not mount the Vault token")
	require.False(t, hasMount(proc, campVaultAgentPrivateVolume), "processor must not mount the Agent token sink")
	require.NotNil(t, proc.ReadinessProbe.Exec)
	require.Contains(t, proc.ReadinessProbe.Exec.Command, "-ready")
	require.NotNil(t, proc.LivenessProbe.Exec)
	require.Contains(t, proc.LivenessProbe.Exec.Command, "-live")
	require.Contains(t, proc.Lifecycle.PreStop.Exec.Command, "drain-wait")
	require.Contains(t, proc.Lifecycle.PreStop.Exec.Command, "-duration=45s")

	// Shared supplemental group for the socket.
	require.Contains(t, podSpec.SecurityContext.SupplementalGroups, campCredentialUID)

	// All credential volumes present; token projection carries audience/expiration.
	for _, v := range []string{campVaultRenderedVolume, campAuthSocketVolume, campVaultTokenVolume, campVaultAgentPrivateVolume, campVaultAgentConfigVolume, campAuthProcessorConfigVolume, campVaultCAVolume} {
		require.Truef(t, hasVolume(podSpec.Volumes, v), "missing volume %q", v)
	}
	tok := volumeByName(t, podSpec.Volumes, campVaultTokenVolume)
	require.Equal(t, "vault", tok.Projected.Sources[0].ServiceAccountToken.Audience)
	require.Equal(t, int64(600), *tok.Projected.Sources[0].ServiceAccountToken.ExpirationSeconds)
}

func TestTerminatingGatewayCredentialPodDisabled(t *testing.T) {
	podSpec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "terminating-gateway-init"}},
		Containers:     []corev1.Container{{Name: "terminating-gateway"}},
	}
	// nil and disabled are both no-ops.
	applyTerminatingGatewayCredentialInjection(&podSpec, nil, "img", corev1.PullIfNotPresent, "info")
	applyTerminatingGatewayCredentialInjection(&podSpec, &consulv1alpha1.TerminatingGatewayCredentialInjection{Enabled: false}, "img", corev1.PullIfNotPresent, "info")
	require.Len(t, podSpec.Containers, 1)
	require.Len(t, podSpec.InitContainers, 1)
	require.Empty(t, podSpec.Volumes)
	require.Nil(t, podSpec.SecurityContext)
	require.Empty(t, podSpec.Containers[0].VolumeMounts)
}

func containerByName(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, c := range containers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found", name)
	return corev1.Container{}
}

func findMount(c corev1.Container, name string) (corev1.VolumeMount, bool) {
	for _, m := range c.VolumeMounts {
		if m.Name == name {
			return m, true
		}
	}
	return corev1.VolumeMount{}, false
}

func hasMount(c corev1.Container, name string) bool {
	_, ok := findMount(c, name)
	return ok
}

func mountReadOnly(c corev1.Container, name string) bool {
	m, ok := findMount(c, name)
	return ok && m.ReadOnly
}

func hasVolume(volumes []corev1.Volume, name string) bool {
	for _, v := range volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func volumeByName(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()
	for _, v := range volumes {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("volume %q not found", name)
	return corev1.Volume{}
}
