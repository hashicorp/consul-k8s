package v1alpha1

import (
	"reflect"
	"sort"
	"time"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const ServiceResolverKubeKind string = "serviceresolver"

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceResolver is the Schema for the serviceresolvers API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
type ServiceResolver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceResolverSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

func (in *ServiceResolver) ConsulKind() string {
	return capi.ServiceResolver
}

func (in *ServiceResolver) KubeKind() string {
	return ServiceResolverKubeKind
}

func (in *ServiceResolver) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceResolver) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ServiceResolver) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceResolver) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceResolver) Name() string {
	return in.ObjectMeta.Name
}

func (in *ServiceResolver) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *ServiceResolver) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceResolver) SyncedConditionStatus() corev1.ConditionStatus {
	return in.Status.GetCondition(ConditionSynced).Status
}

// ToConsul converts the entry into its Consul equivalent struct.
func (in *ServiceResolver) ToConsul() capi.ConfigEntry {
	return &capi.ServiceResolverConfigEntry{
		Kind:           in.ConsulKind(),
		Name:           in.Name(),
		DefaultSubset:  in.Spec.DefaultSubset,
		Subsets:        in.Spec.Subsets.toConsul(),
		Redirect:       in.Spec.Redirect.toConsul(),
		Failover:       in.Spec.Failover.toConsul(),
		ConnectTimeout: in.Spec.ConnectTimeout,
	}
}

func (in *ServiceResolver) MatchesConsul(candidate capi.ConfigEntry) bool {
	serviceResolverCandidate, ok := candidate.(*capi.ServiceResolverConfigEntry)
	if !ok {
		return false
	}

	return in.Name() == serviceResolverCandidate.Name &&
		in.Spec.DefaultSubset == serviceResolverCandidate.DefaultSubset &&
		in.Spec.Subsets.matchesConsul(serviceResolverCandidate.Subsets) &&
		in.Spec.Redirect.matchesConsul(serviceResolverCandidate.Redirect) &&
		in.Spec.Failover.matchesConsul(serviceResolverCandidate.Failover) &&
		in.Spec.ConnectTimeout == serviceResolverCandidate.ConnectTimeout
}

func (in *ServiceResolver) Validate() error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	// Iterate through failover map keys in sorted order so tests are
	// deterministic.
	keys := make([]string, 0, len(in.Spec.Failover))
	for k := range in.Spec.Failover {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := in.Spec.Failover[k]
		if err := f.validate(path.Child("failover").Key(k)); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ServiceResolverKubeKind},
			in.Name(), errs)
	}
	return nil
}

func (in *ServiceResolverFailover) validate(path *field.Path) *field.Error {
	if in.Service == "" && in.ServiceSubset == "" && in.Namespace == "" && len(in.Datacenters) == 0 {
		// NOTE: We're passing "{}" here as our value because we know that the
		// error is we have an empty object.
		return field.Invalid(path, "{}",
			"service, serviceSubset, namespace and datacenters cannot all be empty at once")
	}
	return nil
}

// +kubebuilder:object:root=true

// ServiceResolverList contains a list of ServiceResolver
type ServiceResolverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceResolver `json:"items"`
}

// ServiceResolverSpec defines the desired state of ServiceResolver
type ServiceResolverSpec struct {
	// DefaultSubset is the subset to use when no explicit subset is requested.
	// If empty the unnamed subset is used.
	DefaultSubset string `json:"defaultSubset,omitempty"`
	// Subsets is map of subset name to subset definition for all usable named
	// subsets of this service. The map key is the name of the subset and all
	// names must be valid DNS subdomain elements.
	// This may be empty, in which case only the unnamed default subset will
	// be usable.
	Subsets ServiceResolverSubsetMap `json:"subsets,omitempty"`
	// Redirect when configured, all attempts to resolve the service this
	// resolver defines will be substituted for the supplied redirect
	// EXCEPT when the redirect has already been applied.
	// When substituting the supplied redirect, all other fields besides
	// Kind, Name, and Redirect will be ignored.
	Redirect *ServiceResolverRedirect `json:"redirect,omitempty"`
	// Failover controls when and how to reroute traffic to an alternate pool of
	// service instances.
	// The map is keyed by the service subset it applies to and the special
	// string "*" is a wildcard that applies to any subset not otherwise
	// specified here.
	Failover ServiceResolverFailoverMap `json:"failover,omitempty"`
	// ConnectTimeout is the timeout for establishing new network connections
	// to this service.
	ConnectTimeout time.Duration `json:"connectTimeout,omitempty"`
}

