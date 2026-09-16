// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configentries

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// Volume names, mount paths, and identities for terminating-gateway Vault-only
// credential injection. These mirror the Helm chart projection exactly so the
// controller and chart produce the same pod. No secret material is ever placed
// in the pod spec; credentials are rendered by the Vault Agent at runtime.
const (
	campVaultRenderedVolume       = "camp-vault-rendered"
	campAuthSocketVolume          = "camp-auth-socket"
	campVaultTokenVolume          = "camp-vault-token"
	campVaultAgentPrivateVolume   = "camp-vault-agent-private"
	campVaultAgentConfigVolume    = "camp-vault-agent-config"
	campAuthProcessorConfigVolume = "camp-auth-processor-config"
	campVaultCAVolume             = "camp-vault-ca"

	campVaultRenderedPath   = "/consul/vault-rendered"
	campAuthSocketDir       = "/consul/auth-socket"
	campAuthSocketFile      = "/consul/auth-socket/auth.sock"
	campVaultTokenPath      = "/consul/vault-token"
	campVaultAgentPrivate   = "/consul/vault-agent-private"
	campVaultAgentConfig    = "/consul/vault-agent-config"
	campProcessorConfigPath = "/consul/processor-config"
	campProcessorConfigFile = "/consul/processor-config/config.json"
	campVaultCAPath         = "/consul/vault-ca"

	campCredentialUID    = int64(10001)
	campDefaultAudience  = "vault"
	campDefaultDrainSecs = int64(30)
)

// applyTerminatingGatewayCredentialInjection mutates podSpec in place to add the
// Vault-only credential-injection sidecars, volumes, and shared-socket wiring
// when it is enabled. It must produce the same projection as the Helm chart
// (see charts/consul/templates/terminating-gateways-deployment.yaml). The main
// (Envoy) container is expected to be podSpec.Containers[0]; it receives ONLY
// the socket mount and never the rendered-credential or Vault-token volumes.
func applyTerminatingGatewayCredentialInjection(
	podSpec *corev1.PodSpec,
	ci *consulv1alpha1.TerminatingGatewayCredentialInjection,
	imageK8S string,
	imagePullPolicy corev1.PullPolicy,
	logLevel string,
) {
	if ci == nil || !ci.Enabled {
		return
	}

	podSpec.Volumes = append(podSpec.Volumes, campCredentialVolumes(ci)...)

	podSpec.InitContainers = append(podSpec.InitContainers,
		campSocketInitContainer(imageK8S, imagePullPolicy),
		campVaultAgentContainer("camp-vault-agent-init", true, ci, imagePullPolicy),
	)
	podSpec.Containers = append(podSpec.Containers,
		campVaultAgentContainer("camp-vault-agent", false, ci, imagePullPolicy),
		campAuthProcessorContainer(ci, imagePullPolicy, logLevel),
	)

	// Envoy (the main/dataplane container) may reach the processor only over the
	// ext_proc socket. It must never mount credentials or the Vault token.
	if len(podSpec.Containers) > 0 {
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      campAuthSocketVolume,
			MountPath: campAuthSocketDir,
		})
	}

	// Shared supplemental group so Envoy (non-10001) can reach the group-owned
	// socket directory.
	if podSpec.SecurityContext == nil {
		podSpec.SecurityContext = &corev1.PodSecurityContext{}
	}
	podSpec.SecurityContext.SupplementalGroups = append(podSpec.SecurityContext.SupplementalGroups, campCredentialUID)
}

func campCredentialVolumes(ci *consulv1alpha1.TerminatingGatewayCredentialInjection) []corev1.Volume {
	memory := &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}

	token := corev1.ServiceAccountTokenProjection{
		Path:     "token",
		Audience: defaultIfEmpty(ci.TokenAudience, campDefaultAudience),
	}
	if ci.TokenExpirationSeconds != nil {
		token.ExpirationSeconds = ci.TokenExpirationSeconds
	}

	volumes := []corev1.Volume{
		{Name: campVaultRenderedVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		{Name: campAuthSocketVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		{Name: campVaultTokenVolume, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{ServiceAccountToken: &token}},
		}}},
		{Name: campVaultAgentPrivateVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		{Name: campVaultAgentConfigVolume, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ci.VaultAgentConfigMap},
		}}},
		{Name: campAuthProcessorConfigVolume, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ci.ProcessorConfigMap},
		}}},
	}
	if ci.VaultCAConfigMap != "" {
		volumes = append(volumes, corev1.Volume{
			Name: campVaultCAVolume,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ci.VaultCAConfigMap},
			}},
		})
	}
	return volumes
}

// campCredentialSecurityContext is the hardened, non-root UID/GID 10001 context
// shared by the Vault Agent and credential processor containers.
func campCredentialSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		RunAsNonRoot:             ptr.To(true),
		RunAsUser:                ptr.To(campCredentialUID),
		RunAsGroup:               ptr.To(campCredentialUID),
		SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
}

