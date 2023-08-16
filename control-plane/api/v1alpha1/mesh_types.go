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
	// TLS defines the TLS configuration for the service mesh.
	TLS *MeshTLSConfig `json:"tls,omitempty"`
	// HTTP defines the HTTP configuration for the service mesh.
	HTTP *MeshHTTPConfig `json:"http,omitempty"`
	// Peering defines the peering configuration for the service mesh.
	Peering *PeeringMeshConfig `json:"peering,omitempty"`
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
	SanitizeXForwardedClientCert bool `json:"sanitizeXForwardedClientCert"`
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
		TransparentProxy: in.Spec.TransparentProxy.toConsul(),
		TLS:              in.Spec.TLS.toConsul(),
		HTTP:             in.Spec.HTTP.toConsul(),
		Peering:          in.Spec.Peering.toConsul(),
		Meta:             meta(datacenter),
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

// DefaultNamespaceFields has no behaviour here as meshes have no namespace specific fields.
func (in *Mesh) DefaultNamespaceFields(_ common.ConsulMeta) {
}
