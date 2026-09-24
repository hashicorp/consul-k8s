// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	terminatingGatewayKubeKind = "terminatinggateway"
)

const (
	TerminatingGatewayFailedToSetACLs string = "FailedToSetACLs"
)

// Condition Type.
const ConsulACLStatus ConditionType = "ConsulACLsSynced"

func init() {
	SchemeBuilder.Register(&TerminatingGateway{}, &TerminatingGatewayList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// TerminatingGateway is the Schema for the terminatinggateways API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="terminating-gateway"
type TerminatingGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TerminatingGatewaySpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TerminatingGatewayList contains a list of TerminatingGateway.
type TerminatingGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TerminatingGateway `json:"items"`
}

// TerminatingGatewaySpec defines the desired state of TerminatingGateway.
type TerminatingGatewaySpec struct {
	// Services is a list of service names represented by the terminating gateway.
	Services []LinkedService `json:"services,omitempty"`

	// Deployment contains all deployment-related configuration
	// +kubebuilder:validation:Optional
	Deployment TerminatingGatewayDeploymentSpec `json:"deployment,omitempty"`
}

// TerminatingGatewayDeploymentSpec contains all deployment-related configuration for the terminating gateway.
type TerminatingGatewayDeploymentSpec struct {

	// Enabled controls whether to create a Deployment for this gateway
	EnableDeployment *bool `json:"enabledDeployment,omitempty"`

	// GatewayName is the name of the gateway service in Consul
	// +kubebuilder:validation:Optional
	GatewayName string `json:"gatewayName,omitempty"`

	// Replicas is the number of pod instances to deploy
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources define CPU and memory requests/limits for the pod
	// +kubebuilder:validation:Optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Affinity defines pod scheduling affinity rules
	// +kubebuilder:validation:Optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations define pod tolerations for node taints
	// +kubebuilder:validation:Optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints define pod topology spread constraints
	// +kubebuilder:validation:Optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// NodeSelector defines labels for node selection
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// PriorityClassName is the priority class for pod scheduling
	// +kubebuilder:validation:Optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Annotations are custom annotations applied to the pod
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// ExtraVolumes are additional volumes to mount in the pod
	// +kubebuilder:validation:Optional
	ExtraVolumes []ExtraVolume `json:"extraVolumes,omitempty"`

	// ServiceAccount defines service account configuration
	// +kubebuilder:validation:Optional
	ServiceAccount ServiceAccountConfig `json:"serviceAccount,omitempty"`

	// ConsulNamespace is the Consul namespace where the gateway is registered
	// +kubebuilder:validation:Optional
	ConsulNamespace string `json:"consulNamespace,omitempty"`

	// LogLevel sets the logging level for the gateway
	LogLevel string `json:"logLevel,omitempty"`

	// LogJSON enables JSON formatted logging
	LogJSON *bool `json:"logJSON,omitempty"`

	// CredentialInjection configures optional non-secret credential-processor and
	// Vault Agent sidecars that inject external-model credentials for linked
	// services. This is a Kubernetes deployment concern only: it does not change
	// the Consul terminating-gateway config entry produced by ToConsul.
	// +kubebuilder:validation:Optional
	CredentialInjection *TerminatingGatewayCredentialInjection `json:"credentialInjection,omitempty"`
}

// TerminatingGatewayCredentialInjection configures the optional credential-processor
// and Vault Agent sidecars added to a terminating gateway's deployment. All fields
// here are non-secret: ConfigMaps referenced by name must carry only non-secret
// binding/template/auth configuration, never token values or arbitrary environment
// credential sources. Image fields are shapes only; production deployments must
// pin an immutable image digest.
type TerminatingGatewayCredentialInjection struct {
	// Enabled controls whether the credential processor and Vault Agent sidecars
	// are added to the gateway deployment.
	// +kubebuilder:validation:Optional
	Enabled bool `json:"enabled,omitempty"`

	// Source selects the credential delivery mechanism. "vault" (the default when
	// empty) runs the Vault Agent sidecars; "kubernetesSecret" mounts a Kubernetes
	// Secret read-only into the credential processor instead.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=vault;kubernetesSecret
	Source string `json:"source,omitempty"`

	// SecretName names the Kubernetes Secret, in the gateway's namespace, whose data
	// keys are per-binding credential envelope files. Required when source is
	// "kubernetesSecret"; ignored otherwise.
	// +kubebuilder:validation:Optional
	SecretName string `json:"secretName,omitempty"`

	// ProcessorImage is the container image reference for the credential processor sidecar.
	// +kubebuilder:validation:Optional
	ProcessorImage string `json:"processorImage,omitempty"`

	// VaultAgentImage is the container image reference for the Vault Agent sidecar.
	// +kubebuilder:validation:Optional
	VaultAgentImage string `json:"vaultAgentImage,omitempty"`

	// ProcessorConfigMap names the ConfigMap, in the gateway's namespace, carrying
	// non-secret credential processor binding/template configuration.
	// +kubebuilder:validation:Optional
	ProcessorConfigMap string `json:"processorConfigMap,omitempty"`

	// VaultAgentConfigMap names the ConfigMap, in the gateway's namespace, carrying
	// non-secret Vault Agent configuration. This ConfigMap exclusively owns the
	// Vault auth configuration (auto-auth method, role, and mount); there are no
	// separate auth role/mount fields on this API.
	// +kubebuilder:validation:Optional
	VaultAgentConfigMap string `json:"vaultAgentConfigMap,omitempty"`

	// VaultAddress is the address of the Vault server the sidecar authenticates against.
	// It must be an absolute https:// URL.
	// +kubebuilder:validation:Optional
	VaultAddress string `json:"vaultAddress,omitempty"`

	// VaultNamespace is the optional Vault Enterprise namespace.
	// +kubebuilder:validation:Optional
	VaultNamespace string `json:"vaultNamespace,omitempty"`

	// VaultCAConfigMap optionally names a ConfigMap, in the gateway's namespace, carrying
	// the CA bundle used to validate the Vault server's TLS certificate.
	// +kubebuilder:validation:Optional
	VaultCAConfigMap string `json:"vaultCAConfigMap,omitempty"`

	// TokenAudience is the audience requested for the projected Kubernetes service
	// account token presented to Vault.
	// +kubebuilder:validation:Optional
	TokenAudience string `json:"tokenAudience,omitempty"`

	// TokenExpirationSeconds is the requested expiration, in seconds, of the
	// projected service account token.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=600
	// +kubebuilder:validation:Maximum=43200
	TokenExpirationSeconds *int64 `json:"tokenExpirationSeconds,omitempty"`

	// DrainSeconds bounds how long the sidecars wait to finish in-flight credential
	// refresh work before terminating during a rolling update.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	DrainSeconds *int64 `json:"drainSeconds,omitempty"`
}

// Credential-injection delivery sources for
// spec.deployment.credentialInjection.source.
const (
	CredentialSourceVault            = "vault"
	CredentialSourceKubernetesSecret = "kubernetesSecret"
)

// EffectiveSource returns the configured credential source, defaulting to
// CredentialSourceVault when unset.
func (in *TerminatingGatewayCredentialInjection) EffectiveSource() string {
	if in.Source == "" {
		return CredentialSourceVault
	}
	return in.Source
}

// ExtraVolume defines a volume to be mounted in the pod.
type ExtraVolume struct {
	// Name is the name of the volume
	Name string `json:"name"`

	// Type is the volume type (configMap or secret)
	// +kubebuilder:validation:Enum=configMap;secret
	Type string `json:"type"`

	// Load indicates if the volume should be loaded
	// +kubebuilder:validation:Optional
	Load bool `json:"load,omitempty"`

	Items []KeyToPath `json:"items,omitempty"`
}

// ServiceAccountConfig defines service account configuration.
type ServiceAccountConfig struct {
	// Annotations are annotations for the service account
	// +kubebuilder:validation:Optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

type KeyToPath struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// A LinkedService is a service represented by a terminating gateway.
type LinkedService struct {
	// The namespace the service is registered in.
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the service, as defined in Consul's catalog.
	Name string `json:"name,omitempty"`

	// CAFile is the optional path to a CA certificate to use for TLS connections
	// from the gateway to the linked service.
	CAFile string `json:"caFile,omitempty"`

	// CertFile is the optional path to a client certificate to use for TLS connections
	// from the gateway to the linked service.
	CertFile string `json:"certFile,omitempty"`

	// KeyFile is the optional path to a private key to use for TLS connections
	// from the gateway to the linked service.
	KeyFile string `json:"keyFile,omitempty"`

	// SNI is the optional name to specify during the TLS handshake with a linked service.
	SNI string `json:"sni,omitempty"`

	// DisableAutoHostRewrite disables terminating gateways auto host rewrite feature when set to true.
	DisableAutoHostRewrite bool `json:"disableAutoHostRewrite,omitempty"`

	// SecretRef references a Kubernetes secret containing TLS certificates.
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// SecretReference defines the name of the Kubernetes secret.
// +kubebuilder:object:generate=true
type SecretReference struct {
	// Name is the name of the Kubernetes secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

func (l LinkedService) NamespaceName() string {
	return defaultIfEmpty(l.Namespace) + "." + l.Name
}

func defaultIfEmpty(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func (in *TerminatingGateway) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *TerminatingGateway) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *TerminatingGateway) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *TerminatingGateway) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *TerminatingGateway) ConsulKind() string {
	return capi.TerminatingGateway
}

func (in *TerminatingGateway) ConsulGlobalResource() bool {
	return false
}

func (in *TerminatingGateway) ConsulMirroringNS() string {
	if in.Spec.Deployment.ConsulNamespace != "" {
		return in.Spec.Deployment.ConsulNamespace
	}
	return in.Namespace
}

func (in *TerminatingGateway) KubeKind() string {
	return terminatingGatewayKubeKind
}

func (in *TerminatingGateway) ConsulName() string {
	if in.Spec.Deployment.GatewayName != "" {
		return in.Spec.Deployment.GatewayName
	}
	return in.ObjectMeta.Name
}

func (in *TerminatingGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *TerminatingGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
	cond := Condition{
		Type:               ConditionSynced,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	for idx, c := range in.Status.Conditions {
		if c.Type == ConditionSynced {
			in.Status.Conditions[idx] = cond
			return
		}
	}

	in.Status.Conditions = append(in.Status.Conditions, cond)
}

func (in *TerminatingGateway) SetACLStatusCondition(status corev1.ConditionStatus, reason, message string) {
	cond := Condition{
		Type:               ConsulACLStatus,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	for idx, c := range in.Status.Conditions {
		if c.Type == ConsulACLStatus {
			in.Status.Conditions[idx] = cond
			return
		}
	}

	in.Status.Conditions = append(in.Status.Conditions, cond)
}

func (in *TerminatingGateway) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *TerminatingGateway) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *TerminatingGateway) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *TerminatingGateway) ToConsul(datacenter string) capi.ConfigEntry {
	var svcs []capi.LinkedService
	for _, s := range in.Spec.Services {
		svcs = append(svcs, s.toConsul())
	}
	return &capi.TerminatingGatewayConfigEntry{
		Kind:     in.ConsulKind(),
		Name:     in.ConsulName(),
		Services: svcs,
		Meta:     meta(datacenter),
	}
}

