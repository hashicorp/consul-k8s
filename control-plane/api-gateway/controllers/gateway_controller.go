// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/binding"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/cache"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/gatekeeper"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
)

const (
	gatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	kindGateway = "Gateway"
)

// GatewayControllerConfig holds the values necessary for configuring the GatewayController.
type GatewayControllerConfig struct {
	HelmConfig            apigateway.HelmConfig
	ConsulClientConfig    *consul.Config
	ConsulServerConnMgr   consul.ServerConnectionManager
	NamespacesEnabled     bool
	Partition             string
	AllowK8sNamespacesSet mapset.Set
	DenyK8sNamespacesSet  mapset.Set
}

// GatewayController reconciles a Gateway object.
// The Gateway is responsible for defining the behavior of API gateways.
type GatewayController struct {
	HelmConfig            apigateway.HelmConfig
	Log                   logr.Logger
	Translator            apigateway.ResourceTranslator
	cache                 *cache.Cache
	gatewayCache          *cache.GatewayCache
	allowK8sNamespacesSet mapset.Set
	denyK8sNamespacesSet  mapset.Set
	client.Client
}

// Reconcile handles the reconciliation loop for Gateway objects.
func (r *GatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	consulKey := r.Translator.ConfigEntryReference(api.APIGateway, req.NamespacedName)
	nonNormalizedConsulKey := r.Translator.NonNormalizedConfigEntryReference(api.APIGateway, req.NamespacedName)

	var gateway gwv1beta1.Gateway

	log := r.Log.WithValues("gateway", req.NamespacedName)
	log.Info("Reconciling Gateway")

	// get the gateway
	if err := r.Client.Get(ctx, req.NamespacedName, &gateway); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get Gateway")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	marshalDump("GATEWAY", &gateway)

	// get the gateway class
	gatewayClass, err := r.getGatewayClassForGateway(ctx, gateway)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, err
	}
	marshalDump("GATEWAYCLASS", gatewayClass)

	// get the gateway class config
	gatewayClassConfig, err := r.getConfigForGatewayClass(ctx, gatewayClass)
	if err != nil {
		log.Error(err, "error fetching the gateway class config")
		return ctrl.Result{}, err
	}
	marshalDump("GATEWAYCLASSCONFIG", gatewayClassConfig)

	// get all namespaces
	namespaces, err := r.getNamespaces(ctx)
	if err != nil {
		log.Error(err, "unable to list Namespaces")
		return ctrl.Result{}, err
	}
	marshalDump("NAMESPACES", &namespaces)

	// get related gateway service
	service, err := r.getDeployedGatewayService(ctx, req.NamespacedName)
	if err != nil {
		log.Error(err, "unable to fetch service for Gateway")
	}
	marshalDump("SERVICE", service)

	// get related gateway pods
	pods, err := r.getDeployedGatewayPods(ctx, gateway)
	if err != nil {
		log.Error(err, "unable to list Pods for Gateway")
		return ctrl.Result{}, err
	}
	marshalDump("PODS", &pods)

	// construct our resource map
	resources := apigateway.NewResourceMap(r.Translator)

	fmt.Println("FETCHING CONTROLLED GATEWAYS!!!!!")
	if err := r.fetchControlledGateways(ctx, resources); err != nil {
		log.Error(err, "unable to fetch controlled gateways")
		return ctrl.Result{}, err
	}

	fmt.Println("FETCHING CERTIFICATES!!!!!")
	if err := r.fetchCertificatesForGateway(ctx, resources, gateway); err != nil {
		log.Error(err, "unable to fetch certificates for gateway")
		return ctrl.Result{}, err
	}

	// get all http routes referencing this gateway
	httpRoutes, err := r.getRelatedHTTPRoutes(ctx, req.NamespacedName, resources)
	if err != nil {
		log.Error(err, "unable to list HTTPRoutes")
		return ctrl.Result{}, err
	}
	marshalDump("HTTPROUTES", &httpRoutes)

	// get all tcp routes referencing this gateway
	tcpRoutes, err := r.getRelatedTCPRoutes(ctx, req.NamespacedName, resources)
	if err != nil {
		log.Error(err, "unable to list TCPRoutes")
		return ctrl.Result{}, err
	}
	marshalDump("TCPROUTES", &tcpRoutes)

	fmt.Println("FETCHING SERVICE RESOURCES!!!!!")
	if err := r.fetchServicesForRoutes(ctx, resources, tcpRoutes, httpRoutes); err != nil {
		log.Error(err, "unable to fetch services for routes")
		return ctrl.Result{}, err
	}

	resources.DumpAll()

	fmt.Println("GENERATING SNAPSHOT!!!!!")
	// fetch all consul objects from cache
	consulServices := r.getConsulServices(consulKey)
	marshalDump("CONSUL SERVICES", &consulServices)
	consulGateway := r.getConsulGateway(consulKey)
	marshalDump("CONSUL GATEWAY", consulGateway)
	consulHTTPRoutes := r.getConsulHTTPRoutes(consulKey, resources)
	marshalDump("CONSUL HTTPROUTES", &consulHTTPRoutes)
	consulTCPRoutes := r.getConsulTCPRoutes(consulKey, resources)
	marshalDump("CONSUL TCPROUTES", &consulTCPRoutes)
	consulInlineCertificates := r.getConsulInlineCertificates()
	marshalDump("CONSUL CERTIFICATES", &consulInlineCertificates)

	binder := binding.NewBinder(binding.BinderConfig{
		Translator:               r.Translator,
		ControllerName:           GatewayClassControllerName,
		Namespaces:               namespaces,
		GatewayClassConfig:       gatewayClassConfig,
		GatewayClass:             gatewayClass,
		Gateway:                  gateway,
		Pods:                     pods,
		Service:                  service,
		HTTPRoutes:               httpRoutes,
		TCPRoutes:                tcpRoutes,
		Resources:                resources,
		ConsulGateway:            consulGateway,
		ConsulHTTPRoutes:         consulHTTPRoutes,
		ConsulTCPRoutes:          consulTCPRoutes,
		ConsulInlineCertificates: consulInlineCertificates,
		ConsulGatewayServices:    consulServices,
	})

	fmt.Println("GENERATING SNAPSHOT!!!!!")
	updates := binder.Snapshot()
	marshalDump("UPDATES!!!!!", &updates)

	if updates.UpsertGatewayDeployment {
		log.Info("updating gatekeeper")
		err := r.updateGatekeeperResources(ctx, log, &gateway, updates.GatewayClassConfig)
		if err != nil {
			log.Error(err, "unable to update gateway resources")
			return ctrl.Result{}, err
		}
		r.gatewayCache.EnsureSubscribed(nonNormalizedConsulKey, req.NamespacedName)
	} else {
		log.Info("deleting gatekeeper")
		err := r.deleteGatekeeperResources(ctx, log, &gateway)
		if err != nil {
			log.Error(err, "unable to delete gateway resources")
			return ctrl.Result{}, err
		}
		r.gatewayCache.RemoveSubscription(nonNormalizedConsulKey)
	}

	for _, deletion := range updates.Consul.Deletions {
		log.Info("deleting from Consul", "kind", deletion.Kind, "namespace", deletion.Namespace, "name", deletion.Name)
		if err := r.cache.Delete(ctx, deletion); err != nil {
			log.Error(err, "error deleting config entry")
			return ctrl.Result{}, err
		}
	}

	for _, update := range updates.Consul.Updates {
		log.Info("updating in Consul", "kind", update.GetKind(), "namespace", update.GetNamespace(), "name", update.GetName())
		if err := r.cache.Write(ctx, update); err != nil {
			log.Error(err, "error updating config entry")
			return ctrl.Result{}, err
		}
	}

	for _, registration := range updates.Consul.Registrations {
		log.Info("registering service in Consul", "service", registration.Service.Service, "id", registration.Service.ID)
		if err := r.cache.Register(ctx, registration); err != nil {
			log.Error(err, "error registering service")
			return ctrl.Result{}, err
		}
	}

	for _, deregistration := range updates.Consul.Deregistrations {
		log.Info("deregistering service in Consul", "id", deregistration.ServiceID)
		if err := r.cache.Deregister(ctx, deregistration); err != nil {
			log.Error(err, "error deregistering service")
			return ctrl.Result{}, err
		}
	}

	for _, update := range updates.Kubernetes.Updates {
		log.Info("update in Kubernetes", "kind", update.GetObjectKind().GroupVersionKind().Kind, "namespace", update.GetNamespace(), "name", update.GetName())
		if err := r.updateAndResetStatus(ctx, update); err != nil {
			log.Error(err, "error updating object")
			return ctrl.Result{}, err
		}
	}

	for _, update := range updates.Kubernetes.StatusUpdates {
		log.Info("update status in Kubernetes", "kind", update.GetObjectKind().GroupVersionKind().Kind, "namespace", update.GetNamespace(), "name", update.GetName())
		if err := r.Client.Status().Update(ctx, update); err != nil {
			log.Error(err, "error updating status")
			return ctrl.Result{}, err
		}
	}

	// link up policy - TODO: this is really a nasty hack to inject a known policy with
	// mesh == read on the provisioned gateway token if needed, figure out some other
	// way of handling it.
	if updates.UpsertGatewayDeployment {
		if err := r.cache.LinkPolicy(ctx, nonNormalizedConsulKey.Name, nonNormalizedConsulKey.Namespace); err != nil {
			log.Error(err, "error linking token policy")
			return ctrl.Result{}, err
		}
	}

	/* TODO:
	1.ReferenceGrants
	*/

	return ctrl.Result{}, nil
}

