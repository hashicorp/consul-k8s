package v1alpha1

import (
	"strings"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	ServiceDefaultsKubeKind string = "servicedefaults"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceDefaults is the Schema for the servicedefaults API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
type ServiceDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServiceDefaultsSpec `json:"spec,omitempty"`
	Status            `json:"status,omitempty"`
}

// ServiceDefaultsSpec defines the desired state of ServiceDefaults
type ServiceDefaultsSpec struct {
	// Protocol sets the protocol of the service. This is used by Connect proxies for
	// things like observability features and to unlock usage of the
	// service-splitter and service-router config entries for a service.
	Protocol string `json:"protocol,omitempty"`
	// MeshGateway controls the default mesh gateway configuration for this service.
	MeshGateway MeshGatewayConfig `json:"meshGateway,omitempty"`
	// Expose controls the default expose path configuration for Envoy.
	Expose ExposeConfig `json:"expose,omitempty"`
	// ExternalSNI is an optional setting that allows for the TLS SNI value
	// to be changed to a non-connect value when federating with an external system.
	ExternalSNI string `json:"externalSNI,omitempty"`
}

func (in *ServiceDefaults) ConsulKind() string {
	return capi.ServiceDefaults
}

func (in *ServiceDefaults) ConsulNamespaced() bool {
	return true
}

func (in *ServiceDefaults) KubeKind() string {
	return ServiceDefaultsKubeKind
}

func (in *ServiceDefaults) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *ServiceDefaults) AddFinalizer(f string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), f)
}

func (in *ServiceDefaults) RemoveFinalizer(f string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != f {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *ServiceDefaults) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *ServiceDefaults) Name() string {
	return in.ObjectMeta.Name
}

func (in *ServiceDefaults) SetSyncedCondition(status corev1.ConditionStatus, reason string, message string) {
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

func (in *ServiceDefaults) SyncedCondition() (status corev1.ConditionStatus, reason string, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	return cond.Status, cond.Reason, cond.Message
}

func (in *ServiceDefaults) SyncedConditionStatus() corev1.ConditionStatus {
	return in.Status.GetCondition(ConditionSynced).Status
}

// +kubebuilder:object:root=true

// ServiceDefaultsList contains a list of ServiceDefaults
type ServiceDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceDefaults `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceDefaults{}, &ServiceDefaultsList{})
}

// ToConsul converts the entry into it's Consul equivalent struct.
func (in *ServiceDefaults) ToConsul() capi.ConfigEntry {
	return &capi.ServiceConfigEntry{
		Kind:        in.ConsulKind(),
		Name:        in.Name(),
		Protocol:    in.Spec.Protocol,
		MeshGateway: in.Spec.MeshGateway.toConsul(),
		Expose:      in.Spec.Expose.toConsul(),
		ExternalSNI: in.Spec.ExternalSNI,
	}
}

// Validate validates the fields provided in the spec of the ServiceDefaults and
// returns an error which lists all invalid fields in the resource spec.
func (in *ServiceDefaults) Validate() error {
	var allErrs field.ErrorList
	path := field.NewPath("spec")

	if err := in.Spec.MeshGateway.validate(path.Child("meshGateway")); err != nil {
		allErrs = append(allErrs, err)
	}
	allErrs = append(allErrs, in.Spec.Expose.validate(path.Child("expose"))...)

	if len(allErrs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ServiceDefaultsKubeKind},
			in.Name(), allErrs)
	}

	return nil
}

// MatchesConsul returns true if entry has the same config as this struct.
func (in *ServiceDefaults) MatchesConsul(candidate capi.ConfigEntry) bool {
	serviceDefaultsCandidate, ok := candidate.(*capi.ServiceConfigEntry)
	if !ok {
		return false
	}
	return in.Name() == serviceDefaultsCandidate.Name &&
		in.Spec.Protocol == serviceDefaultsCandidate.Protocol &&
		in.Spec.MeshGateway.Mode == string(serviceDefaultsCandidate.MeshGateway.Mode) &&
		in.Spec.Expose.matches(serviceDefaultsCandidate.Expose) &&
		in.Spec.ExternalSNI == serviceDefaultsCandidate.ExternalSNI
}

// ExposeConfig describes HTTP paths to expose through Envoy outside of Connect.
// Users can expose individual paths and/or all HTTP/GRPC paths for checks.
type ExposeConfig struct {
	// Checks defines whether paths associated with Consul checks will be exposed.
	// This flag triggers exposing all HTTP and GRPC check paths registered for the service.
	Checks bool `json:"checks,omitempty"`

	// Paths is the list of paths exposed through the proxy.
	Paths []ExposePath `json:"paths,omitempty"`
}

type ExposePath struct {
	// ListenerPort defines the port of the proxy's listener for exposed paths.
	ListenerPort int `json:"listenerPort,omitempty"`

	// Path is the path to expose through the proxy, ie. "/metrics".
	Path string `json:"path,omitempty"`

	// LocalPathPort is the port that the service is listening on for the given path.
	LocalPathPort int `json:"localPathPort,omitempty"`

	// Protocol describes the upstream's service protocol.
	// Valid values are "http" and "http2", defaults to "http".
	Protocol string `json:"protocol,omitempty"`
}

// matches returns true if the expose config of the entry is the same as the struct
func (e ExposeConfig) matches(expose capi.ExposeConfig) bool {
	if e.Checks != expose.Checks {
		return false
	}

	if len(e.Paths) != len(expose.Paths) {
		return false
	}

	for _, path := range e.Paths {
		found := false
		for _, entryPath := range expose.Paths {
			if path.Protocol == entryPath.Protocol &&
				path.Path == entryPath.Path &&
				path.ListenerPort == entryPath.ListenerPort &&
				path.LocalPathPort == entryPath.LocalPathPort {
				found = true
				break
			}
		}

		if !found {
			return false
		}
	}
	return true
}

// toConsul returns the ExposeConfig for the entry
func (e ExposeConfig) toConsul() capi.ExposeConfig {
	var paths []capi.ExposePath
	for _, path := range e.Paths {
		paths = append(paths, capi.ExposePath{
			ListenerPort:  path.ListenerPort,
			Path:          path.Path,
			LocalPathPort: path.LocalPathPort,
			Protocol:      path.Protocol,
		})
	}
	return capi.ExposeConfig{
		Checks: e.Checks,
		Paths:  paths,
	}
}

func (e ExposeConfig) validate(path *field.Path) []*field.Error {
	var errs field.ErrorList
	protocols := []string{"http", "http2"}
	for i, pathCfg := range e.Paths {
		indexPath := path.Child("paths").Index(i)
		if pathCfg.Path != "" && !strings.HasPrefix(pathCfg.Path, "/") {
			errs = append(errs, field.Invalid(
				indexPath.Child("path"),
				pathCfg.Path,
				`must begin with a '/'`))
		}
		if !sliceContains(protocols, pathCfg.Protocol) {
			errs = append(errs, field.Invalid(
				indexPath.Child("protocol"),
				pathCfg.Protocol,
				notInSliceMessage(protocols)))
		}
	}
	return errs
}
