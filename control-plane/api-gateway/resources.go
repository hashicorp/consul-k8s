package apigateway

import (
	"encoding/json"
	"fmt"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

type certificate struct {
	secret   corev1.Secret
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
	translator ResourceTranslator

	services     map[types.NamespacedName]api.ResourceReference
	meshServices map[types.NamespacedName]api.ResourceReference

	certificateGateways map[api.ResourceReference]*certificate
	tcpRouteGateways    map[api.ResourceReference]*tcpRoute
	httpRouteGateways   map[api.ResourceReference]*httpRoute
	gatewayResources    map[api.ResourceReference]*resourceSet

	// consul resources for a gateway
	consulTCPRoutes          map[api.ResourceReference]*consulTCPRoute
	consulHTTPRoutes         map[api.ResourceReference]*consulHTTPRoute
	consulInlineCertificates map[api.ResourceReference]mapset.Set

	// mutations
	consulMutations []api.ConfigEntry
}

func NewResourceMap(translator ResourceTranslator) *ResourceMap {
	return &ResourceMap{
		translator:               translator,
		services:                 make(map[types.NamespacedName]api.ResourceReference),
		meshServices:             make(map[types.NamespacedName]api.ResourceReference),
		consulTCPRoutes:          make(map[api.ResourceReference]*consulTCPRoute),
		consulHTTPRoutes:         make(map[api.ResourceReference]*consulHTTPRoute),
		consulInlineCertificates: make(map[api.ResourceReference]mapset.Set),
		certificateGateways:      make(map[api.ResourceReference]*certificate),
		tcpRouteGateways:         make(map[api.ResourceReference]*tcpRoute),
		httpRouteGateways:        make(map[api.ResourceReference]*httpRoute),
		gatewayResources:         make(map[api.ResourceReference]*resourceSet),
	}
}

func (s *ResourceMap) AddService(id types.NamespacedName, name string) {
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
	fmt.Println("SERVICE CHECK", id)
	_, ok := s.services[id]
	return ok
}

func (s *ResourceMap) AddMeshService(service v1alpha1.MeshService) {
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

func (s *ResourceMap) Certificate(ref api.ResourceReference) *corev1.Secret {
	if secret, ok := s.certificateGateways[NormalizeMeta(ref)]; ok {
		return &secret.secret
	}
	return nil
}

func (s *ResourceMap) ReferenceCountCertificate(secret corev1.Secret) {
	key := client.ObjectKeyFromObject(&secret)
	consulKey := s.toConsulReference(api.InlineCertificate, key)

	if _, ok := s.certificateGateways[consulKey]; ok {
		s.certificateGateways[consulKey] = &certificate{
			secret:   secret,
			gateways: mapset.NewSet(),
		}
	}
}

func (s *ResourceMap) ReferenceCountGateway(gateway gwv1beta1.Gateway) {
	key := client.ObjectKeyFromObject(&gateway)
	consulKey := s.toConsulReference(api.APIGateway, key)

	set := &resourceSet{
		httpRoutes:    mapset.NewSet(),
		tcpRoutes:     mapset.NewSet(),
		certificates:  mapset.NewSet(),
		consulObjects: NewReferenceSet(),
	}

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil || *listener.TLS.Mode != gwv1beta1.TLSModeTerminate {
			continue
		}
		for _, cert := range listener.TLS.CertificateRefs {
			if NilOrEqual(cert.Group, "") && NilOrEqual(cert.Kind, "Secret") {
				certificateKey := IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace)

				set.certificates.Add(certificateKey)

				certificate, ok := s.certificateGateways[s.toConsulReference(
					api.InlineCertificate,
					certificateKey,
				)]
				if ok {
					certificate.gateways.Add(key)
				}
			}
		}
	}

	s.gatewayResources[consulKey] = set
}

func (s *ResourceMap) ResourcesToGC(key types.NamespacedName) []api.ResourceReference {
	consulKey := s.toConsulReference(api.APIGateway, key)
	resources := s.gatewayResources[consulKey]

	var toGC []api.ResourceReference

	fmt.Println("ITERATING")
	for id := range resources.consulObjects.data {
		// if any of these objects exist in the below maps
		// it means we haven't "popped" it to be created
		switch id.Kind {
		case api.HTTPRoute:
			fmt.Println("HTTPROUTE START")
			if route, ok := s.consulHTTPRoutes[id]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
			fmt.Println("HTTPROUTE DONE")
		case api.TCPRoute:
			fmt.Println("TCPROUTE START")
			if route, ok := s.consulTCPRoutes[id]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
			fmt.Println("TCPROUTE DONE")
		case api.InlineCertificate:
			fmt.Println("CERT START")
			if route, ok := s.certificateGateways[id]; ok && route.gateways.Cardinality() <= 1 {
				// we only have a single reference, which will be this gateway, so drop
				// the route altogether
				toGC = append(toGC, id)
			}
			fmt.Println("CERT DONE")
		}
	}
	fmt.Println("ITERATION DONE")

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

	s.consulHTTPRoutes[key] = set
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

	s.consulTCPRoutes[key] = set
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
	consulKey := s.toConsulReference(api.HTTPRoute, key)

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
	consulKey := s.toConsulReference(api.TCPRoute, key)

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
			consulKey := s.toConsulReference(api.APIGateway, key)

			if _, ok := s.gatewayResources[consulKey]; ok {
				gateways.Add(consulKey)
			}
		}
	}

	return gateways
}