func (r *GatewayController) updateAndResetStatus(ctx context.Context, o client.Object) error {
	// we create a copy so that we can re-update its status if need be
	status := reflect.ValueOf(o.DeepCopyObject()).Elem().FieldByName("Status")
	if err := r.Client.Update(ctx, o); err != nil {
		return err
	}
	// reset the status in case it needs to be updated below
	reflect.ValueOf(o).Elem().FieldByName("Status").Set(status)
	return nil
}

func derefAll[T any](vs []*T) []T {
	e := make([]T, 0, len(vs))
	for _, v := range vs {
		e = append(e, *v)
	}
	return e
}

func configEntriesTo[T api.ConfigEntry](entries []api.ConfigEntry) []T {
	es := []T{}
	for _, e := range entries {
		es = append(es, e.(T))
	}
	return es
}

func (r *GatewayController) deleteGatekeeperResources(ctx context.Context, log logr.Logger, gw *gwv1beta1.Gateway) error {
	gk := gatekeeper.New(log, r.Client)
	err := gk.Delete(ctx, types.NamespacedName{
		Namespace: gw.Namespace,
		Name:      gw.Name,
	})
	if err != nil {
		return err
	}

	return nil
}

func (r *GatewayController) updateGatekeeperResources(ctx context.Context, log logr.Logger, gw *gwv1beta1.Gateway, gwcc *v1alpha1.GatewayClassConfig) error {
	gk := gatekeeper.New(log, r.Client)
	err := gk.Upsert(ctx, *gw, *gwcc, r.HelmConfig)
	if err != nil {
		return err
	}

	return nil
}

