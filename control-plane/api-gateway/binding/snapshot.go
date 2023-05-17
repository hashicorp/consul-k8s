package binding

import (
	"errors"
	"fmt"
	"sort"
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
			Group: (*gwv1beta1.Group)(&gwv1beta1.GroupVersion.Group),
			Kind:  "TCPRoute",
		}},
	}
)

type KubernetesSnapshot struct {
	Updates       []client.Object
	StatusUpdates []client.Object
}

type ConsulSnapshot struct {
	Updates   []api.ConfigEntry
	Deletions []api.ResourceReference
}

type Snapshot struct {
	Kubernetes KubernetesSnapshot
	Consul     ConsulSnapshot
}

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

type Binder struct {
	config BinderConfig
}

func NewBinder(config BinderConfig) *Binder {
	return &Binder{config: config}
}

func (b *Binder) consulHTTPRouteFor(ref api.ResourceReference) *api.HTTPRouteConfigEntry {
	for _, route := range b.config.ConsulHTTPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

func (b *Binder) consulTCPRouteFor(ref api.ResourceReference) *api.TCPRouteConfigEntry {
	for _, route := range b.config.ConsulTCPRoutes {
		if route.Namespace == ref.Namespace && route.Partition == ref.Partition && route.Name == ref.Name {
			return &route
		}
	}
	return nil
}

type referenceTracker struct {
	httpRouteReferencesGateways      map[types.NamespacedName]int
	tcpRouteReferencesGateways       map[types.NamespacedName]int
	certificatesReferencedByGateways map[types.NamespacedName]int
}

func (r referenceTracker) isLastReference(object client.Object) bool {
	key := types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}

	switch object.(type) {
	case *gwv1alpha2.TCPRoute:
		return r.tcpRouteReferencesGateways[key] == 1
	case *gwv1beta1.HTTPRoute:
		return r.httpRouteReferencesGateways[key] == 1
	default:
		return false
	}
}

func (r referenceTracker) canGCSecret(key types.NamespacedName) bool {
	return r.certificatesReferencedByGateways[key] == 0
}

func (b *Binder) references() referenceTracker {
	tracker := referenceTracker{
		httpRouteReferencesGateways:      make(map[types.NamespacedName]int),
		tcpRouteReferencesGateways:       make(map[types.NamespacedName]int),
		certificatesReferencedByGateways: make(map[types.NamespacedName]int),
	}

	for _, route := range b.config.HTTPRoutes {
		references := map[types.NamespacedName]struct{}{}
		for _, ref := range route.Spec.ParentRefs {
			for _, gateway := range b.config.ControlledGateways {
				parentName := string(ref.Name)
				parentNamespace := valueOr(ref.Namespace, route.Namespace)
				if nilOrEqual(ref.Group, betaGroup) &&
					nilOrEqual(ref.Kind, kindGateway) &&
					gateway.Namespace == parentNamespace &&
					gateway.Name == parentName {
					// the route references a gateway we control, store the ref to this gateway
					references[types.NamespacedName{
						Namespace: parentNamespace,
						Name:      parentName,
					}] = struct{}{}
				}
			}
		}
		tracker.httpRouteReferencesGateways[types.NamespacedName{
			Namespace: route.Namespace,
			Name:      route.Name,
		}] = len(references)
	}

	for _, route := range b.config.TCPRoutes {
		references := map[types.NamespacedName]struct{}{}
		for _, ref := range route.Spec.ParentRefs {
			for _, gateway := range b.config.ControlledGateways {
				parentName := string(ref.Name)
				parentNamespace := valueOr(ref.Namespace, route.Namespace)
				if nilOrEqual(ref.Group, betaGroup) &&
					nilOrEqual(ref.Kind, kindGateway) &&
					gateway.Namespace == parentNamespace &&
					gateway.Name == parentName {
					// the route references a gateway we control, store the ref to this gateway
					references[types.NamespacedName{
						Namespace: parentNamespace,
						Name:      parentName,
					}] = struct{}{}
				}
			}
		}
		tracker.tcpRouteReferencesGateways[types.NamespacedName{
			Namespace: route.Namespace,
			Name:      route.Name,
		}] = len(references)
	}

	for _, gateway := range b.config.ControlledGateways {
		references := map[types.NamespacedName]struct{}{}
		for _, listener := range gateway.Spec.Listeners {
			if listener.TLS == nil {
				continue
			}
			for _, ref := range listener.TLS.CertificateRefs {
				if nilOrEqual(ref.Group, "") &&
					nilOrEqual(ref.Kind, kindSecret) {
					// the gateway references a secret, store it
					references[types.NamespacedName{
						Namespace: valueOr(ref.Namespace, gateway.Namespace),
						Name:      string(ref.Name),
					}] = struct{}{}
				}
			}
		}

		for ref := range references {
			count := tracker.certificatesReferencedByGateways[ref]
			tracker.certificatesReferencedByGateways[ref] = count + 1
		}
	}

	return tracker
}

