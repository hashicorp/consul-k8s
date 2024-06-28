// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"errors"
	"maps"
	"slices"
	"time"

	capi "github.com/hashicorp/consul/api"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&Registration{}, &RegistrationList{})
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status

// Registration defines the resource for working with service registrations.
type Registration struct {
	// Standard Kubernetes resource metadata.
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the desired state of Registration.
	Spec RegistrationSpec `json:"spec,omitempty"`

	Status RegistrationStatus `json:"status,omitempty"`
}

// RegistrationStatus defines the observed state of Registration.
type RegistrationStatus struct {
	// Conditions indicate the latest available observations of a resource's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions Conditions `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	// LastSyncedTime is the last time the resource successfully synced with Consul.
	// +optional
	LastSyncedTime *metav1.Time `json:"lastSyncedTime,omitempty" description:"last time the condition transitioned from one status to another"`
}

// +k8s:deepcopy-gen=true

// RegistrationSpec specifies the desired state of the Config CRD.
type RegistrationSpec struct {
	ID              string            `json:"id,omitempty"`
	Node            string            `json:"node,omitempty"`
	Address         string            `json:"address,omitempty"`
	TaggedAddresses map[string]string `json:"taggedAddresses,omitempty"`
	NodeMeta        map[string]string `json:"nodeMeta,omitempty"`
	Datacenter      string            `json:"datacenter,omitempty"`
	Service         Service           `json:"service,omitempty"`
	SkipNodeUpdate  bool              `json:"skipNodeUpdate,omitempty"`
	Partition       string            `json:"partition,omitempty"`
	HealthCheck     *HealthCheck      `json:"check,omitempty"`
	Locality        *Locality         `json:"locality,omitempty"`
}

// +k8s:deepcopy-gen=true

type Service struct {
	ID                string                    `json:"id,omitempty"`
	Name              string                    `json:"name"`
	Tags              []string                  `json:"tags,omitempty"`
	Meta              map[string]string         `json:"meta,omitempty"`
	Port              int                       `json:"port"`
	Address           string                    `json:"address,omitempty"`
	SocketPath        string                    `json:"socketPath,omitempty"`
	TaggedAddresses   map[string]ServiceAddress `json:"taggedAddresses,omitempty"`
	Weights           Weights                   `json:"weights,omitempty"`
	EnableTagOverride bool                      `json:"enableTagOverride,omitempty"`
	Locality          *Locality                 `json:"locality,omitempty"`
	Namespace         string                    `json:"namespace,omitempty"`
	Partition         string                    `json:"partition,omitempty"`
}

// +k8s:deepcopy-gen=true

type ServiceAddress struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// +k8s:deepcopy-gen=true

type Weights struct {
	Passing int `json:"passing"`
	Warning int `json:"warning"`
}

// +k8s:deepcopy-gen=true

type Locality struct {
	Region string `json:"region,omitempty"`
	Zone   string `json:"zone,omitempty"`
}

// +k8s:deepcopy-gen=true

// HealthCheck is used to represent a single check.
type HealthCheck struct {
	Node        string                `json:"node,omitempty"`
	CheckID     string                `json:"checkId"`
	Name        string                `json:"name"`
	Status      string                `json:"status"`
	Notes       string                `json:"notes,omitempty"`
	Output      string                `json:"output,omitempty"`
	ServiceID   string                `json:"serviceId"`
	ServiceName string                `json:"serviceName"`
	Type        string                `json:"type,omitempty"`
	ExposedPort int                   `json:"exposedPort,omitempty"`
	Definition  HealthCheckDefinition `json:"definition"`
	Namespace   string                `json:"namespace,omitempty"`
	Partition   string                `json:"partition,omitempty"`
}

// HealthCheckDefinition is used to store the details about
// a health check's execution.
type HealthCheckDefinition struct {
	HTTP                                   string              `json:"http,omitempty"`
	Header                                 map[string][]string `json:"header,omitempty"`
	Method                                 string              `json:"method,omitempty"`
	Body                                   string              `json:"body,omitempty"`
	TLSServerName                          string              `json:"tlsServerName,omitempty"`
	TLSSkipVerify                          bool                `json:"tlsSkipVerify,omitempty"`
	TCP                                    string              `json:"tcp,omitempty"`
	TCPUseTLS                              bool                `json:"tcpUseTLS,omitempty"`
	UDP                                    string              `json:"udp,omitempty"`
	GRPC                                   string              `json:"grpc,omitempty"`
	OSService                              string              `json:"osService,omitempty"`
	GRPCUseTLS                             bool                `json:"grpcUseTLS,omitempty"`
	IntervalDuration                       string              `json:"intervalDuration"`
	TimeoutDuration                        string              `json:"timeoutDuration,omitempty"`
	DeregisterCriticalServiceAfterDuration string              `json:"deregisterCriticalServiceAfterDuration,omitempty"`
}

// +kubebuilder:object:root=true

// RegistrationList is a list of Registration resources.
type RegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of Registrations.
	Items []Registration `json:"items"`
}

