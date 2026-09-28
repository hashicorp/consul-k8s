// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package configentries

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"
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

	applyTerminatingGatewayCredentialInjection(&podSpec, ci, corev1.PullIfNotPresent, "info", true, false)

	// Envoy (main container) gets only the socket mount and the Consul-login
	// token, never credentials/token-sink/Vault token.
	envoy := containerByName(t, podSpec.Containers, "terminating-gateway")
	require.True(t, hasMount(envoy, campAuthSocketVolume))
	for _, v := range []string{campVaultRenderedVolume, campVaultTokenVolume, campVaultAgentPrivateVolume, campVaultAgentConfigVolume, campAuthProcessorConfigVolume, campVaultCAVolume} {
		require.Falsef(t, hasMount(envoy, v), "Envoy must not mount %q", v)
	}

	// Default ServiceAccount-token automount is disabled; the default-audience
	// token is projected and mounted only into the base init container and Envoy.
	require.NotNil(t, podSpec.AutomountServiceAccountToken)
	require.False(t, *podSpec.AutomountServiceAccountToken)
	require.True(t, hasVolume(podSpec.Volumes, campConsulAuthTokenVolume))
	require.True(t, mountReadOnly(envoy, campConsulAuthTokenVolume))
	baseInit := containerByName(t, podSpec.InitContainers, "terminating-gateway-init")
	require.True(t, mountReadOnly(baseInit, campConsulAuthTokenVolume))

	// No privileged socket-init container: the shared socket dir is group-owned
	// via the pod fsGroup instead.
	require.False(t, hasContainer(podSpec.InitContainers, "camp-auth-socket-init"))

	// Vault Agent init: hardened non-root, pinned to the image's non-root vault
	// UID, exits after auth, read-only token mount, and loads the
	// Vault CA as a single file (VAULT_CACERT), not a directory (VAULT_CAPATH).
	ai := containerByName(t, podSpec.InitContainers, "camp-vault-agent-init")
	require.Contains(t, ai.Args, "-exit-after-auth")
	require.Equal(t, ptr.To(true), ai.SecurityContext.RunAsNonRoot)
	require.Equal(t, ptr.To(campVaultAgentUID), ai.SecurityContext.RunAsUser)
	require.Nil(t, ai.SecurityContext.RunAsGroup)
	require.True(t, mountReadOnly(ai, campVaultTokenVolume))
	require.Contains(t, ai.Env, corev1.EnvVar{Name: "VAULT_CACERT", Value: "/consul/vault-ca/ca.crt"})
	require.NotContains(t, envNames(ai), "VAULT_CAPATH")

	// Vault Agent sidecar runs continuously (no exit-after-auth).
	as := containerByName(t, podSpec.Containers, "camp-vault-agent")
	require.NotContains(t, as.Args, "-exit-after-auth")
	require.Equal(t, ptr.To(true), as.SecurityContext.RunAsNonRoot)
	require.Equal(t, ptr.To(campVaultAgentUID), as.SecurityContext.RunAsUser)
	require.Contains(t, as.Env, corev1.EnvVar{Name: "VAULT_CACERT", Value: "/consul/vault-ca/ca.crt"})
	require.False(t, hasMount(as, campConsulAuthTokenVolume), "vault agent must not mount the Consul-login token")

	// Processor: non-root, read-only creds, writable socket, no token/token-sink,
	// health + drain contract.
	proc := containerByName(t, podSpec.Containers, "camp-auth-processor")
	require.Equal(t, ptr.To(true), proc.SecurityContext.RunAsNonRoot)
	require.Nil(t, proc.SecurityContext.RunAsUser)
	require.True(t, mountReadOnly(proc, campVaultRenderedVolume))
	require.True(t, hasMount(proc, campAuthSocketVolume))
	require.False(t, mountReadOnly(proc, campAuthSocketVolume), "socket must be writable")
	require.False(t, hasMount(proc, campVaultTokenVolume), "processor must not mount the Vault token")
	require.False(t, hasMount(proc, campVaultAgentPrivateVolume), "processor must not mount the Agent token sink")
	require.False(t, hasMount(proc, campConsulAuthTokenVolume), "processor must not mount the Consul-login token")
	require.NotNil(t, proc.ReadinessProbe.Exec)
	require.Contains(t, proc.ReadinessProbe.Exec.Command, "-ready")
	require.NotNil(t, proc.LivenessProbe.Exec)
	require.Contains(t, proc.LivenessProbe.Exec.Command, "-live")
	require.Contains(t, proc.Lifecycle.PreStop.Exec.Command, "drain-wait")
	require.Contains(t, proc.Lifecycle.PreStop.Exec.Command, "-duration=45s")

	// Liveness is tolerant (higher failure threshold) than readiness.
	require.Equal(t, int32(6), proc.LivenessProbe.FailureThreshold)
	require.Equal(t, int32(3), proc.ReadinessProbe.FailureThreshold)

	// Grace period exceeds drain (45) by the shutdown allowance (15).
	require.NotNil(t, podSpec.TerminationGracePeriodSeconds)
	require.Equal(t, int64(60), *podSpec.TerminationGracePeriodSeconds)

	// Shared fsGroup group-owns the socket without a privileged init container.
	require.NotNil(t, podSpec.SecurityContext.FSGroup)
	require.Equal(t, campCredentialFSGroup, *podSpec.SecurityContext.FSGroup)

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
	applyTerminatingGatewayCredentialInjection(&podSpec, nil, corev1.PullIfNotPresent, "info", true, false)
	applyTerminatingGatewayCredentialInjection(&podSpec, &consulv1alpha1.TerminatingGatewayCredentialInjection{Enabled: false}, corev1.PullIfNotPresent, "info", true, false)
	require.Len(t, podSpec.Containers, 1)
	require.Len(t, podSpec.InitContainers, 1)
	require.Empty(t, podSpec.Volumes)
	require.Nil(t, podSpec.SecurityContext)
	require.Nil(t, podSpec.AutomountServiceAccountToken)
	require.Empty(t, podSpec.Containers[0].VolumeMounts)
}

