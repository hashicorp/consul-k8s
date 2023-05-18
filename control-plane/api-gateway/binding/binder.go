package binding

import (
	"errors"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
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

func serviceMap(services []api.CatalogService) map[types.NamespacedName]api.CatalogService {
	smap := make(map[types.NamespacedName]api.CatalogService)
	for _, service := range services {
		smap[serviceToNamespacedName(&service)] = service
	}
	return smap
}

func serviceToNamespacedName(s *api.CatalogService) types.NamespacedName {
	var (
		metaKeyKubeNS          = "k8s-namespace"
		metaKeyKubeServiceName = "k8s-service-name"
	)
	return types.NamespacedName{
		Namespace: s.ServiceMeta[metaKeyKubeNS],
		Name:      s.ServiceMeta[metaKeyKubeServiceName],
	}
}

func (b *Binder) Snapshot() Snapshot {
	// at this point we assume all tcp routes and http routes
	// actually reference this gateway
	tracker := b.references()
	serviceMap := serviceMap(b.config.ConnectInjectedServices)
	seenRoutes := map[api.ResourceReference]struct{}{}
	snapshot := Snapshot{}

	isGatewayDeleted := b.isGatewayDeleted()
	if !isGatewayDeleted {
		// we don't have a deletion but if we add a finalizer for the gateway, then just add it and return
		// otherwise try and resolve as much as possible
		if ensureFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
			return snapshot
		}
	}

	httpRouteBinder := b.newHTTPRouteBinder(tracker, serviceMap)
	tcpRouteBinder := b.newTCPRouteBinder(tracker, serviceMap)
	boundCounts := make(map[gwv1beta1.SectionName]int)

	for _, r := range b.config.HTTPRoutes {
		snapshot = httpRouteBinder.bind(pointerTo(r), boundCounts, seenRoutes, snapshot)
	}

	for _, r := range b.config.TCPRoutes {
		snapshot = tcpRouteBinder.bind(pointerTo(r), boundCounts, seenRoutes, snapshot)
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

		var status gwv1beta1.GatewayStatus
		gatewayValidation := validateGateway(b.config.Gateway)
		listenerValidation := validateListeners(b.config.Gateway.Namespace, b.config.Gateway.Spec.Listeners, b.config.Secrets)
		for i, listener := range b.config.Gateway.Spec.Listeners {
			status.Listeners = append(status.Listeners, gwv1beta1.ListenerStatus{
				Name:           listener.Name,
				SupportedKinds: supportedKindsForProtocol[listener.Protocol],
				AttachedRoutes: int32(boundCounts[listener.Name]),
				Conditions:     listenerValidation.Conditions(b.config.Gateway.Generation, i),
			})
		}
		// TODO: addresses
		status.Conditions = gatewayValidation.Conditions(b.config.Gateway.Generation, listenerValidation.Invalid())
		status.Addresses = []gwv1beta1.GatewayAddress{}

		if !cmp.Equal(status, b.config.Gateway.Status, cmp.FilterPath(func(p cmp.Path) bool {
			path := p.String()
			return path == "Listeners.Conditions.LastTransitionTime" || path == "Conditions.LastTransitionTime"
		}, cmp.Ignore())) {
			b.config.Gateway.Status = status
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, &b.config.Gateway)
		}
	} else {
		snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, b.gatewayRef())
		if removeFinalizer(&b.config.Gateway) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, &b.config.Gateway)
		}
	}

	return snapshot
}

var (
	errGatewayUnsupportedAddress = errors.New("gateway does not support specifying addresses")
	errGatewayListenersNotValid  = errors.New("one or more listeners are invalid")
)

type gatewayValidationResult struct {
	acceptedErr error
	// TODO: programmed
}