// SetupWithGatewayControllerManager registers the controller with the given manager.
func SetupGatewayControllerWithManager(ctx context.Context, mgr ctrl.Manager, config GatewayControllerConfig) (*cache.Cache, error) {
	cacheConfig := cache.Config{
		ConsulClientConfig:  config.ConsulClientConfig,
		ConsulServerConnMgr: config.ConsulServerConnMgr,
		NamespacesEnabled:   config.NamespacesEnabled,
		Logger:              mgr.GetLogger(),
	}
	c := cache.New(cacheConfig)
	gwc := cache.NewGatewayCache(ctx, cacheConfig)

	r := &GatewayController{
		Client:     mgr.GetClient(),
		Log:        mgr.GetLogger(),
		HelmConfig: config.HelmConfig,
		Translator: apigateway.ResourceTranslator{
			EnableConsulNamespaces: config.HelmConfig.EnableNamespaces,
			ConsulDestNamespace:    config.HelmConfig.ConsulDestinationNamespace,
			EnableK8sMirroring:     config.HelmConfig.EnableNamespaceMirroring,
			MirroringPrefix:        config.HelmConfig.NamespaceMirroringPrefix,
			ConsulPartition:        config.HelmConfig.ConsulPartition,
		},
		denyK8sNamespacesSet:  config.DenyK8sNamespacesSet,
		allowK8sNamespacesSet: config.AllowK8sNamespacesSet,
		cache:                 c,
		gatewayCache:          gwc,
	}

	return c, ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Pod{}).
		Watches(
			source.NewKindWithCache(&gwv1beta1.GatewayClass{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformGatewayClass(ctx)),
		).
		Watches(
			source.NewKindWithCache(&gwv1beta1.HTTPRoute{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformHTTPRoute(ctx)),
		).
		Watches(
			source.NewKindWithCache(&gwv1alpha2.TCPRoute{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformTCPRoute(ctx)),
		).
		Watches(
			source.NewKindWithCache(&corev1.Secret{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformSecret(ctx)),
		).
		Watches(
			source.NewKindWithCache(&gwv1beta1.ReferenceGrant{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformReferenceGrant(ctx)),
		).
		Watches(
			source.NewKindWithCache(&v1alpha1.MeshService{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformMeshService(ctx)),
		).
		Watches(
			source.NewKindWithCache(&corev1.Endpoints{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(r.transformEndpoints(ctx)),
		).
		Watches(
			// Subscribe to changes from Consul for APIGateways
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, r.transformConsulGateway).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for HTTPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.HTTPRoute, r.transformConsulHTTPRoute(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for TCPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.TCPRoute, r.transformConsulTCPRoute(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for InlineCertificates
			&source.Channel{Source: c.Subscribe(ctx, api.InlineCertificate, r.transformConsulInlineCertificate(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).Complete(r)
}

func serviceToNamespacedName(s *api.CatalogService) types.NamespacedName {
	return types.NamespacedName{
		Namespace: s.ServiceMeta[constants.MetaKeyKubeNS],
		Name:      s.ServiceMeta[constants.MetaKeyKubeServiceName],
	}
}

// transformGatewayClass will check the list of GatewayClass objects for a matching
// class, then return a list of reconcile Requests for it.
func (r *GatewayController) transformGatewayClass(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		gatewayClass := o.(*gwv1beta1.GatewayClass)
		gatewayList := &gwv1beta1.GatewayList{}
		if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(Gateway_GatewayClassIndex, gatewayClass.Name),
		}); err != nil {
			return nil
		}
		return objectsToRequests(pointersOf(gatewayList.Items))
	}
}

// transformHTTPRoute will check the HTTPRoute object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformHTTPRoute(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		route := o.(*gwv1beta1.HTTPRoute)
		return refsToRequests(parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs))
	}
}

// transformTCPRoute will check the TCPRoute object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformTCPRoute(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		route := o.(*gwv1alpha2.TCPRoute)
		return refsToRequests(parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs))
	}
}

// transformSecret will check the Secret object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformSecret(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		secret := o.(*corev1.Secret)
		gatewayList := &gwv1beta1.GatewayList{}
		if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(Secret_GatewayIndex, secret.Name),
		}); err != nil {
			return nil
		}
		return objectsToRequests(pointersOf(gatewayList.Items))
	}
}

