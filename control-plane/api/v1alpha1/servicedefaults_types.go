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
	ServiceDefaultsKubeKind = "servicedefaults"
	defaultUpstream         = "default"
	overrideUpstream        = "override"
)

func init() {
	SchemeBuilder.Register(&ServiceDefaults{}, &ServiceDefaultsList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceDefaults is the Schema for the servicedefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ServiceDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceDefaultsList contains a list of ServiceDefaults
type ServiceDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceDefaults `json:"items"`
}

// ServiceDefaultsSpec defines the desired state of ServiceDefaults
type ServiceDefaultsSpec struct {
	// Protocol sets the protocol of the service. This is used by Connect proxies for
	// things like observability features and to unlock usage of the
	// service-splitter and service-router config entries for a service.
	Protocol string `json:"protocol,omitempty"`
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGateway `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose Expose `json:"expose,omitempty"`
	// ExternalSNI is an optional setting that allows for the TLS SNI value
	// to be changed to a non-connect value when federating with an external system.
	ExternalSNI string `json:"externalSNI,omitempty"`
	// TransparentProxy controls configuration specific to proxies in transparent mode.
	// Note: This cannot be set using the CRD and should be set using annotations on the
	// services that are part of the mesh.
	TransparentProxy *TransparentProxy `json:"transparentProxy,omitempty"`
	// Mode can be one of "direct" or "transparent". "transparent" represents that inbound and outbound
	// application traffic is being captured and redirected through the proxy. This mode does not
	// enable the traffic redirection itself. Instead it signals Consul to configure Envoy as if
	// traffic is already being redirected. "direct" represents that the proxy's listeners must be
	// dialed directly by the local application and other proxies.
	// Note: This cannot be set using the CRD and should be set using annotations on the
	// services that are part of the mesh.
	Mode *ProxyMode `json:"mode,omitempty"`
	// UpstreamConfig controls default configuration settings that apply across all upstreams,
	// and per-upstream configuration overrides. Note that per-upstream configuration applies
	// across all federated datacenters to the pairing of source and upstream destination services.
	UpstreamConfig *Upstreams `json:"upstreamConfig,omitempty"`
}

type Upstreams struct {
	// Defaults contains default configuration for all upstreams of a given
	// service. The name field must be empty.
	Defaults *Upstream `json:"defaults,omitempty"`
	// Overrides is a slice of per-service configuration. The name field is
	// required.
	Overrides []*Upstream `json:"overrides,omitempty"`
}

type Upstream struct {
	// Name is only accepted within a service-defaults config entry.
	Name string `json:"name,omitempty"`
	// Namespace is only accepted within a service-defaults config entry.
	Namespace string `json:"namespace,omitempty"`
	// Partition is only accepted within a service-defaults config entry.
	Partition string `json:"partition,omitempty"`
	// EnvoyListenerJSON is a complete override ("escape hatch") for the upstream's
	// listener.
	// Note: This escape hatch is NOT compatible with the discovery chain and
	// will be ignored if a discovery chain is active.
	EnvoyListenerJSON string `json:"envoyListenerJSON,omitempty"`
	// EnvoyClusterJSON is a complete override ("escape hatch") for the upstream's
	// cluster. The Connect client TLS certificate and context will be injected
	// overriding any TLS settings present.
	// Note: This escape hatch is NOT compatible with the discovery chain and
	// will be ignored if a discovery chain is active.
	EnvoyClusterJSON string `json:"envoyClusterJSON,omitempty"`
	// Protocol describes the upstream's service protocol. Valid values are "tcp",
	// "http" and "grpc". Anything else is treated as tcp. This enables protocol
	// aware features like per-request metrics and connection pooling, tracing,
	// routing etc.
	Protocol string `json:"protocol,omitempty"`
	// ConnectTimeoutMs is the number of milliseconds to timeout making a new
	// connection to this upstream. Defaults to 5000 (5 seconds) if not set.
	ConnectTimeoutMs int `json:"connectTimeoutMs,omitempty"`
	// Limits are the set of limits that are applied to the proxy for a specific upstream of a
	// service instance.
	Limits *UpstreamLimits `json:"limits,omitempty"`
	// PassiveHealthCheck configuration determines how upstream proxy instances will
	// be monitored for removal from the load balancing pool.
	PassiveHealthCheck *PassiveHealthCheck `json:"passiveHealthCheck,omitempty"`
	// MeshGatewayConfig controls how Mesh Gateways are configured and used.
	MeshGateway MeshGateway `json:"meshGateway,omitempty"`
}

// UpstreamLimits describes the limits that are associated with a specific
// upstream of a service instance.
type UpstreamLimits struct {
	// MaxConnections is the maximum number of connections the local proxy can
	// make to the upstream service.
	MaxConnections *int `json:"maxConnections,omitempty"`
	// MaxPendingRequests is the maximum number of requests that will be queued
	// waiting for an available connection. This is mostly applicable to HTTP/1.1
	// clusters since all HTTP/2 requests are streamed over a single
	// connection.
	MaxPendingRequests *int `json:"maxPendingRequests,omitempty"`
	// MaxConcurrentRequests is the maximum number of in-flight requests that will be allowed
	// to the upstream cluster at a point in time. This is mostly applicable to HTTP/2
	// clusters since all HTTP/1.1 requests are limited by MaxConnections.
	MaxConcurrentRequests *int `json:"maxConcurrentRequests,omitempty"`
}

// PassiveHealthCheck configuration determines how upstream proxy instances will
// be monitored for removal from the load balancing pool.
type PassiveHealthCheck struct {
	// Interval between health check analysis sweeps. Each sweep may remove
	// hosts or return hosts to the pool.
	Interval metav1.Duration `json:"interval,omitempty"`
	// MaxFailures is the count of consecutive failures that results in a host
	// being removed from the pool.
	MaxFailures uint32 `json:"maxFailures,omitempty"`
}

func (in *ServiceDefaults) ConsulKind() string {
	return capi.ServiceDefaults
}

func (in *ServiceDefaults) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *ServiceDefaults) KubeKind() string {
	return ServiceDefaultsKubeKind
}

func (in *ServiceDefaults) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceDefaults) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *ServiceDefaults) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceDefaults) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceDefaults) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceDefaults) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceDefaults) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *ServiceDefaults) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ServiceDefaults) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceDefaults) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

// ToConsul converts the entry into it's Consul equivalent struct.
func (in *ServiceDefaults) ToConsul(datacenter string) capi.ConfigEntry {
	return &capi.ServiceConfigEntry{
		Kind:             in.ConsulKind(),
		Name:             in.ConsulName(),
		Protocol:         in.Spec.Protocol,
		MeshGateway:      in.Spec.MeshGateway.toConsul(),
		Expose:           in.Spec.Expose.toConsul(),
		ExternalSNI:      in.Spec.ExternalSNI,
		TransparentProxy: in.Spec.TransparentProxy.toConsul(),
		UpstreamConfig:   in.Spec.UpstreamConfig.toConsul(),
		Meta:             meta(datacenter),
	}
}

// Validate validates the fields provided in the spec of the ServiceDefaults and
// returns an error which lists all invalid fields in the resource spec.
func (in *ServiceDefaults) Validate(consulMeta common.ConsulMeta) error {
	var allErrs field.ErrorList
	path := field.NewPath("spec")

	validProtocols := []string{"tcp", "http", "http2", "grpc"}
	if in.Spec.Protocol != "" && !sliceContains(validProtocols, in.Spec.Protocol) {
		allErrs = append(allErrs, field.Invalid(path.Child("protocol"), in.Spec.Protocol, notInSliceMessage(validProtocols)))
	}
	if err := in.Spec.MeshGateway.validate(path.Child("meshGateway")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.TransparentProxy.validate(path.Child("transparentProxy")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.Mode.validate(path.Child("mode")); err != nil {
		allErrs = append(allErrs, err)
	}
	allErrs = append(allErrs, in.Spec.UpstreamConfig.validate(path.Child("upstreamConfig"), consulMeta.PartitionsEnabled)...)
	allErrs = append(allErrs, in.Spec.Expose.validate(path.Child("expose"))...)

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ServiceDefaultsKubeKind},
			in.KubernetesName(), allErrs)
	}

	return nil
}

func (in *Upstreams) validate(path *field.Path, partitionsEnabled bool) field.ErrorList {
	if in == nil {
		return nil
	}
	var errs field.ErrorList
	if err := in.Defaults.validate(path.Child("defaults"), defaultUpstream, partitionsEnabled); err != nil {
		errs = append(errs, err...)
	}
	for i, override := range in.Overrides {
		if err := override.validate(path.Child("overrides").Index(i), overrideUpstream, partitionsEnabled); err != nil {
			errs = append(errs, err...)
		}
	}
	return errs
}

func (in *Upstreams) toConsul() *capi.UpstreamConfiguration {
	if in == nil {
		return nil
	}
	upstreams := &capi.UpstreamConfiguration{}
	upstreams.Defaults = in.Defaults.toConsul()
	for _, override := range in.Overrides {
		upstreams.Overrides = append(upstreams.Overrides, override.toConsul())
	}
	return upstreams
}

func (in *Upstream) validate(path *field.Path, kind string, partitionsEnabled bool) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList
	if kind == defaultUpstream {
		if in.Name != "" {
			errs = append(errs, field.Invalid(path.Child("name"), in.Name, "upstream.name for a default upstream must be \"\""))
		}
	} else if kind == overrideUpstream {
		if in.Name == "" {
			errs = append(errs, field.Invalid(path.Child("name"), in.Name, "upstream.name for an override upstream cannot be \"\""))
		}
	}
	if !partitionsEnabled && in.Partition != "" {
		errs = append(errs, field.Invalid(path.Child("partition"), in.Partition, "Consul Enterprise Admin Partitions must be enabled to set upstream.partition"))
	}
	if err := in.MeshGateway.validate(path.Child("meshGateway")); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (in *Upstream) toConsul() *capi.UpstreamConfig {
	if in == nil {
		return nil
	}
	return &capi.UpstreamConfig{
		Name:               in.Name,
		Namespace:          in.Namespace,
		Partition:          in.Partition,
		EnvoyListenerJSON:  in.EnvoyListenerJSON,
		EnvoyClusterJSON:   in.EnvoyClusterJSON,
		Protocol:           in.Protocol,
		ConnectTimeoutMs:   in.ConnectTimeoutMs,
		Limits:             in.Limits.toConsul(),
		PassiveHealthCheck: in.PassiveHealthCheck.toConsul(),
		MeshGateway:        in.MeshGateway.toConsul(),
	}
}

func (in *UpstreamLimits) toConsul() *capi.UpstreamLimits {
	if in == nil {
		return nil
	}
	return &capi.UpstreamLimits{
		MaxConnections:        in.MaxConnections,
		MaxPendingRequests:    in.MaxPendingRequests,
		MaxConcurrentRequests: in.MaxConcurrentRequests,
	}
}

func (in *PassiveHealthCheck) toConsul() *capi.PassiveHealthCheck {
	if in == nil {
		return nil
	}
	return &capi.PassiveHealthCheck{
		Interval:    in.Interval.Duration,
		MaxFailures: in.MaxFailures,
	}
}

// DefaultNamespaceFields has no behaviour here as service-defaults have no namespace specific fields.
func (in *ServiceDefaults) DefaultNamespaceFields(_ common.ConsulMeta) {
}

// MatchesConsul returns true if entry has the same config as this struct.
func (in *ServiceDefaults) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(),
		cmp.Comparer(transparentProxyConfigComparer))
}

func (in *ServiceDefaults) ConsulGlobalResource() bool {
	return false
}