func (in *TerminatingGateway) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.TerminatingGatewayConfigEntry)
	if !ok {
		return false
	}
	desired, ok := in.ToConsul("").(*capi.TerminatingGatewayConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(
		normalizeTerminatingGatewayForCompare(desired),
		normalizeTerminatingGatewayForCompare(configEntry),
		cmpopts.IgnoreFields(capi.TerminatingGatewayConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"),
		cmpopts.IgnoreUnexported(),
		cmpopts.EquateEmpty(),
	)
}

func normalizeTerminatingGatewayForCompare(in *capi.TerminatingGatewayConfigEntry) *capi.TerminatingGatewayConfigEntry {
	if in == nil {
		return nil
	}

	normalized := *in
	normalized.Services = append([]capi.LinkedService(nil), in.Services...)
	for i := range normalized.Services {
		clearLinkedServiceStringField(&normalized.Services[i], "Namespace")
		clearLinkedServiceStringField(&normalized.Services[i], "Partition")
	}

	return &normalized
}

func clearLinkedServiceStringField(in *capi.LinkedService, field string) {
	v := reflect.ValueOf(in)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return
	}
	fieldValue := v.Elem().FieldByName(field)
	if fieldValue.IsValid() && fieldValue.CanSet() && fieldValue.Kind() == reflect.String {
		fieldValue.SetString("")
	}
}