// transformReferenceGrant will check the ReferenceGrant object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformReferenceGrant(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		// just reconcile all gateways within the namespace
		grant := o.(*gwv1beta1.ReferenceGrant)
		gatewayList := &gwv1beta1.GatewayList{}
		if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
			Namespace: grant.Namespace,
		}); err != nil {
			return nil
		}
		return objectsToRequests(pointersOf(gatewayList.Items))
	}
}

// transformMeshService will return a list of gateways that are referenced
// by a TCPRoute or HTTPRoute that references the mesh service.
func (r *GatewayController) transformMeshService(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		service := o.(*v1alpha1.MeshService)
		key := client.ObjectKeyFromObject(service).String()

		return r.gatewaysForRoutesReferencing(ctx, TCPRoute_MeshServiceIndex, HTTPRoute_MeshServiceIndex, key)
	}
}

// transformConsulGateway will return a list of gateways that this corresponds to.
func (r *GatewayController) transformConsulGateway(entry api.ConfigEntry) []types.NamespacedName {
	return []types.NamespacedName{apigateway.EntryToNamespacedName(entry)}
}

// transformConsulHTTPRoute will return a list of gateways that need to be reconciled.
func (r *GatewayController) transformConsulHTTPRoute(ctx context.Context) func(entry api.ConfigEntry) []types.NamespacedName {
	return func(entry api.ConfigEntry) []types.NamespacedName {
		parents := mapset.NewSet()
		for _, parent := range entry.(*api.HTTPRouteConfigEntry).Parents {
			parents.Add(api.ResourceReference{
				Kind:      parent.Kind,
				Name:      parent.Name,
				Namespace: parent.Namespace,
				Partition: parent.Partition,
			})
		}

		var gateways []types.NamespacedName
		for parent := range parents.Iter() {
			if gateway := r.cache.Get(parent.(api.ResourceReference)); gateway != nil {
				gateways = append(gateways, apigateway.EntryToNamespacedName(gateway))
			}
		}
		return gateways
	}
}

