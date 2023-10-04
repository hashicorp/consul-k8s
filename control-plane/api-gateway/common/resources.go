// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	mapset "github.com/deckarep/golang-set"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
)

// ConsulUpdateOperation is an operation representing an
// update in Consul.
type ConsulUpdateOperation struct {
	// Entry is the ConfigEntry to write to Consul.
	Entry api.ConfigEntry
	// OnUpdate is an optional callback to fire after running
	// the Consul update operation. If specified, then no more
	// error handling occurs after the function is called, otherwise
	// normal error handling logic applies.
	OnUpdate func(err error)
}

type gvkNamespacedName struct {
	gvk string
	nsn types.NamespacedName
}

// KubernetesUpdates holds all update operations (including status)
// that need to be synced to Kubernetes. So long as you're
// modifying the same pointer object passed in to its Add
// function, this de-duplicates any calls to Add, in order
// for us to Add any previously unseen entires, but ignore
// them if they've already been added.
type KubernetesUpdates struct {
	operations map[gvkNamespacedName]client.Object
}

func NewKubernetesUpdates() *KubernetesUpdates {
	return &KubernetesUpdates{
		operations: make(map[gvkNamespacedName]client.Object),
	}
}

func (k *KubernetesUpdates) Add(object client.Object) {
	k.operations[gvkNamespacedName{
		gvk: object.GetObjectKind().GroupVersionKind().String(),
		nsn: client.ObjectKeyFromObject(object),
	}] = object
}

func (k *KubernetesUpdates) Operations() []client.Object {
	return ConvertMapValuesToSlice(k.operations)
}

type ReferenceValidator interface {
	GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) bool
	HTTPRouteCanReferenceBackend(httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) bool
	TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) bool
}

type certificate struct {
	secret   *corev1.Secret
	gateways mapset.Set
}

type httpRoute struct {
	route    gwv1beta1.HTTPRoute
	gateways mapset.Set
}

type tcpRoute struct {
	route    gwv1alpha2.TCPRoute
	gateways mapset.Set
}

type consulHTTPRoute struct {
	route    api.HTTPRouteConfigEntry
	gateways mapset.Set
}

type consulTCPRoute struct {
	route    api.TCPRouteConfigEntry
	gateways mapset.Set
}

type resourceSet struct {
	httpRoutes   mapset.Set
	tcpRoutes    mapset.Set
	certificates mapset.Set

	consulObjects *ReferenceSet
}

type ResourceMap struct {
	translator         ResourceTranslator
	referenceValidator ReferenceValidator
	logger             logr.Logger

	services     map[types.NamespacedName]api.ResourceReference
	meshServices map[types.NamespacedName]api.ResourceReference
	certificates mapset.Set

	// this acts a a secondary store of what has not yet
	// been processed for the sake of garbage collection.
	processedCertificates mapset.Set
	certificateGateways   map[api.ResourceReference]*certificate
	tcpRouteGateways      map[api.ResourceReference]*tcpRoute
	httpRouteGateways     map[api.ResourceReference]*httpRoute
	gatewayResources      map[api.ResourceReference]*resourceSet

	// consul resources for a gateway
	consulTCPRoutes  map[api.ResourceReference]*consulTCPRoute
	consulHTTPRoutes map[api.ResourceReference]*consulHTTPRoute

	// mutations
	consulMutations []*ConsulUpdateOperation
}

func NewResourceMap(translator ResourceTranslator, validator ReferenceValidator, logger logr.Logger) *ResourceMap {
	return &ResourceMap{
		translator:            translator,
		referenceValidator:    validator,
		logger:                logger,
		processedCertificates: mapset.NewSet(),
		services:              make(map[types.NamespacedName]api.ResourceReference),
		meshServices:          make(map[types.NamespacedName]api.ResourceReference),
		certificates:          mapset.NewSet(),
		consulTCPRoutes:       make(map[api.ResourceReference]*consulTCPRoute),
		consulHTTPRoutes:      make(map[api.ResourceReference]*consulHTTPRoute),
		certificateGateways:   make(map[api.ResourceReference]*certificate),
		tcpRouteGateways:      make(map[api.ResourceReference]*tcpRoute),
		httpRouteGateways:     make(map[api.ResourceReference]*httpRoute),
		gatewayResources:      make(map[api.ResourceReference]*resourceSet),
	}
}