// TestTerminatingGatewayCredentialPodKubernetesSecret asserts the controller's
// projection when the credential source is a Kubernetes Secret: the processor and
// socket wiring stay identical, camp-vault-rendered becomes a read-only Secret
// volume, and no Vault Agent containers/volumes/token exist.
func TestTerminatingGatewayCredentialPodKubernetesSecret(t *testing.T) {
	ci := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:            true,
		Source:             consulv1alpha1.CredentialSourceKubernetesSecret,
		SecretName:         "camp-egress-credentials",
		ProcessorImage:     "camp-auth-processor:test",
		ProcessorConfigMap: "camp-proc",
	}
	podSpec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "terminating-gateway-init"}},
		Containers:     []corev1.Container{{Name: "terminating-gateway"}},
	}

	// ACLs disabled: no Consul-login token volume is projected, but automount is
	// still disabled.
	applyTerminatingGatewayCredentialInjection(&podSpec, ci, corev1.PullIfNotPresent, "info", false, false)

	// No Vault Agent containers in either init or main containers.
	for _, n := range []string{"camp-vault-agent", "camp-vault-agent-init"} {
		require.Falsef(t, hasContainer(podSpec.InitContainers, n), "unexpected init container %q", n)
		require.Falsef(t, hasContainer(podSpec.Containers, n), "unexpected container %q", n)
	}

	// camp-vault-rendered is a read-only Secret volume for the named Secret.
	rendered := volumeByName(t, podSpec.Volumes, campVaultRenderedVolume)
	require.NotNil(t, rendered.Secret, "camp-vault-rendered must be a Secret volume")
	require.Equal(t, "camp-egress-credentials", rendered.Secret.SecretName)
	require.Nil(t, rendered.EmptyDir)

	// No Vault token / agent-config / private / CA volumes exist.
	for _, n := range []string{campVaultTokenVolume, campVaultAgentPrivateVolume, campVaultAgentConfigVolume, campVaultCAVolume} {
		require.Falsef(t, hasVolume(podSpec.Volumes, n), "unexpected volume %q", n)
	}

	// Processor still present, reads rendered read-only; no socket-init container.
	proc := containerByName(t, podSpec.Containers, "camp-auth-processor")
	require.True(t, mountReadOnly(proc, campVaultRenderedVolume))
	require.False(t, hasContainer(podSpec.InitContainers, "camp-auth-socket-init"))

	// Envoy mounts only the socket, never credentials.
	envoy := containerByName(t, podSpec.Containers, "terminating-gateway")
	require.True(t, hasMount(envoy, campAuthSocketVolume))
	require.False(t, hasMount(envoy, campVaultRenderedVolume))

	// Shared fsGroup is set for every credential source.
	require.NotNil(t, podSpec.SecurityContext)
	require.Equal(t, ptr.To(campCredentialFSGroup), podSpec.SecurityContext.FSGroup)
	require.NotNil(t, podSpec.TerminationGracePeriodSeconds)

	// Automount is disabled, but with ACLs disabled no Consul-login token is
	// projected or mounted anywhere.
	require.NotNil(t, podSpec.AutomountServiceAccountToken)
	require.False(t, *podSpec.AutomountServiceAccountToken)
	require.False(t, hasVolume(podSpec.Volumes, campConsulAuthTokenVolume))
	require.False(t, hasMount(envoy, campConsulAuthTokenVolume))
	require.False(t, hasMount(containerByName(t, podSpec.InitContainers, "terminating-gateway-init"), campConsulAuthTokenVolume))
}

