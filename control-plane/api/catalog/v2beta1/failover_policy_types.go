// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"fmt"
	"regexp"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	failoverPolicyKubeKind = "failoverpolicy"
)

var (
	dnsLabelRegex   = `^[a-z0-9]([a-z0-9\-_]*[a-z0-9])?$`
	dnsLabelMatcher = regexp.MustCompile(dnsLabelRegex)
)

func init() {
	CatalogSchemeBuilder.Register(&FailoverPolicy{}, &FailoverPolicyList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FailoverPolicy is the Schema for the failover-policy API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="failover-policy"
type FailoverPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbcatalog.FailoverPolicy `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FailoverPolicyList contains a list of FailoverPolicy.
type FailoverPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*FailoverPolicy `json:"items"`
}

func (in *FailoverPolicy) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbcatalog.FailoverPolicyType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func (in *FailoverPolicy) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *FailoverPolicy) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *FailoverPolicy) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *FailoverPolicy) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *FailoverPolicy) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *FailoverPolicy) KubeKind() string {
	return failoverPolicyKubeKind
}

func (in *FailoverPolicy) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *FailoverPolicy) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *FailoverPolicy) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *FailoverPolicy) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *FailoverPolicy) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *FailoverPolicy) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	var policy pbcatalog.FailoverPolicy
	path := field.NewPath("spec")

	if policy.Config == nil && len(policy.PortConfigs) == 0 {
		errs = append(errs, field.Required(path, "at least one of config or portConfigs must be set."))
	}

	if err := validateFailoverConfig(policy.Config, false, path.Child("config")); err != nil {
		errs = append(errs, err...)
	}

	for portName, config := range policy.PortConfigs {
		if portName == "" {
			errs = append(errs, field.Required(path.Child("portConfigs").Key(portName), "cannot be empty"))
		} else {
			if !isValidDNSLabel(portName) {
				errs = append(errs, field.Invalid(path.Child("portConfigs").Key(portName), portName, fmt.Sprintf("value must match regex: %s", dnsLabelRegex)))
			}
		}

		if err := validateFailoverConfig(config, true, path.Child("portConfigs").Key(portName)); err != nil {
			errs = append(errs, err...)
		}
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: CatalogGroup, Kind: common.FailoverPolicy},
			in.KubernetesName(), errs)
	}
	return nil
}

func validateFailoverConfig(config *pbcatalog.FailoverConfig, ported bool, path *field.Path) field.ErrorList {
	if config == nil {
		return nil
	}
	var errs field.ErrorList
	if config.SamenessGroup != "" {
		errs = append(errs, field.Invalid(path.Child("samenessGroup"), config.SamenessGroup, "this field is not supported"))
	}
	if len(config.Regions) > 0 {
		errs = append(errs, field.Invalid(path.Child("regions"), config.Regions, "this field is not supported"))
	}
	if (len(config.Destinations) > 0) == (config.SamenessGroup != "") {
		errs = append(errs, field.Invalid(path, config, "exactly one of destinations or samenessGroup should be set"))
	}
	destinationPath := path.Child("destinations")
	for i, dest := range config.Destinations {
		if err := validateFailoverPolicyDestination(dest, ported, destinationPath.Index(i)); err != nil {
			errs = append(errs, err...)
		}
	}

	if config.Mode != pbcatalog.FailoverMode_FAILOVER_MODE_UNSPECIFIED {
		errs = append(errs, field.Invalid(path.Child("mode"), config.Mode, "not supported in this release"))
	}

	return errs
}

func validateFailoverPolicyDestination(dest *pbcatalog.FailoverDestination, ported bool, path *field.Path) field.ErrorList {
	if dest == nil {
		return nil
	}
	var errs field.ErrorList

	if err := validateLocalServiceRef(dest.Ref, path.Child("ref")); err != nil {
		errs = append(errs, err...)
	}
	if dest.Port != "" {
		if ported {
			if !isValidDNSLabel(dest.Port) {
				errs = append(errs, field.Invalid(path.Child("port"), dest.Port, fmt.Sprintf("value must match regex: %s", dnsLabelRegex)))
			}
		} else {
			errs = append(errs, field.Invalid(path.Child("port"), dest.Port, "ports cannot be specified explicitly for the general failover section since it relies upon port alignment"))
		}
	}

	hasPeer := false
	if dest.Ref != nil {
		hasPeer = dest.Ref.Tenancy.PeerName != "" && dest.Ref.Tenancy.PeerName != "local"
	}

	if hasPeer && dest.Datacenter != "" {
		errs = append(errs, field.Invalid(path.Child("datacenter"), dest.Datacenter, "ref.tenancy.peerName and datacenter are mutually exclusive fields"))
	}
	return errs
}

func isValidDNSLabel(port string) bool {
	return dnsLabelMatcher.Match([]byte(port))
}

func validateLocalServiceRef(ref *pbresource.Reference, path *field.Path) field.ErrorList {
	if ref == nil {
		return field.ErrorList{field.Required(path, "missing required field")}
	}
	if ref.Type != pbcatalog.ServiceType {
		return field.ErrorList{field.Invalid(path.Child("type"), ref.Type, "only supports Service type")}
	}

	var errs field.ErrorList
	if ref.Section != "" {
		errs = append(errs, field.Invalid(path.Child("section"), ref.Section, "section cannot be set here"))
	}
	if ref.Tenancy == nil {
		errs = append(errs, field.Required(path.Child("tenancy"), "cannot be empty"))
	} else {
		tenancyPath := path.Child("tenancy")
		if ref.Tenancy.Partition == "" {
			errs = append(errs, field.Required(tenancyPath.Child("partition"), "cannot be empty"))
		}
		if ref.Tenancy.Namespace == "" {
			errs = append(errs, field.Required(tenancyPath.Child("namespace"), "cannot be empty"))
		}
		if ref.Tenancy.PeerName != "local" {
			errs = append(errs, field.Invalid(tenancyPath.Child("peerName"), ref.Tenancy.PeerName, "must be set to \"local\""))
		}
	}
	if ref.Name == "" {
		errs = append(errs, field.Required(path.Child("name"), "cannot be empty"))
	}

	return errs
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *FailoverPolicy) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