func (b *Binder) Snapshot() Snapshot {
	// at this point we assume all tcp routes and http routes
	// actually reference this gateway
	tracker := b.references()
	seenRoutes := map[api.ResourceReference]struct{}{}
	snapshot := Snapshot{}

	gatewayRef := b.config.Translator.ReferenceForGateway(b.config.Gateway)
	gatewayClassMismatch := b.config.GatewayClass == nil || b.config.ControllerName != string(b.config.GatewayClass.Spec.ControllerName)

	isGatewayDeleted := isDeleted(&b.config.Gateway) || gatewayClassMismatch
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

	for _, r := range b.config.HTTPRoutes {
		route := &r
		routeRef := b.config.Translator.ReferenceForHTTPRoute(r)

		existing := b.consulHTTPRouteFor(routeRef)
		seenRoutes[routeRef] = struct{}{}
		gatewayRefs := filterParentRefs(objectToMeta(&b.config.Gateway), route.Namespace, route.Spec.ParentRefs)

		if isDeleted(route) {
			snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
			if removeFinalizer(route) {
				snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
			}
			// TODO: drop the number of bound routes from the gateway if necessary
			continue
		}

		if isGatewayDeleted {
			// first check if this is our only ref for the route
			if tracker.isLastReference(route) {
				// if it is, then mark everything for deletion
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
				if removeFinalizer(route) {
					snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
				}
				continue
			}

			// otherwise remove the condition since we no longer know if we should
			// control the route and drop any references for the Consul route
			if existing != nil {
				// this drops all the parent refs
				existing.Parents = parentsForRoute(gatewayRef, existing.Parents, nil)
				// and then we mark the route as needing updated
				snapshot.Consul.Updates = append(snapshot.Consul.Updates, existing)
				// drop the status conditions
				if b.config.Setter.RemoveHTTPRouteReferences(route, gatewayRefs) {
					snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
				}
			}
			continue
		}

		if ensureFinalizer(route) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates)
			continue
		}

		// TODO: add validation statuses for routes for referenced services
		results := bindRoute(&b.config.Gateway, b.config.Namespaces[route.GetNamespace()], route.GroupVersionKind().GroupKind(), route.Spec.Hostnames, route.Spec.ParentRefs)
		// TODO: increment the number of bound routes if necessary

		updated := false
		for ref, result := range results {
			if b.config.Setter.SetHTTPRouteCondition(route, ref, result.Condition()) {
				updated = true
			}
		}

		if updated {
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
		}

		entry := b.config.Translator.HTTPRouteToHTTPRoute(*route, nil)
		// make all parent refs explicit based on what actually bound
		if existing != nil {
			entry.Parents = parentsForRoute(gatewayRef, nil, results)
		} else {
			entry.Parents = parentsForRoute(gatewayRef, existing.Parents, results)
		}
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &entry)
	}

	for _, r := range b.config.TCPRoutes {
		route := &r
		routeRef := b.config.Translator.ReferenceForTCPRoute(r)

		existing := b.consulTCPRouteFor(routeRef)
		seenRoutes[routeRef] = struct{}{}
		gatewayRefs := filterParentRefs(objectToMeta(&b.config.Gateway), route.Namespace, route.Spec.ParentRefs)

		if isDeleted(route) {
			snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
			if removeFinalizer(route) {
				snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
			}
			// TODO: drop the number of bound routes from the gateway if necessary
			continue
		}

		if isGatewayDeleted {
			// first check if this is our only ref for the route
			if tracker.isLastReference(route) {
				// if it is, then mark everything for deletion
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
				if removeFinalizer(route) {
					snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates, route)
				}
				continue
			}

			// otherwise remove the condition since we no longer know if we should
			// control the route and drop any references for the Consul route
			if existing != nil {
				// this drops all the parent refs
				existing.Parents = parentsForRoute(gatewayRef, existing.Parents, nil)
				// and then we mark the route as needing updated
				snapshot.Consul.Updates = append(snapshot.Consul.Updates, existing)
				// drop the status conditions
				if b.config.Setter.RemoveTCPRouteReferences(route, gatewayRefs) {
					snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
				}
			}
			continue
		}

		if ensureFinalizer(route) {
			snapshot.Kubernetes.Updates = append(snapshot.Kubernetes.Updates)
			continue
		}

		// TODO: add validation statuses for routes for referenced services
		results := bindRoute(&b.config.Gateway, b.config.Namespaces[route.GetNamespace()], route.GroupVersionKind().GroupKind(), nil, route.Spec.ParentRefs)
		// TODO: increment the number of bound routes if necessary

		updated := false
		for ref, result := range results {
			if b.config.Setter.SetTCPRouteCondition(route, ref, result.Condition()) {
				updated = true
			}
		}

		if updated {
			snapshot.Kubernetes.StatusUpdates = append(snapshot.Kubernetes.StatusUpdates, route)
		}

		entry := b.config.Translator.TCPRouteToTCPRoute(*route, nil)
		// make all parent refs explicit based on what actually bound
		if existing != nil {
			entry.Parents = parentsForRoute(gatewayRef, nil, results)
		} else {
			entry.Parents = parentsForRoute(gatewayRef, existing.Parents, results)
		}
		snapshot.Consul.Updates = append(snapshot.Consul.Updates, &entry)
	}

	for _, route := range b.config.ConsulHTTPRoutes {
		routeRef := translation.EntryToReference(&route)
		if _, ok := seenRoutes[routeRef]; !ok {
			parents := parentsForRoute(gatewayRef, route.Parents, nil)
			if len(parents) == 0 {
				// we can GC this now since we've dropped all refs from it
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
			} else if len(route.Parents) != len(parents) {
				// we've mutated the length, which means this route needs an update
				route.Parents = parents
				snapshot.Consul.Updates = append(snapshot.Consul.Updates, pointerTo(route))
			}
		}
	}

	for _, route := range b.config.ConsulTCPRoutes {
		routeRef := translation.EntryToReference(&route)
		if _, ok := seenRoutes[routeRef]; !ok {
			parents := parentsForRoute(gatewayRef, route.Parents, nil)
			if len(parents) == 0 {
				// we can GC this now since we've dropped all refs from it
				snapshot.Consul.Deletions = append(snapshot.Consul.Deletions, routeRef)
			} else if len(route.Parents) != len(parents) {
				// we've mutated the length, which means this route needs an update
				route.Parents = parents
				snapshot.Consul.Updates = append(snapshot.Consul.Updates, pointerTo(route))
			}
		}
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

func parentsForRoute(ref api.ResourceReference, existing []api.ResourceReference, results map[gwv1beta1.ParentReference]bindResult) []api.ResourceReference {
	// store all section names that bound
	parentSet := map[string]struct{}{}
	for _, result := range results {
		for section, err := range result {
			if err != nil {
				parentSet[string(section)] = struct{}{}
			}
		}
	}

	// first, filter out all of the parent refs that don't correspond to this gateway
	parents := []api.ResourceReference{}
	for _, parent := range existing {
		if parent.Kind == api.APIGateway &&
			parent.Name == ref.Name &&
			parent.Namespace == ref.Namespace {
			continue
		}
		parents = append(parents, parent)
	}

	// now construct the bound set
	for parent := range parentSet {
		parents = append(parents, api.ResourceReference{
			Kind:        api.APIGateway,
			Name:        ref.Name,
			Namespace:   ref.Namespace,
			SectionName: parent,
		})
	}
	return parents
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

type bindResult map[gwv1beta1.SectionName]error

func (b bindResult) Error() string {
	messages := []string{}
	for section, err := range b {
		if err != nil {
			messages = append(messages, fmt.Sprintf("%s: %s", section, err.Error()))
		}
	}

	sort.Strings(messages)
	return strings.Join(messages, "; ")
}

func (b bindResult) DidBind() bool {
	for _, err := range b {
		if err == nil {
			return true
		}
	}
	return false
}

func (b bindResult) Condition() metav1.Condition {
	// if we bound to any listeners, say we're accepted
	if b.DidBind() {
		return metav1.Condition{
			Type:    "Accepted",
			Status:  metav1.ConditionTrue,
			Reason:  "Accepted",
			Message: "route accepted",
		}
	}

	// default to the most generic reason in the spec "NotAllowedByListeners"
	reason := "NotAllowedByListeners"

	// if we only have a single binding error, we can get more specific
	if len(b) == 1 {
		for _, err := range b {
			// if we have a hostname mismatch error, then use the more specific reason
			if err == errNoMatchingListenerHostname {
				reason = "NoMatchingListenerHostname"
			}
		}
	}

	return metav1.Condition{
		Type:    "Accepted",
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: b.Error(),
	}
}

func bindRoute(gateway *gwv1beta1.Gateway, namespace corev1.Namespace, gk schema.GroupKind, hostnames []gwv1beta1.Hostname, refs []gwv1beta1.ParentReference) map[gwv1beta1.ParentReference]bindResult {
	results := map[gwv1beta1.ParentReference]bindResult{}
	for _, ref := range filterParentRefs(objectToMeta(gateway), namespace.Name, refs) {
		result := make(bindResult)

		for _, listener := range listenersFor(gateway, ref.SectionName) {
			if !routeKindIsAllowedForListener(supportedKindsForProtocol[listener.Protocol], gk) {
				result[listener.Name] = errNotAllowedByListenerProtocol
				continue
			}

			if !routeAllowedForListenerNamespaces(gateway.Namespace, listener.AllowedRoutes, namespace) {
				result[listener.Name] = errNotAllowedByListenerNamespace
				continue
			}

			if !routeAllowedForListenerHostname(listener.Hostname, hostnames) {
				result[listener.Name] = errNoMatchingListenerHostname
				continue
			}

			result[listener.Name] = nil
		}

		results[ref] = result
	}

	return results
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
