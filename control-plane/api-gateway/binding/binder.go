package binding

import (
	"errors"
	"reflect"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/statuses"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	gatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
	NamespaceNameLabel = "kubernetes.io/metadata.name"
)

var (
	errNotAllowedByListenerNamespace = errors.New("listener does not allow binding routes from the given namespace")
	errNotAllowedByListenerProtocol  = errors.New("listener does not support route protocol")
	errNoMatchingListenerHostname    = errors.New("listener cannot bind route with a non-aligned hostname")

	kindGateway               = "Gateway"
	kindSecret                = "Secret"
	betaGroup                 = gwv1beta1.GroupVersion.Group
	betaVersion               = gwv1beta1.GroupVersion.Version
	supportedKindsForProtocol = map[gwv1beta1.ProtocolType][]gwv1beta1.RouteGroupKind{
		gwv1beta1.HTTPProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.HTTPSProtocolType: {{
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "HTTPRoute",
		}},
		gwv1beta1.TCPProtocolType: {{
			Group: (*gwv1alpha2.Group)(&gwv1alpha2.GroupVersion.Group),
			Kind:  "TCPRoute",
		}},
	}
)

type BinderConfig struct {
	Setter         *statuses.Setter
	Translator     translation.K8sToConsulTranslator
	ControllerName string

	GatewayClass *gwv1beta1.GatewayClass
	Gateway      gwv1beta1.Gateway
	HTTPRoutes   []gwv1beta1.HTTPRoute
	TCPRoutes    []gwv1alpha2.TCPRoute
	Secrets      []corev1.Secret

	// All routes that are currently bound in Consul or correspond to the
	// routes in the *Routes members above
	ConsulHTTPRoutes []api.HTTPRouteConfigEntry
	ConsulTCPRoutes  []api.TCPRouteConfigEntry
	// All certificates that are currently bound in Consul or correspond
	// to the Secrets member above
	ConsulInlineCertificates []api.InlineCertificateConfigEntry
	// All the connect services that we're aware of
	ConnectInjectedServices []api.CatalogService

	// used for namespace label checking
	Namespaces map[string]corev1.Namespace
	// used for reference counting
	ControlledGateways map[types.NamespacedName]gwv1beta1.Gateway
}

// TODO: DRY up a bunch of these implementations, the boilerplate is almost
// identical for each route type

type Binder struct {
	config BinderConfig
}

func NewBinder(config BinderConfig) *Binder {
	return &Binder{config: config}
}

func (b *Binder) gatewayRef() api.ResourceReference {
	return b.config.Translator.ReferenceForGateway(&b.config.Gateway)
}

func (b *Binder) isGatewayDeleted() bool {
	gatewayClassMismatch := b.config.GatewayClass == nil || b.config.ControllerName != string(b.config.GatewayClass.Spec.ControllerName)
	isGatewayDeleted := isDeleted(&b.config.Gateway) || gatewayClassMismatch
	return isGatewayDeleted
}

func (b *Binder) Snapshot() Snapshot {
	// at this point we assume all tcp routes and http routes
	// actually reference this gateway
	tracker := b.references()
	seenRoutes := map[api.ResourceReference]struct{}{}
	snapshot := Snapshot{}

	gatewayRef := b.gatewayRef()
	isGatewayDeleted := b.isGatewayDeleted()

	if isGatewayDeleted {
		snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, gatewayRef)
		if removeFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
		}
	} else {
		// we don't have a deletion but if we add a finalizer for the gateway, then just add it and return
		// otherwise try and resolve as much as possible
		if ensureFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
			return snapshot
		}
	}

	httpRouteBinder := b.newHTTPRouteBinder(tracker)
	tcpRouteBinder := b.newTCPRouteBinder(tracker)

	for _, r := range b.config.HTTPRoutes {
		snapshot = httpRouteBinder.bind(pointerTo(r), seenRoutes, snapshot)
	}

	for _, r := range b.config.TCPRoutes {
		snapshot = tcpRouteBinder.bind(pointerTo(r), seenRoutes, snapshot)
	}

	for _, route := range b.config.ConsulHTTPRoutes {
		snapshot = b.cleanHTTPRoute(pointerTo(route), seenRoutes, snapshot)
	}

	for _, route := range b.config.ConsulTCPRoutes {
		snapshot = b.cleanTCPRoute(pointerTo(route), seenRoutes, snapshot)
	}

	seenCerts := make(map[types.NamespacedName]api.ResourceReference)
	for _, secret := range b.config.Secrets {
		if isGatewayDeleted {
			// we bypass the secret creation since we want to be able to GC if necessary
			continue
		}
		certificate := b.config.Translator.SecretToInlineCertificate(secret)
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &certificate)
		certificateRef := translation.EntryToReference(&certificate)
		seenCerts[objectToMeta(&secret)] = certificateRef
	}

	// clean up any inline certs that are now stale and can be GC'd
	for _, cert := range b.config.ConsulInlineCertificates {
		certRef := translation.EntryToNamespacedName(&cert)
		if _, ok := seenCerts[certRef]; !ok {
			if tracker.canGCSecret(certRef) {
				ref := translation.EntryToReference(&cert)
				// we can GC this now since it's not referenced by any Gateway
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, ref)
			}
		}
	}

	if !isGatewayDeleted {
		entry := b.config.Translator.GatewayToAPIGateway(b.config.Gateway, seenCerts)
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &entry)
		// TODO: update gateway status
	}

	return snapshot
}

func listenersFor(gateway *gwv1beta1.Gateway, name *gwv1beta1.SectionName) []gwv1beta1.Listener {
	listeners := []gwv1beta1.Listener{}
	for _, listener := range gateway.Spec.Listeners {
		if name == nil {
			listeners = append(listeners, listener)
			continue
		}
		if listener.Name == *name {
			listeners = append(listeners, listener)
		}
	}
	return listeners
}