func (r *GatewayController) transformConsulTCPRoute(ctx context.Context) func(entry api.ConfigEntry) []types.NamespacedName {
	return func(entry api.ConfigEntry) []types.NamespacedName {
		parents := mapset.NewSet()
		for _, parent := range entry.(*api.TCPRouteConfigEntry).Parents {
			parents.Add(api.ResourceReference{
				Kind:      parent.Kind,
				Name:      parent.Name,
				Namespace: parent.Namespace,
				Partition: parent.Partition,
			})
		}

		var gateways []types.NamespacedName
		for parent := range parents.Iter() {
			if gateway := r.cache.Get(parent.(api.ResourceReference)); gateway != nil {
				gateways = append(gateways, apigateway.EntryToNamespacedName(gateway))
			}
		}
		return gateways
	}
}

func (r *GatewayController) transformConsulInlineCertificate(ctx context.Context) func(entry api.ConfigEntry) []types.NamespacedName {
	return func(entry api.ConfigEntry) []types.NamespacedName {
		certificateKey := api.ResourceReference{
			Kind:      entry.GetKind(),
			Name:      entry.GetName(),
			Namespace: entry.GetNamespace(),
			Partition: entry.GetPartition(),
		}

		var gateways []types.NamespacedName
		for _, entry := range r.cache.List(api.APIGateway) {
			gateway := entry.(*api.APIGatewayConfigEntry)
			if gatewayReferencesCertificate(certificateKey, gateway) {
				gateways = append(gateways, apigateway.EntryToNamespacedName(gateway))
			}
		}

		return gateways
	}
}

func gatewayReferencesCertificate(certificateKey api.ResourceReference, gateway *api.APIGatewayConfigEntry) bool {
	for _, listener := range gateway.Listeners {
		for _, cert := range listener.TLS.Certificates {
			if cert == certificateKey {
				return true
			}
		}
	}
	return false
}

// transformEndpoints will return a list of gateways that are referenced
// by a TCPRoute or HTTPRoute that references the service.
func (r *GatewayController) transformEndpoints(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		key := client.ObjectKeyFromObject(o).String()

		return r.gatewaysForRoutesReferencing(ctx, TCPRoute_ServiceIndex, HTTPRoute_ServiceIndex, key)
	}
}

// gatewaysForRoutesReferencing returns a mapping of all gateways that are referenced by routes that
// have a backend associated with the given key and index.
func (r *GatewayController) gatewaysForRoutesReferencing(ctx context.Context, tcpIndex, httpIndex, key string) []reconcile.Request {
	requestSet := make(map[types.NamespacedName]struct{})

	tcpRouteList := &gwv1alpha2.TCPRouteList{}
	if err := r.Client.List(ctx, tcpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(tcpIndex, key),
	}); err != nil {
		r.Log.Error(err, "unable to list TCPRoutes")
	}
	for _, route := range tcpRouteList.Items {
		for _, ref := range parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs) {
			requestSet[ref] = struct{}{}
		}
	}

	httpRouteList := &gwv1beta1.HTTPRouteList{}
	if err := r.Client.List(ctx, httpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(httpIndex, key),
	}); err != nil {
		r.Log.Error(err, "unable to list HTTPRoutes")
	}
	for _, route := range httpRouteList.Items {
		for _, ref := range parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs) {
			requestSet[ref] = struct{}{}
		}
	}

	requests := []reconcile.Request{}
	for request := range requestSet {
		requests = append(requests, reconcile.Request{NamespacedName: request})
	}
	return requests
}

// objectsToRequests takes a list of objects and returns a list of
// reconcile Requests.
func objectsToRequests[T metav1.Object](objects []T) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objects))

	// TODO: is it possible to receive empty objects?
	for _, object := range objects {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: object.GetNamespace(),
				Name:      object.GetName(),
			},
		})
	}
	return requests
}

// pointersOf returns a list of pointers to the list of objects passed in.
func pointersOf[T any](objects []T) []*T {
	pointers := make([]*T, 0, len(objects))
	for _, object := range objects {
		pointers = append(pointers, pointerTo(object))
	}
	return pointers
}

// pointerTo returns a pointer to the object type passed in.
func pointerTo[T any](v T) *T {
	return &v
}

// refsToRequests takes a list of NamespacedName objects and returns a list of
// reconcile Requests.
func refsToRequests(objects []types.NamespacedName) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objects))
	for _, object := range objects {
		requests = append(requests, reconcile.Request{
			NamespacedName: object,
		})
	}
	return requests
}

