// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
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

// Registration defines the.
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

// HealthCheck is used to represent a single check
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
