// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configentries

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	// campCredentialFSGroup is the shared supplemental group used for the
	// group-owned ext_proc socket and rendered-credential volumes. It is applied
	// via the pod's fsGroup so the kubelet (not a privileged init container) sets
	// group ownership and the setgid bit on the emptyDir volumes. The sidecars do
	// not pin a fixed runAsUser/runAsGroup, so an OpenShift-assigned UID from the
	// restricted-v2 SCC range still works: every container is a member of this
	// fsGroup and can reach the socket without running as root.
	campCredentialFSGroup     = int64(10001)
	campDefaultAudience       = "vault"
	campDefaultDrainSecs      = int64(30)
	campShutdownAllowanceSecs = int64(15)
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
	imagePullPolicy corev1.PullPolicy,
	logLevel string,
) {
	if ci == nil || !ci.Enabled {
		return
	}

	podSpec.Volumes = append(podSpec.Volumes, campCredentialVolumes(ci)...)

	if ci.EffectiveSource() == consulv1alpha1.CredentialSourceVault {
		podSpec.InitContainers = append(podSpec.InitContainers,
			campVaultAgentContainer("camp-vault-agent-init", true, ci, imagePullPolicy),
		)
		podSpec.Containers = append(podSpec.Containers,
			campVaultAgentContainer("camp-vault-agent", false, ci, imagePullPolicy),
		)
	}
	podSpec.Containers = append(podSpec.Containers,
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

	// Shared fsGroup so the kubelet group-owns the ext_proc socket and rendered
	// credential emptyDir volumes (with the setgid bit) without a privileged
	// root init container. Every container joins this group, so a sidecar running
	// under an OpenShift-assigned UID can still create and reach the socket, and
	// Envoy can connect to it.
	if podSpec.SecurityContext == nil {
		podSpec.SecurityContext = &corev1.PodSecurityContext{}
	}
	podSpec.SecurityContext.FSGroup = ptr.To(campCredentialFSGroup)

	// Grace period must exceed the processor's preStop drain plus a shutdown
	// allowance so in-flight requests drain before the pod is killed.
	drainSeconds := campDefaultDrainSecs
	if ci.DrainSeconds != nil {
		drainSeconds = *ci.DrainSeconds
	}
	podSpec.TerminationGracePeriodSeconds = ptr.To(drainSeconds + campShutdownAllowanceSecs)
}

func campCredentialVolumes(ci *consulv1alpha1.TerminatingGatewayCredentialInjection) []corev1.Volume {
	memory := &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}

	// The shared ext_proc socket and the non-secret processor config exist in
	// every source.
	volumes := []corev1.Volume{
		{Name: campAuthSocketVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		{Name: campAuthProcessorConfigVolume, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ci.ProcessorConfigMap},
		}}},
	}

	if ci.EffectiveSource() == consulv1alpha1.CredentialSourceKubernetesSecret {
		// Credentials arrive via a read-only Secret mounted at the rendered path.
		// The volume keeps its name/mount so the processor container is unchanged.
		volumes = append(volumes, corev1.Volume{
			Name: campVaultRenderedVolume,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: ci.SecretName,
			}},
		})
		return volumes
	}

	// Vault source: memory rendered dir written by the Vault Agent, plus its
	// projected token and agent config/private (and optional CA) volumes.
	token := corev1.ServiceAccountTokenProjection{
		Path:     "token",
		Audience: defaultIfEmpty(ci.TokenAudience, campDefaultAudience),
	}
	if ci.TokenExpirationSeconds != nil {
		token.ExpirationSeconds = ci.TokenExpirationSeconds
	}
	volumes = append(volumes,
		corev1.Volume{Name: campVaultRenderedVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		corev1.Volume{Name: campVaultTokenVolume, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{{ServiceAccountToken: &token}},
		}}},
		corev1.Volume{Name: campVaultAgentPrivateVolume, VolumeSource: corev1.VolumeSource{EmptyDir: memory}},
		corev1.Volume{Name: campVaultAgentConfigVolume, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ci.VaultAgentConfigMap},
		}}},
	)
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

// campCredentialSecurityContext is the hardened, non-root context shared by the
// Vault Agent and credential processor containers. It intentionally does NOT pin
// runAsUser/runAsGroup so an OpenShift-assigned UID (restricted-v2 SCC) is
// honored; shared socket/volume access is provided by the pod's fsGroup instead.
func campCredentialSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		RunAsNonRoot:             ptr.To(true),
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

	probe := func(mode string, initialDelay, failureThreshold int32) *corev1.Probe {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{
				"camp-auth-processor", "health", fmt.Sprintf("-uds-path=%s", campAuthSocketFile), mode,
			}}},
			InitialDelaySeconds: initialDelay,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    failureThreshold,
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
		// Readiness surfaces credential availability; liveness only checks the
		// process is alive and stays tolerant of recoverable Vault/file errors.
		ReadinessProbe: probe("-ready", 5, 3),
		LivenessProbe:  probe("-live", 15, 6),
		Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{
			"camp-auth-processor", "drain-wait", fmt.Sprintf("-duration=%ds", drainSeconds),
		}}}},
		Resources: campBoundedResources("50Mi", "50m"),
	}
}

