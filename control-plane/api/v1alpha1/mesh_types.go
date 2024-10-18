// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	MeshKubeKind = "mesh"
)

func init() {
	SchemeBuilder.Register(&Mesh{}, &MeshList{})
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Mesh is the Schema for the mesh API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type Mesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MeshSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MeshList contains a list of Mesh.
type MeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Mesh `json:"items"`
}

// MeshSpec defines the desired state of Mesh.
type MeshSpec struct {
	// TransparentProxy controls the configuration specific to proxies in "transparent" mode. Added in v1.10.0.
	TransparentProxy TransparentProxyMeshConfig `json:"transparentProxy,omitempty"`
	// AllowEnablingPermissiveMutualTLS must be true in order to allow setting
	// MutualTLSMode=permissive in either service-defaults or proxy-defaults.
	AllowEnablingPermissiveMutualTLS bool `json:"allowEnablingPermissiveMutualTLS,omitempty"`
	// TLS defines the TLS configuration for the service mesh.
	TLS *MeshTLSConfig `json:"tls,omitempty"`
	// HTTP defines the HTTP configuration for the service mesh.
	HTTP *MeshHTTPConfig `json:"http,omitempty"`
	// Peering defines the peering configuration for the service mesh.
	Peering *PeeringMeshConfig `json:"peering,omitempty"`
	// ValidateClusters controls whether the clusters the route table refers to are validated. The default value is
	// false. When set to false and a route refers to a cluster that does not exist, the route table loads and routing
	// to a non-existent cluster results in a 404. When set to true and the route is set to a cluster that do not exist,
	// the route table will not load. For more information, refer to
	// [HTTP route configuration in the Envoy docs](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route.proto#envoy-v3-api-field-config-route-v3-routeconfiguration-validate-clusters)
	// for more details.
	ValidateClusters bool `json:"validateClusters,omitempty"`
}

// TransparentProxyMeshConfig controls configuration specific to proxies in "transparent" mode. Added in v1.10.0.
type TransparentProxyMeshConfig struct {
	// MeshDestinationsOnly determines whether sidecar proxies operating in "transparent" mode can proxy traffic
	// to IP addresses not registered in Consul's catalog. If enabled, traffic will only be proxied to upstreams
	// with service registrations in the catalog.
	MeshDestinationsOnly bool `json:"meshDestinationsOnly,omitempty"`
}

type MeshTLSConfig struct {
	// Incoming defines the TLS configuration for inbound mTLS connections targeting
	// the public listener on Connect and TerminatingGateway proxy kinds.
	Incoming *MeshDirectionalTLSConfig `json:"incoming,omitempty"`
	// Outgoing defines the TLS configuration for outbound mTLS connections dialing upstreams
	// from Connect and IngressGateway proxy kinds.
	Outgoing *MeshDirectionalTLSConfig `json:"outgoing,omitempty"`
}

type MeshHTTPConfig struct {
	SanitizeXForwardedClientCert bool `json:"sanitizeXForwardedClientCert,omitempty"`
	// Incoming configures settings for incoming HTTP traffic to mesh proxies.
	Incoming *MeshDirectionalHTTPConfig `json:"incoming,omitempty"`
	// There is not currently an outgoing MeshDirectionalHTTPConfig, as
	// the only required config for either direction at present is inbound
	// request normalization.
}

// MeshDirectionalHTTPConfig holds mesh configuration specific to HTTP
// requests for a given traffic direction.
type MeshDirectionalHTTPConfig struct {
	RequestNormalization *RequestNormalizationMeshConfig `json:"requestNormalization,omitempty"`
}

type PeeringMeshConfig struct {
	// PeerThroughMeshGateways determines whether peering traffic between
	// control planes should flow through mesh gateways. If enabled,
	// Consul servers will advertise mesh gateway addresses as their own.
	// Additionally, mesh gateways will configure themselves to expose
	// the local servers using a peering-specific SNI.
	PeerThroughMeshGateways bool `json:"peerThroughMeshGateways,omitempty"`
}