func (s *ResourceMap) AddService(id types.NamespacedName, name string) {
	// this needs to be not-normalized since it gets written straight
	// to Consul's configuration, including in non-enterprise builds.
	s.services[id] = api.ResourceReference{
		Name:      name,
		Namespace: s.translator.Namespace(id.Namespace),
		Partition: s.translator.ConsulPartition,
	}
}

func (s *ResourceMap) Service(id types.NamespacedName) api.ResourceReference {
	return s.services[id]
}

func (s *ResourceMap) HasService(id types.NamespacedName) bool {
	_, ok := s.services[id]
	return ok
}

func (s *ResourceMap) AddMeshService(service v1alpha1.MeshService) {
	// this needs to be not-normalized since it gets written straight
	// to Consul's configuration, including in non-enterprise builds.
	key := client.ObjectKeyFromObject(&service)
	s.meshServices[key] = api.ResourceReference{
		Name:      service.Spec.Name,
		Namespace: s.translator.Namespace(service.Namespace),
		Partition: s.translator.ConsulPartition,
	}
}

func (s *ResourceMap) MeshService(id types.NamespacedName) api.ResourceReference {
	return s.meshServices[id]
}

func (s *ResourceMap) HasMeshService(id types.NamespacedName) bool {
	_, ok := s.meshServices[id]
	return ok
}

func (s *ResourceMap) Certificate(key types.NamespacedName) *corev1.Secret {
	if !s.certificates.Contains(key) {
		return nil
	}
	consulKey := NormalizeMeta(s.toConsulReference(api.InlineCertificate, key))
	if secret, ok := s.certificateGateways[consulKey]; ok {
		return secret.secret
	}
	return nil
}

func (s *ResourceMap) ReferenceCountCertificate(secret corev1.Secret) {
	key := client.ObjectKeyFromObject(&secret)
	s.certificates.Add(key)
	consulKey := NormalizeMeta(s.toConsulReference(api.InlineCertificate, key))
	if _, ok := s.certificateGateways[consulKey]; !ok {
		s.certificateGateways[consulKey] = &certificate{
			secret:   &secret,
			gateways: mapset.NewSet(),
		}
	}
}

func (s *ResourceMap) ReferenceCountGateway(gateway gwv1beta1.Gateway) {
	key := client.ObjectKeyFromObject(&gateway)
	consulKey := NormalizeMeta(s.toConsulReference(api.APIGateway, key))

	set := &resourceSet{
		httpRoutes:    mapset.NewSet(),
		tcpRoutes:     mapset.NewSet(),
		certificates:  mapset.NewSet(),
		consulObjects: NewReferenceSet(),
	}

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil || (listener.TLS.Mode != nil && *listener.TLS.Mode != gwv1beta1.TLSModeTerminate) {
			continue
		}
		for _, cert := range listener.TLS.CertificateRefs {
			if NilOrEqual(cert.Group, "") && NilOrEqual(cert.Kind, "Secret") {
				certificateKey := IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace)

				set.certificates.Add(certificateKey)

				consulCertificateKey := s.toConsulReference(api.InlineCertificate, certificateKey)
				certificate, ok := s.certificateGateways[NormalizeMeta(consulCertificateKey)]
				if ok {
					certificate.gateways.Add(key)
					set.consulObjects.Mark(consulCertificateKey)
				}
			}
		}
	}

	s.gatewayResources[consulKey] = set
}

