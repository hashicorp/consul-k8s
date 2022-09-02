package v1alpha1

import (
	"encoding/json"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const ServiceResolverKubeKind string = "serviceresolver"

func init() {
	SchemeBuilder.Register(&ServiceResolver{}, &ServiceResolverList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceResolver is the Schema for the serviceresolvers API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
// +kubebuilder:resource:shortName="service-resolver"
type ServiceResolver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceResolverSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceResolverList contains a list of ServiceResolver.
type ServiceResolverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceResolver `json:"items"`
}

// ServiceResolverSpec defines the desired state of ServiceResolver.
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
	ConnectTimeout metav1.Duration `json:"connectTimeout,omitempty"`
	// LoadBalancer determines the load balancing policy and configuration for services
	// issuing requests to this upstream service.
	LoadBalancer *LoadBalancer `json:"loadBalancer,omitempty"`
}

type ServiceResolverRedirect struct {
	// Service is a service to resolve instead of the current service.
	Service string `json:"service,omitempty"`
	// ServiceSubset is a named subset of the given service to resolve instead
	// of one defined as that service's DefaultSubset If empty the default
	// subset is used.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// Namespace is the Consul namespace to resolve the service from instead of
	// the current namespace. If empty the current namespace is assumed.
	Namespace string `json:"namespace,omitempty"`
	// Partition is the Consul partition to resolve the service from instead of
	// the current partition. If empty the current partition is assumed.
	Partition string `json:"partition,omitempty"`
	// Datacenter is the datacenter to resolve the service from instead of the
	// current one.
	Datacenter string `json:"datacenter,omitempty"`
	// Peer is the name of the cluster peer to resolve the service from instead
	// of the current one.
	Peer string `json:"peer,omitempty"`
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
	Namespace string `json:"namespace,omitempty"`
	// Datacenters is a fixed list of datacenters to try during failover.
	Datacenters []string `json:"datacenters,omitempty"`
	// Targets specifies a fixed list of failover targets to try during failover.
	Targets []ServiceResolverFailoverTarget `json:"targets,omitempty"`
}

type ServiceResolverFailoverTarget struct {
	// Service specifies the name of the service to try during failover.
	Service string `json:"service,omitempty"`
	// ServiceSubset specifies the service subset to try during failover.
	ServiceSubset string `json:"serviceSubset,omitempty"`
	// Partition specifies the partition to try during failover.
	Partition string `json:"partition,omitempty"`
	// Namespace specifies the namespace to try during failover.
	Namespace string `json:"namespace,omitempty"`
	// Datacenter specifies the datacenter to try during failover.
	Datacenter string `json:"datacenter,omitempty"`
	// Peer specifies the name of the cluster peer to try during failover.
	Peer string `json:"peer,omitempty"`
}

type LoadBalancer struct {
	// Policy is the load balancing policy used to select a host.
	Policy string `json:"policy,omitempty"`

	// RingHashConfig contains configuration for the "ringHash" policy type.
	RingHashConfig *RingHashConfig `json:"ringHashConfig,omitempty"`

	// LeastRequestConfig contains configuration for the "leastRequest" policy type.
	LeastRequestConfig *LeastRequestConfig `json:"leastRequestConfig,omitempty"`

	// HashPolicies is a list of hash policies to use for hashing load balancing algorithms.
	// Hash policies are evaluated individually and combined such that identical lists
	// result in the same hash.
	// If no hash policies are present, or none are successfully evaluated,
	// then a random backend host will be selected.
	HashPolicies []HashPolicy `json:"hashPolicies,omitempty"`
}

type RingHashConfig struct {
	// MinimumRingSize determines the minimum number of entries in the hash ring.
	MinimumRingSize uint64 `json:"minimumRingSize,omitempty"`

	// MaximumRingSize determines the maximum number of entries in the hash ring.
	MaximumRingSize uint64 `json:"maximumRingSize,omitempty"`
}

type LeastRequestConfig struct {
	// ChoiceCount determines the number of random healthy hosts from which to select the one with the least requests.
	ChoiceCount uint32 `json:"choiceCount,omitempty"`
}

type HashPolicy struct {
	// Field is the attribute type to hash on.
	// Must be one of "header", "cookie", or "query_parameter".
	// Cannot be specified along with sourceIP.
	Field string `json:"field,omitempty"`

	// FieldValue is the value to hash.
	// ie. header name, cookie name, URL query parameter name
	// Cannot be specified along with sourceIP.
	FieldValue string `json:"fieldValue,omitempty"`

	// CookieConfig contains configuration for the "cookie" hash policy type.
	CookieConfig *CookieConfig `json:"cookieConfig,omitempty"`

	// SourceIP determines whether the hash should be of the source IP rather than of a field and field value.
	// Cannot be specified along with field or fieldValue.
	SourceIP bool `json:"sourceIP,omitempty"`

	// Terminal will short circuit the computation of the hash when multiple hash policies are present.
	// If a hash is computed when a Terminal policy is evaluated,
	// then that hash will be used and subsequent hash policies will be ignored.
	Terminal bool `json:"terminal,omitempty"`
}

type CookieConfig struct {
	// Session determines whether to generate a session cookie with no expiration.
	Session bool `json:"session,omitempty"`

	// TTL is the ttl for generated cookies. Cannot be specified for session cookies.
	TTL metav1.Duration `json:"ttl,omitempty"`

	// Path is the path to set for the cookie.
	Path string `json:"path,omitempty"`
}

func (in *ServiceResolver) ConsulKind() string {
	return capi.ServiceResolver
}

func (in *ServiceResolver) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *ServiceResolver) KubeKind() string {
	return ServiceResolverKubeKind
}