type MeshDirectionalTLSConfig struct {
	// TLSMinVersion sets the default minimum TLS version supported.
	// One of `TLS_AUTO`, `TLSv1_0`, `TLSv1_1`, `TLSv1_2`, or `TLSv1_3`.
	// If unspecified, Envoy v1.22.0 and newer will default to TLS 1.2 as a min version,
	// while older releases of Envoy default to TLS 1.0.
	TLSMinVersion string `json:"tlsMinVersion,omitempty"`
	// TLSMaxVersion sets the default maximum TLS version supported. Must be greater than or equal to `TLSMinVersion`.
	// One of `TLS_AUTO`, `TLSv1_0`, `TLSv1_1`, `TLSv1_2`, or `TLSv1_3`.
	// If unspecified, Envoy will default to TLS 1.3 as a max version for incoming connections.
	TLSMaxVersion string `json:"tlsMaxVersion,omitempty"`
	// CipherSuites sets the default list of TLS cipher suites to support when negotiating connections using TLS 1.2 or earlier.
	// If unspecified, Envoy will use a default server cipher list. The list of supported cipher suites can be seen in
	// https://github.com/hashicorp/consul/blob/v1.11.2/types/tls.go#L154-L169 and is dependent on underlying support in Envoy.
	// Future releases of Envoy may remove currently-supported but insecure cipher suites,
	// and future releases of Consul may add new supported cipher suites if any are added to Envoy.
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

// RequestNormalizationMeshConfig contains options pertaining to the
// normalization of HTTP requests processed by mesh proxies.
type RequestNormalizationMeshConfig struct {
	// InsecureDisablePathNormalization sets the value of the \`normalize_path\` option in the Envoy listener's
	// `HttpConnectionManager`. The default value is \`false\`. When set to \`true\` in Consul, \`normalize_path\` is
	// set to \`false\` for the Envoy proxy. This parameter disables the normalization of request URL paths according to
	// RFC 3986, conversion of \`\\\` to \`/\`, and decoding non-reserved %-encoded characters. When using L7 intentions
	// with path match rules, we recommend enabling path normalization in order to avoid match rule circumvention with
	// non-normalized path values.
	InsecureDisablePathNormalization bool `json:"insecureDisablePathNormalization,omitempty"`
	// MergeSlashes sets the value of the \`merge_slashes\` option in the Envoy listener's \`HttpConnectionManager\`.
	// The default value is \`false\`. This option controls the normalization of request URL paths by merging
	// consecutive \`/\` characters. This normalization is not part of RFC 3986. When using L7 intentions with path
	// match rules, we recommend enabling this setting to avoid match rule circumvention through non-normalized path
	// values, unless legitimate service traffic depends on allowing for repeat \`/\` characters, or upstream services
	// are configured to differentiate between single and multiple slashes.
	MergeSlashes bool `json:"mergeSlashes,omitempty"`
	// PathWithEscapedSlashesAction sets the value of the \`path_with_escaped_slashes_action\` option in the Envoy
	// listener's \`HttpConnectionManager\`. The default value of this option is empty, which is equivalent to
	// \`IMPLEMENTATION_SPECIFIC_DEFAULT\`. This parameter controls the action taken in response to request URL paths
	// with escaped slashes in the path. When using L7 intentions with path match rules, we recommend enabling this
	// setting to avoid match rule circumvention through non-normalized path values, unless legitimate service traffic
	// depends on allowing for escaped \`/\` or \`\\\` characters, or upstream services are configured to differentiate
	// between escaped and unescaped slashes. Refer to the Envoy documentation for more information on available
	// options.
	PathWithEscapedSlashesAction string `json:"pathWithEscapedSlashesAction,omitempty"`
	// HeadersWithUnderscoresAction sets the value of the \`headers_with_underscores_action\` option in the Envoy
	// listener's \`HttpConnectionManager\` under \`common_http_protocol_options\`. The default value of this option is
	// empty, which is equivalent to \`ALLOW\`. Refer to the Envoy documentation for more information on available
	// options.
	HeadersWithUnderscoresAction string `json:"headersWithUnderscoresAction,omitempty"`
}

// PathWithEscapedSlashesAction is an enum that defines the action to take when
// a request path contains escaped slashes. It mirrors exactly the set of options
// in Envoy's UriPathNormalizationOptions.PathWithEscapedSlashesAction enum.
// See github.com/envoyproxy/go-control-plane envoy_http_v3.HttpConnectionManager_PathWithEscapedSlashesAction.
const (
	PathWithEscapedSlashesActionDefault             = "IMPLEMENTATION_SPECIFIC_DEFAULT"
	PathWithEscapedSlashesActionKeep                = "KEEP_UNCHANGED"
	PathWithEscapedSlashesActionReject              = "REJECT_REQUEST"
	PathWithEscapedSlashesActionUnescapeAndRedirect = "UNESCAPE_AND_REDIRECT"
	PathWithEscapedSlashesActionUnescapeAndForward  = "UNESCAPE_AND_FORWARD"
)

// HeadersWithUnderscoresAction is an enum that defines the action to take when
// a request contains headers with underscores. It mirrors exactly the set of
// options in Envoy's HttpProtocolOptions.HeadersWithUnderscoresAction enum.
// See github.com/envoyproxy/go-control-plane envoy_core_v3.HttpProtocolOptions_HeadersWithUnderscoresAction.
const (
	HeadersWithUnderscoresActionAllow         = "ALLOW"
	HeadersWithUnderscoresActionRejectRequest = "REJECT_REQUEST"
	HeadersWithUnderscoresActionDropHeader    = "DROP_HEADER"
)

func (in *TransparentProxyMeshConfig) toConsul() capi.TransparentProxyMeshConfig {
	return capi.TransparentProxyMeshConfig{MeshDestinationsOnly: in.MeshDestinationsOnly}
}

func (in *Mesh) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *Mesh) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *Mesh) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers

}