func campVaultAgentContainer(
	name string,
	exitAfterAuth bool,
	ci *consulv1alpha1.TerminatingGatewayCredentialInjection,
	imagePullPolicy corev1.PullPolicy,
) corev1.Container {
	args := []string{"agent", fmt.Sprintf("-config=%s", campVaultAgentConfig)}
	if exitAfterAuth {
		args = append(args, "-exit-after-auth")
	}

	env := []corev1.EnvVar{{Name: "VAULT_ADDR", Value: ci.VaultAddress}}
	if ci.VaultNamespace != "" {
		env = append(env, corev1.EnvVar{Name: "VAULT_NAMESPACE", Value: ci.VaultNamespace})
	}
	if ci.VaultCAConfigMap != "" {
		env = append(env, corev1.EnvVar{Name: "VAULT_CAPATH", Value: campVaultCAPath})
	}

	mounts := []corev1.VolumeMount{
		{Name: campVaultTokenVolume, MountPath: campVaultTokenPath, ReadOnly: true},
		{Name: campVaultAgentConfigVolume, MountPath: campVaultAgentConfig, ReadOnly: true},
		{Name: campVaultRenderedVolume, MountPath: campVaultRenderedPath},
		{Name: campVaultAgentPrivateVolume, MountPath: campVaultAgentPrivate},
	}
	if ci.VaultCAConfigMap != "" {
		mounts = append(mounts, corev1.VolumeMount{Name: campVaultCAVolume, MountPath: campVaultCAPath, ReadOnly: true})
	}

	return corev1.Container{
		Name:            name,
		Image:           ci.VaultAgentImage,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{"vault"},
		Args:            args,
		Env:             env,
		SecurityContext: campCredentialSecurityContext(),
		VolumeMounts:    mounts,
	}
}

func campAuthProcessorContainer(
	ci *consulv1alpha1.TerminatingGatewayCredentialInjection,
	imagePullPolicy corev1.PullPolicy,
	logLevel string,
) corev1.Container {
	drainSeconds := campDefaultDrainSecs
	if ci.DrainSeconds != nil {
		drainSeconds = *ci.DrainSeconds
	}

	probe := func(mode string) *corev1.Probe {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
				"camp-auth-processor", "health", fmt.Sprintf("-uds-path=%s", campAuthSocketFile), mode,
			}}},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		}
	}

	return corev1.Container{
		Name:            "camp-auth-processor",
		Image:           ci.ProcessorImage,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{"camp-auth-processor"},
		Args: []string{
			fmt.Sprintf("-config-file=%s", campProcessorConfigFile),
			fmt.Sprintf("-uds-path=%s", campAuthSocketFile),
			fmt.Sprintf("-log-level=%s", logLevel),
		},
		SecurityContext: campCredentialSecurityContext(),
		VolumeMounts: []corev1.VolumeMount{
			{Name: campVaultRenderedVolume, MountPath: campVaultRenderedPath, ReadOnly: true},
			{Name: campAuthSocketVolume, MountPath: campAuthSocketDir},
			{Name: campAuthProcessorConfigVolume, MountPath: campProcessorConfigPath, ReadOnly: true},
		},
		ReadinessProbe: probe("-ready"),
		LivenessProbe:  probe("-live"),
		Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{
			"camp-auth-processor", "drain-wait", fmt.Sprintf("-duration=%ds", drainSeconds),
		}}}},
		Resources: campBoundedResources("50Mi", "50m"),
	}
}

// campSocketInitContainer prepares the shared ext_proc socket directory so the
// processor (uid 10001) owns it and Envoy (a member of the shared supplemental
// group) can reach the socket. It runs with the narrowest capabilities required
// (CHOWN/FOWNER) and mounts only the socket volume.
func campSocketInitContainer(imageK8S string, imagePullPolicy corev1.PullPolicy) corev1.Container {
	return corev1.Container{
		Name:            "camp-auth-socket-init",
		Image:           imageK8S,
		ImagePullPolicy: imagePullPolicy,
		Command:         []string{"/bin/sh", "-ec"},
		Args:            []string{fmt.Sprintf("chown 10001:10001 %s\nchmod 2770 %s\n", campAuthSocketDir, campAuthSocketDir)},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:                ptr.To(int64(0)),
			RunAsNonRoot:             ptr.To(false),
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add:  []corev1.Capability{"CHOWN", "FOWNER"},
			},
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: campAuthSocketVolume, MountPath: campAuthSocketDir}},
		Resources:    campBoundedResources("16Mi", "10m"),
	}
}

func campBoundedResources(mem, cpu string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(mem),
			corev1.ResourceCPU:    resource.MustParse(cpu),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse(mem),
			corev1.ResourceCPU:    resource.MustParse(cpu),
		},
	}
}
