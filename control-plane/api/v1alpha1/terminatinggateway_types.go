// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"

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

	//DisableAutoHostRewrite disables terminating gateways auto host rewrite feature when set to true.
	DisableAutoHostRewrite bool `json:"disableAutoHostRewrite,omitempty"`
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
	return in.Namespace
}

func (in *TerminatingGateway) KubeKind() string {
	return terminatingGatewayKubeKind
}

func (in *TerminatingGateway) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *TerminatingGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *TerminatingGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.TerminatingGatewayConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *TerminatingGateway) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	for i, v := range in.Spec.Services {
		errs = append(errs, v.validate(path.Child("services").Index(i))...)
	}

	errs = append(errs, in.validateNamespaces(consulMeta.NamespacesEnabled)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: terminatingGatewayKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
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
