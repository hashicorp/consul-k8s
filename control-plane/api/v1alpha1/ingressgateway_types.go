// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	ingressGatewayKubeKind = "ingressgateway"
	wildcardServiceName    = "*"
)

func init() {
	SchemeBuilder.Register(&IngressGateway{}, &IngressGatewayList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// IngressGateway is the Schema for the ingressgateways API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="ingress-gateway"
type IngressGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngressGatewaySpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IngressGatewayList contains a list of IngressGateway.
type IngressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngressGateway `json:"items"`
}

// IngressGatewaySpec defines the desired state of IngressGateway.
type IngressGatewaySpec struct {
	// TLS holds the TLS configuration for this gateway.
	TLS GatewayTLSConfig `json:"tls,omitempty"`
	// Listeners declares what ports the ingress gateway should listen on, and
	// what services to associated to those ports.
	Listeners []IngressListener `json:"listeners,omitempty"`

	// Defaults is default configuration for all upstream services
	Defaults *IngressServiceConfig `json:"defaults,omitempty"`
}

type IngressServiceConfig struct {
	// The maximum number of connections a service instance
	// will be allowed to establish against the given upstream. Use this to limit
	// HTTP/1.1 traffic, since HTTP/1.1 has a request per connection.
	MaxConnections *uint32 `json:"maxConnections,omitempty"`
	// The maximum number of requests that will be queued
	// while waiting for a connection to be established.
	MaxPendingRequests *uint32 `json:"maxPendingRequests,omitempty"`
	// The maximum number of concurrent requests that
	// will be allowed at a single point in time. Use this to limit HTTP/2 traffic,
	// since HTTP/2 has many requests per connection.
	MaxConcurrentRequests *uint32 `json:"maxConcurrentRequests,omitempty"`
	// PassiveHealthCheck configuration determines how upstream proxy instances will
	// be monitored for removal from the load balancing pool.
	PassiveHealthCheck *PassiveHealthCheck `json:"passiveHealthCheck,omitempty"`
}

type GatewayTLSConfig struct {
	// Indicates that TLS should be enabled for this gateway service.
	Enabled bool `json:"enabled"`
	// SDS allows configuring TLS certificate from an SDS service.
	SDS *GatewayTLSSDSConfig `json:"sds,omitempty"`
	// TLSMinVersion sets the default minimum TLS version supported.
	// One of `TLS_AUTO`, `TLSv1_0`, `TLSv1_1`, `TLSv1_2`, or `TLSv1_3`.
	// If unspecified, Envoy v1.22.0 and newer will default to TLS 1.2 as a min version,
	// while older releases of Envoy default to TLS 1.0.
	TLSMinVersion string `json:"tlsMinVersion,omitempty"`
	// TLSMaxVersion sets the default maximum TLS version supported. Must be greater than or equal to `TLSMinVersion`.
	// One of `TLS_AUTO`, `TLSv1_0`, `TLSv1_1`, `TLSv1_2`, or `TLSv1_3`.
	// If unspecified, Envoy will default to TLS 1.3 as a max version for incoming connections.
	TLSMaxVersion string `json:"tlsMaxVersion,omitempty"`
	// Define a subset of cipher suites to restrict
	// Only applicable to connections negotiated via TLS 1.2 or earlier.
	CipherSuites []string `json:"cipherSuites,omitempty"`
}

type GatewayServiceTLSConfig struct {
	// SDS allows configuring TLS certificate from an SDS service.
	SDS *GatewayTLSSDSConfig `json:"sds,omitempty"`
}

type GatewayTLSSDSConfig struct {
	// ClusterName is the SDS cluster name to connect to, to retrieve certificates.
	// This cluster must be specified in the Gateway's bootstrap configuration.
	ClusterName string `json:"clusterName,omitempty"`
	// CertResource is the SDS resource name to request when fetching the certificate from the SDS service.
	CertResource string `json:"certResource,omitempty"`
}

// IngressListener manages the configuration for a listener on a specific port.
type IngressListener struct {
	// Port declares the port on which the ingress gateway should listen for traffic.
	Port int `json:"port,omitempty"`
	// Protocol declares what type of traffic this listener is expected to
	// receive. Depending on the protocol, a listener might support multiplexing
	// services over a single port, or additional discovery chain features. The
	// current supported values are: (tcp | http | http2 | grpc).
	Protocol string `json:"protocol,omitempty"`
	// TLS config for this listener.
	TLS *GatewayTLSConfig `json:"tls,omitempty"`
	// Services declares the set of services to which the listener forwards
	// traffic.
	// For "tcp" protocol listeners, only a single service is allowed.
	// For "http" listeners, multiple services can be declared.
	Services []IngressService `json:"services,omitempty"`
}

// IngressService manages configuration for services that are exposed to
// ingress traffic.
type IngressService struct {
	// Name declares the service to which traffic should be forwarded.
	//
	// This can either be a specific service, or the wildcard specifier,
	// "*". If the wildcard specifier is provided, the listener must be of "http"
	// protocol and means that the listener will forward traffic to all services.
	//
	// A name can be specified on multiple listeners, and will be exposed on both
	// of the listeners.
	Name string `json:"name,omitempty"`
	// Hosts is a list of hostnames which should be associated to this service on
	// the defined listener. Only allowed on layer 7 protocols, this will be used
	// to route traffic to the service by matching the Host header of the HTTP
	// request.
	//
	// If a host is provided for a service that also has a wildcard specifier
	// defined, the host will override the wildcard-specifier-provided
	// "<service-name>.*" domain for that listener.
	//
	// This cannot be specified when using the wildcard specifier, "*", or when
	// using a "tcp" listener.
	Hosts []string `json:"hosts,omitempty"`
	// Namespace is the namespace where the service is located.
	// Namespacing is a Consul Enterprise feature.
	Namespace string `json:"namespace,omitempty"`
	// Partition is the admin-partition where the service is located.
	// Partitioning is a Consul Enterprise feature.
	Partition string `json:"partition,omitempty"`
	// TLS allows specifying some TLS configuration per listener.
	TLS *GatewayServiceTLSConfig `json:"tls,omitempty"`
	// Allow HTTP header manipulation to be configured.
	RequestHeaders  *HTTPHeaderModifiers `json:"requestHeaders,omitempty"`
	ResponseHeaders *HTTPHeaderModifiers `json:"responseHeaders,omitempty"`

	IngressServiceConfig `json:",inline"`
}

func (in *IngressGateway) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *IngressGateway) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *IngressGateway) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *IngressGateway) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *IngressGateway) ConsulKind() string {
	return capi.IngressGateway
}