func (s *ResourceMap) TranslateAndMutateHTTPRoute(key types.NamespacedName, mutateFn func(old *api.HTTPRouteConfigEntry, new api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry) {
	consulKey := s.toConsulReference(api.HTTPRoute, key)

	route, ok := s.httpRouteGateways[consulKey]
	if !ok {
		return
	}

	translated := s.translator.ToHTTPRoute(route.route, s)

	consulRoute, ok := s.consulHTTPRoutes[consulKey]
	if ok {
		// remove from the consulHTTPRoutes map since we don't want to
		// GC it in the end
		delete(s.consulHTTPRoutes, consulKey)
		mutated := mutateFn(&consulRoute.route, *translated)
		s.consulMutations = append(s.consulMutations, &mutated)
		return
	}
	mutated := mutateFn(nil, *translated)
	s.consulMutations = append(s.consulMutations, &mutated)
}

func (s *ResourceMap) MutateHTTPRoute(key types.NamespacedName, mutateFn func(api.HTTPRouteConfigEntry) api.HTTPRouteConfigEntry) {
	consulKey := s.toConsulReference(api.HTTPRoute, key)

	consulRoute, ok := s.consulHTTPRoutes[consulKey]
	if ok {
		// remove from the consulHTTPRoutes map since we don't want to
		// GC it in the end
		delete(s.consulHTTPRoutes, consulKey)
		mutated := mutateFn(consulRoute.route)
		// add it to the mutation set
		s.consulMutations = append(s.consulMutations, &mutated)
	}
}

func (s *ResourceMap) CanGCHTTPRouteOnUnbind(id api.ResourceReference) bool {
	if set := s.httpRouteGateways[id]; set != nil {
		return set.gateways.Cardinality() <= 1
	}
	return true
}

func (s *ResourceMap) TranslateAndMutateTCPRoute(key types.NamespacedName, mutateFn func(*api.TCPRouteConfigEntry, api.TCPRouteConfigEntry) api.TCPRouteConfigEntry) {
	consulKey := s.toConsulReference(api.TCPRoute, key)

	route, ok := s.tcpRouteGateways[consulKey]
	if !ok {

		return
	}

	translated := s.translator.ToTCPRoute(route.route, s)

	consulRoute, ok := s.consulTCPRoutes[consulKey]
	if ok {
		// remove from the consulTCPRoutes map since we don't want to
		// GC it in the end
		mutated := mutateFn(&consulRoute.route, *translated)
		s.consulMutations = append(s.consulMutations, &mutated)
		return
	}
	mutated := mutateFn(nil, *translated)
	s.consulMutations = append(s.consulMutations, &mutated)
}

func (s *ResourceMap) MutateTCPRoute(key types.NamespacedName, mutateFn func(api.TCPRouteConfigEntry) api.TCPRouteConfigEntry) {
	consulKey := s.toConsulReference(api.TCPRoute, key)

	consulRoute, ok := s.consulTCPRoutes[consulKey]
	if ok {
		// remove from the consulTCPRoutes map since we don't want to
		// GC it in the end
		delete(s.consulTCPRoutes, consulKey)
		mutated := mutateFn(consulRoute.route)
		// add it to the mutation set
		s.consulMutations = append(s.consulMutations, &mutated)
	}
}

func (s *ResourceMap) AddMutation(entry api.ConfigEntry) {
	s.consulMutations = append(s.consulMutations, entry)
}

func (s *ResourceMap) CanGCTCPRouteOnUnbind(id api.ResourceReference) bool {
	if set := s.tcpRouteGateways[id]; set != nil {
		return set.gateways.Cardinality() <= 1
	}
	return true
}

func (s *ResourceMap) TranslateInlineCertificate(key types.NamespacedName) error {
	consulKey := s.toConsulReference(api.InlineCertificate, key)

	certificate, ok := s.certificateGateways[consulKey]
	if !ok {
		return nil
	}

	consulCertificate, err := s.translator.ToInlineCertificate(certificate.secret)
	if err != nil {
		return err
	}
	// remove from the certificate map since we don't want to
	// GC it in the end
	delete(s.certificateGateways, consulKey)
	s.consulMutations = append(s.consulMutations, consulCertificate)

	return nil
}

func (s *ResourceMap) Secret(key types.NamespacedName) *corev1.Secret {
	consulKey := s.toConsulReference(api.InlineCertificate, key)

	certificate, ok := s.certificateGateways[consulKey]
	if !ok {
		return nil
	}
	return &certificate.secret
}

func (s *ResourceMap) Mutations() []api.ConfigEntry {
	return s.consulMutations
}

func (s *ResourceMap) objectReference(o api.ConfigEntry) api.ResourceReference {
	return NormalizeMeta(api.ResourceReference{
		Kind:      o.GetKind(),
		Name:      o.GetName(),
		Namespace: o.GetNamespace(),
		Partition: s.translator.ConsulPartition,
	})
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

func (s *ResourceMap) DumpAll() {
	fmt.Println("====CERTS====")
	for ref := range s.certificateGateways {
		marshalDump(ref)
	}
	fmt.Println("====CONSUL CERTS====")
	for ref := range s.consulInlineCertificates {
		marshalDump(ref)
	}
	fmt.Println("====HTTPROUTES====")
	for ref := range s.httpRouteGateways {
		marshalDump(ref)
	}
	fmt.Println("====CONSUL HTTPROUTES====")
	for ref := range s.consulHTTPRoutes {
		marshalDump(ref)
	}
	fmt.Println("====TCPROUTES====")
	for ref := range s.tcpRouteGateways {
		marshalDump(ref)
	}
	fmt.Println("====CONSUL TCPROUTES====")
	for ref := range s.consulTCPRoutes {
		marshalDump(ref)
	}
	fmt.Println("====SERVICES====")
	for k, v := range s.services {
		marshalDump(k)
		marshalDump(v)
	}
	fmt.Println("====MESH SERVICES====")
	for k, v := range s.meshServices {
		marshalDump(k)
		marshalDump(v)
	}
}

func marshalDump(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