func (s *ResourceMap) ResourcesToGC(key types.NamespacedName) []api.ResourceReference {
	consulKey := NormalizeMeta(s.toConsulReference(api.APIGateway, key))

	resources, ok := s.gatewayResources[consulKey]
	if !ok {
		return nil
	}

	var toGC []api.ResourceReference

	for _, id := range resources.consulObjects.IDs() {
		// if any of these objects exist in the below maps
		// it means we haven't "popped" it to be created
		switch id.Kind {
		case api.HTTPRoute:
			if route, ok := s.consulHTTPRoutes[NormalizeMeta(id)]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
		case api.TCPRoute:
			if route, ok := s.consulTCPRoutes[NormalizeMeta(id)]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
		case api.InlineCertificate:
			if s.processedCertificates.Contains(id) {
				continue
			}
			if route, ok := s.certificateGateways[NormalizeMeta(id)]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
		}
	}

	return toGC
}

func (s *ResourceMap) ReferenceCountConsulHTTPRoute(route api.HTTPRouteConfigEntry) {
	key := s.objectReference(&route)

	set := &consulHTTPRoute{
		route:    route,
		gateways: mapset.NewSet(),
	}

	for gatewayKey := range s.consulGatewaysForRoute(route.Namespace, route.Parents).Iter() {
		if gateway, ok := s.gatewayResources[gatewayKey.(api.ResourceReference)]; ok {
			gateway.consulObjects.Mark(key)
		}

		set.gateways.Add(gatewayKey)
	}

	s.consulHTTPRoutes[NormalizeMeta(key)] = set
}

func (s *ResourceMap) ReferenceCountConsulTCPRoute(route api.TCPRouteConfigEntry) {
	key := s.objectReference(&route)

	set := &consulTCPRoute{
		route:    route,
		gateways: mapset.NewSet(),
	}

	for gatewayKey := range s.consulGatewaysForRoute(route.Namespace, route.Parents).Iter() {
		if gateway, ok := s.gatewayResources[gatewayKey.(api.ResourceReference)]; ok {
			gateway.consulObjects.Mark(key)
		}

		set.gateways.Add(gatewayKey)
	}

	s.consulTCPRoutes[NormalizeMeta(key)] = set
}

func (s *ResourceMap) ReferenceCountConsulCertificate(cert api.InlineCertificateConfigEntry) {
	key := s.objectReference(&cert)

	var referenced *certificate
	if existing, ok := s.certificateGateways[NormalizeMeta(key)]; ok {
		referenced = existing
	} else {
		referenced = &certificate{
			gateways: mapset.NewSet(),
		}
	}

	s.certificateGateways[NormalizeMeta(key)] = referenced
}

func (s *ResourceMap) consulGatewaysForRoute(namespace string, refs []api.ResourceReference) mapset.Set {
	gateways := mapset.NewSet()

	for _, parent := range refs {
		if EmptyOrEqual(parent.Kind, api.APIGateway) {
			key := s.sectionlessParentReference(api.APIGateway, namespace, parent)
			gateways.Add(key)
		}
	}

	return gateways
}

func (s *ResourceMap) ReferenceCountHTTPRoute(route gwv1beta1.HTTPRoute) {
	key := client.ObjectKeyFromObject(&route)
	consulKey := NormalizeMeta(s.toConsulReference(api.HTTPRoute, key))

	set := &httpRoute{
		route:    route,
		gateways: mapset.NewSet(),
	}

	for gatewayKey := range s.gatewaysForRoute(route.Namespace, route.Spec.ParentRefs).Iter() {
		set.gateways.Add(gatewayKey.(api.ResourceReference))

		gateway := s.gatewayResources[gatewayKey.(api.ResourceReference)]
		gateway.httpRoutes.Add(consulKey)
	}

	s.httpRouteGateways[consulKey] = set
}

func (s *ResourceMap) ReferenceCountTCPRoute(route gwv1alpha2.TCPRoute) {
	key := client.ObjectKeyFromObject(&route)
	consulKey := NormalizeMeta(s.toConsulReference(api.TCPRoute, key))

	set := &tcpRoute{
		route:    route,
		gateways: mapset.NewSet(),
	}

	for gatewayKey := range s.gatewaysForRoute(route.Namespace, route.Spec.ParentRefs).Iter() {
		set.gateways.Add(gatewayKey.(api.ResourceReference))

		gateway := s.gatewayResources[gatewayKey.(api.ResourceReference)]
		gateway.tcpRoutes.Add(consulKey)
	}

	s.tcpRouteGateways[consulKey] = set
}