func (in *Mesh) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *Mesh) ConsulKind() string {
	return capi.MeshConfig
}

func (in *Mesh) ConsulMirroringNS() string {
	return common.DefaultConsulNamespace
}

func (in *Mesh) KubeKind() string {
	return MeshKubeKind
}

func (in *Mesh) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *Mesh) SyncedConditionStatus() corev1.ConditionStatus {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown
	}
	return cond.Status
}

func (in *Mesh) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *Mesh) ConsulGlobalResource() bool {
	return true
}

func (in *Mesh) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *Mesh) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *Mesh) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *Mesh) ToConsul(datacenter string) capi.ConfigEntry {
	return &capi.MeshConfigEntry{
		TransparentProxy:                 in.Spec.TransparentProxy.toConsul(),
		AllowEnablingPermissiveMutualTLS: in.Spec.AllowEnablingPermissiveMutualTLS,
		TLS:                              in.Spec.TLS.toConsul(),
		HTTP:                             in.Spec.HTTP.toConsul(),
		Peering:                          in.Spec.Peering.toConsul(),
		ValidateClusters:                 in.Spec.ValidateClusters,
		Meta:                             meta(datacenter),
	}
}

func (in *Mesh) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.MeshConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.MeshConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *Mesh) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	errs = append(errs, in.Spec.TLS.validate(path.Child("tls"))...)
	errs = append(errs, in.Spec.Peering.validate(path.Child("peering"), consulMeta.PartitionsEnabled, consulMeta.Partition)...)
	if in.Spec.HTTP != nil &&
		in.Spec.HTTP.Incoming != nil &&
		in.Spec.HTTP.Incoming.RequestNormalization != nil {
		errs = append(errs, in.Spec.HTTP.Incoming.RequestNormalization.validate(path.Child("http", "incoming", "requestNormalization"))...)
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: MeshKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in *MeshTLSConfig) toConsul() *capi.MeshTLSConfig {
	if in == nil {
		return nil
	}
	return &capi.MeshTLSConfig{
		Incoming: in.Incoming.toConsul(),
		Outgoing: in.Outgoing.toConsul(),
	}
}

func (in *MeshHTTPConfig) toConsul() *capi.MeshHTTPConfig {
	if in == nil {
		return nil
	}
	return &capi.MeshHTTPConfig{
		SanitizeXForwardedClientCert: in.SanitizeXForwardedClientCert,
		Incoming:                     in.Incoming.toConsul(),
	}
}

