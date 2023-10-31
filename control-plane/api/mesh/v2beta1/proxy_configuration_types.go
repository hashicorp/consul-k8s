// // Copyright (c) HashiCorp, Inc.
// // SPDX-License-Identifier: MPL-2.0
package v2beta1

import (
	"fmt"
	"math"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
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
	proxyConfigurationKubeKind = "proxyconfiguration"
)

func init() {
	MeshSchemeBuilder.Register(&ProxyConfiguration{}, &ProxyConfigurationList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ProxyConfiguration is the Schema for the TCP Routes API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="proxy-configuration"
type ProxyConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   pbmesh.ProxyConfiguration `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ProxyConfigurationList contains a list of ProxyConfiguration.
type ProxyConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []*ProxyConfiguration `json:"items"`
}

func (in *ProxyConfiguration) ResourceID(namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: in.Name,
		Type: pbmesh.ProxyConfigurationType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
	}
}

func (in *ProxyConfiguration) Resource(namespace, partition string) *pbresource.Resource {
	return &pbresource.Resource{
		Id:       in.ResourceID(namespace, partition),
		Data:     inject.ToProtoAny(&in.Spec),
		Metadata: meshConfigMeta(),
	}
}

func (in *ProxyConfiguration) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ProxyConfiguration) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ProxyConfiguration) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ProxyConfiguration) MatchesConsul(candidate *pbresource.Resource, namespace, partition string) bool {
	return cmp.Equal(
		in.Resource(namespace, partition),
		candidate,
		protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
		protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
		protocmp.Transform(),
		cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprintf("%v", a) < fmt.Sprintf("%v", b) }),
	)
}

func (in *ProxyConfiguration) KubeKind() string {
	return proxyConfigurationKubeKind
}

func (in *ProxyConfiguration) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ProxyConfiguration) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ProxyConfiguration) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ProxyConfiguration) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ProxyConfiguration) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *ProxyConfiguration) Validate(tenancy common.ConsulTenancyConfig) error {
	var errs field.ErrorList
	var config pbmesh.ProxyConfiguration
	path := field.NewPath("spec")

	res := in.Resource(tenancy.ConsulDestinationNamespace, tenancy.ConsulPartition)

	if err := res.Data.UnmarshalTo(&config); err != nil {
		return fmt.Errorf("error parsing resource data as type %q: %s", &config, err)
	}

	if err := validateSelector(config.Workloads, path.Child("workloads")); err != nil {
		errs = append(errs, err...)
	}

	if config.DynamicConfig == nil && config.BootstrapConfig == nil {
		errs = append(errs, field.Required(path, "at least one of \"bootstrap_config\" or \"dynamic_config\" fields must be set"))
	}

	if err := validateDynamicConfig(config.DynamicConfig, path.Child("dynamicConfig")); err != nil {
		errs = append(errs, err...)
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: MeshGroup, Kind: common.ProxyConfiguration},
			in.KubernetesName(), errs)
	}
	return nil
}

func validateDynamicConfig(cfg *pbmesh.DynamicConfig, path *field.Path) field.ErrorList {
	if cfg == nil {
		return nil
	}

	var errs field.ErrorList

	if cfg.MutualTlsMode != pbmesh.MutualTLSMode_MUTUAL_TLS_MODE_DEFAULT {
		errs = append(errs, field.Invalid(path.Child("mutualTlsMode"), cfg.MutualTlsMode, "field is currently not supported"))
	}

	if cfg.MeshGatewayMode != pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_UNSPECIFIED {
		errs = append(errs, field.Invalid(path.Child("meshGatewayMode"), cfg.MeshGatewayMode, "field is currently not supported"))
	}

	if cfg.AccessLogs != nil {
		errs = append(errs, field.Invalid(path.Child("accessLogs"), cfg.AccessLogs, "field is currently not supported"))
	}

	if cfg.PublicListenerJson != "" {
		errs = append(errs, field.Invalid(path.Child("publicListenerJson"), cfg.PublicListenerJson, "field is currently not supported"))
	}

	if cfg.ListenerTracingJson != "" {
		errs = append(errs, field.Invalid(path.Child("listenerTracingJson"), cfg.ListenerTracingJson, "field is currently not supported"))
	}

	if cfg.LocalClusterJson != "" {
		errs = append(errs, field.Invalid(path.Child("localClusterJson"), cfg.LocalClusterJson, "field is currently not supported"))
	}

	// nolint:staticcheck
	if cfg.LocalWorkloadAddress != "" {
		errs = append(errs, field.Invalid(path.Child("localWorkloadAddress"), cfg.LocalWorkloadAddress, "field is currently not supported"))
	}

	// nolint:staticcheck
	if cfg.LocalWorkloadPort != 0 {
		errs = append(errs, field.Invalid(path.Child("localWorkloadPort"), cfg.LocalWorkloadPort, "field is currently not supported"))
	}

	// nolint:staticcheck
	if cfg.LocalWorkloadSocketPath != "" {
		errs = append(errs, field.Invalid(path.Child("localWorkloadSocketPath"), cfg.LocalWorkloadSocketPath, "field is currently not supported"))
	}

	if cfg.TransparentProxy != nil {
		if cfg.TransparentProxy.DialedDirectly {
			errs = append(errs, field.Invalid(path.Child("transparentProxy").Child("dialedDirectely"), cfg.TransparentProxy.DialedDirectly, "field is currently not supported"))
		}
		if err := validatePort(cfg.TransparentProxy.OutboundListenerPort, path.Child("transparentProxy").Child("outboundListenerPort")); err != nil {
			errs = append(errs, err)
		}
	}

	if cfg.ExposeConfig != nil {
		exposePath := path.Child("exposeConfig")
		for i, path := range cfg.ExposeConfig.ExposePaths {
			if err := validatePort(path.ListenerPort, exposePath.Child("exposePaths").Index(i).Child("listenerPort")); err != nil {
				errs = append(errs, err)
			}

			if err := validatePort(path.LocalPathPort, exposePath.Child("exposePaths").Index(i).Child("localPathPort")); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

func validatePort(port uint32, path *field.Path) *field.Error {
	if port < 1 || port > math.MaxUint16 {
		return field.Invalid(path, port, "port number is outside the range 1 to 65535")
	}
	return nil
}

func validateSelector(workloads *pbcatalog.WorkloadSelector, path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if workloads == nil {
		errs = append(errs, field.Required(path, "cannot be empty"))
		return errs
	}

	if len(workloads.Names) == 0 && len(workloads.Prefixes) == 0 {
		errs = append(errs, field.Required(path, "both workloads.names and workloads.prefixes cannot be empty"))
		return errs
	}

	for i, name := range workloads.Names {
		namePath := path.Child("names")
		if name == "" {
			errs = append(errs, field.Invalid(namePath.Index(i), name, "cannot be empty"))
		}
	}
	return errs
}

// DefaultNamespaceFields is required as part of the common.MeshConfig interface.
func (in *ProxyConfiguration) DefaultNamespaceFields(tenancy common.ConsulTenancyConfig) {}