func (in *TerminatingGateway) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	for i, v := range in.Spec.Services {
		errs = append(errs, v.validate(path.Child("services").Index(i))...)
	}

	errs = append(errs, in.validateNamespaces(consulMeta.NamespacesEnabled)...)
	errs = append(errs, in.Spec.Deployment.validateCredentialInjection(path.Child("deployment"))...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: terminatingGatewayKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

// validateCredentialInjection validates spec.deployment.credentialInjection. Validation
// is skipped entirely when the block is nil or not enabled, preserving legacy behavior
// for resources that don't use this feature.
func (in TerminatingGatewayDeploymentSpec) validateCredentialInjection(path *field.Path) field.ErrorList {
	ci := in.CredentialInjection
	if ci == nil || !ci.Enabled {
		return nil
	}
	return ci.validate(in.EnableDeployment, path.Child("credentialInjection"))
}

// ValidateForWorkload runs the same structural validation as the admission
// webhook for an enabled credential-injection block and returns a single
// aggregated error. It lets the controller's reconcile-path guard reuse the full
// Vault-specific constraints (HTTPS Vault address, required tokenAudience,
// supported source, etc.) instead of re-implementing a weaker subset, so a
// resource admitted without the webhook cannot construct an invalid workload.
func (in *TerminatingGatewayCredentialInjection) ValidateForWorkload(enableDeployment *bool) error {
	errs := in.validate(enableDeployment, field.NewPath("spec", "deployment", "credentialInjection"))
	if len(errs) == 0 {
		return nil
	}
	return errs.ToAggregate()
}

const (
	// minTokenExpirationSeconds mirrors Kubernetes' documented floor for
	// projected service account tokens (10 minutes).
	minTokenExpirationSeconds int64 = 600
	// maxTokenExpirationSeconds is a conservative ceiling (12 hours) to keep
	// the projected token lifetime bounded.
	maxTokenExpirationSeconds int64 = 43200
)

func (in *TerminatingGatewayCredentialInjection) validate(enableDeployment *bool, path *field.Path) field.ErrorList {
	var errs field.ErrorList

	// Feature/deployment mismatch: sidecars can only be injected into a Deployment
	// that will actually be created, so enabledDeployment must be explicitly true.
	if enableDeployment == nil || !*enableDeployment {
		errs = append(errs, field.Invalid(path.Child("enabled"), in.Enabled,
			"credentialInjection.enabled requires spec.deployment.enabledDeployment to be true"))
	}

	// The credential processor runs in every source.
	if in.ProcessorImage == "" {
		errs = append(errs, field.Required(path.Child("processorImage"), "processorImage is required when credentialInjection is enabled"))
	}
	if in.ProcessorConfigMap == "" {
		errs = append(errs, field.Required(path.Child("processorConfigMap"), "processorConfigMap is required when credentialInjection is enabled"))
	}

	// drainSeconds must be non-negative regardless of source: a negative value
	// produces an invalid negative drain-wait and grace period for both the
	// Vault and Kubernetes Secret sources. (The upper bound vs. tokenExpiration
	// stays Vault-specific in validateProjectedToken.)
	if in.DrainSeconds != nil && *in.DrainSeconds < 0 {
		errs = append(errs, field.Invalid(path.Child("drainSeconds"), *in.DrainSeconds, "drainSeconds must not be negative"))
	}

	switch in.EffectiveSource() {
	case CredentialSourceKubernetesSecret:
		if in.SecretName == "" {
			errs = append(errs, field.Required(path.Child("secretName"),
				`secretName is required when credentialInjection.source is "kubernetesSecret"`))
		}
		if strings.Contains(in.SecretName, WildcardSpecifier) {
			errs = append(errs, field.Invalid(path.Child("secretName"), in.SecretName, `must not contain the wildcard character "*"`))
		}
		// The processor ConfigMap is used by every source, so guard its
		// identifier here too (the Vault branch covers it via
		// validateWildcardAndModeCombinations).
		if strings.Contains(in.ProcessorConfigMap, WildcardSpecifier) {
			errs = append(errs, field.Invalid(path.Child("processorConfigMap"), in.ProcessorConfigMap, `must not contain the wildcard character "*"`))
		}
	case CredentialSourceVault:
		if in.VaultAgentImage == "" {
			errs = append(errs, field.Required(path.Child("vaultAgentImage"), "vaultAgentImage is required when credentialInjection is enabled"))
		}
		if in.VaultAgentConfigMap == "" {
			errs = append(errs, field.Required(path.Child("vaultAgentConfigMap"), "vaultAgentConfigMap is required when credentialInjection is enabled"))
		}
		errs = append(errs, in.validateVaultAddress(path.Child("vaultAddress"))...)
		errs = append(errs, in.validateProjectedToken(path)...)
		errs = append(errs, in.validateWildcardAndModeCombinations(path)...)
	default:
		// Reject unsupported source values rather than silently treating them as
		// Vault: the injection logic only starts a Vault Agent for the exact
		// "vault" value. (The CRD enum guards normal admission; this is
		// defense-in-depth for the API validation path.)
		errs = append(errs, field.NotSupported(path.Child("source"), in.Source,
			[]string{CredentialSourceVault, CredentialSourceKubernetesSecret}))
	}

	return errs
}

// validateVaultAddress enforces "invalid Vault URL/TLS": the address must be an
// absolute URL with a host, and must use the https scheme (plaintext Vault
// connections are forbidden for this feature).
func (in *TerminatingGatewayCredentialInjection) validateVaultAddress(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in.VaultAddress == "" {
		return append(errs, field.Required(path, "vaultAddress is required when credentialInjection is enabled"))
	}
	u, err := url.Parse(in.VaultAddress)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return append(errs, field.Invalid(path, in.VaultAddress, "vaultAddress must be a valid absolute URL"))
	}
	if u.Scheme != "https" {
		errs = append(errs, field.Invalid(path, in.VaultAddress,
			`vaultAddress must use the "https" scheme; plaintext Vault connections are not permitted`))
	}
	return errs
}