func (l gatewayValidationResult) acceptedCondition(generation int64, listenersInvalid bool) metav1.Condition {
	now := metav1.Now()

	if l.acceptedErr == nil {
		if listenersInvalid {
			return metav1.Condition{
				Type: "Accepted",
				// should one invalid listener cause the entire gateway to become invalid?
				Status:             metav1.ConditionFalse,
				Reason:             "ListenersNotValid",
				ObservedGeneration: generation,
				Message:            errGatewayListenersNotValid.Error(),
				LastTransitionTime: now,
			}
		}

		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			ObservedGeneration: generation,
			Message:            "gateway accepted",
			LastTransitionTime: now,
		}
	}

	if l.acceptedErr == errGatewayUnsupportedAddress {
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "UnsupportedAddress",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}

	// fallback to Invalid reason
	return metav1.Condition{
		Type:               "Accepted",
		Status:             metav1.ConditionFalse,
		Reason:             "Invalid",
		ObservedGeneration: generation,
		Message:            l.acceptedErr.Error(),
		LastTransitionTime: now,
	}
}

func (l gatewayValidationResult) Conditions(generation int64, listenersInvalid bool) []metav1.Condition {
	return []metav1.Condition{
		l.acceptedCondition(generation, listenersInvalid),
	}
}

func validateGateway(gateway gwv1beta1.Gateway) gatewayValidationResult {
	var result gatewayValidationResult

	if len(gateway.Spec.Addresses) > 0 {
		result.acceptedErr = errGatewayUnsupportedAddress
	}

	return result
}

var (
	errListenerUnsupportedProtocol               = errors.New("listener protocol is unsupported")
	errListenerPortUnavailable                   = errors.New("listener port is unavailable")
	errListenerHostnameConflict                  = errors.New("listener hostname conflicts with another listener")
	errListenerProtocolConflict                  = errors.New("listener protocol conflicts with another listener")
	errListenerInvalidCertificateRefNotFound     = errors.New("certificate not found")
	errListenerInvalidCertificateRefNotSupported = errors.New("certificate type is not supported")

	// custom stuff
	errListenerNoTLSPassthrough = errors.New("TLS passthrough is not supported")
)

type listenerValidationResult struct {
	acceptedErr   error
	conflictedErr error
	refErr        error
	// TODO: programmed
}

func (l listenerValidationResult) acceptedCondition(generation int64) metav1.Condition {
	now := metav1.Now()
	switch l.acceptedErr {
	case errListenerPortUnavailable:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "PortUnavailable",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	case errListenerUnsupportedProtocol:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "UnsupportedProtocol",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	case nil:
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			ObservedGeneration: generation,
			Message:            "listener accepted",
			LastTransitionTime: now,
		}
	default:
		// falback to invalid
		return metav1.Condition{
			Type:               "Accepted",
			Status:             metav1.ConditionFalse,
			Reason:             "Invalid",
			ObservedGeneration: generation,
			Message:            l.acceptedErr.Error(),
			LastTransitionTime: now,
		}
	}
}

func (l listenerValidationResult) conflictedCondition(generation int64) metav1.Condition {
	now := metav1.Now()

	switch l.conflictedErr {
	case errListenerProtocolConflict:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionTrue,
			Reason:             "ProtocolConflict",
			ObservedGeneration: generation,
			Message:            l.conflictedErr.Error(),
			LastTransitionTime: now,
		}
	case errListenerHostnameConflict:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionTrue,
			Reason:             "HostnameConflict",
			ObservedGeneration: generation,
			Message:            l.conflictedErr.Error(),
			LastTransitionTime: now,
		}
	default:
		return metav1.Condition{
			Type:               "Conflicted",
			Status:             metav1.ConditionFalse,
			Reason:             "NoConflicts",
			ObservedGeneration: generation,
			Message:            "listener has no conflicts",
			LastTransitionTime: now,
		}
	}
}

func (l listenerValidationResult) resolvedRefsCondition(generation int64) metav1.Condition {
	now := metav1.Now()

	switch l.refErr {
	case errListenerInvalidCertificateRefNotFound:
		return metav1.Condition{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidCertificateRef",
			ObservedGeneration: generation,
			Message:            l.refErr.Error(),
			LastTransitionTime: now,
		}
	case errListenerInvalidCertificateRefNotSupported:
		return metav1.Condition{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidCertificateRef",
			ObservedGeneration: generation,
			Message:            l.refErr.Error(),
			LastTransitionTime: now,
		}
	default:
		return metav1.Condition{
			Type:               "ResolvedRefs",
			Status:             metav1.ConditionTrue,
			Reason:             "ResolvedRefs",
			ObservedGeneration: generation,
			Message:            "resolved certificate references",
			LastTransitionTime: now,
		}
	}
}