func (in *MeshDirectionalHTTPConfig) toConsul() *capi.MeshDirectionalHTTPConfig {
	if in == nil {
		return nil
	}
	return &capi.MeshDirectionalHTTPConfig{
		RequestNormalization: in.RequestNormalization.toConsul(),
	}
}

func (in *RequestNormalizationMeshConfig) toConsul() *capi.RequestNormalizationMeshConfig {
	if in == nil {
		return nil
	}
	return &capi.RequestNormalizationMeshConfig{
		InsecureDisablePathNormalization: in.InsecureDisablePathNormalization,
		MergeSlashes:                     in.MergeSlashes,
		PathWithEscapedSlashesAction:     in.PathWithEscapedSlashesAction,
		HeadersWithUnderscoresAction:     in.HeadersWithUnderscoresAction,
	}
}

func (in *MeshTLSConfig) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList
	errs = append(errs, in.Incoming.validate(path.Child("incoming"))...)
	errs = append(errs, in.Outgoing.validate(path.Child("outgoing"))...)
	return errs
}

func (in *MeshDirectionalTLSConfig) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList
	versions := []string{"TLS_AUTO", "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3", ""}

	if !sliceContains(versions, in.TLSMaxVersion) {
		errs = append(errs, field.Invalid(path.Child("tlsMaxVersion"), in.TLSMaxVersion, notInSliceMessage(versions)))
	}
	if !sliceContains(versions, in.TLSMinVersion) {
		errs = append(errs, field.Invalid(path.Child("tlsMinVersion"), in.TLSMinVersion, notInSliceMessage(versions)))
	}
	return errs
}

func (in *MeshDirectionalTLSConfig) toConsul() *capi.MeshDirectionalTLSConfig {
	if in == nil {
		return nil
	}
	return &capi.MeshDirectionalTLSConfig{
		TLSMinVersion: in.TLSMinVersion,
		TLSMaxVersion: in.TLSMaxVersion,
		CipherSuites:  in.CipherSuites,
	}
}

func (in *PeeringMeshConfig) toConsul() *capi.PeeringMeshConfig {
	if in == nil {
		return nil
	}
	return &capi.PeeringMeshConfig{PeerThroughMeshGateways: in.PeerThroughMeshGateways}
}

func (in *PeeringMeshConfig) validate(path *field.Path, partitionsEnabled bool, partition string) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList

	if partitionsEnabled && in.PeerThroughMeshGateways && partition != common.DefaultConsulPartition {
		errs = append(errs, field.Forbidden(path.Child("peerThroughMeshGateways"),
			"\"peerThroughMeshGateways\" is only valid in the \"default\" partition"))
	}

	return errs
}

func (in *RequestNormalizationMeshConfig) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList
	pathWithEscapedSlashesActions := []string{
		PathWithEscapedSlashesActionDefault,
		PathWithEscapedSlashesActionKeep,
		PathWithEscapedSlashesActionReject,
		PathWithEscapedSlashesActionUnescapeAndRedirect,
		PathWithEscapedSlashesActionUnescapeAndForward,
		"",
	}
	headersWithUnderscoresActions := []string{
		HeadersWithUnderscoresActionAllow,
		HeadersWithUnderscoresActionRejectRequest,
		HeadersWithUnderscoresActionDropHeader,
		"",
	}

	if !sliceContains(pathWithEscapedSlashesActions, in.PathWithEscapedSlashesAction) {
		errs = append(errs, field.Invalid(path.Child("pathWithEscapedSlashesAction"), in.PathWithEscapedSlashesAction, notInSliceMessage(pathWithEscapedSlashesActions)))
	}
	if !sliceContains(headersWithUnderscoresActions, in.HeadersWithUnderscoresAction) {
		errs = append(errs, field.Invalid(path.Child("headersWithUnderscoresAction"), in.HeadersWithUnderscoresAction, notInSliceMessage(headersWithUnderscoresActions)))
	}
	return errs
}

// DefaultNamespaceFields has no behaviour here as meshes have no namespace specific fields.
func (in *Mesh) DefaultNamespaceFields(_ common.ConsulMeta) {
}