func (s *ResourceMap) gatewaysForRoute(namespace string, refs []gwv1beta1.ParentReference) mapset.Set {
	gateways := mapset.NewSet()

	for _, parent := range refs {
		if NilOrEqual(parent.Group, gwv1beta1.GroupVersion.Group) && NilOrEqual(parent.Kind, "Gateway") {
			key := IndexedNamespacedNameWithDefault(parent.Name, parent.Namespace, namespace)
			consulKey := NormalizeMeta(s.toConsulReference(api.APIGateway, key))

			if _, ok := s.gatewayResources[consulKey]; ok {
				gateways.Add(consulKey)
			}
		}
	}

	return gateways
}

func (s *ResourceMap) TranslateAndMutateHTTPRoute(key types.NamespacedName, onUpdate func(error, api.ConfigEntryStatus), mutateFn func(old *api.HTTPRouteConfigEntry, new api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry) {
	consulKey := NormalizeMeta(s.toConsulReference(api.HTTPRoute, key))

	route, ok := s.httpRouteGateways[consulKey]
	if !ok {
		return
	}

	translated := s.translator.ToHTTPRoute(route.route, s)

	consulRoute, ok := s.consulHTTPRoutes[consulKey]
	if ok {
		mutated := mutateFn(&consulRoute.route, *translated)
		if len(mutated.Parents) != 0 {
			// if we don't have any parents set, we keep this around to allow the route
			// to be GC'd.
			delete(s.consulHTTPRoutes, consulKey)
			s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
				Entry: &mutated,
				OnUpdate: func(err error) {
					onUpdate(err, mutated.Status)
				},
			})
		}
		return
	}
	mutated := mutateFn(nil, *translated)
	if len(mutated.Parents) != 0 {
		// if we don't have any parents set, we keep this around to allow the route
		// to be GC'd.
		delete(s.consulHTTPRoutes, consulKey)
		s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
			Entry: &mutated,
			OnUpdate: func(err error) {
				onUpdate(err, mutated.Status)
			},
		})
	}
}

func (s *ResourceMap) MutateHTTPRoute(key types.NamespacedName, onUpdate func(error, api.ConfigEntryStatus), mutateFn func(api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry) {
	consulKey := NormalizeMeta(s.toConsulReference(api.HTTPRoute, key))

	consulRoute, ok := s.consulHTTPRoutes[consulKey]
	if ok {
		mutated := mutateFn(consulRoute.route)
		if len(mutated.Parents) != 0 {
			// if we don't have any parents set, we keep this around to allow the route
			// to be GC'd.
			delete(s.consulHTTPRoutes, consulKey)
			s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
				Entry: &mutated,
				OnUpdate: func(err error) {
					onUpdate(err, mutated.Status)
				},
			})
		}
	}
}

func (s *ResourceMap) CanGCHTTPRouteOnUnbind(id api.ResourceReference) bool {
	if set := s.httpRouteGateways[NormalizeMeta(id)]; set != nil {
		return set.gateways.Cardinality() <= 1
	}
	return true
}

func (s *ResourceMap) TranslateAndMutateTCPRoute(key types.NamespacedName, onUpdate func(error, api.ConfigEntryStatus), mutateFn func(*api.TCPRouteConfigEntry, api.TCPRouteConfigEntry) api.TCPRouteConfigEntry) {
	consulKey := NormalizeMeta(s.toConsulReference(api.TCPRoute, key))

	route, ok := s.tcpRouteGateways[consulKey]
	if !ok {
		return
	}

	translated := s.translator.ToTCPRoute(route.route, s)

	consulRoute, ok := s.consulTCPRoutes[consulKey]
	if ok {
		mutated := mutateFn(&consulRoute.route, *translated)
		if len(mutated.Parents) != 0 {
			// if we don't have any parents set, we keep this around to allow the route
			// to be GC'd.
			delete(s.consulTCPRoutes, consulKey)
			s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
				Entry: &mutated,
				OnUpdate: func(err error) {
					onUpdate(err, mutated.Status)
				},
			})
		}
		return
	}
	mutated := mutateFn(nil, *translated)
	if len(mutated.Parents) != 0 {
		// if we don't have any parents set, we keep this around to allow the route
		// to be GC'd.
		delete(s.consulTCPRoutes, consulKey)
		s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
			Entry: &mutated,
			OnUpdate: func(err error) {
				onUpdate(err, mutated.Status)
			},
		})
	}
}