func (l listenerValidationResult) Conditions(generation int64) []metav1.Condition {
	return []metav1.Condition{
		l.acceptedCondition(generation),
		l.conflictedCondition(generation),
		l.resolvedRefsCondition(generation),
	}
}

type listenerValidationResults []listenerValidationResult

func (l listenerValidationResults) Invalid() bool {
	for _, r := range l {
		if r.acceptedErr != nil {
			return true
		}
	}
	return false
}

func (l listenerValidationResults) Conditions(generation int64, index int) []metav1.Condition {
	result := l[index]
	return result.Conditions(generation)
}

type mergedListener struct {
	index    int
	listener gwv1beta1.Listener
}

type mergedListeners []mergedListener

func (m mergedListeners) validateProtocol() error {
	var protocol *gwv1beta1.ProtocolType
	for _, l := range m {
		if protocol == nil {
			protocol = pointerTo(l.listener.Protocol)
		}
		if *protocol != l.listener.Protocol {
			return errListenerProtocolConflict
		}
	}
	return nil
}

func bothNilOrEqual[T comparable](one, two *T) bool {
	if one == nil && two == nil {
		return true
	}
	if one == nil {
		return false
	}
	if two == nil {
		return false
	}
	return *one == *two
}

func (m mergedListeners) validateHostname(index int, listener gwv1beta1.Listener) error {
	for _, l := range m {
		if l.index == index {
			continue
		}
		if bothNilOrEqual(listener.Hostname, l.listener.Hostname) {
			return errListenerHostnameConflict
		}
	}
	return nil
}

func validateTLS(namespace string, tls *gwv1beta1.GatewayTLSConfig, certificates []corev1.Secret) (error, error) {
	if tls == nil {
		return nil, nil
	}

	// TODO: Resource Grants

	var err error
MAIN_LOOP:
	for _, ref := range tls.CertificateRefs {
		// break on the first error
		if !nilOrEqual(ref.Group, "") || !nilOrEqual(ref.Kind, "Secret") {
			err = errListenerInvalidCertificateRefNotSupported
			break MAIN_LOOP
		}
		ns := valueOr(ref.Namespace, namespace)

		for _, secret := range certificates {
			if secret.Namespace == ns && secret.Name == string(ref.Name) {
				continue MAIN_LOOP
			}
		}

		// not found, set error
		err = errListenerInvalidCertificateRefNotFound
		break MAIN_LOOP
	}

	if tls.Mode != nil && *tls.Mode == gwv1beta1.TLSModePassthrough {
		return errListenerNoTLSPassthrough, err
	}

	// TODO: validate tls options
	return nil, nil
}

func validateListeners(namespace string, listeners []gwv1beta1.Listener, secrets []corev1.Secret) listenerValidationResults {
	var results listenerValidationResults
	merged := make(map[gwv1beta1.PortNumber]mergedListeners)
	for i, listener := range listeners {
		merged[listener.Port] = append(merged[listener.Port], mergedListener{
			index:    i,
			listener: listener,
		})
	}

	for i, listener := range listeners {
		var result listenerValidationResult

		err, refErr := validateTLS(namespace, listener.TLS, secrets)
		result.refErr = refErr
		if err != nil {
			result.acceptedErr = err
		} else {
			_, supported := supportedKindsForProtocol[listener.Protocol]
			if !supported {
				result.acceptedErr = errListenerUnsupportedProtocol
			} else if listener.Port == 20000 { //admin port
				result.acceptedErr = errListenerPortUnavailable
			}
		}

		if err := merged[listener.Port].validateProtocol(); err != nil {
			result.conflictedErr = err
		} else {
			result.conflictedErr = merged[listener.Port].validateHostname(i, listener)
		}

		results = append(results, result)
	}
	return results
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