// validateProjectedToken enforces "invalid projected-token fields": tokenAudience
// is required and tokenExpirationSeconds must fall within the supported range.
// The source-independent non-negative drainSeconds check lives in validate; the
// only Vault-specific drain rule here is that it must not exceed
// tokenExpirationSeconds.
func (in *TerminatingGatewayCredentialInjection) validateProjectedToken(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in.TokenAudience == "" {
		errs = append(errs, field.Required(path.Child("tokenAudience"), "tokenAudience is required when credentialInjection is enabled"))
	}
	if in.TokenExpirationSeconds != nil {
		v := *in.TokenExpirationSeconds
		if v < minTokenExpirationSeconds || v > maxTokenExpirationSeconds {
			errs = append(errs, field.Invalid(path.Child("tokenExpirationSeconds"), v,
				fmt.Sprintf("tokenExpirationSeconds must be between %d and %d seconds", minTokenExpirationSeconds, maxTokenExpirationSeconds)))
		}
	}
	if in.DrainSeconds != nil && in.TokenExpirationSeconds != nil && *in.DrainSeconds > *in.TokenExpirationSeconds {
		errs = append(errs, field.Invalid(path.Child("drainSeconds"), *in.DrainSeconds, "drainSeconds must not exceed tokenExpirationSeconds"))
	}
	return errs
}