func (s *ResourceMap) MutateTCPRoute(key types.NamespacedName, onUpdate func(error, api.ConfigEntryStatus), mutateFn func(api.TCPRouteConfigEntry) api.TCPRouteConfigEntry) {
	consulKey := NormalizeMeta(s.toConsulReference(api.TCPRoute, key))

	consulRoute, ok := s.consulTCPRoutes[consulKey]
	if ok {
		mutated := mutateFn(consulRoute.route)
		if len(mutated.Parents) != 0 {
			// if we don't have any parents set, we keep this around to allow the route
			// to be GC'd.
			delete(s.consulTCPRoutes, consulKey)
			s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
				Entry: &mutated,
				OnUpdate: func(err error) {
					onUpdate(err, mutated.Status)
				},
			})
		}
	}
}

func (s *ResourceMap) CanGCTCPRouteOnUnbind(id api.ResourceReference) bool {
	if set := s.tcpRouteGateways[NormalizeMeta(id)]; set != nil {
		return set.gateways.Cardinality() <= 1
	}
	return true
}

func (s *ResourceMap) TranslateInlineCertificate(key types.NamespacedName) error {
	consulKey := s.toConsulReference(api.InlineCertificate, key)

	certificate, ok := s.certificateGateways[NormalizeMeta(consulKey)]
	if !ok {
		return nil
	}

	if certificate.secret == nil {
		return nil
	}

	consulCertificate, err := s.translator.ToInlineCertificate(*certificate.secret)
	if err != nil {
		return err
	}

	// add to the processed set so we don't GC it.
	s.processedCertificates.Add(consulKey)
	s.consulMutations = append(s.consulMutations, &ConsulUpdateOperation{
		Entry: consulCertificate,
		// just swallow the error and log it since we can't propagate status back on a certificate.
		OnUpdate: func(error) {
			if err != nil {
				s.logger.Error(err, "error syncing certificate to Consul")
			}
		},
	})

	return nil
}

func (s *ResourceMap) Mutations() []*ConsulUpdateOperation {
	return s.consulMutations
}

func (s *ResourceMap) objectReference(o api.ConfigEntry) api.ResourceReference {
	return api.ResourceReference{
		Kind:      o.GetKind(),
		Name:      o.GetName(),
		Namespace: o.GetNamespace(),
		Partition: s.translator.ConsulPartition,
	}
}

func (s *ResourceMap) sectionlessParentReference(kind, namespace string, parent api.ResourceReference) api.ResourceReference {
	return NormalizeMeta(api.ResourceReference{
		Kind:      kind,
		Name:      parent.Name,
		Namespace: orDefault(parent.Namespace, namespace),
		Partition: s.translator.ConsulPartition,
	})
}

func (s *ResourceMap) toConsulReference(kind string, key types.NamespacedName) api.ResourceReference {
	return api.ResourceReference{
		Kind:      kind,
		Name:      key.Name,
		Namespace: s.translator.Namespace(key.Namespace),
		Partition: s.translator.ConsulPartition,
	}
}

func (s *ResourceMap) GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, ref gwv1beta1.SecretObjectReference) bool {
	return s.referenceValidator.GatewayCanReferenceSecret(gateway, ref)
}

func (s *ResourceMap) HTTPRouteCanReferenceBackend(route gwv1beta1.HTTPRoute, ref gwv1beta1.BackendRef) bool {
	return s.referenceValidator.HTTPRouteCanReferenceBackend(route, ref)
}

func (s *ResourceMap) TCPRouteCanReferenceBackend(route gwv1alpha2.TCPRoute, ref gwv1beta1.BackendRef) bool {
	return s.referenceValidator.TCPRouteCanReferenceBackend(route, ref)
}