// parentRefs takes a list of ParentReference objects and returns a list of NamespacedName objects.
func parentRefs(group, kind, namespace string, refs []gwv1beta1.ParentReference) []types.NamespacedName {
	indexed := make([]types.NamespacedName, 0, len(refs))
	for _, parent := range refs {
		if nilOrEqual(parent.Group, group) && nilOrEqual(parent.Kind, kind) {
			indexed = append(indexed, indexedNamespacedNameWithDefault(parent.Name, parent.Namespace, namespace))
		}
	}
	return indexed
}

func nilOrEqual[T ~string](v *T, check string) bool {
	return v == nil || string(*v) == check
}

func indexedNamespacedNameWithDefault[T ~string, U ~string, V ~string](t T, u *U, v V) types.NamespacedName {
	return types.NamespacedName{
		Namespace: derefStringOr(u, v),
		Name:      string(t),
	}
}

func derefStringOr[T ~string, U ~string](v *T, val U) string {
	if v == nil {
		return string(val)
	}
	return string(*v)
}

// kubernetes helpers

func (c *GatewayController) getNamespaces(ctx context.Context) (map[string]corev1.Namespace, error) {
	var list corev1.NamespaceList

	if err := c.Client.List(ctx, &list); err != nil {
		return nil, err
	}
	namespaces := map[string]corev1.Namespace{}
	for _, namespace := range list.Items {
		namespaces[namespace.Name] = namespace
	}

	return namespaces, nil
}

func (c *GatewayController) getDeployedGatewayService(ctx context.Context, gateway types.NamespacedName) (*corev1.Service, error) {
	service := &corev1.Service{}

	// we use the implicit association of a service name/namespace with a corresponding gateway
	if err := c.Client.Get(ctx, gateway, service); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return service, nil
}

func (c *GatewayController) getDeployedGatewayPods(ctx context.Context, gateway gwv1beta1.Gateway) ([]corev1.Pod, error) {
	labels := apigateway.LabelsForGateway(&gateway)

	var list corev1.PodList

	if err := c.Client.List(ctx, &list, client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	return list.Items, nil
}

func (c *GatewayController) getRelatedHTTPRoutes(ctx context.Context, gateway types.NamespacedName, resources *apigateway.ResourceMap) ([]gwv1beta1.HTTPRoute, error) {
	var list gwv1beta1.HTTPRouteList

	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(HTTPRoute_GatewayIndex, gateway.String()),
	}); err != nil {
		return nil, err
	}

	for _, route := range list.Items {
		resources.ReferenceCountHTTPRoute(route)
	}

	return list.Items, nil
}

func (c *GatewayController) getRelatedTCPRoutes(ctx context.Context, gateway types.NamespacedName, resources *apigateway.ResourceMap) ([]gwv1alpha2.TCPRoute, error) {
	var list gwv1alpha2.TCPRouteList

	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(TCPRoute_GatewayIndex, gateway.String()),
	}); err != nil {
		return nil, err
	}

	for _, route := range list.Items {
		resources.ReferenceCountTCPRoute(route)
	}

	return list.Items, nil
}

func (c *GatewayController) getConfigForGatewayClass(ctx context.Context, gatewayClassConfig *gwv1beta1.GatewayClass) (*v1alpha1.GatewayClassConfig, error) {
	if gatewayClassConfig == nil {
		// if we don't have a gateway class we can't fetch the corresponding config
		return nil, nil
	}

	config := &v1alpha1.GatewayClassConfig{}
	if ref := gatewayClassConfig.Spec.ParametersRef; ref != nil {
		if string(ref.Group) != v1alpha1.GroupVersion.Group ||
			ref.Kind != v1alpha1.GatewayClassConfigKind ||
			gatewayClassConfig.Spec.ControllerName != GatewayClassControllerName {
			// we don't have supported params, so return nil
			return nil, nil
		}

		if err := c.Client.Get(ctx, types.NamespacedName{Name: ref.Name}, config); err != nil {
			return nil, client.IgnoreNotFound(err)
		}
	}
	return config, nil
}

func (c *GatewayController) getGatewayClassForGateway(ctx context.Context, gateway gwv1beta1.Gateway) (*gwv1beta1.GatewayClass, error) {
	var gatewayClass gwv1beta1.GatewayClass
	if err := c.Client.Get(ctx, types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, &gatewayClass); err != nil {
		fmt.Println(err)
		return nil, client.IgnoreNotFound(err)
	}
	return &gatewayClass, nil
}

