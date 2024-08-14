// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
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
// +kubebuilder:resource:shortName="service-defaults"
type ServiceDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceDefaultsList contains a list of ServiceDefaults.
type ServiceDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceDefaults `json:"items"`
}

// ServiceDefaultsSpec defines the desired state of ServiceDefaults.
type ServiceDefaultsSpec struct {
	// Protocol sets the protocol of the service. This is used by Connect proxies for
	// things like observability features and to unlock usage of the
	// service-splitter and service-router config entries for a service.
	Protocol string `json:"protocol,omitempty"`
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
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGateway `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose Expose `json:"expose,omitempty"`
	// ExternalSNI is an optional setting that allows for the TLS SNI value
	// to be changed to a non-connect value when federating with an external system.
	ExternalSNI string `json:"externalSNI,omitempty"`
	// UpstreamConfig controls default configuration settings that apply across all upstreams,
	// and per-upstream configuration overrides. Note that per-upstream configuration applies
	// across all federated datacenters to the pairing of source and upstream destination services.
	UpstreamConfig *Upstreams `json:"upstreamConfig,omitempty"`
	// Destination is an address(es)/port combination that represents an endpoint
	// outside the mesh. This is only valid when the mesh is configured in "transparent"
	// mode. Destinations live outside of Consul's catalog, and because of this, they
	// do not require an artificial node to be created.
	Destination *ServiceDefaultsDestination `json:"destination,omitempty"`
	// MaxInboundConnections is the maximum number of concurrent inbound connections to
	// each service instance. Defaults to 0 (using consul's default) if not set.
	MaxInboundConnections int `json:"maxInboundConnections,omitempty"`
	// LocalConnectTimeoutMs is the number of milliseconds allowed to make connections to the local application
	// instance before timing out. Defaults to 5000.
	LocalConnectTimeoutMs int `json:"localConnectTimeoutMs,omitempty"`
	// LocalRequestTimeoutMs is the timeout for HTTP requests to the local application instance in milliseconds.
	// Applies to HTTP-based protocols only. If not specified, inherits the Envoy default for
	// route timeouts (15s).
	LocalRequestTimeoutMs int `json:"localRequestTimeoutMs,omitempty"`
	// BalanceInboundConnections sets the strategy for allocating inbound connections to the service across
	// proxy threads. The only supported value is exact_balance. By default, no connection balancing is used.
	// Refer to the Envoy Connection Balance config for details.
	BalanceInboundConnections string `json:"balanceInboundConnections,omitempty"`
	// RateLimits is rate limiting configuration that is applied to
	// inbound traffic for a service. Rate limiting is a Consul enterprise feature.
	RateLimits *RateLimits `json:"rateLimits,omitempty"`
	// EnvoyExtensions are a list of extensions to modify Envoy proxy configuration.
	EnvoyExtensions EnvoyExtensions `json:"envoyExtensions,omitempty"`
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
	// Name is only accepted within service ServiceDefaultsSpec.UpstreamConfig.Overrides config entry.
	Name string `json:"name,omitempty"`
	// Namespace is only accepted within service ServiceDefaultsSpec.UpstreamConfig.Overrides config entry.
	Namespace string `json:"namespace,omitempty"`
	// Partition is only accepted within service ServiceDefaultsSpec.UpstreamConfig.Overrides config entry.
	Partition string `json:"partition,omitempty"`
	// Peer is only accepted within service ServiceDefaultsSpec.UpstreamConfig.Overrides config entry.
	Peer string `json:"peer,omitempty"`
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
	// hosts or return hosts to the pool. Ex. setting this to "10s" will set
	// the interval to 10 seconds.
	Interval metav1.Duration `json:"interval,omitempty"`
	// MaxFailures is the count of consecutive failures that results in a host
	// being removed from the pool.
	MaxFailures uint32 `json:"maxFailures,omitempty"`
	// EnforcingConsecutive5xx is the % chance that a host will be actually ejected
	// when an outlier status is detected through consecutive 5xx.
	// This setting can be used to disable ejection or to ramp it up slowly.
	// Ex. Setting this to 10 will make it a 10% chance that the host will be ejected.
	EnforcingConsecutive5xx *uint32 `json:"enforcingConsecutive5xx,omitempty"`
	// The maximum % of an upstream cluster that can be ejected due to outlier detection.
	// Defaults to 10% but will eject at least one host regardless of the value.
	MaxEjectionPercent *uint32 `json:"maxEjectionPercent,omitempty"`
	// The base time that a host is ejected for. The real time is equal to the base time
	// multiplied by the number of times the host has been ejected and is capped by
	// max_ejection_time (Default 300s). Defaults to 30s.
	BaseEjectionTime *metav1.Duration `json:"baseEjectionTime,omitempty"`
}

type ServiceDefaultsDestination struct {
	// Addresses is a list of IPs and/or hostnames that can be dialed
	// and routed through a terminating gateway.
	Addresses []string `json:"addresses,omitempty"`
	// Port is the port that can be dialed on any of the addresses in this
	// Destination.
	Port uint32 `json:"port,omitempty"`
}

// RateLimits is rate limiting configuration that is applied to
// inbound traffic for a service.
// Rate limiting is a Consul Enterprise feature.
type RateLimits struct {
	// InstanceLevel represents rate limit configuration
	// that is applied per service instance.
	InstanceLevel InstanceLevelRateLimits `json:"instanceLevel,omitempty"`
}

func (rl *RateLimits) toConsul() *capi.RateLimits {
	if rl == nil {
		return nil
	}
	routes := make([]capi.InstanceLevelRouteRateLimits, len(rl.InstanceLevel.Routes))
	for i, r := range rl.InstanceLevel.Routes {
		routes[i] = capi.InstanceLevelRouteRateLimits{
			PathExact:         r.PathExact,
			PathPrefix:        r.PathPrefix,
			PathRegex:         r.PathRegex,
			RequestsPerSecond: r.RequestsPerSecond,
			RequestsMaxBurst:  r.RequestsMaxBurst,
		}
	}
	return &capi.RateLimits{
		InstanceLevel: capi.InstanceLevelRateLimits{
			RequestsPerSecond: rl.InstanceLevel.RequestsPerSecond,
			RequestsMaxBurst:  rl.InstanceLevel.RequestsMaxBurst,
			Routes:            routes,
		},
	}
}

func (rl *RateLimits) validate(path *field.Path) field.ErrorList {
	if rl == nil {
		return nil
	}

	return rl.InstanceLevel.validate(path.Child("instanceLevel"))
}

type InstanceLevelRateLimits struct {
	// RequestsPerSecond is the average number of requests per second that can be
	// made without being throttled. This field is required if RequestsMaxBurst
	// is set. The allowed number of requests may exceed RequestsPerSecond up to
	// the value specified in RequestsMaxBurst.
	//
	// Internally, this is the refill rate of the token bucket used for rate limiting.
	RequestsPerSecond int `json:"requestsPerSecond,omitempty"`

	// RequestsMaxBurst is the maximum number of requests that can be sent
	// in a burst. Should be equal to or greater than RequestsPerSecond.
	// If unset, defaults to RequestsPerSecond.
	//
	// Internally, this is the maximum size of the token bucket used for rate limiting.
	RequestsMaxBurst int `json:"requestsMaxBurst,omitempty"`

	// Routes is a list of rate limits applied to specific routes.
	// For a given request, the first matching route will be applied, if any.
	// Overrides any top-level configuration.
	Routes []InstanceLevelRouteRateLimits `json:"routes,omitempty"`
}

func (irl InstanceLevelRateLimits) validate(path *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Track if RequestsPerSecond is set in at least one place in the config
	isRateLimitSet := irl.RequestsPerSecond > 0

	// Top-level RequestsPerSecond can be 0 (unset) or a positive number.
	if irl.RequestsPerSecond < 0 {
		allErrs = append(allErrs,
			field.Invalid(path.Child("requestsPerSecond"),
				irl.RequestsPerSecond,
				"RequestsPerSecond must be positive"))
	}

	if irl.RequestsPerSecond == 0 && irl.RequestsMaxBurst > 0 {
		allErrs = append(allErrs,
			field.Invalid(path.Child("requestsPerSecond"),
				irl.RequestsPerSecond,
				"RequestsPerSecond must be greater than 0 if RequestsMaxBurst is set"))
	}

	if irl.RequestsMaxBurst < 0 {
		allErrs = append(allErrs,
			field.Invalid(path.Child("requestsMaxBurst"),
				irl.RequestsMaxBurst,
				"RequestsMaxBurst must be positive"))
	}

	for i, route := range irl.Routes {
		if exact, prefix, regex := route.PathExact != "", route.PathPrefix != "", route.PathRegex != ""; (!exact && !prefix && !regex) ||
			(exact && prefix) || (exact && regex) || (prefix && regex) {
			allErrs = append(allErrs, field.Required(
				path.Child("routes").Index(i),
				"Route must define exactly one of PathExact, PathPrefix, or PathRegex"))
		}

		isRateLimitSet = isRateLimitSet || route.RequestsPerSecond > 0

		// Unlike top-level RequestsPerSecond, any route MUST have a RequestsPerSecond defined.
		if route.RequestsPerSecond <= 0 {
			allErrs = append(allErrs, field.Invalid(
				path.Child("routes").Index(i).Child("requestsPerSecond"),
				route.RequestsPerSecond, "RequestsPerSecond must be greater than 0"))
		}

		if route.RequestsMaxBurst < 0 {
			allErrs = append(allErrs, field.Invalid(
				path.Child("routes").Index(i).Child("requestsMaxBurst"),
				route.RequestsMaxBurst, "RequestsMaxBurst must be positive"))
		}
	}

	if !isRateLimitSet {
		allErrs = append(allErrs, field.Invalid(
			path.Child("requestsPerSecond"),
			irl.RequestsPerSecond, "At least one of top-level or route-level RequestsPerSecond must be set"))
	}
	return allErrs
}

type InstanceLevelRouteRateLimits struct {
	// Exact path to match. Exactly one of PathExact, PathPrefix, or PathRegex must be specified.
	PathExact string `json:"pathExact,omitempty"`
	// Prefix to match. Exactly one of PathExact, PathPrefix, or PathRegex must be specified.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// Regex to match. Exactly one of PathExact, PathPrefix, or PathRegex must be specified.
	PathRegex string `json:"pathRegex,omitempty"`

	// RequestsPerSecond is the average number of requests per
	// second that can be made without being throttled. This field is required
	// if RequestsMaxBurst is set. The allowed number of requests may exceed
	// RequestsPerSecond up to the value specified in RequestsMaxBurst.
	// Internally, this is the refill rate of the token bucket used for rate limiting.
	RequestsPerSecond int `json:"requestsPerSecond,omitempty"`

	// RequestsMaxBurst is the maximum number of requests that can be sent
	// in a burst. Should be equal to or greater than RequestsPerSecond. If unset,
	// defaults to RequestsPerSecond. Internally, this is the maximum size of the token
	// bucket used for rate limiting.
	RequestsMaxBurst int `json:"requestsMaxBurst,omitempty"`
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
		Kind:                      in.ConsulKind(),
		Name:                      in.ConsulName(),
		Protocol:                  in.Spec.Protocol,
		MeshGateway:               in.Spec.MeshGateway.toConsul(),
		Expose:                    in.Spec.Expose.toConsul(),
		ExternalSNI:               in.Spec.ExternalSNI,
		TransparentProxy:          in.Spec.TransparentProxy.toConsul(),
		MutualTLSMode:             in.Spec.MutualTLSMode.toConsul(),
		UpstreamConfig:            in.Spec.UpstreamConfig.toConsul(),
		Destination:               in.Spec.Destination.toConsul(),
		Meta:                      meta(datacenter),
		MaxInboundConnections:     in.Spec.MaxInboundConnections,
		LocalConnectTimeoutMs:     in.Spec.LocalConnectTimeoutMs,
		LocalRequestTimeoutMs:     in.Spec.LocalRequestTimeoutMs,
		BalanceInboundConnections: in.Spec.BalanceInboundConnections,
		RateLimits:                in.Spec.RateLimits.toConsul(),
		EnvoyExtensions:           in.Spec.EnvoyExtensions.toConsul(),
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
	if err := in.Spec.MutualTLSMode.validate(); err != nil {
		allErrs = append(allErrs, field.Invalid(path.Child("mutualTLSMode"), in.Spec.MutualTLSMode, err.Error()))
	}
	if err := in.Spec.Mode.validate(path.Child("mode")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := in.Spec.Destination.validate(path.Child("destination")); err != nil {
		allErrs = append(allErrs, err...)
	}

	if in.Spec.MaxInboundConnections < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("maxinboundconnections"), in.Spec.MaxInboundConnections, "MaxInboundConnections must be > 0"))
	}

	if in.Spec.LocalConnectTimeoutMs < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("localConnectTimeoutMs"), in.Spec.LocalConnectTimeoutMs, "LocalConnectTimeoutMs must be > 0"))
	}

	if in.Spec.LocalRequestTimeoutMs < 0 {
		allErrs = append(allErrs, field.Invalid(path.Child("localRequestTimeoutMs"), in.Spec.LocalRequestTimeoutMs, "LocalRequestTimeoutMs must be > 0"))
	}

	if in.Spec.BalanceInboundConnections != "" && in.Spec.BalanceInboundConnections != "exact_balance" {
		allErrs = append(allErrs, field.Invalid(path.Child("balanceInboundConnections"), in.Spec.BalanceInboundConnections, "BalanceInboundConnections must be an empty string or exact_balance"))
	}

	allErrs = append(allErrs, in.Spec.UpstreamConfig.validate(path.Child("upstreamConfig"), consulMeta.PartitionsEnabled)...)
	allErrs = append(allErrs, in.Spec.Expose.validate(path.Child("expose"))...)
	allErrs = append(allErrs, in.Spec.RateLimits.validate(path.Child("rateLimits"))...)
	allErrs = append(allErrs, in.Spec.EnvoyExtensions.validate(path.Child("envoyExtensions"))...)

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
		if in.Namespace != "" {
			errs = append(errs, field.Invalid(path.Child("namespace"), in.Namespace, "upstream.namespace for a default upstream must be \"\""))
		}
		if in.Partition != "" {
			errs = append(errs, field.Invalid(path.Child("partition"), in.Partition, "upstream.partition for a default upstream must be \"\""))
		}
		if in.Peer != "" {
			errs = append(errs, field.Invalid(path.Child("peer"), in.Peer, "upstream.peer for a default upstream must be \"\""))
		}
	} else if kind == overrideUpstream {
		if in.Name == "" {
			errs = append(errs, field.Invalid(path.Child("name"), in.Name, "upstream.name for an override upstream cannot be \"\""))
		}
		if in.Namespace != "" && in.Peer != "" {
			errs = append(errs, field.Invalid(path, in, "both namespace and peer cannot be specified."))
		}
		if in.Partition != "" && in.Peer != "" {
			errs = append(errs, field.Invalid(path, in, "both partition and peer cannot be specified."))
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
		Peer:               in.Peer,
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
	var baseEjectiontime *time.Duration
	if in.BaseEjectionTime == nil {
		dur := time.Second * 30
		baseEjectiontime = &dur
	} else {
		baseEjectiontime = &in.BaseEjectionTime.Duration
	}

	return &capi.PassiveHealthCheck{
		Interval:                in.Interval.Duration,
		MaxFailures:             in.MaxFailures,
		EnforcingConsecutive5xx: in.EnforcingConsecutive5xx,
		MaxEjectionPercent:      in.MaxEjectionPercent,
		BaseEjectionTime:        baseEjectiontime,
	}
}

func (in *ServiceDefaultsDestination) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}

	var errs field.ErrorList

	if len(in.Addresses) == 0 {
		errs = append(errs, field.Required(path.Child("addresses"), "at least one address must be define per destination"))
	}

	seen := make(map[string]bool, len(in.Addresses))
	for idx, address := range in.Addresses {
		if _, ok := seen[address]; ok {
			errs = append(errs, field.Duplicate(path.Child("addresses").Index(idx), address))
			continue
		}
		seen[address] = true

		if !validEndpointAddress(address) {
			errs = append(errs, field.Invalid(path.Child("addresses").Index(idx), address, fmt.Sprintf("address %s is not a valid IP or hostname", address)))
		}
	}

	if in.Port < 1 || in.Port > 65535 {
		errs = append(errs, field.Invalid(path.Child("port"), in.Port, "invalid port number"))
	}

	return errs
}

func validEndpointAddress(address string) bool {
	var valid bool

	if address == "" {
		return false
	}

	ip := net.ParseIP(address)
	valid = ip != nil

	hasWildcard := strings.Contains(address, "*")
	_, ok := dns.IsDomainName(address)
	valid = valid || (ok && !hasWildcard)

	return valid
}

func (in *ServiceDefaultsDestination) toConsul() *capi.DestinationConfig {
	if in == nil {
		return nil
	}
	return &capi.DestinationConfig{
		Addresses: in.Addresses,
		Port:      int(in.Port),
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

	specialEquality := cmp.Options{
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "UpstreamConfig.Overrides.Namespace"
		}, cmp.Transformer("NormalizeNamespace", normalizeEmptyToDefault)),
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "UpstreamConfig.Overrides.Partition"
		}, cmp.Transformer("NormalizePartition", normalizeEmptyToDefault)),
		cmp.Comparer(transparentProxyConfigComparer),
	}

	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(), specialEquality)
}

func (in *ServiceDefaults) ConsulGlobalResource() bool {
	return false
}