type ServiceResolverRedirect struct {
	// Service is a service to resolve instead of the current service.
	Service string `json:"service,omitempty"`
	// ServiceSubset is a named subset of the given service to resolve instead
	// of one defined as that service's DefaultSubset If empty the default
	// subset is used.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// Namespace is the namespace to resolve the service from instead of the
	// current one.
	Namespace string `json:"namespace,omitempty"`
	// Datacenter is the datacenter to resolve the service from instead of the
	// current one.
	Datacenter string `json:"datacenter,omitempty"`
}

type ServiceResolverSubsetMap map[string]ServiceResolverSubset

type ServiceResolverFailoverMap map[string]ServiceResolverFailover

type ServiceResolverSubset struct {
	// Filter is the filter expression to be used for selecting instances of the
	// requested service. If empty all healthy instances are returned. This
	// expression can filter on the same selectors as the Health API endpoint.
	Filter string `json:"filter,omitempty"`
	// OnlyPassing specifies the behavior of the resolver's health check
	// interpretation. If this is set to false, instances with checks in the
	// passing as well as the warning states will be considered healthy. If this
	// is set to true, only instances with checks in the passing state will be
	// considered healthy.
	OnlyPassing bool `json:"onlyPassing,omitempty"`
}

type ServiceResolverFailover struct {
	// Service is the service to resolve instead of the default as the failover
	// group of instances during failover.
	Service string `json:"service,omitempty"`
	// ServiceSubset is the named subset of the requested service to resolve as
	// the failover group of instances. If empty the default subset for the
	// requested service is used.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// Namespace is the namespace to resolve the requested service from to form
	// the failover group of instances. If empty the current namespace is used.
	Namespace string `json:"namespaces,omitempty"`
	// Datacenters is a fixed list of datacenters to try during failover.
	Datacenters []string `json:"datacenters,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ServiceResolver{}, &ServiceResolverList{})
}

func (in ServiceResolverSubsetMap) toConsul() map[string]capi.ServiceResolverSubset {
	if in == nil {
		return nil
	}
	m := make(map[string]capi.ServiceResolverSubset)
	for k, v := range in {
		m[k] = v.toConsul()
	}
	return m
}

func (in ServiceResolverSubsetMap) matchesConsul(candidate map[string]capi.ServiceResolverSubset) bool {
	if len(in) != len(candidate) {
		return false
	}

	for thisKey, thisVal := range in {
		candidateVal, ok := candidate[thisKey]
		if !ok {
			return false
		}
		if !thisVal.matchesConsul(candidateVal) {
			return false
		}
	}
	return true
}

func (in ServiceResolverSubset) toConsul() capi.ServiceResolverSubset {
	return capi.ServiceResolverSubset{
		Filter:      in.Filter,
		OnlyPassing: in.OnlyPassing,
	}
}

func (in ServiceResolverSubset) matchesConsul(candidate capi.ServiceResolverSubset) bool {
	return in.OnlyPassing == candidate.OnlyPassing && in.Filter == candidate.Filter
}

func (in *ServiceResolverRedirect) toConsul() *capi.ServiceResolverRedirect {
	if in == nil {
		return nil
	}
	return &capi.ServiceResolverRedirect{
		Service:       in.Service,
		ServiceSubset: in.ServiceSubset,
		Namespace:     in.Namespace,
		Datacenter:    in.Datacenter,
	}
}

func (in *ServiceResolverRedirect) matchesConsul(candidate *capi.ServiceResolverRedirect) bool {
	if in == nil || candidate == nil {
		return in == nil && candidate == nil
	}
	return in.Service == candidate.Service &&
		in.ServiceSubset == candidate.ServiceSubset &&
		in.Namespace == candidate.Namespace &&
		in.Datacenter == candidate.Datacenter
}

func (in ServiceResolverFailoverMap) toConsul() map[string]capi.ServiceResolverFailover {
	if in == nil {
		return nil
	}
	m := make(map[string]capi.ServiceResolverFailover)
	for k, v := range in {
		m[k] = v.toConsul()
	}
	return m
}

func (in ServiceResolverFailoverMap) matchesConsul(candidate map[string]capi.ServiceResolverFailover) bool {
	if len(in) != len(candidate) {
		return false
	}

	for thisKey, thisVal := range in {
		candidateVal, ok := candidate[thisKey]
		if !ok {
			return false
		}

		if !thisVal.matchesConsul(candidateVal) {
			return false
		}
	}
	return true
}

func (in ServiceResolverFailover) toConsul() capi.ServiceResolverFailover {
	return capi.ServiceResolverFailover{
		Service:       in.Service,
		ServiceSubset: in.ServiceSubset,
		Namespace:     in.Namespace,
		Datacenters:   in.Datacenters,
	}
}

func (in ServiceResolverFailover) matchesConsul(candidate capi.ServiceResolverFailover) bool {
	return in.Service == candidate.Service &&
		in.ServiceSubset == candidate.ServiceSubset &&
		in.Namespace == candidate.Namespace &&
		reflect.DeepEqual(in.Datacenters, candidate.Datacenters)
}