// validateWildcardAndModeCombinations enforces "forbidden wildcard/mode
// combinations": Vault/ConfigMap identifiers must not contain the wildcard
// specifier, and the processor/Vault Agent ConfigMaps (which carry different,
// non-interchangeable configuration shapes) must not reference the same
// ConfigMap.
func (in *TerminatingGatewayCredentialInjection) validateWildcardAndModeCombinations(path *field.Path) field.ErrorList {
	var errs field.ErrorList

	wildcardFields := []struct {
		name  string
		value string
	}{
		{"vaultNamespace", in.VaultNamespace},
		{"vaultCAConfigMap", in.VaultCAConfigMap},
		{"processorConfigMap", in.ProcessorConfigMap},
		{"vaultAgentConfigMap", in.VaultAgentConfigMap},
		{"tokenAudience", in.TokenAudience},
	}
	for _, f := range wildcardFields {
		if strings.Contains(f.value, WildcardSpecifier) {
			errs = append(errs, field.Invalid(path.Child(f.name), f.value, `must not contain the wildcard character "*"`))
		}
	}

	if in.ProcessorConfigMap != "" && in.ProcessorConfigMap == in.VaultAgentConfigMap {
		errs = append(errs, field.Invalid(path.Child("vaultAgentConfigMap"), in.VaultAgentConfigMap,
			"vaultAgentConfigMap must reference a different ConfigMap than processorConfigMap"))
	}

	return errs
}