func (in *ServiceResolver) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceResolver) KubernetesName() string {
	return in.ObjectMeta.Name
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

func (in *ServiceResolver) ConsulName() string {
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

func (in *ServiceResolver) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *ServiceResolver) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceResolver) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

// ToConsul converts the entry into its Consul equivalent struct.
func (in *ServiceResolver) ToConsul(datacenter string) capi.ConfigEntry {
	return &capi.ServiceResolverConfigEntry{
		Kind:           in.ConsulKind(),
		Name:           in.ConsulName(),
		DefaultSubset:  in.Spec.DefaultSubset,
		Subsets:        in.Spec.Subsets.toConsul(),
		Redirect:       in.Spec.Redirect.toConsul(),
		Failover:       in.Spec.Failover.toConsul(),
		ConnectTimeout: in.Spec.ConnectTimeout.Duration,
		LoadBalancer:   in.Spec.LoadBalancer.toConsul(),
		Meta:           meta(datacenter),
	}
}

func (in *ServiceResolver) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.ServiceResolverConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.ServiceResolverConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *ServiceResolver) ConsulGlobalResource() bool {
	return false
}

func (in *ServiceResolver) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	for k, v := range in.Spec.Failover {
		if err := v.validate(path.Child("failover").Key(k)); err != nil {
			errs = append(errs, err)
		}
	}

	errs = append(errs, in.Spec.LoadBalancer.validate(path.Child("loadBalancer"))...)

	errs = append(errs, in.validateEnterprise(consulMeta)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ServiceResolverKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields has no behaviour here as service-resolver have namespace fields
// that do not default.
func (in *ServiceResolver) DefaultNamespaceFields(_ common.ConsulMeta) {
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

func (in ServiceResolverSubset) toConsul() capi.ServiceResolverSubset {
	return capi.ServiceResolverSubset{
		Filter:      in.Filter,
		OnlyPassing: in.OnlyPassing,
	}
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
		Partition:     in.Partition,
		Peer:          in.Peer,
	}
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

func (in ServiceResolverFailover) toConsul() capi.ServiceResolverFailover {
	var targets []capi.ServiceResolverFailoverTarget
	for _, target := range in.Targets {
		targets = append(targets, target.toConsul())
	}

	return capi.ServiceResolverFailover{
		Service:       in.Service,
		ServiceSubset: in.ServiceSubset,
		Namespace:     in.Namespace,
		Datacenters:   in.Datacenters,
		Targets:       targets,
	}
}

func (in ServiceResolverFailoverTarget) toConsul() capi.ServiceResolverFailoverTarget {
	return capi.ServiceResolverFailoverTarget{
		Service:       in.Service,
		ServiceSubset: in.ServiceSubset,
		Namespace:     in.Namespace,
		Partition:     in.Partition,
		Datacenter:    in.Datacenter,
		Peer:          in.Peer,
	}
}

func (in *LoadBalancer) toConsul() *capi.LoadBalancer {
	if in == nil {
		return nil
	}
	var policies []capi.HashPolicy
	for _, p := range in.HashPolicies {
		policies = append(policies, p.toConsul())
	}
	return &capi.LoadBalancer{
		Policy:             in.Policy,
		RingHashConfig:     in.RingHashConfig.toConsul(),
		LeastRequestConfig: in.LeastRequestConfig.toConsul(),
		HashPolicies:       policies,
	}
}

func (in *RingHashConfig) toConsul() *capi.RingHashConfig {
	if in == nil {
		return nil
	}
	return &capi.RingHashConfig{
		MinimumRingSize: in.MinimumRingSize,
		MaximumRingSize: in.MaximumRingSize,
	}
}

func (in *LeastRequestConfig) toConsul() *capi.LeastRequestConfig {
	if in == nil {
		return nil
	}

	return &capi.LeastRequestConfig{
		ChoiceCount: in.ChoiceCount,
	}
}

func (in HashPolicy) toConsul() capi.HashPolicy {
	return capi.HashPolicy{
		Field:        in.Field,
		FieldValue:   in.FieldValue,
		CookieConfig: in.CookieConfig.toConsul(),
		SourceIP:     in.SourceIP,
		Terminal:     in.Terminal,
	}
}

func (in *CookieConfig) toConsul() *capi.CookieConfig {
	if in == nil {
		return nil
	}
	return &capi.CookieConfig{
		Session: in.Session,
		TTL:     in.TTL.Duration,
		Path:    in.Path,
	}
}

func (in *CookieConfig) validate(path *field.Path) *field.Error {
	if in == nil {
		return nil
	}

	if in.Session && in.TTL.Duration > 0 {
		asJSON, _ := json.Marshal(in)
		return field.Invalid(path, string(asJSON), "cannot set both session and ttl")
	}
	return nil
}

func (in *ServiceResolver) validateEnterprise(consulMeta common.ConsulMeta) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !consulMeta.NamespacesEnabled {
		if in.Spec.Redirect != nil {
			if in.Spec.Redirect.Namespace != "" {
				errs = append(errs, field.Invalid(path.Child("redirect").Child("namespace"), in.Spec.Redirect.Namespace, `Consul Enterprise namespaces must be enabled to set redirect.namespace`))
			}
		}
		for k, v := range in.Spec.Failover {
			if v.Namespace != "" {
				errs = append(errs, field.Invalid(path.Child("failover").Key(k).Child("namespace"), v.Namespace, `Consul Enterprise namespaces must be enabled to set failover.namespace`))
			}
		}
	}
	if !consulMeta.PartitionsEnabled {
		if in.Spec.Redirect != nil {
			if in.Spec.Redirect.Partition != "" {
				errs = append(errs, field.Invalid(path.Child("redirect").Child("partition"), in.Spec.Redirect.Partition, `Consul Enterprise partitions must be enabled to set redirect.partition`))
			}
		}
	}
	return errs
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

func (in *LoadBalancer) validate(path *field.Path) field.ErrorList {
	if in == nil {
		return nil
	}
	var errs field.ErrorList
	for i, p := range in.HashPolicies {
		errs = append(errs, p.validate(path.Child("hashPolicies").Index(i))...)
	}
	return errs
}

func (in HashPolicy) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	if in.Field != "" {
		validFields := []string{"header", "cookie", "query_parameter"}
		if !sliceContains(validFields, in.Field) {
			errs = append(errs, field.Invalid(path.Child("field"), in.Field,
				notInSliceMessage(validFields)))
		}

		if in.SourceIP {
			asJSON, _ := json.Marshal(in)
			errs = append(errs, field.Invalid(path, string(asJSON),
				"cannot set both field and sourceIP"))
		} else if in.FieldValue == "" {
			errs = append(errs, field.Invalid(path.Child("fieldValue"), in.FieldValue,
				"fieldValue cannot be empty if field is set"))
		}
	}

	if err := in.CookieConfig.validate(path.Child("cookieConfig")); err != nil {
		errs = append(errs, err)
	}
	return errs
}
