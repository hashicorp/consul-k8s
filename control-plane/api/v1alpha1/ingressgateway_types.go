package v1alpha1

import (
	"encoding/json"
	"fmt"

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
	ingressGatewayKubeKind = "ingressgateway"
	wildcardServiceName    = "*"
)

func init() {
	SchemeBuilder.Register(&IngressGateway{}, &IngressGatewayList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// IngressGateway is the Schema for the ingressgateways API
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=".status.conditions[?(@.type==\"Synced\")].status",description="The sync status of the resource with Consul"
// +kubebuilder:printcolumn:name="Last Synced",type="date",JSONPath=".status.lastSyncedTime",description="The last successful synced time of the resource with Consul"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
type IngressGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngressGatewaySpec `json:"spec,omitempty"`
	Status `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IngressGatewayList contains a list of IngressGateway
type IngressGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngressGateway `json:"items"`
}

// IngressGatewaySpec defines the desired state of IngressGateway
type IngressGatewaySpec struct {
	// TLS holds the TLS configuration for this gateway.
	TLS GatewayTLSConfig `json:"tls,omitempty"`
	// Listeners declares what ports the ingress gateway should listen on, and
	// what services to associated to those ports.
	Listeners []IngressListener `json:"listeners,omitempty"`
}

type GatewayTLSConfig struct {
	// Indicates that TLS should be enabled for this gateway service.
	Enabled bool `json:"enabled"`
}

// IngressListener manages the configuration for a listener on a specific port.
type IngressListener struct {
	// Port declares the port on which the ingress gateway should listen for traffic.
	Port int `json:"port,omitempty"`

	// Protocol declares what type of traffic this listener is expected to
	// receive. Depending on the protocol, a listener might support multiplexing
	// services over a single port, or additional discovery chain features. The
	// current supported values are: (tcp | http | http2 | grpc).
	Protocol string `json:"protocol,omitempty"`

	// Services declares the set of services to which the listener forwards
	// traffic.
	//
	// For "tcp" protocol listeners, only a single service is allowed.
	// For "http" listeners, multiple services can be declared.
	Services []IngressService `json:"services,omitempty"`
}

// IngressService manages configuration for services that are exposed to
// ingress traffic.
type IngressService struct {
	// Name declares the service to which traffic should be forwarded.
	//
	// This can either be a specific service, or the wildcard specifier,
	// "*". If the wildcard specifier is provided, the listener must be of "http"
	// protocol and means that the listener will forward traffic to all services.
	//
	// A name can be specified on multiple listeners, and will be exposed on both
	// of the listeners.
	Name string `json:"name,omitempty"`

	// Hosts is a list of hostnames which should be associated to this service on
	// the defined listener. Only allowed on layer 7 protocols, this will be used
	// to route traffic to the service by matching the Host header of the HTTP
	// request.
	//
	// If a host is provided for a service that also has a wildcard specifier
	// defined, the host will override the wildcard-specifier-provided
	// "<service-name>.*" domain for that listener.
	//
	// This cannot be specified when using the wildcard specifier, "*", or when
	// using a "tcp" listener.
	Hosts []string `json:"hosts,omitempty"`

	// Namespace is the namespace where the service is located.
	// Namespacing is a Consul Enterprise feature.
	Namespace string `json:"namespace,omitempty"`
}

func (in *IngressGateway) GetObjectMeta() metav1.ObjectMeta {
	return in.ObjectMeta
}

func (in *IngressGateway) AddFinalizer(name string) {
	in.ObjectMeta.Finalizers = append(in.Finalizers(), name)
}

func (in *IngressGateway) RemoveFinalizer(name string) {
	var newFinalizers []string
	for _, oldF := range in.Finalizers() {
		if oldF != name {
			newFinalizers = append(newFinalizers, oldF)
		}
	}
	in.ObjectMeta.Finalizers = newFinalizers
}

func (in *IngressGateway) Finalizers() []string {
	return in.ObjectMeta.Finalizers
}

func (in *IngressGateway) ConsulKind() string {
	return capi.IngressGateway
}

func (in *IngressGateway) ConsulGlobalResource() bool {
	return false
}

func (in *IngressGateway) ConsulMirroringNS() string {
	return in.Namespace
}

func (in *IngressGateway) KubeKind() string {
	return ingressGatewayKubeKind
}

func (in *IngressGateway) ConsulName() string {
	return in.ObjectMeta.Name
}

func (in *IngressGateway) KubernetesName() string {
	return in.ObjectMeta.Name
}

func (in *IngressGateway) SetSyncedCondition(status corev1.ConditionStatus, reason, message string) {
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

func (in *IngressGateway) SetLastSyncedTime(time *metav1.Time) {
	in.Status.LastSyncedTime = time
}

func (in *IngressGateway) SyncedCondition() (status corev1.ConditionStatus, reason, message string) {
	cond := in.Status.GetCondition(ConditionSynced)
	if cond == nil {
		return corev1.ConditionUnknown, "", ""
	}
	return cond.Status, cond.Reason, cond.Message
}

func (in *IngressGateway) SyncedConditionStatus() corev1.ConditionStatus {
	condition := in.Status.GetCondition(ConditionSynced)
	if condition == nil {
		return corev1.ConditionUnknown
	}
	return condition.Status
}

func (in *IngressGateway) ToConsul(datacenter string) capi.ConfigEntry {
	var listeners []capi.IngressListener
	for _, l := range in.Spec.Listeners {
		listeners = append(listeners, l.toConsul())
	}
	return &capi.IngressGatewayConfigEntry{
		Kind:      in.ConsulKind(),
		Name:      in.ConsulName(),
		TLS:       in.Spec.TLS.toConsul(),
		Listeners: listeners,
		Meta:      meta(datacenter),
	}
}

func (in *IngressGateway) MatchesConsul(candidate capi.ConfigEntry) bool {
	configEntry, ok := candidate.(*capi.IngressGatewayConfigEntry)
	if !ok {
		return false
	}
	// No datacenter is passed to ToConsul as we ignore the Meta field when checking for equality.
	return cmp.Equal(in.ToConsul(""), configEntry, cmpopts.IgnoreFields(capi.IngressGatewayConfigEntry{}, "Partition", "Namespace", "Meta", "ModifyIndex", "CreateIndex"), cmpopts.IgnoreUnexported(), cmpopts.EquateEmpty())
}

func (in *IngressGateway) Validate(consulMeta common.ConsulMeta) error {
	var errs field.ErrorList
	path := field.NewPath("spec")

	for i, v := range in.Spec.Listeners {
		errs = append(errs, v.validate(path.Child("listeners").Index(i))...)
	}

	errs = append(errs, in.validateNamespaces(consulMeta.NamespacesEnabled)...)

	if len(errs) > 0 {
		return apierrors.NewInvalid(
			schema.GroupKind{Group: ConsulHashicorpGroup, Kind: ingressGatewayKubeKind},
			in.KubernetesName(), errs)
	}
	return nil
}

// DefaultNamespaceFields sets the namespace field on spec.listeners[].services to their default values if namespaces are enabled.
func (in *IngressGateway) DefaultNamespaceFields(consulMeta common.ConsulMeta) {
	// If namespaces are enabled we want to set the namespace fields to their
	// defaults. If namespaces are not enabled (i.e. OSS) we don't set the
	// namespace fields because this would cause errors
	// making API calls (because namespace fields can't be set in OSS).
	if consulMeta.NamespacesEnabled {
		// Default to the current namespace (i.e. the namespace of the config entry).
		namespace := namespaces.ConsulNamespace(in.Namespace, consulMeta.NamespacesEnabled, consulMeta.DestinationNamespace, consulMeta.Mirroring, consulMeta.Prefix)
		for i, listener := range in.Spec.Listeners {
			for j, service := range listener.Services {
				if service.Namespace == "" {
					in.Spec.Listeners[i].Services[j].Namespace = namespace
				}
			}
		}
	}
}

func (in GatewayTLSConfig) toConsul() capi.GatewayTLSConfig {
	return capi.GatewayTLSConfig{
		Enabled: in.Enabled,
	}
}

func (in IngressListener) toConsul() capi.IngressListener {
	var services []capi.IngressService
	for _, s := range in.Services {
		services = append(services, s.toConsul())
	}
	return capi.IngressListener{
		Port:     in.Port,
		Protocol: in.Protocol,
		Services: services,
	}
}

func (in IngressService) toConsul() capi.IngressService {
	return capi.IngressService{
		Name:      in.Name,
		Hosts:     in.Hosts,
		Namespace: in.Namespace,
	}
}

func (in IngressListener) validate(path *field.Path) field.ErrorList {
	var errs field.ErrorList
	validProtocols := []string{"tcp", "http", "http2", "grpc"}
	if !sliceContains(validProtocols, in.Protocol) {
		errs = append(errs, field.Invalid(path.Child("protocol"),
			in.Protocol,
			notInSliceMessage(validProtocols)))
	}

	if in.Protocol == "tcp" && len(in.Services) > 1 {
		asJSON, _ := json.Marshal(in.Services)
		errs = append(errs, field.Invalid(path.Child("services"),
			string(asJSON),
			fmt.Sprintf("if protocol is \"tcp\", only a single service is allowed, found %d", len(in.Services))))
	}

	for i, svc := range in.Services {
		if svc.Name == wildcardServiceName && in.Protocol != "http" {
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("name"),
				svc.Name,
				fmt.Sprintf("if name is %q, protocol must be \"http\" but was %q", wildcardServiceName, in.Protocol)))
		}

		if svc.Name == wildcardServiceName && len(svc.Hosts) > 0 {
			asJSON, _ := json.Marshal(svc.Hosts)
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("hosts"),
				string(asJSON),
				fmt.Sprintf("hosts must be empty if name is %q", wildcardServiceName)))
		}

		if len(svc.Hosts) > 0 && in.Protocol == "tcp" {
			asJSON, _ := json.Marshal(svc.Hosts)
			errs = append(errs, field.Invalid(path.Child("services").Index(i).Child("hosts"),
				string(asJSON),
				"hosts must be empty if protocol is \"tcp\""))
		}
	}
	return errs
}

func (in *IngressGateway) validateNamespaces(namespacesEnabled bool) field.ErrorList {
	var errs field.ErrorList
	path := field.NewPath("spec")
	if !namespacesEnabled {
		for i, listener := range in.Spec.Listeners {
			for j, service := range listener.Services {
				if service.Namespace != "" {
					errs = append(errs, field.Invalid(path.Child("listeners").Index(i).Child("services").Index(j).Child("namespace"),
						service.Namespace, `Consul Enterprise namespaces must be enabled to set service.namespace`))
				}
			}
		}
	}
	return errs
}