// resource map construction routines

func (c *GatewayController) fetchControlledGateways(ctx context.Context, resources *apigateway.ResourceMap) error {
	set := mapset.NewSet()

	list := gwv1beta1.GatewayClassList{}
	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(GatewayClass_ControllerNameIndex, GatewayClassControllerName),
	}); err != nil {
		return err
	}
	for _, gatewayClass := range list.Items {
		set.Add(gatewayClass.Name)
	}

	gateways := &gwv1beta1.GatewayList{}
	if err := c.Client.List(ctx, gateways); err != nil {
		return err
	}

	for _, gateway := range gateways.Items {
		if set.Contains(string(gateway.Spec.GatewayClassName)) {
			resources.ReferenceCountGateway(gateway)
		}
	}
	return nil
}

func (c *GatewayController) fetchCertificatesForGateway(ctx context.Context, resources *apigateway.ResourceMap, gateway gwv1beta1.Gateway) error {
	certificates := mapset.NewSet()

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS != nil {
			for _, cert := range listener.TLS.CertificateRefs {
				if nilOrEqual(cert.Group, "") && nilOrEqual(cert.Kind, "Secret") {
					certificates.Add(apigateway.IndexedNamespacedNameWithDefault(gateway.Namespace, cert.Namespace, cert.Name))
				}
			}
		}
	}

	for key := range certificates.Iter() {
		if err := c.fetchSecret(ctx, resources, key.(types.NamespacedName)); err != nil {
			return err
		}
	}

	return nil
}

func (c *GatewayController) fetchSecret(ctx context.Context, resources *apigateway.ResourceMap, key types.NamespacedName) error {
	var secret corev1.Secret
	if err := c.Client.Get(ctx, key, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	resources.ReferenceCountCertificate(secret)

	return nil
}

func (c *GatewayController) fetchServicesForRoutes(ctx context.Context, resources *apigateway.ResourceMap, tcpRoutes []gwv1alpha2.TCPRoute, httpRoutes []gwv1beta1.HTTPRoute) error {
	serviceBackends := mapset.NewSet()
	meshServiceBackends := mapset.NewSet()

	for _, route := range httpRoutes {
		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				if apigateway.DerefEqual(backend.Group, v1alpha1.ConsulHashicorpGroup) &&
					apigateway.DerefEqual(backend.Kind, v1alpha1.MeshServiceKind) {
					meshServiceBackends.Add(apigateway.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				} else if apigateway.NilOrEqual(backend.Group, "") && apigateway.NilOrEqual(backend.Kind, "Service") {
					serviceBackends.Add(apigateway.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				}
			}
		}
	}

	for _, route := range tcpRoutes {
		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				if apigateway.DerefEqual(backend.Group, v1alpha1.ConsulHashicorpGroup) &&
					apigateway.DerefEqual(backend.Kind, v1alpha1.MeshServiceKind) {
					meshServiceBackends.Add(apigateway.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				} else if apigateway.NilOrEqual(backend.Group, "") && apigateway.NilOrEqual(backend.Kind, "Service") {
					serviceBackends.Add(apigateway.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				}
			}
		}
	}

	for key := range meshServiceBackends.Iter() {
		if err := c.fetchMeshService(ctx, resources, key.(types.NamespacedName)); err != nil {
			return err
		}
	}

	for key := range serviceBackends.Iter() {
		if err := c.fetchServicesForEndpoints(ctx, resources, key.(types.NamespacedName)); err != nil {
			return err
		}
	}
	return nil
}

func (c *GatewayController) fetchMeshService(ctx context.Context, resources *apigateway.ResourceMap, key types.NamespacedName) error {
	var service v1alpha1.MeshService
	if err := c.Client.Get(ctx, key, &service); err != nil {
		return client.IgnoreNotFound(err)
	}

	resources.AddMeshService(service)

	return nil
}

func (c *GatewayController) fetchServicesForEndpoints(ctx context.Context, resources *apigateway.ResourceMap, key types.NamespacedName) error {
	if shouldIgnore(key.Namespace, c.denyK8sNamespacesSet, c.allowK8sNamespacesSet) {
		return nil
	}

	var endpoints corev1.Endpoints
	if err := c.Client.Get(ctx, key, &endpoints); err != nil {
		return client.IgnoreNotFound(err)
	}

	if isLabeledIgnore(endpoints.Labels) {
		return nil
	}

	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				objectKey := types.NamespacedName{Name: address.TargetRef.Name, Namespace: address.TargetRef.Namespace}

				var pod corev1.Pod
				if err := c.Client.Get(ctx, objectKey, &pod); err != nil {
					if k8serrors.IsNotFound(err) {
						continue
					}
					return err
				}

				resources.AddService(key, serviceName(pod, endpoints))
			}
		}
	}

	return nil
}