func (in *IngressGateway) ConsulGlobalResource() bool {
	return false
}

func (in *IngressGateway) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *IngressGateway) KubeKind() string {
	return ingressGatewayKubeKind
}

func (in *IngressGateway) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *IngressGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *IngressGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *IngressGateway) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *IngressGateway) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *IngressGateway) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *IngressGateway) ToConsul(datacenter string) capi.ConfigEntry {
	var listeners []capi.IngressListener
	for _, l := range in.Spec.Listeners {
		listeners = append(listeners, l.toConsul())
	}
	return &capi.IngressGatewayConfigEntry{
		Kind:      in.ConsulKind(),
		Name:      in.ConsulName(),
		TLS:       *in.Spec.TLS.toConsul(),
		Listeners: listeners,
		Meta:      meta(datacenter),
		Defaults:  in.Spec.Defaults.toConsul(),
	}
}

func (in *IngressGateway) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.IngressGatewayConfigEntry)
	if !ok {
		return false
	}

	specialEquality := cmp.Options{
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "Listeners.Services.Namespace"
		}, cmp.Transformer("NormalizeNamespace", normalizeEmptyToDefault)),
		cmp.FilterPath(func(path cmp.Path) bool {
			return path.String() == "Listeners.Services.Partition"
		}, cmp.Transformer("NormalizePartition", normalizeEmptyToDefault)),
	}

	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.IngressGatewayConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty(), specialEquality)
}

func (in *IngressGateway) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	errs = append(errs, in.Spec.TLS.validate(path.Child("tls"))...)

	for i, v := range in.Spec.Listeners {
		errs = append(errs, v.validate(path.Child("listeners").Index(i), consulMeta)...)
	}

	errs = append(errs, in.Spec.Defaults.validate(path.Child("defaults"))...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ingressGatewayKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields sets the namespace field on spec.listeners[].services to their default values if namespaces are enabled.
func (in *IngressGateway) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
	// If namespaces are enabled we want to set the namespace fields to their
	// defaults. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulMeta.NamespacesEnabled {
		// Default to the current namespace (i.e. the namespace of the config entry).
		namespace := namespaces.ConsulNamespace(in.Namespace, consulMeta.NamespacesEnabled, consulMeta.DestinationNamespace, consulMeta.Mirroring, consulMeta.Prefix)
		for i, listener := range in.Spec.Listeners {
			for j, service := range listener.Services {
				if service.Namespace == "" {
					in.Spec.Listeners[i].Services[j].Namespace = namespace
				}
			}
		}
	}
}

func (in *GatewayTLSConfig) toConsul() *capi.GatewayTLSConfig {
	if in == nil {
		return nil
	}
	return &capi.GatewayTLSConfig{
		Enabled:       in.Enabled,
		SDS:           in.SDS.toConsul(),
		TLSMaxVersion: in.TLSMaxVersion,
		TLSMinVersion: in.TLSMinVersion,
		CipherSuites:  in.CipherSuites,
	}
}