// TestTerminatingGatewayCredentialPodVaultInjectorToken asserts that when the
// global Vault Agent Injector is active (and ACLs are disabled), disabling the
// default automount still projects the dedicated ServiceAccount token volume so
// vault-k8s can discover it — without mounting it into any existing container.
func TestTerminatingGatewayCredentialPodVaultInjectorToken(t *testing.T) {
	ci := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:             true,
		ProcessorImage:      "camp-auth-processor:test",
		VaultAgentImage:     "hashicorp/vault:test",
		ProcessorConfigMap:  "camp-proc",
		VaultAgentConfigMap: "camp-agent",
		VaultAddress:        "https://vault:8200",
		TokenAudience:       "vault",
	}
	podSpec := corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "terminating-gateway-init"}},
		Containers:     []corev1.Container{{Name: "terminating-gateway"}},
	}

	// aclsEnabled=false, vaultAgentInjectorEnabled=true.
	applyTerminatingGatewayCredentialInjection(&podSpec, ci, corev1.PullIfNotPresent, "info", false, true)

	require.NotNil(t, podSpec.AutomountServiceAccountToken)
	require.False(t, *podSpec.AutomountServiceAccountToken)

	// The token volume exists for the injector to reference.
	require.True(t, hasVolume(podSpec.Volumes, campConsulAuthTokenVolume))

	// But it is not mounted into any existing container (no Consul login here).
	envoy := containerByName(t, podSpec.Containers, "terminating-gateway")
	baseInit := containerByName(t, podSpec.InitContainers, "terminating-gateway-init")
	require.False(t, hasMount(envoy, campConsulAuthTokenVolume))
	require.False(t, hasMount(baseInit, campConsulAuthTokenVolume))
}

// TestConstructDeploymentFromCRDCredentialInjectionOpenShift asserts credential
// injection is rejected on OpenShift (unsupported) while a gateway without it
// still builds unchanged there.
func TestConstructDeploymentFromCRDCredentialInjectionOpenShift(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, consulv1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	r := &TerminatingGatewayController{Scheme: scheme}

	helmValues := func(openShift bool) *helmvalues.HelmValues {
		return &helmvalues.HelmValues{
			Global: helmvalues.GlobalConfig{
				Name:                 "consul",
				Datacenter:           "dc1",
				ImageK8S:             "hashicorp/consul-k8s-control-plane:1.0.0",
				ImageConsulDataplane: "hashicorp/consul-dataplane:1.0.0",
				ImagePullPolicy:      "IfNotPresent",
				OpenShiftEnabled:     openShift,
			},
			Release: helmvalues.ReleaseConfig{Name: "consul", Namespace: "consul", Service: "Helm"},
		}
	}
	termGW := func(ci *consulv1alpha1.TerminatingGatewayCredentialInjection) *consulv1alpha1.TerminatingGateway {
		svc := consulv1alpha1.LinkedService{Name: "external-api"}
		if ci != nil && ci.Enabled {
			svc.CAFile = "/etc/ssl/certs/ca-certificates.crt"
			svc.SNI = "api.example.com"
			svc.Credential = &consulv1alpha1.LinkedServiceCredential{Mode: consulv1alpha1.CredentialModeInject, BindingID: "external-api"}
		}
		return &consulv1alpha1.TerminatingGateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gateway", Namespace: "consul"},
			Spec: consulv1alpha1.TerminatingGatewaySpec{
				Services: []consulv1alpha1.LinkedService{svc},
				Deployment: consulv1alpha1.TerminatingGatewayDeploymentSpec{
					GatewayName:         "terminating-gateway",
					LogLevel:            "info",
					LogJSON:             ptr.To(false),
					EnableDeployment:    ptr.To(true),
					CredentialInjection: ci,
				},
			},
		}
	}
	enabledCI := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:             true,
		ProcessorImage:      "camp-auth-processor:test",
		VaultAgentImage:     "hashicorp/vault:test",
		ProcessorConfigMap:  "camp-proc",
		VaultAgentConfigMap: "camp-agent",
		VaultAddress:        "https://vault:8200",
		TokenAudience:       "vault",
	}

	// Enabled on OpenShift: rejected, no workload built.
	deployment, err := r.constructDeploymentFromCRD(termGW(enabledCI), helmValues(true))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported on OpenShift")
	require.Nil(t, deployment)

	// The same config off OpenShift is accepted.
	deployment, err = r.constructDeploymentFromCRD(termGW(enabledCI), helmValues(false))
	require.NoError(t, err)
	require.True(t, hasContainer(deployment.Spec.Template.Spec.Containers, "camp-auth-processor"))

	// Sidecars without complete routing are refused: Envoy would never reach the
	// processor, so the gateway would look healthy without injecting credentials.
	noRouting := termGW(enabledCI)
	noRouting.Spec.Services[0].Credential = nil
	deployment, err = r.constructDeploymentFromCRD(noRouting, helmValues(false))
	require.Error(t, err)
	require.Contains(t, err.Error(), "credentialInjection routing is invalid")
	require.Contains(t, err.Error(), "spec.services[0].credential")
	require.Nil(t, deployment)

	// Disabled or unset on OpenShift: the gateway builds with no credential
	// injection projection.
	for _, ci := range []*consulv1alpha1.TerminatingGatewayCredentialInjection{nil, {Enabled: false}} {
		deployment, err = r.constructDeploymentFromCRD(termGW(ci), helmValues(true))
		require.NoError(t, err)
		spec := deployment.Spec.Template.Spec
		require.Len(t, spec.Containers, 1)
		require.Len(t, spec.InitContainers, 1)
		require.Nil(t, spec.AutomountServiceAccountToken)
		for _, v := range spec.Volumes {
			require.Falsef(t, strings.HasPrefix(v.Name, "camp-"), "unexpected credential volume %q", v.Name)
		}
		if spec.SecurityContext != nil {
			require.Nil(t, spec.SecurityContext.FSGroup)
		}
	}
}