// cache routines

func (c *GatewayController) getConsulServices(ref api.ResourceReference) []api.CatalogService {
	return c.gatewayCache.ServicesFor(ref)
}

func (c *GatewayController) getConsulGateway(ref api.ResourceReference) *api.APIGatewayConfigEntry {
	if entry := c.cache.Get(ref); entry != nil {
		return entry.(*api.APIGatewayConfigEntry)
	}
	return nil
}

func (c *GatewayController) getConsulHTTPRoutes(ref api.ResourceReference, resources *apigateway.ResourceMap) []api.HTTPRouteConfigEntry {
	var filtered []api.HTTPRouteConfigEntry

	for _, route := range configEntriesTo[*api.HTTPRouteConfigEntry](c.cache.List(api.HTTPRoute)) {
		if routeReferencesGateway(route.Namespace, ref, route.Parents) {
			filtered = append(filtered, *route)
			resources.ReferenceCountConsulHTTPRoute(*route)
		}
	}
	return filtered
}

func (c *GatewayController) getConsulTCPRoutes(ref api.ResourceReference, resources *apigateway.ResourceMap) []api.TCPRouteConfigEntry {
	var filtered []api.TCPRouteConfigEntry

	for _, route := range configEntriesTo[*api.TCPRouteConfigEntry](c.cache.List(api.TCPRoute)) {
		if routeReferencesGateway(route.Namespace, ref, route.Parents) {
			filtered = append(filtered, *route)
			resources.ReferenceCountConsulTCPRoute(*route)
		}
	}
	return filtered
}

func (c *GatewayController) getConsulInlineCertificates() []api.InlineCertificateConfigEntry {
	var filtered []api.InlineCertificateConfigEntry

	for _, cert := range configEntriesTo[*api.InlineCertificateConfigEntry](c.cache.List(api.InlineCertificate)) {
		filtered = append(filtered, *cert)
	}
	return filtered
}

func routeReferencesGateway(namespace string, ref api.ResourceReference, refs []api.ResourceReference) bool {
	// we don't need to check partition here since they're all in the same partition
	if namespace == "" {
		namespace = "default"
	}

	for _, parent := range refs {
		if apigateway.EmptyOrEqual(parent.Kind, api.APIGateway) {
			if apigateway.DefaultOrEqual(parent.Namespace, namespace, ref.Namespace) &&
				parent.Name == ref.Name {
				return true
			}
		}
	}

	return false
}

func serviceName(pod corev1.Pod, serviceEndpoints corev1.Endpoints) string {
	svcName := serviceEndpoints.Name
	// If the annotation has a comma, it is a multi port Pod. In that case we always use the name of the endpoint.
	if serviceNameFromAnnotation, ok := pod.Annotations[constants.AnnotationService]; ok && serviceNameFromAnnotation != "" && !strings.Contains(serviceNameFromAnnotation, ",") {
		svcName = serviceNameFromAnnotation
	}
	return svcName
}

func isLabeledIgnore(labels map[string]string) bool {
	value, labelExists := labels[constants.LabelServiceIgnore]
	shouldIgnore, err := strconv.ParseBool(value)

	return shouldIgnore && labelExists && err == nil
}

// shouldIgnore ignores namespaces where we don't connect-inject.
func shouldIgnore(namespace string, denySet, allowSet mapset.Set) bool {
	// Ignores system namespaces.
	if namespace == metav1.NamespaceSystem || namespace == metav1.NamespacePublic || namespace == "local-path-storage" {
		return true
	}

	// Ignores deny list.
	if denySet.Contains(namespace) {
		return true
	}

	// Ignores if not in allow list or allow list is not *.
	if !allowSet.Contains("*") && !allowSet.Contains(namespace) {
		return true
	}

	return false
}

func marshalDump[T any](message string, item *T) {
	if item == nil {
		fmt.Println(message, "is nil")
		return
	}
	data, _ := json.MarshalIndent(item, "", "  ")
	fmt.Println(message, string(data))
}