// DefaultNamespaceFields sets the namespace field on spec.services to their default values if namespaces are enabled.
func (in *TerminatingGateway) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
	// If namespaces are enabled we want to set the namespace fields to their
	// defaults. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulMeta.NamespacesEnabled {
		// Default to the current namespace (i.e. the namespace of the config entry).
		namespace := namespaces.ConsulNamespace(in.Namespace, consulMeta.NamespacesEnabled, consulMeta.DestinationNamespace, consulMeta.Mirroring, consulMeta.Prefix)
		for i, service := range in.Spec.Services {
			if service.Namespace == "" {
				in.Spec.Services[i].Namespace = namespace
			}
		}
	}
}

func (in LinkedService) toConsul() capi.LinkedService {
	return capi.LinkedService{
		Namespace:              in.Namespace,
		Name:                   in.Name,
		CAFile:                 in.CAFile,
		CertFile:               in.CertFile,
		KeyFile:                in.KeyFile,
		SNI:                    in.SNI,
		DisableAutoHostRewrite: in.DisableAutoHostRewrite,
	}
}

func (in LinkedService) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if (in.CertFile != "" && in.KeyFile == "") || (in.KeyFile != "" && in.CertFile == "") {
		asJSON, _ := json.Marshal(in)
		errs = append(errs, field.Invalid(path,
			string(asJSON),
			"if certFile or keyFile is set, the other must also be set"))
	}
	return errs
}

func (in *TerminatingGateway) validateNamespaces(namespacesEnabled bool) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !namespacesEnabled {
		for i, service := range in.Spec.Services {
			if service.Namespace != "" {
				errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("namespace"),
					service.Namespace, `Consul Enterprise namespaces must be enabled to set service.namespace`))
			}
		}
	}
	return errs
}