// ToCatalogRegistration converts a Registration to a Consul CatalogRegistration.
func (r *Registration) ToCatalogRegistration() (*capi.CatalogRegistration, error) {
	check, err := copyHealthCheck(r.Spec.HealthCheck)
	if err != nil {
		return nil, err
	}

	return &capi.CatalogRegistration{
		ID:              r.Spec.ID,
		Node:            r.Spec.Node,
		Address:         r.Spec.Address,
		TaggedAddresses: maps.Clone(r.Spec.TaggedAddresses),
		NodeMeta:        maps.Clone(r.Spec.NodeMeta),
		Datacenter:      r.Spec.Datacenter,
		Service: &capi.AgentService{
			ID:                r.Spec.Service.ID,
			Service:           r.Spec.Service.Name,
			Tags:              slices.Clone(r.Spec.Service.Tags),
			Meta:              maps.Clone(r.Spec.Service.Meta),
			Port:              r.Spec.Service.Port,
			Address:           r.Spec.Service.Address,
			SocketPath:        r.Spec.Service.SocketPath,
			TaggedAddresses:   copyTaggedAddresses(r.Spec.Service.TaggedAddresses),
			Weights:           capi.AgentWeights(r.Spec.Service.Weights),
			EnableTagOverride: r.Spec.Service.EnableTagOverride,
			Namespace:         r.Spec.Service.Namespace,
			Partition:         r.Spec.Service.Partition,
			Locality:          copyLocality(r.Spec.Service.Locality),
		},
		Check:          check,
		SkipNodeUpdate: r.Spec.SkipNodeUpdate,
		Partition:      r.Spec.Partition,
		Locality:       copyLocality(r.Spec.Locality),
	}, nil
}

func copyTaggedAddresses(taggedAddresses map[string]ServiceAddress) map[string]capi.ServiceAddress {
	if taggedAddresses == nil {
		return nil
	}
	result := make(map[string]capi.ServiceAddress, len(taggedAddresses))
	for k, v := range taggedAddresses {
		result[k] = capi.ServiceAddress(v)
	}
	return result
}

func copyLocality(locality *Locality) *capi.Locality {
	if locality == nil {
		return nil
	}
	return &capi.Locality{
		Region: locality.Region,
		Zone:   locality.Zone,
	}
}

var (
	ErrInvalidInterval       = errors.New("invalid value for IntervalDuration")
	ErrInvalidTimeout        = errors.New("invalid value for TimeoutDuration")
	ErrInvalidDergisterAfter = errors.New("invalid value for DeregisterCriticalServiceAfterDuration")
)

func copyHealthCheck(healthCheck *HealthCheck) (*capi.AgentCheck, error) {
	if healthCheck == nil {
		return nil, nil
	}

	// TODO: handle error
	intervalDuration, err := time.ParseDuration(healthCheck.Definition.IntervalDuration)
	if err != nil {
		return nil, ErrInvalidInterval
	}

	timeoutDuration, err := time.ParseDuration(healthCheck.Definition.TimeoutDuration)
	if err != nil {
		return nil, ErrInvalidTimeout
	}

	deregisterAfter, err := time.ParseDuration(healthCheck.Definition.DeregisterCriticalServiceAfterDuration)
	if err != nil {
		return nil, ErrInvalidDergisterAfter
	}

	return &capi.AgentCheck{
		Node:        healthCheck.Node,
		Notes:       healthCheck.Notes,
		ServiceName: healthCheck.ServiceName,
		CheckID:     healthCheck.CheckID,
		Name:        healthCheck.Name,
		Type:        healthCheck.Type,
		Status:      healthCheck.Status,
		ServiceID:   healthCheck.ServiceID,
		ExposedPort: healthCheck.ExposedPort,
		Output:      healthCheck.Output,
		Namespace:   healthCheck.Namespace,
		Partition:   healthCheck.Partition,
		Definition: capi.HealthCheckDefinition{
			HTTP:                                   healthCheck.Definition.HTTP,
			TCP:                                    healthCheck.Definition.TCP,
			GRPC:                                   healthCheck.Definition.GRPC,
			GRPCUseTLS:                             healthCheck.Definition.GRPCUseTLS,
			Method:                                 healthCheck.Definition.Method,
			Header:                                 healthCheck.Definition.Header,
			Body:                                   healthCheck.Definition.Body,
			TLSServerName:                          healthCheck.Definition.TLSServerName,
			TLSSkipVerify:                          healthCheck.Definition.TLSSkipVerify,
			OSService:                              healthCheck.Definition.OSService,
			IntervalDuration:                       intervalDuration,
			TimeoutDuration:                        timeoutDuration,
			DeregisterCriticalServiceAfterDuration: deregisterAfter,
		},
	}, nil
}

// ToCatalogDeregistration converts a Registration to a Consul CatalogDeregistration.
func (r *Registration) ToCatalogDeregistration() *capi.CatalogDeregistration {
	checkID := ""
	if r.Spec.HealthCheck != nil {
		checkID = r.Spec.HealthCheck.CheckID
	}

	return &capi.CatalogDeregistration{
		Node:       r.Spec.Node,
		Address:    r.Spec.Address,
		Datacenter: r.Spec.Datacenter,
		ServiceID:  r.Spec.Service.ID,
		CheckID:    checkID,
		Namespace:  r.Spec.Service.Namespace,
		Partition:  r.Spec.Service.Partition,
	}
}

