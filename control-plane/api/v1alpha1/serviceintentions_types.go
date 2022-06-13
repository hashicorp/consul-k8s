package v1alpha1

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func init() {
	SchemeBuilder.Register(&ServiceIntentions{}, &ServiceIntentionsList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceIntentions is the Schema for the serviceintentions API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="service-intentions"
type ServiceIntentions struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceIntentionsSpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceIntentionsList contains a list of ServiceIntentions.
type ServiceIntentionsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceIntentions `json:"items"`
}

// ServiceIntentionsSpec defines the desired state of ServiceIntentions.
type ServiceIntentionsSpec struct {
	// Destination is the intention destination that will have the authorization granted to.
	Destination Destination `json:"destination,omitempty"`
	// Sources is the list of all intention sources and the authorization granted to those sources.
	// The order of this list does not matter, but out of convenience Consul will always store this
	// reverse sorted by intention precedence, as that is the order that they will be evaluated at enforcement time.
	Sources SourceIntentions `json:"sources,omitempty"`
}

type Destination struct {
	// Name is the destination of all intentions defined in this config entry.
	// This may be set to the wildcard character (*) to match
	// all services that don't otherwise have intentions defined.
	Name string `json:"name,omitempty"`
	// Namespace specifies the namespace the config entry will apply to.
	// This may be set to the wildcard character (*) to match all services
	// in all namespaces that don't otherwise have intentions defined.
	Namespace string `json:"namespace,omitempty"`
}

type SourceIntentions []*SourceIntention
type IntentionPermissions []*IntentionPermission
type IntentionHTTPHeaderPermissions []IntentionHTTPHeaderPermission

type SourceIntention struct {
	// Name is the source of the intention. This is the name of a
	// Consul service. The service doesn't need to be registered.
	Name string `json:"name,omitempty"`
	// Namespace is the namespace for the Name parameter.
	Namespace string `json:"namespace,omitempty"`
	// [Experimental] Peer is the peer name for the Name parameter.
	Peer string `json:"peer,omitempty"`
	// Partition is the Admin Partition for the Name parameter.
	Partition string `json:"partition,omitempty"`
	// Action is required for an L4 intention, and should be set to one of
	// "allow" or "deny" for the action that should be taken if this intention matches a request.
	Action IntentionAction `json:"action,omitempty"`
	// Permissions is the list of all additional L7 attributes that extend the intention match criteria.
	// Permission precedence is applied top to bottom. For any given request the first permission to match
	// in the list is terminal and stops further evaluation. As with L4 intentions, traffic that fails to
	// match any of the provided permissions in this intention will be subject to the default intention
	// behavior is defined by the default ACL policy. This should be omitted for an L4 intention
	// as it is mutually exclusive with the Action field.
	Permissions IntentionPermissions `json:"permissions,omitempty"`
	// Description for the intention. This is not used by Consul, but is presented in API responses to assist tooling.
	Description string `json:"description,omitempty"`
}

type IntentionPermission struct {
	// Action is one of "allow" or "deny" for the action that
	// should be taken if this permission matches a request.
	Action IntentionAction `json:"action,omitempty"`
	// HTTP is a set of HTTP-specific authorization criteria.
	HTTP *IntentionHTTPPermission `json:"http,omitempty"`
}

type IntentionHTTPPermission struct {
	// PathExact is the exact path to match on the HTTP request path.
	PathExact string `json:"pathExact,omitempty"`
	// PathPrefix is the path prefix to match on the HTTP request path.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// PathRegex is the regular expression to match on the HTTP request path.
	PathRegex string `json:"pathRegex,omitempty"`
	// Header is a set of criteria that can match on HTTP request headers.
	// If more than one is configured all must match for the overall match to apply.
	Header IntentionHTTPHeaderPermissions `json:"header,omitempty"`
	// Methods is a list of HTTP methods for which this match applies. If unspecified
	// all HTTP methods are matched. If provided the names must be a valid method.
	Methods []string `json:"methods,omitempty"`
}

type IntentionHTTPHeaderPermission struct {
	// Name is the name of the header to match.
	Name string `json:"name,omitempty"`
	// Present matches if the header with the given name is present with any value.
	Present bool `json:"present,omitempty"`
	// Exact matches if the header with the given name is this value.
	Exact string `json:"exact,omitempty"`
	// Prefix matches if the header with the given name has this prefix.
	Prefix string `json:"prefix,omitempty"`
	// Suffix matches if the header with the given name has this suffix.
	Suffix string `json:"suffix,omitempty"`
	// Regex matches if the header with the given name matches this pattern.
	Regex string `json:"regex,omitempty"`
	// Invert inverts the logic of the match.
	Invert bool `json:"invert,omitempty"`
}

// IntentionAction is the action that the intention represents. This
// can be "allow" or "deny" to allowlist or denylist intentions.
type IntentionAction string

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

func (in *ServiceIntentions) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
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

func (in *ServiceIntentions) ConsulGlobalResource() bool {
	return false
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
		cmpopts.IgnoreFields(capi.ServiceIntentionsConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"),
		cmpopts.IgnoreFields(capi.SourceIntention{}, "LegacyID", "LegacyMeta", "LegacyCreateTime", "LegacyUpdateTime", "Precedence", "Type"),
		cmpopts.IgnoreUnexported(),
		cmpopts.EquateEmpty(),
		// Consul will sort the sources by precedence when returning the resource
		// so we need to re-sort the sources to ensure our comparison is accurate.
		cmpopts.SortSlices(func(a *capi.SourceIntention, b *capi.SourceIntention) bool {
			// SortSlices expects a "less than" comparator function so we can
			// piggyback on strings.Compare that returns -1 if a < b.
			return strings.Compare(sourceIntentionSortKey(a), sourceIntentionSortKey(b)) == -1
		}),
	)
}

func (in *ServiceIntentions) Validate(consulMeta common.ConsulMeta) error {
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
			errs = append(errs, source.Permissions.validate(path.Child("sources").Index(i))...)
		}
	}

	errs = append(errs, in.validateNamespaces(consulMeta.NamespacesEnabled)...)
	errs = append(errs, in.validateSourcePeerAndPartitions(consulMeta.PartitionsEnabled)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: common.ServiceIntentions},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields sets the namespace field on spec.destination to their default values if namespaces are enabled.