func envNames(c corev1.Container) []string {
	names := make([]string, 0, len(c.Env))
	for _, e := range c.Env {
		names = append(names, e.Name)
	}
	return names
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

func hasContainer(containers []corev1.Container, name string) bool {
	for _, c := range containers {
		if c.Name == name {
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

func TestValidateCredentialInjectionWorkload(t *testing.T) {
	enabled := ptr.To(true)
	complete := &consulv1alpha1.TerminatingGatewayCredentialInjection{
		Enabled:             true,
		ProcessorImage:      "camp-auth-processor:test",
		VaultAgentImage:     "hashicorp/vault:test",
		ProcessorConfigMap:  "camp-proc",
		VaultAgentConfigMap: "camp-agent",
		VaultAddress:        "https://vault:8200",
		TokenAudience:       "vault",
	}
	services := []consulv1alpha1.LinkedService{{Name: "external-api"}}

	// nil / disabled are always valid no-ops.
	require.NoError(t, validateCredentialInjectionWorkload(nil, nil, enabled))
	require.NoError(t, validateCredentialInjectionWorkload(&consulv1alpha1.TerminatingGatewayCredentialInjection{Enabled: false}, nil, enabled))

	// Missing required field is rejected (no default inferred).
	missing := *complete
	missing.ProcessorImage = ""
	err := validateCredentialInjectionWorkload(&missing, services, enabled)
	require.Error(t, err)
	require.Contains(t, err.Error(), "processorImage")

	// The reconcile guard now reuses the full webhook validation: a plaintext
	// Vault address is rejected even though only vaultAddress non-emptiness was
	// checked before.
	plaintext := *complete
	plaintext.VaultAddress = "http://vault:8200"
	err = validateCredentialInjectionWorkload(&plaintext, services, enabled)
	require.Error(t, err)
	require.Contains(t, err.Error(), "vaultAddress")

	// A missing tokenAudience is likewise rejected via the shared validation.
	noAudience := *complete
	noAudience.TokenAudience = ""
	err = validateCredentialInjectionWorkload(&noAudience, services, enabled)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tokenAudience")

	// Enabled but no linked services to route to is rejected.
	err = validateCredentialInjectionWorkload(complete, nil, enabled)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no linked services")

	// Complete config with at least one linked service is valid.
	require.NoError(t, validateCredentialInjectionWorkload(complete, services, enabled))

	// An unsupported source is rejected rather than treated as Vault (the
	// injection logic only starts a Vault Agent for the exact "vault" value).
	unknownSource := *complete
	unknownSource.Source = "consul"
	err = validateCredentialInjectionWorkload(&unknownSource, services, enabled)
	require.Error(t, err)
	require.Contains(t, err.Error(), "source")
}

func TestCampCredentialConfigChecksum(t *testing.T) {
	proc := map[string]string{"config.json": `{"a":1}`}
	agent := map[string]string{"agent.hcl": "template {}"}

	base := campCredentialConfigChecksum(proc, agent)

	// Deterministic and order-independent within a map.
	require.Equal(t, base, campCredentialConfigChecksum(proc, agent))

	// A config content change rolls the workload (checksum changes).
	changed := map[string]string{"config.json": `{"a":2}`}
	require.NotEqual(t, base, campCredentialConfigChecksum(changed, agent))

	// It covers only the ConfigMaps passed in; the rendered credential emptyDir
	// is never an input, so credential rotation cannot change this checksum.
	require.NotEmpty(t, base)
}
