// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

const (
	ProxyDefaultsKubeKind string = "proxydefaults"
)

func init() {
	SchemeBuilder.Register(&ProxyDefaults{}, &ProxyDefaultsList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProxyDefaults is the Schema for the proxydefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="proxy-defaults"
type ProxyDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ProxyDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyDefaultsList contains a list of ProxyDefaults.
type ProxyDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ProxyDefaults `json:"items"`
}

// RawMessage for Config based on recommendation here: https://github.com/kubernetes-sigs/controller-tools/issues/294#issuecomment-518380816

// ProxyDefaultsSpec defines the desired state of ProxyDefaults.
type ProxyDefaultsSpec struct {
	// Mode can be one of "direct" or "transparent". "transparent" represents that inbound and outbound
	// application traffic is being captured and redirected through the proxy. This mode does not
	// enable the traffic redirection itself. Instead it signals Consul to configure Envoy as if
	// traffic is already being redirected. "direct" represents that the proxy's listeners must be
	// dialed directly by the local application and other proxies.
	// Note: This cannot be set using the CRD and should be set using annotations on the
	// services that are part of the mesh.
	Mode *ProxyMode `json:"mode,omitempty"`
	// TransparentProxy controls configuration specific to proxies in transparent mode.
	// Note: This cannot be set using the CRD and should be set using annotations on the
	// services that are part of the mesh.
	TransparentProxy *TransparentProxy `json:"transparentProxy,omitempty"`
	// MutualTLSMode controls whether mutual TLS is required for all incoming
	// connections when transparent proxy is enabled. This can be set to
	// "permissive" or "strict". "strict" is the default which requires mutual
	// TLS for incoming connections. In the insecure "permissive" mode,
	// connections to the sidecar proxy public listener port require mutual
	// TLS, but connections to the service port do not require mutual TLS and
	// are proxied to the application unmodified. Note: Intentions are not
	// enforced for non-mTLS connections. To keep your services secure, we
	// recommend using "strict" mode whenever possible and enabling
	// "permissive" mode only when necessary.
	MutualTLSMode MutualTLSMode `json:"mutualTLSMode,omitempty"`
	// Config is an arbitrary map of configuration values used by Connect proxies.
	// Any values that your proxy allows can be configured globally here.
	// Supports JSON config values. See https://www.consul.io/docs/connect/proxies/envoy#configuration-formatting
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	Config json.RawMessage `json:"config,omitempty"`
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGateway `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose Expose `json:"expose,omitempty"`
	// AccessLogs controls all envoy instances' access logging configuration.
	AccessLogs *AccessLogs `json:"accessLogs,omitempty"`
	// EnvoyExtensions are a list of extensions to modify Envoy proxy configuration.
	EnvoyExtensions EnvoyExtensions `json:"envoyExtensions,omitempty"`
	// FailoverPolicy specifies the exact mechanism used for failover.
	FailoverPolicy *FailoverPolicy `json:"failoverPolicy,omitempty"`
	// PrioritizeByLocality controls whether the locality of services within the
	// local partition will be used to prioritize connectivity.
	PrioritizeByLocality *PrioritizeByLocality `json:"prioritizeByLocality,omitempty"`
}

func (in *ProxyDefaults) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ProxyDefaults) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ProxyDefaults) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers

}

func (in *ProxyDefaults) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ProxyDefaults) ConsulKind() string {
	return capi.ProxyDefaults
}

func (in *ProxyDefaults) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *ProxyDefaults) KubeKind() string {
	return ProxyDefaultsKubeKind
}

func (in *ProxyDefaults) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ProxyDefaults) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *ProxyDefaults) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ProxyDefaults) ConsulGlobalResource() bool {
	return true
}

func (in *ProxyDefaults) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ProxyDefaults) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
	in.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}

func (in *ProxyDefaults) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ProxyDefaults) ToConsul(datacenter string) capi.ConfigEntry {
	consulConfig := in.convertConfig()
	return &capi.ProxyConfigEntry{
		Kind:                 in.ConsulKind(),
		Name:                 in.ConsulName(),
		MeshGateway:          in.Spec.MeshGateway.toConsul(),
		Expose:               in.Spec.Expose.toConsul(),
		Config:               consulConfig,
		TransparentProxy:     in.Spec.TransparentProxy.toConsul(),
		MutualTLSMode:        in.Spec.MutualTLSMode.toConsul(),
		AccessLogs:           in.Spec.AccessLogs.toConsul(),
		EnvoyExtensions:      in.Spec.EnvoyExtensions.toConsul(),
		FailoverPolicy:       in.Spec.FailoverPolicy.toConsul(),
		PrioritizeByLocality: in.Spec.PrioritizeByLocality.toConsul(),
		Meta:                 meta(datacenter),
	}
}

func (in *ProxyDefaults) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ProxyConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ProxyConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(),
		cmp.Comparer(transparentProxyConfigComparer))
}

func (in *ProxyDefaults) Validate(_ common.ConsulMeta) error {
	var allErrs field.ErrorList
	path := field.NewPath("spec")

	if err := in.Spec.MeshGateway.validate(path.Child("meshGateway")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.TransparentProxy.validate(path.Child("transparentProxy")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.MutualTLSMode.validate(); err != nil {
		allErrs = append(allErrs, field.Invalid(path.Child("mutualTLSMode"), in.Spec.MutualTLSMode, err.Error()))
	}
	if err := in.Spec.Mode.validate(path.Child("mode")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.validateConfig(path.Child("config")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.AccessLogs.validate(path.Child("accessLogs")); err != nil {
		allErrs = append(allErrs, err)
	}
	allErrs = append(allErrs, in.Spec.Expose.validate(path.Child("expose"))...)
	allErrs = append(allErrs, in.Spec.EnvoyExtensions.validate(path.Child("envoyExtensions"))...)
	allErrs = append(allErrs, in.Spec.FailoverPolicy.validate(path.Child("failoverPolicy"))...)
	allErrs = append(allErrs, in.Spec.PrioritizeByLocality.validate(path.Child("prioritizeByLocality"))...)

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ProxyDefaultsKubeKind},
			in.KubernetesName(), allErrs)
	}

	return nil
}

// DefaultNamespaceFields has no behaviour here as proxy-defaults have no namespace specific fields.
func (in *ProxyDefaults) DefaultNamespaceFields(_ common.ConsulMeta) {
}

// convertConfig converts the config of type json.RawMessage which is stored
// by the resource into type map[string]interface{} which is saved by the
// consul API.
func (in *ProxyDefaults) convertConfig() map[string]interface{} {
	if in.Spec.Config == nil {
		return nil
	}
	var outConfig map[string]interface{}
	// We explicitly ignore the error returned by Unmarshall
	// because validate() ensures that if we get to here that it
	// won't return an error.
	_ = json.Unmarshal(in.Spec.Config, &outConfig)
	return outConfig
}

// validateConfig attempts to unmarshall the provided config into a map[string]interface{}
// and returns an error if the provided value for config isn't successfully unmarshalled
// and it implies the provided value is an invalid config.
func (in *ProxyDefaults) validateConfig(path *field.Path) *field.Error {
	if in.Spec.Config == nil {
		return nil
	}
	var outConfig map[string]interface{}
	if err := json.Unmarshal(in.Spec.Config, &outConfig); err != nil {
		return field.Invalid(path, string(in.Spec.Config), fmt.Sprintf(`must be valid map value: %s`, err))
	}
	return nil
}

// LogSinkType represents the destination for Envoy access logs.
// One of "file", "stderr", or "stdout".
type LogSinkType string

const (
	DefaultLogSinkType LogSinkType = ""
	FileLogSinkType    LogSinkType = "file"
	StdErrLogSinkType  LogSinkType = "stderr"
	StdOutLogSinkType  LogSinkType = "stdout"
)

// AccessLogs describes the access logging configuration for all Envoy proxies in the mesh.
type AccessLogs struct {
	// Enabled turns on all access logging
	Enabled bool `json:"enabled,omitempty"`

	// DisableListenerLogs turns off just listener logs for connections rejected by Envoy because they don't
	// have a matching listener filter.
	DisableListenerLogs bool `json:"disableListenerLogs,omitempty"`

	// Type selects the output for logs
	// one of "file", "stderr". "stdout"
	Type LogSinkType `json:"type,omitempty"`

	// Path is the output file to write logs for file-type logging
	Path string `json:"path,omitempty"`

	// JSONFormat is a JSON-formatted string of an Envoy access log format dictionary.
	// See for more info on formatting: https://www.envoyproxy.io/docs/envoy/latest/configuration/observability/access_log/usage#format-dictionaries
	// Defining JSONFormat and TextFormat is invalid.
	JSONFormat string `json:"jsonFormat,omitempty"`

	// TextFormat is a representation of Envoy access logs format.
	// See for more info on formatting: https://www.envoyproxy.io/docs/envoy/latest/configuration/observability/access_log/usage#format-strings
	// Defining JSONFormat and TextFormat is invalid.
	TextFormat string `json:"textFormat,omitempty"`
}

func (in *AccessLogs) validate(path *field.Path) *field.Error {
	if in == nil {
		return nil
	}

	switch in.Type {
	case DefaultLogSinkType, StdErrLogSinkType, StdOutLogSinkType:
		// OK
	case FileLogSinkType:
		if in.Path == "" {
			return field.Invalid(path.Child("path"), in.Path, "path must be specified when using file type access logs")
		}
	default:
		return field.Invalid(path.Child("type"), in.Type, "invalid access log type (must be one of \"stdout\", \"stderr\", \"file\"")
	}

	if in.JSONFormat != "" && in.TextFormat != "" {
		return field.Invalid(path.Child("textFormat"), in.TextFormat, "cannot specify both access log jsonFormat and textFormat")
	}

	if in.Type != FileLogSinkType && in.Path != "" {
		return field.Invalid(path.Child("path"), in.Path, "path is only valid for file type access logs")
	}

	if in.JSONFormat != "" {
		msg := json.RawMessage{}
		if err := json.Unmarshal([]byte(in.JSONFormat), &msg); err != nil {
			return field.Invalid(path.Child("jsonFormat"), in.JSONFormat, "invalid access log json")
		}
	}

	return nil
}

func (in *AccessLogs) toConsul() *capi.AccessLogsConfig {
	if in == nil {
		return nil
	}
	return &capi.AccessLogsConfig{
		Enabled:             in.Enabled,
		DisableListenerLogs: in.DisableListenerLogs,
		JSONFormat:          in.JSONFormat,
		Path:                in.Path,
		TextFormat:          in.TextFormat,
		Type:                capi.LogSinkType(in.Type),
	}
}
