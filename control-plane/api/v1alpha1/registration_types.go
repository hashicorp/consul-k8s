// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"maps"
	"slices"
	"time"

	capi "github.com/hashicorp/consul/api"

	corev1 "k8s.io/api/core/v1"
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
	Address           string                    `json:"address"`
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
	Node        string                `json:"node"`
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
	TimeoutDuration                        string              `json:"timeoutDuration"`
	DeregisterCriticalServiceAfterDuration string              `json:"deregisterCriticalServiceAfterDuration"`
}

// +kubebuilder:object:root=true

// RegistrationList is a list of Config resources.
type RegistrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	// Items is the list of Configs.
	Items []Registration `json:"items"`
}

// ToCatalogRegistration converts a Registration to a Consul CatalogRegistration.
func (r *Registration) ToCatalogRegistration() *capi.CatalogRegistration {
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
		Check:          copyHealthCheck(r.Spec.HealthCheck),
		SkipNodeUpdate: r.Spec.SkipNodeUpdate,
		Partition:      r.Spec.Partition,
		Locality:       copyLocality(r.Spec.Locality),
	}
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

func copyHealthCheck(healthCheck *HealthCheck) *capi.AgentCheck {
	if healthCheck == nil {
		return nil
	}

	// TODO: handle error
	intervalDuration, _ := time.ParseDuration(healthCheck.Definition.IntervalDuration)
	timeoutDuration, _ := time.ParseDuration(healthCheck.Definition.TimeoutDuration)
	deregisterAfter, _ := time.ParseDuration(healthCheck.Definition.DeregisterCriticalServiceAfterDuration)

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
	}
}

// ToCatalogDeregistration converts a Registration to a Consul CatalogDergistration.
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

// SetSyncedCondition sets the synced condition on the Registration.
func (r *Registration) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
	r.Status.Conditions = Conditions{
		{
			Type:               ConditionSynced,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		},
	}
}
