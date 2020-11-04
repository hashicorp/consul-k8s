package v1alpha1

import (
	"encoding/json"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul-k8s/namespaces"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ServiceIntentionsSpec defines the desired state of ServiceIntentions
type ServiceIntentionsSpec struct {
	Destination Destination      `json:"destination,omitempty"`
	Sources     SourceIntentions `json:"sources,omitempty"`
}

type Destination struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type SourceIntentions []*SourceIntention
type IntentionPermissions []*IntentionPermission
type IntentionHTTPHeaderPermissions []IntentionHTTPHeaderPermission

type SourceIntention struct {
	Name        string               `json:"name,omitempty"`
	Namespace   string               `json:"namespace,omitempty"`
	Action      IntentionAction      `json:"action,omitempty"`
	Permissions IntentionPermissions `json:"permissions,omitempty"`
	Description string               `json:"description,omitempty"`
}

type IntentionPermission struct {
	Action IntentionAction          `json:"action,omitempty"`
	HTTP   *IntentionHTTPPermission `json:"http,omitempty"`
}

type IntentionHTTPPermission struct {
	PathExact  string `json:"pathExact,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty"`
	PathRegex  string `json:"pathRegex,omitempty"`

	Header IntentionHTTPHeaderPermissions `json:"header,omitempty"`

	Methods []string `json:"methods,omitempty"`
}

type IntentionHTTPHeaderPermission struct {
	Name    string `json:"name,omitempty"`
	Present bool   `json:"present,omitempty"`
	Exact   string `json:"exact,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Suffix  string `json:"suffix,omitempty"`
	Regex   string `json:"regex,omitempty"`
	Invert  bool   `json:"invert,omitempty"`
}

// IntentionAction is the action that the intention represents. This
// can be "allow" or "deny" to allowlist or denylist intentions.
type IntentionAction string

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceIntentions is the Schema for the serviceintentions API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type ServiceIntentions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceIntentionsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

func (in *ServiceIntentions) ConsulMirroringNS() string {
	return in.Spec.Destination.Namespace
}

func (in *ServiceIntentions) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceIntentions) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ServiceIntentions) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceIntentions) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceIntentions) ConsulKind() string {
	return capi.ServiceIntentions
}

func (in *ServiceIntentions) KubeKind() string {
	return common.ServiceIntentions
}

func (in *ServiceIntentions) ConsulName() string {
	return in.Spec.Destination.Name
}

func (in *ServiceIntentions) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *ServiceIntentions) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *ServiceIntentions) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceIntentions) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *ServiceIntentions) ToConsul(datacenter string) api.ConfigEntry {
	return &capi.ServiceIntentionsConfigEntry{
		Kind:      in.ConsulKind(),
		Name:      in.Spec.Destination.Name,
		Namespace: in.Spec.Destination.Namespace,
		Sources:   in.Spec.Sources.toConsul(),
		Meta:      meta(datacenter),
	}
}

func (in SourceIntentions) toConsul() []*capi.SourceIntention {
	var consulSourceIntentions []*capi.SourceIntention
	for _, intention := range in {
		consulSourceIntentions = append(consulSourceIntentions, intention.toConsul())
	}
	return consulSourceIntentions
}

func (in *ServiceIntentions) ConsulGlobalResource() bool {
	return false
}

func (in *SourceIntention) toConsul() *capi.SourceIntention {
	if in == nil {
		return nil
	}
	return &capi.SourceIntention{
		Name:        in.Name,
		Namespace:   in.Namespace,
		Action:      in.Action.toConsul(),
		Permissions: in.Permissions.toConsul(),
		Description: in.Description,
	}
}

func (in IntentionAction) toConsul() capi.IntentionAction {
	return capi.IntentionAction(in)
}

func (in IntentionPermissions) toConsul() []*capi.IntentionPermission {
	var consulIntentionPermissions []*capi.IntentionPermission
	for _, permission := range in {
		consulIntentionPermissions = append(consulIntentionPermissions, &capi.IntentionPermission{
			Action: permission.Action.toConsul(),
			HTTP:   permission.HTTP.ToConsul(),
		})
	}
	return consulIntentionPermissions
}

func (in *IntentionHTTPPermission) ToConsul() *capi.IntentionHTTPPermission {
	if in == nil {
		return nil
	}
	return &capi.IntentionHTTPPermission{
		PathExact:  in.PathExact,
		PathPrefix: in.PathPrefix,
		PathRegex:  in.PathRegex,
		Header:     in.Header.toConsul(),
		Methods:    in.Methods,
	}
}

func (in IntentionHTTPHeaderPermissions) toConsul() []capi.IntentionHTTPHeaderPermission {
	var headerPermissions []capi.IntentionHTTPHeaderPermission
	for _, permission := range in {
		headerPermissions = append(headerPermissions, capi.IntentionHTTPHeaderPermission{
			Name:    permission.Name,
			Present: permission.Present,
			Exact:   permission.Exact,
			Prefix:  permission.Prefix,
			Suffix:  permission.Suffix,
			Regex:   permission.Regex,
			Invert:  permission.Invert,
		})
	}

	return headerPermissions
}

func (in *ServiceIntentions) MatchesConsul(candidate api.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceIntentionsConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(
		in.ToConsul(""),
		configEntry,
		cmpopts.IgnoreFields(capi.ServiceIntentionsConfigEntry{}, "Namespace", "Meta", "ModifyIndex", "CreateIndex"),
		cmpopts.IgnoreFields(capi.SourceIntention{}, "LegacyID", "LegacyMeta", "LegacyCreateTime", "LegacyUpdateTime", "Precedence", "Type"),
		cmpopts.IgnoreUnexported(),
		cmpopts.EquateEmpty(),
	)
}

func (in *ServiceIntentions) Validate(namespacesEnabled bool) error {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if len(in.Spec.Sources) == 0 {
		errs = append(errs, field.Required(path.Child("sources"), `at least one source must be specified`))
	}
	for i, source := range in.Spec.Sources {
		if len(source.Permissions) > 0 && source.Action != "" {
			asJSON, _ := json.Marshal(source)
			errs = append(errs, field.Invalid(path.Child("sources").Index(i), string(asJSON), `action and permissions are mutually exclusive and only one of them can be specified`))
		} else if len(source.Permissions) == 0 {
			if err := source.Action.validate(path.Child("sources").Index(i)); err != nil {
				errs = append(errs, err)
			}
		} else {
			if err := source.Permissions.validate(path.Child("sources").Index(i)); err != nil {
				errs = append(errs, err...)
			}
		}
	}

	errs = append(errs, in.validateNamespaces(namespacesEnabled)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: common.ServiceIntentions},
			in.KubernetesName(), errs)
	}
	return nil
}

func (in IntentionPermissions) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, permission := range in {
		if err := permission.Action.validate(path.Child("permissions").Index(i)); err != nil {
			errs = append(errs, err)
		}
		if permission.HTTP != nil {
			if err := permission.HTTP.validate(path.Child("permissions").Index(i)); err != nil {
				errs = append(errs, err...)
			}
		}
	}
	return errs
}

func (in *IntentionHTTPPermission) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if invalidPathPrefix(in.PathPrefix) {
		errs = append(errs, field.Invalid(path.Child("pathPrefix"), in.PathPrefix, `must begin with a '/'`))
	}
	if invalidPathPrefix(in.PathExact) {
		errs = append(errs, field.Invalid(path.Child("pathExact"), in.PathExact, `must begin with a '/'`))
	}
	return errs
}

// Default sets the namespace field on spec.destination to their default values if namespaces are enabled.
func (in *ServiceIntentions) Default(consulNamespacesEnabled bool, destinationNamespace string, mirroring bool, prefix string) {
	// If namespaces are enabled we want to set the destination namespace field to it's
	// default. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulNamespacesEnabled {
		namespace := namespaces.ConsulNamespace(in.Namespace, consulNamespacesEnabled, destinationNamespace, mirroring, prefix)
		if in.Spec.Destination.Namespace == "" {
			in.Spec.Destination.Namespace = namespace
		}
	}
}

func (in *ServiceIntentions) validateNamespaces(namespacesEnabled bool) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !namespacesEnabled {
		if in.Spec.Destination.Namespace != "" {
			errs = append(errs, field.Invalid(path.Child("destination").Child("namespace"), in.Spec.Destination.Namespace, `Consul Enterprise namespaces must be enabled to set destination.namespace`))
		}
		for i, source := range in.Spec.Sources {
			if source.Namespace != "" {
				errs = append(errs, field.Invalid(path.Child("sources").Index(i).Child("namespace"), source.Namespace, `Consul Enterprise namespaces must be enabled to set source.namespace`))
			}
		}
	}
	return errs
}

func (in IntentionAction) validate(path *field.Path) *field.Error {
	actions := []string{"allow", "deny"}
	if !sliceContains(actions, string(in)) {
		return field.Invalid(path.Child("action"), in, notInSliceMessage(actions))
	}
	return nil
}

// +kubebuilder:object:root=true

// ServiceIntentionsList contains a list of ServiceIntentions
type ServiceIntentionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceIntentions `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceIntentions{}, &ServiceIntentionsList{})
}