func (in *GatewayTLSConfig) validate(path *field.Path) field.ErrorList {
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

func (in IngressListener) toConsul() capi.IngressListener {
	var services []capi.IngressService
	for _, s := range in.Services {
		services = append(services, s.toConsul())
	}

	return capi.IngressListener{
		Port:     in.Port,
		Protocol: in.Protocol,
		TLS:      in.TLS.toConsul(),
		Services: services,
	}
}

func (in IngressService) toConsul() capi.IngressService {
	return capi.IngressService{
		Name:                  in.Name,
		Hosts:                 in.Hosts,
		Namespace:             in.Namespace,
		Partition:             in.Partition,
		TLS:                   in.TLS.toConsul(),
		RequestHeaders:        in.RequestHeaders.toConsul(),
		ResponseHeaders:       in.ResponseHeaders.toConsul(),
		MaxConnections:        in.MaxConnections,
		MaxPendingRequests:    in.MaxPendingRequests,
		MaxConcurrentRequests: in.MaxConcurrentRequests,
		PassiveHealthCheck:    in.PassiveHealthCheck.toConsul(),
	}
}

func (in *GatewayTLSSDSConfig) toConsul() *capi.GatewayTLSSDSConfig {
	if in == nil {
		return nil
	}
	return &capi.GatewayTLSSDSConfig{
		ClusterName:  in.ClusterName,
		CertResource: in.CertResource,
	}
}

func (in *GatewayServiceTLSConfig) toConsul() *capi.GatewayServiceTLSConfig {
	if in == nil {
		return nil
	}
	return &capi.GatewayServiceTLSConfig{
		SDS: in.SDS.toConsul(),
	}
}

func (in IngressListener) validate(path *field.Path, consulMeta common.ConsulMeta) field.ErrorList {
	var errs field.ErrorList
	validProtocols := []string{"tcp", "http", "http2", "grpc"}
	if !sliceContains(validProtocols, in.Protocol) {
		errs = append(errs, field.Invalid(path.Child("protocol"),
			in.Protocol,
			notInSliceMessage(validProtocols)))
	}

	if in.Protocol == "tcp" && len(in.Services) > 1 {
		asJSON, _ := json.Marshal(in.Services)
		errs = append(errs, field.Invalid(path.Child("services"),
			string(asJSON),
			fmt.Sprintf("if protocol is \"tcp\", only a single service is allowed, found %d", len(in.Services))))
	}

	errs = append(errs, in.TLS.validate(path.Child("tls"))...)

	for i, svc := range in.Services {
		if svc.Name == wildcardServiceName && in.Protocol != "http" {
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("name"),
				svc.Name,
				fmt.Sprintf("if name is %q, protocol must be \"http\" but was %q", wildcardServiceName, in.Protocol)))
		}

		if svc.Name == wildcardServiceName && len(svc.Hosts) > 0 {
			asJSON, _ := json.Marshal(svc.Hosts)
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("hosts"),
				string(asJSON),
				fmt.Sprintf("hosts must be empty if name is %q", wildcardServiceName)))
		}

		if svc.Partition != "" && !consulMeta.PartitionsEnabled {
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("partition"),
				svc.Partition, `Consul Enterprise admin-partitions must be enabled to set service.partition`))
		}

		if svc.Namespace != "" && !consulMeta.NamespacesEnabled {
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("namespace"),
				svc.Namespace, `Consul Enterprise namespaces must be enabled to set service.namespace`))
		}

		if len(svc.Hosts) > 0 && in.Protocol == "tcp" {
			asJSON, _ := json.Marshal(svc.Hosts)
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("hosts"),
				string(asJSON),
				"hosts must be empty if protocol is \"tcp\""))
		}

		errs = append(errs, svc.IngressServiceConfig.validate(path)...)
	}
	return errs
}

func (in *IngressServiceConfig) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}
	var errs field.ErrorList

	if in.MaxConnections != nil && *in.MaxConnections <= 0 {
		errs = append(errs, field.Invalid(path.Child("maxconnections"), *in.MaxConnections, "MaxConnections must be > 0"))
	}

	if in.MaxConcurrentRequests != nil && *in.MaxConcurrentRequests <= 0 {
		errs = append(errs, field.Invalid(path.Child("maxconcurrentrequests"), *in.MaxConcurrentRequests, "MaxConcurrentRequests must be > 0"))
	}

	if in.MaxPendingRequests != nil && *in.MaxPendingRequests <= 0 {
		errs = append(errs, field.Invalid(path.Child("maxpendingrequests"), *in.MaxPendingRequests, "MaxPendingRequests must be > 0"))
	}
	return errs
}

func (in *IngressServiceConfig) toConsul() *capi.IngressServiceConfig {
	if in == nil {
		return nil
	}
	return &capi.IngressServiceConfig{
		MaxConnections:        in.MaxConnections,
		MaxPendingRequests:    in.MaxPendingRequests,
		MaxConcurrentRequests: in.MaxConcurrentRequests,
		PassiveHealthCheck:    in.PassiveHealthCheck.toConsul(),
	}
}