func (r *Registration) EqualExceptStatus(other *Registration) bool {
	if r == nil || other == nil {
		return false
	}

	if r.Spec.ID != other.Spec.ID {
		return false
	}

	if r.Spec.Node != other.Spec.Node {
		return false
	}

	if r.Spec.Address != other.Spec.Address {
		return false
	}

	if !maps.Equal(r.Spec.TaggedAddresses, other.Spec.TaggedAddresses) {
		return false
	}

	if !maps.Equal(r.Spec.NodeMeta, other.Spec.NodeMeta) {
		return false
	}

	if r.Spec.Datacenter != other.Spec.Datacenter {
		return false
	}

	if !r.Spec.Service.Equal(&other.Spec.Service) {
		return false
	}

	if r.Spec.SkipNodeUpdate != other.Spec.SkipNodeUpdate {
		return false
	}

	if r.Spec.Partition != other.Spec.Partition {
		return false
	}

	if !r.Spec.HealthCheck.Equal(other.Spec.HealthCheck) {
		return false
	}

	if !r.Spec.Locality.Equal(other.Spec.Locality) {
		return false
	}

	return true
}

func (s *Service) Equal(other *Service) bool {
	if s == nil && other == nil {
		return true
	}

	if s == nil || other == nil {
		return false
	}

	if s.ID != other.ID {
		return false
	}

	if s.Name != other.Name {
		return false
	}

	if !slices.Equal(s.Tags, other.Tags) {
		return false
	}

	if !maps.Equal(s.Meta, other.Meta) {
		return false
	}

	if s.Port != other.Port {
		return false
	}

	if s.Address != other.Address {
		return false
	}

	if s.SocketPath != other.SocketPath {
		return false
	}

	if !maps.Equal(s.TaggedAddresses, other.TaggedAddresses) {
		return false
	}

	if !s.Weights.Equal(other.Weights) {
		return false
	}

	if s.EnableTagOverride != other.EnableTagOverride {
		return false
	}

	if s.Namespace != other.Namespace {
		return false
	}

	if s.Partition != other.Partition {
		return false
	}

	if !s.Locality.Equal(other.Locality) {
		return false
	}
	return true
}

func (l *Locality) Equal(other *Locality) bool {
	if l == nil && other == nil {
		return true
	}

	if l == nil || other == nil {
		return false
	}
	if l.Region != other.Region {
		return false
	}
	if l.Zone != other.Zone {
		return false
	}
	return true
}

func (w Weights) Equal(other Weights) bool {
	if w.Passing != other.Passing {
		return false
	}

	if w.Warning != other.Warning {
		return false
	}
	return true
}

func (h *HealthCheck) Equal(other *HealthCheck) bool {
	if h == nil && other == nil {
		return true
	}

	if h == nil || other == nil {
		return false
	}

	if h.Node != other.Node {
		return false
	}

	if h.CheckID != other.CheckID {
		return false
	}

	if h.Name != other.Name {
		return false
	}

	if h.Status != other.Status {
		return false
	}

	if h.Notes != other.Notes {
		return false
	}

	if h.Output != other.Output {
		return false
	}

	if h.ServiceID != other.ServiceID {
		return false
	}

	if h.ServiceName != other.ServiceName {
		return false
	}

	if h.Type != other.Type {
		return false
	}

	if h.ExposedPort != other.ExposedPort {
		return false
	}

	if h.Namespace != other.Namespace {
		return false
	}

	if h.Partition != other.Partition {
		return false
	}

	if !h.Definition.Equal(other.Definition) {
		return false
	}

	return true
}

func (h HealthCheckDefinition) Equal(other HealthCheckDefinition) bool {
	if h.HTTP != other.HTTP {
		return false
	}

	if len(h.Header) != len(other.Header) {
		return false
	}

	for k, v := range h.Header {
		otherValues, ok := other.Header[k]
		if !ok {
			return false
		}

		if !slices.Equal(v, otherValues) {
			return false
		}
	}

	if h.Method != other.Method {
		return false
	}

	if h.Body != other.Body {
		return false
	}

	if h.TLSServerName != other.TLSServerName {
		return false
	}

	if h.TLSSkipVerify != other.TLSSkipVerify {
		return false
	}

	if h.TCP != other.TCP {
		return false
	}

	if h.TCPUseTLS != other.TCPUseTLS {
		return false
	}

	if h.UDP != other.UDP {
		return false
	}

	if h.GRPC != other.GRPC {
		return false
	}

	if h.OSService != other.OSService {
		return false
	}

	if h.GRPCUseTLS != other.GRPCUseTLS {
		return false
	}

	if h.IntervalDuration != other.IntervalDuration {
		return false
	}

	if h.TimeoutDuration != other.TimeoutDuration {
		return false
	}

	if h.DeregisterCriticalServiceAfterDuration != other.DeregisterCriticalServiceAfterDuration {
		return false
	}

	return true
}

func (r *Registration) KubernetesName() string {
	return r.ObjectMeta.Name
}