// routeAllowedForListenerNamespaces determines whether the route is allowed
// to bind to the Gateway based on the AllowedRoutes namespace selectors.
func routeAllowedForListenerNamespaces(gatewayNamespace string, allowedRoutes *gwv1beta1.AllowedRoutes, namespace corev1.Namespace) bool {
	var namespaceSelector *gwv1beta1.RouteNamespaces
	if allowedRoutes != nil {
		// check gateway namespace
		namespaceSelector = allowedRoutes.Namespaces
	}

	// set default if namespace selector is nil
	from := gwv1beta1.NamespacesFromSame
	if namespaceSelector != nil && namespaceSelector.From != nil && *namespaceSelector.From != "" {
		from = *namespaceSelector.From
	}

	switch from {
	case gwv1beta1.NamespacesFromAll:
		return true
	case gwv1beta1.NamespacesFromSame:
		return gatewayNamespace == namespace.Name
	case gwv1beta1.NamespacesFromSelector:
		namespaceSelector, err := metav1.LabelSelectorAsSelector(namespaceSelector.Selector)
		if err != nil {
			// log the error here, the label selector is invalid
			return false
		}

		return namespaceSelector.Matches(toNamespaceSet(namespace.GetName(), namespace.GetLabels()))
	default:
		return false
	}
}

func routeAllowedForListenerHostname(hostname *gwv1beta1.Hostname, hostnames []gwv1beta1.Hostname) bool {
	if hostname == nil || len(hostnames) == 0 {
		return true
	}

	for _, name := range hostnames {
		if hostnamesMatch(name, *hostname) {
			return true
		}
	}
	return false
}

func hostnamesMatch(a gwv1alpha2.Hostname, b gwv1beta1.Hostname) bool {
	if a == "" || a == "*" || b == "" || b == "*" {
		// any wildcard always matches
		return true
	}

	if strings.HasPrefix(string(a), "*.") || strings.HasPrefix(string(b), "*.") {
		aLabels, bLabels := strings.Split(string(a), "."), strings.Split(string(b), ".")
		if len(aLabels) != len(bLabels) {
			return false
		}

		for i := 1; i < len(aLabels); i++ {
			if !strings.EqualFold(aLabels[i], bLabels[i]) {
				return false
			}
		}
		return true
	}

	return string(a) == string(b)
}

func routeKindIsAllowedForListener(kinds []gwv1beta1.RouteGroupKind, gk schema.GroupKind) bool {
	if kinds == nil {
		return true
	}

	for _, kind := range kinds {
		if string(kind.Kind) == gk.Kind && nilOrEqual(kind.Group, gk.Group) {
			return true
		}
	}

	return false
}

func routeKindIsAllowedForListenerExplicit(allowedRoutes *gwv1alpha2.AllowedRoutes, gk schema.GroupKind) bool {
	if allowedRoutes == nil {
		return true
	}

	if len(allowedRoutes.Kinds) == 0 {
		return true
	}

	for _, kind := range allowedRoutes.Kinds {
		if string(kind.Kind) == gk.Kind && nilOrEqual(kind.Group, gk.Group) {
			return true
		}
	}

	return false
}

func toNamespaceSet(name string, labels map[string]string) klabels.Labels {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return klabels.Set(labels)
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return klabels.Set(ret)
}

func valueOr[T ~string](v *T, fallback string) string {
	if v == nil {
		return fallback
	}
	return string(*v)
}

func nilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

func filterParentRefs(gateway types.NamespacedName, namespace string, refs []gwv1beta1.ParentReference) []gwv1beta1.ParentReference {
	references := []gwv1beta1.ParentReference{}
	for _, ref := range refs {
		if nilOrEqual(ref.Group, betaGroup) &&
			nilOrEqual(ref.Kind, kindGateway) &&
			gateway.Namespace == valueOr(ref.Namespace, namespace) &&
			gateway.Name == string(ref.Name) {
			references = append(references, ref)
		}
	}

	return references
}

func routeMatchesListener(listenerName gwv1beta1.SectionName, routeSectionName *gwv1alpha2.SectionName) (can bool, must bool) {
	if routeSectionName == nil {
		return true, false
	}
	return string(listenerName) == string(*routeSectionName), true
}

func stringPointer[T ~string](v T) *string {
	x := string(v)
	return &x
}

func objectsToMeta[T metav1.Object](objects []T) []types.NamespacedName {
	var meta []types.NamespacedName
	for _, object := range objects {
		meta = append(meta, types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		})
	}
	return meta
}

func objectToMeta[T metav1.Object](object T) types.NamespacedName {
	return types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}

func isDeleted(object client.Object) bool {
	return !object.GetDeletionTimestamp().IsZero()
}

func ensureFinalizer(object client.Object) bool {
	if !object.GetDeletionTimestamp().IsZero() {
		return false
	}

	finalizers := object.GetFinalizers()
	for _, f := range finalizers {
		if f == gatewayFinalizer {
			return false
		}
	}

	object.SetFinalizers(append(finalizers, gatewayFinalizer))
	return true
}

func removeFinalizer(object client.Object) bool {
	found := false
	filtered := []string{}
	for _, f := range object.GetFinalizers() {
		if f == gatewayFinalizer {
			found = true
			continue
		}
		filtered = append(filtered, f)
	}

	object.SetFinalizers(filtered)
	return found
}

func pointerTo[T any](v T) *T {
	return &v
}

func isNil(arg interface{}) bool {
	return arg == nil || reflect.ValueOf(arg).IsNil()
}