// campSocketInitContainer removed: the shared ext_proc socket directory is now
// group-owned by the pod's fsGroup (set by the kubelet with the setgid bit), so
// no privileged root init container is needed to prepare it. This keeps the
// workload schedulable under OpenShift restricted-v2 and Kubernetes Restricted
// Pod Security.

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

// validateCredentialInjectionWorkload is the controller-side guard applied before
// a credential-injection workload is created. It rejects an enabled config that
// is missing any required non-secret binding metadata and refuses to inject
// credentials for a gateway that routes to no linked service. It never infers a
// default credential: an incomplete config is an error, not a silent fallback.
// (Admission webhook validation is the first line of defense; this guards the
// reconcile path even if a resource reaches it unvalidated.)
func validateCredentialInjectionWorkload(
	ci *consulv1alpha1.TerminatingGatewayCredentialInjection,
	services []consulv1alpha1.LinkedService,
) error {
	if ci == nil || !ci.Enabled {
		return nil
	}

	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"processorImage", ci.ProcessorImage},
		{"processorConfigMap", ci.ProcessorConfigMap},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}

	if ci.EffectiveSource() == consulv1alpha1.CredentialSourceKubernetesSecret {
		if ci.SecretName == "" {
			missing = append(missing, "secretName")
		}
	} else {
		for _, f := range []struct {
			name  string
			value string
		}{
			{"vaultAgentImage", ci.VaultAgentImage},
			{"vaultAgentConfigMap", ci.VaultAgentConfigMap},
			{"vaultAddress", ci.VaultAddress},
		} {
			if f.value == "" {
				missing = append(missing, f.name)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("credentialInjection is enabled but missing required fields: %v", missing)
	}

	if len(services) == 0 {
		return fmt.Errorf("credentialInjection is enabled but the terminating gateway has no linked services to route to")
	}

	return nil
}

// campCredentialConfigChecksumAnnotation carries a hash of the non-secret
// credential-injection ConfigMaps so that changing their content rolls the
// terminating-gateway workload. It intentionally covers only the ConfigMaps:
// the Vault Agent-rendered credential files live on a memory emptyDir and their
// runtime rotation must never change the pod template (no pod restart).
const campCredentialConfigChecksumAnnotation = "consul.hashicorp.com/credential-config-checksum"

// campCredentialConfigChecksum returns a deterministic hash over the given
// ConfigMap data maps (order-independent within each map).
func campCredentialConfigChecksum(dataMaps ...map[string]string) string {
	h := sha256.New()
	for _, data := range dataMaps {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Write([]byte(k))
			h.Write([]byte{0})
			h.Write([]byte(data[k]))
			h.Write([]byte{0})
		}
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// credentialConfigChecksum reads the referenced non-secret ConfigMaps and
// returns a checksum of their content. A missing ConfigMap contributes empty
// content so the workload rolls once it is created.
func (r *TerminatingGatewayController) credentialConfigChecksum(
	ctx context.Context,
	namespace string,
	ci *consulv1alpha1.TerminatingGatewayCredentialInjection,
) (string, error) {
	names := []string{ci.ProcessorConfigMap}
	if ci.VaultAgentConfigMap != "" {
		names = append(names, ci.VaultAgentConfigMap)
	}
	if ci.VaultCAConfigMap != "" {
		names = append(names, ci.VaultCAConfigMap)
	}

	dataMaps := make([]map[string]string, 0, len(names))
	for _, name := range names {
		cm := &corev1.ConfigMap{}
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, cm)
		switch {
		case apierrors.IsNotFound(err):
			dataMaps = append(dataMaps, nil)
		case err != nil:
			return "", fmt.Errorf("get configmap %s/%s: %w", namespace, name, err)
		default:
			combined := make(map[string]string, len(cm.Data)+len(cm.BinaryData))
			for k, v := range cm.Data {
				combined[k] = v
			}
			for k, v := range cm.BinaryData {
				combined[k] = string(v)
			}
			dataMaps = append(dataMaps, combined)
		}
	}
	return campCredentialConfigChecksum(dataMaps...), nil
}