func (in *ServiceIntentions) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
	// If namespaces are enabled we want to set the destination namespace field to it's
	// default. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulMeta.NamespacesEnabled {
		// Default to the current namespace (i.e. the namespace of the config entry).
		namespace := namespaces.ConsulNamespace(in.Namespace, consulMeta.NamespacesEnabled, consulMeta.DestinationNamespace, consulMeta.Mirroring, consulMeta.Prefix)
		if in.Spec.Destination.Namespace == "" {
			in.Spec.Destination.Namespace = namespace
		}
	}
}

func (in SourceIntentions) toConsul() []*capi.SourceIntention {
	var consulSourceIntentions []*capi.SourceIntention
	for _, intention := range in {
		consulSourceIntentions = append(consulSourceIntentions, intention.toConsul())
	}
	return consulSourceIntentions
}

func (in *SourceIntention) toConsul() *capi.SourceIntention {
	if in == nil {
		return nil
	}
	return &capi.SourceIntention{
		Name:        in.Name,
		Namespace:   in.Namespace,
		Partition:   in.Partition,
		Peer:        in.Peer,
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
			HTTP:   permission.HTTP.toConsul(),
		})
	}
	return consulIntentionPermissions
}

func (in *IntentionHTTPPermission) toConsul() *capi.IntentionHTTPPermission {
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

func (in IntentionPermissions) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, permission := range in {
		if err := permission.Action.validate(path.Child("permissions").Index(i)); err != nil {
			errs = append(errs, err)
		}
		if permission.HTTP != nil {
			errs = append(errs, permission.HTTP.validate(path.Child("permissions").Index(i))...)
		}
	}
	return errs
}

func (in *IntentionHTTPPermission) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	pathParts := 0
	if in.PathRegex != "" {
		pathParts++
	}
	if in.PathPrefix != "" {
		pathParts++
		if invalidPathPrefix(in.PathPrefix) {
			errs = append(errs, field.Invalid(path.Child("pathPrefix"), in.PathPrefix, `must begin with a '/'`))
		}
	}
	if in.PathExact != "" {
		pathParts++
		if invalidPathPrefix(in.PathExact) {
			errs = append(errs, field.Invalid(path.Child("pathExact"), in.PathExact, `must begin with a '/'`))
		}
	}
	if pathParts > 1 {
		asJSON, _ := json.Marshal(in)
		errs = append(errs, field.Invalid(path, string(asJSON), `at most only one of pathExact, pathPrefix, or pathRegex may be configured.`))
	}

	found := make(map[string]struct{})
	for i, method := range in.Methods {
		methods := []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodConnect,
			http.MethodOptions,
			http.MethodTrace,
		}
		if !sliceContains(methods, method) {
			errs = append(errs, field.Invalid(path.Child("methods").Index(i), method, notInSliceMessage(methods)))
		}
		if _, ok := found[method]; ok {
			errs = append(errs, field.Invalid(path.Child("methods").Index(i), method, `method listed more than once.`))
		}
		found[method] = struct{}{}
	}
	errs = append(errs, in.Header.validate(path.Child("header"))...)
	return errs
}

func (in IntentionHTTPHeaderPermissions) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	for i, permission := range in {
		hdrParts := 0
		if permission.Present {
			hdrParts++
		}
		hdrParts += numNotEmpty(permission.Exact, permission.Regex, permission.Prefix, permission.Suffix)
		if hdrParts > 1 {
			asJson, _ := json.Marshal(in[i])
			errs = append(errs, field.Invalid(path.Index(i), string(asJson), `at most only one of exact, prefix, suffix, regex, or present may be configured.`))
		}
	}
	return errs
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

func (in *ServiceIntentions) validateSourcePeerAndPartitions(partitionsEnabled bool) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	for i, source := range in.Spec.Sources {
		if source.Partition != "" && !partitionsEnabled {
			errs = append(errs, field.Invalid(path.Child("sources").Index(i).Child("partition"), source.Partition, `Consul Enterprise Admin Partitions must be enabled to set source.partition`))
		}

		if source.Peer != "" && source.Partition != "" {
			errs = append(errs, field.Invalid(path.Child("sources").Index(i), source, `Both source.peer and source.partition cannot be set.`))
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

func numNotEmpty(ss ...string) int {
	count := 0
	for _, s := range ss {
		if s != "" {
			count++
		}
	}
	return count
}

// sourceIntentionSortKey returns a string that can be used to sort intention
// sources.
func sourceIntentionSortKey(ixn *capi.SourceIntention) string {
	if ixn == nil {
		return ""
	}

	// Zero out fields Consul sets automatically because the Kube resource
	// won't have them set.
	ixn.LegacyCreateTime = nil
	ixn.LegacyUpdateTime = nil
	ixn.LegacyID = ""
	ixn.LegacyMeta = nil
	ixn.Precedence = 0

	// It's okay to swallow this error because we know the intention is JSON
	// marshal-able since it was ingested as JSON.
	asJSON, _ := json.Marshal(ixn)
	return string(asJSON)
}
