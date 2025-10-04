// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/binding"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/cache"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/gatekeeper"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// GatewayControllerConfig holds the values necessary for configuring the GatewayController.
type GatewayControllerConfig struct {
	HelmConfig              common.HelmConfig
	ConsulClientConfig      *consul.Config
	ConsulServerConnMgr     consul.ServerConnectionManager
	NamespacesEnabled       bool
	CrossNamespaceACLPolicy string
	Partition               string
	Datacenter              string
	AllowK8sNamespacesSet   mapset.Set
	DenyK8sNamespacesSet    mapset.Set
}

// GatewayController reconciles a Gateway object.
// The Gateway is responsible for defining the behavior of API gateways.
type GatewayController struct {
	HelmConfig common.HelmConfig
	Log        logr.Logger
	Translator common.ResourceTranslator

	cache                 *cache.Cache
	gatewayCache          *cache.GatewayCache
	allowK8sNamespacesSet mapset.Set
	denyK8sNamespacesSet  mapset.Set
	client.Client
	ConsulConfig *consul.Config
}

// Reconcile handles the reconciliation loop for Gateway objects.
func (r *GatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	consulKey := r.Translator.ConfigEntryReference(api.APIGateway, req.NamespacedName)
	nonNormalizedConsulKey := r.Translator.NonNormalizedConfigEntryReference(api.APIGateway, req.NamespacedName)

	var gateway gwv1beta1.Gateway

	log := r.Log.V(1).WithValues("gateway", req.NamespacedName)
	log.Info("Reconciling Gateway")

	// get the gateway
	if err := r.Client.Get(ctx, req.NamespacedName, &gateway); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get Gateway")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// get the gateway class
	gatewayClass, err := r.getGatewayClassForGateway(ctx, gateway)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, err
	}

	// get the gateway class config
	gatewayClassConfig, err := r.getConfigForGatewayClass(ctx, gatewayClass)
	if err != nil {
		log.Error(err, "error fetching the gateway class config")
		return ctrl.Result{}, err
	}

	// get all namespaces
	namespaces, err := r.getNamespaces(ctx)
	if err != nil {
		log.Error(err, "unable to list Namespaces")
		return ctrl.Result{}, err
	}

	// get all reference grants
	grants, err := r.getReferenceGrants(ctx)
	if err != nil {
		log.Error(err, "unable to list ReferenceGrants")
		return ctrl.Result{}, err
	}

	// get related gateway service
	service, err := r.getDeployedGatewayService(ctx, req.NamespacedName)
	if err != nil {
		log.Error(err, "unable to fetch service for Gateway")
	}

	// get related gateway pods
	pods, err := r.getDeployedGatewayPods(ctx, gateway)
	if err != nil {
		log.Error(err, "unable to list Pods for Gateway")
		return ctrl.Result{}, err
	}

	// construct our resource map
	referenceValidator := binding.NewReferenceValidator(grants)
	resources := common.NewResourceMap(r.Translator, referenceValidator, log)

	if err := r.fetchCertificatesForGateway(ctx, resources, gateway); err != nil {
		log.Error(err, "unable to fetch certificates for gateway")
		return ctrl.Result{}, err
	}

	// fetch our file-system-certificates from cache, this needs to happen
	// here since the certificates need to be reference counted before
	// the gateways.
	r.fetchConsulFileSystemCertificates(resources)

	// add our current gateway even if it's not controlled by us so we
	// can garbage collect any resources for it.
	resources.ReferenceCountGateway(gateway)

	if err := r.fetchControlledGateways(ctx, resources); err != nil {
		log.Error(err, "unable to fetch controlled gateways")
		return ctrl.Result{}, err
	}

	// get all http routes referencing this gateway
	httpRoutes, err := r.getRelatedHTTPRoutes(ctx, req.NamespacedName, resources)
	if err != nil {
		log.Error(err, "unable to list HTTPRoutes")
		return ctrl.Result{}, err
	}

	// get all tcp routes referencing this gateway
	tcpRoutes, err := r.getRelatedTCPRoutes(ctx, req.NamespacedName, resources)
	if err != nil {
		log.Error(err, "unable to list TCPRoutes")
		return ctrl.Result{}, err
	}

	if err := r.fetchServicesForRoutes(ctx, resources, tcpRoutes, httpRoutes); err != nil {
		log.Error(err, "unable to fetch services for routes")
		return ctrl.Result{}, err
	}

	// get all gatewaypolicies referencing this gateway
	policies, err := r.getRelatedGatewayPolicies(ctx, req.NamespacedName, resources)
	if err != nil {
		log.Error(err, "unable to list gateway policies")
		return ctrl.Result{}, err
	}

	_, err = r.getJWTProviders(ctx, resources)
	if err != nil {
		log.Error(err, "unable to list JWT providers")
		return ctrl.Result{}, err
	}

	// fetch the rest of the consul objects from cache
	consulServices := r.getConsulServices(consulKey)
	consulGateway := r.getConsulGateway(consulKey)
	r.fetchConsulHTTPRoutes(consulKey, resources)
	r.fetchConsulTCPRoutes(consulKey, resources)

	binder := binding.NewBinder(binding.BinderConfig{
		Logger:                log,
		Translator:            r.Translator,
		ControllerName:        common.GatewayClassControllerName,
		Namespaces:            namespaces,
		GatewayClassConfig:    gatewayClassConfig,
		GatewayClass:          gatewayClass,
		Gateway:               gateway,
		Pods:                  pods,
		Service:               service,
		HTTPRoutes:            httpRoutes,
		TCPRoutes:             tcpRoutes,
		Resources:             resources,
		ConsulGateway:         consulGateway,
		ConsulGatewayServices: consulServices,
		Policies:              policies,
		HelmConfig:            r.HelmConfig,
	})

	updates := binder.Snapshot()

	if updates.UpsertGatewayDeployment {
		if err := r.cache.EnsureRoleBinding(r.HelmConfig.AuthMethod, gateway.Name, gateway.Namespace); err != nil {
			log.Error(err, "error creating role binding")
			return ctrl.Result{}, err
		}

		err := r.updateGatekeeperResources(ctx, log, &gateway, updates.GatewayClassConfig)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("error updating object when updating gateway resources, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "unable to update gateway resources")
			return ctrl.Result{}, err
		}
		r.gatewayCache.EnsureSubscribed(nonNormalizedConsulKey, req.NamespacedName)
	} else {
		err := r.deleteGatekeeperResources(ctx, log, &gateway)
		if err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("error updating object when deleting gateway resources, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "unable to delete gateway resources")
			return ctrl.Result{}, err
		}
		r.gatewayCache.RemoveSubscription(nonNormalizedConsulKey)
		// make sure we have deregistered all services even if they haven't
		// hit cache yet
		if err := r.deregisterAllServices(ctx, nonNormalizedConsulKey); err != nil {
			log.Error(err, "error deregistering services")
			return ctrl.Result{}, err
		}

		err = r.cache.RemoveRoleBinding(r.HelmConfig.AuthMethod, gateway.Name, gateway.Namespace)
		if err != nil {
			log.Error(err, "error removing acl role bindings")
			return ctrl.Result{}, err
		}
	}

	for _, deletion := range updates.Consul.Deletions {
		log.Info("deleting from Consul", "kind", deletion.Kind, "namespace", deletion.Namespace, "name", deletion.Name)
		if err := r.cache.Delete(ctx, deletion); err != nil {
			log.Error(err, "error deleting config entry")
			return ctrl.Result{}, err
		}
	}

	for _, update := range updates.Consul.Updates {
		entry := update.Entry
		log.Info("updating in Consul", "kind", entry.GetKind(), "namespace", entry.GetNamespace(), "name", entry.GetName())
		err := r.cache.Write(ctx, entry)
		if update.OnUpdate != nil {
			// swallow any potential error with our handler if one is provided
			update.OnUpdate(err)
			continue
		}

		if err != nil {
			log.Error(err, "error updating config entry")
			return ctrl.Result{}, err
		}
	}

	if updates.UpsertGatewayDeployment {
		// We only do some registration/deregistraion if we still have a valid gateway
		// otherwise, we've already deregistered everything related to the gateway, so
		// no need to do any of the following.
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
	}

	for _, update := range updates.Kubernetes.Updates.Operations() {
		log.Info("update in Kubernetes", "kind", update.GetObjectKind().GroupVersionKind().Kind, "namespace", update.GetNamespace(), "name", update.GetName())
		if err := r.updateAndResetStatus(ctx, update); err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("error updating object for gateway, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "error updating object")
			return ctrl.Result{}, err
		}
	}

	for _, update := range updates.Kubernetes.StatusUpdates.Operations() {
		log.Info("update status in Kubernetes", "kind", update.GetObjectKind().GroupVersionKind().Kind, "namespace", update.GetNamespace(), "name", update.GetName())
		if err := r.Client.Status().Update(ctx, update); err != nil {
			if k8serrors.IsConflict(err) {
				log.Info("error updating status for gateway, will try to re-reconcile")

				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "error updating status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *GatewayController) deregisterAllServices(ctx context.Context, consulKey api.ResourceReference) error {
	services, err := r.gatewayCache.FetchServicesFor(ctx, consulKey)
	if err != nil {
		return err
	}
	for _, service := range services {
		if err := r.cache.Deregister(ctx, api.CatalogDeregistration{
			Node:      service.Node,
			ServiceID: service.ServiceID,
			Namespace: service.Namespace,
		}); err != nil {
			return err
		}
	}
	return nil
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

func configEntriesTo[T api.ConfigEntry](entries []api.ConfigEntry) []T {
	es := []T{}
	for _, e := range entries {
		es = append(es, e.(T))
	}
	return es
}

func (r *GatewayController) deleteGatekeeperResources(ctx context.Context, log logr.Logger, gw *gwv1beta1.Gateway) error {
	gk := gatekeeper.New(log, r.Client, r.ConsulConfig)
	err := gk.Delete(ctx, *gw)
	if err != nil {
		return err
	}

	return nil
}

func (r *GatewayController) updateGatekeeperResources(ctx context.Context, log logr.Logger, gw *gwv1beta1.Gateway, gwcc *v1alpha1.GatewayClassConfig) error {
	gk := gatekeeper.New(log, r.Client, r.ConsulConfig)
	err := gk.Upsert(ctx, *gw, *gwcc, r.HelmConfig)
	if err != nil {
		return err
	}

	return nil
}

// SetupWithGatewayControllerManager registers the controller with the given manager.
func SetupGatewayControllerWithManager(ctx context.Context, mgr ctrl.Manager, config GatewayControllerConfig) (*cache.Cache, binding.Cleaner, error) {
	cacheConfig := cache.Config{
		ConsulClientConfig:      config.ConsulClientConfig,
		ConsulServerConnMgr:     config.ConsulServerConnMgr,
		NamespacesEnabled:       config.NamespacesEnabled,
		Datacenter:              config.Datacenter,
		CrossNamespaceACLPolicy: config.CrossNamespaceACLPolicy,
		Logger:                  mgr.GetLogger(),
	}
	c := cache.New(cacheConfig)
	gwc := cache.NewGatewayCache(ctx, cacheConfig)

	predicate, _ := predicate.LabelSelectorPredicate(
		*metav1.SetAsLabelSelector(map[string]string{
			common.ManagedLabel: "true",
		}),
	)

	r := &GatewayController{
		Client:     mgr.GetClient(),
		Log:        mgr.GetLogger(),
		HelmConfig: config.HelmConfig.Normalize(),
		Translator: common.ResourceTranslator{
			EnableConsulNamespaces: config.HelmConfig.EnableNamespaces,
			ConsulDestNamespace:    config.HelmConfig.ConsulDestinationNamespace,
			EnableK8sMirroring:     config.HelmConfig.EnableNamespaceMirroring,
			MirroringPrefix:        config.HelmConfig.NamespaceMirroringPrefix,
			ConsulPartition:        config.HelmConfig.ConsulPartition,
			Datacenter:             config.Datacenter,
		},
		denyK8sNamespacesSet:  config.DenyK8sNamespacesSet,
		allowK8sNamespacesSet: config.AllowK8sNamespacesSet,
		cache:                 c,
		gatewayCache:          gwc,
		ConsulConfig:          config.ConsulClientConfig,
	}

	cleaner := binding.Cleaner{
		Logger:       mgr.GetLogger(),
		ConsulConfig: config.ConsulClientConfig,
		ServerMgr:    config.ConsulServerConnMgr,
		AuthMethod:   config.HelmConfig.AuthMethod,
	}

	return c, cleaner, ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Pod{}).
		Watches(
			&gwv1beta1.ReferenceGrant{},
			handler.EnqueueRequestsFromMapFunc(r.transformReferenceGrant),
		).
		Watches(
			&gwv1beta1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(r.transformGatewayClass),
		).
		Watches(
			&gwv1beta1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.transformHTTPRoute),
		).
		Watches(
			&gwv1alpha2.TCPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.transformTCPRoute),
		).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.transformSecret),
		).
		Watches(
			&v1alpha1.MeshService{},
			handler.EnqueueRequestsFromMapFunc(r.transformMeshService),
		).
		Watches(
			&corev1.Endpoints{},
			handler.EnqueueRequestsFromMapFunc(r.transformEndpoints),
		).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.transformPods),
			builder.WithPredicates(predicate),
		).
		WatchesRawSource(
			// Subscribe to changes from Consul for APIGateways
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, r.transformConsulGateway).Events()},
			&handler.EnqueueRequestForObject{},
		).
		WatchesRawSource(
			// Subscribe to changes from Consul for HTTPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.HTTPRoute, r.transformConsulHTTPRoute(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		WatchesRawSource(
			// Subscribe to changes from Consul for TCPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.TCPRoute, r.transformConsulTCPRoute(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		WatchesRawSource(
			// Subscribe to changes from Consul for FileSystemCertificates
			&source.Channel{Source: c.Subscribe(ctx, api.FileSystemCertificate, r.transformConsulFileSystemCertificate(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		WatchesRawSource(
			&source.Channel{Source: c.Subscribe(ctx, api.JWTProvider, r.transformConsulJWTProvider(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			&v1alpha1.GatewayPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.transformGatewayPolicy),
		).
		Watches(
			&v1alpha1.RouteRetryFilter{},
			handler.EnqueueRequestsFromMapFunc(r.transformRouteRetryFilter),
		).
		Watches(
			&v1alpha1.RouteTimeoutFilter{},
			handler.EnqueueRequestsFromMapFunc(r.transformRouteTimeoutFilter),
		).
		Watches(
			// Subscribe to changes in RouteAuthFilter custom resources referenced by HTTPRoutes.
			&v1alpha1.RouteAuthFilter{},
			handler.EnqueueRequestsFromMapFunc(r.transformRouteAuthFilter),
		).
		Complete(r)
}

// transformGatewayClass will check the list of GatewayClass objects for a matching
// class, then return a list of reconcile Requests for it.
func (r *GatewayController) transformGatewayClass(ctx context.Context, o client.Object) []reconcile.Request {
	gatewayClass := o.(*gwv1beta1.GatewayClass)
	gatewayList := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Gateway_GatewayClassIndex, gatewayClass.Name),
	}); err != nil {
		return nil
	}
	return common.ObjectsToReconcileRequests(pointersOf(gatewayList.Items))
}

// transformHTTPRoute will check the HTTPRoute object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformHTTPRoute(ctx context.Context, o client.Object) []reconcile.Request {
	route := o.(*gwv1beta1.HTTPRoute)

	refs := refsToRequests(common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, route.Spec.ParentRefs))
	statusRefs := refsToRequests(common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, common.ConvertSliceFunc(route.Status.Parents, func(parentStatus gwv1beta1.RouteParentStatus) gwv1beta1.ParentReference {
		return parentStatus.ParentRef
	})))
	return append(refs, statusRefs...)
}

// transformTCPRoute will check the TCPRoute object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformTCPRoute(ctx context.Context, o client.Object) []reconcile.Request {
	route := o.(*gwv1alpha2.TCPRoute)

	refs := refsToRequests(common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, route.Spec.ParentRefs))
	statusRefs := refsToRequests(common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, common.ConvertSliceFunc(route.Status.Parents, func(parentStatus gwv1beta1.RouteParentStatus) gwv1beta1.ParentReference {
		return parentStatus.ParentRef
	})))
	return append(refs, statusRefs...)
}

// transformSecret will check the Secret object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformSecret(ctx context.Context, o client.Object) []reconcile.Request {
	secret := o.(*corev1.Secret)
	gatewayList := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Secret_GatewayIndex, client.ObjectKeyFromObject(secret).String()),
	}); err != nil {
		return nil
	}
	return common.ObjectsToReconcileRequests(pointersOf(gatewayList.Items))
}

// transformReferenceGrant will check the ReferenceGrant object for a matching
// class, then return a list of reconcile Requests for Gateways referring to it.
func (r *GatewayController) transformReferenceGrant(ctx context.Context, o client.Object) []reconcile.Request {
	// just re-reconcile all gateways for now ideally this will filter down to gateways
	// affected, but technically the blast radius is gateways in the namespace + referencing
	// the namespace + the routes that bind to them.
	gatewayList := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, gatewayList); err != nil {
		return nil
	}

	return common.ObjectsToReconcileRequests(pointersOf(gatewayList.Items))
}

// transformMeshService will return a list of gateways that are referenced
// by a TCPRoute or HTTPRoute that references the mesh service.
func (r *GatewayController) transformMeshService(ctx context.Context, o client.Object) []reconcile.Request {
	service := o.(*v1alpha1.MeshService)
	key := client.ObjectKeyFromObject(service).String()

	return r.gatewaysForRoutesReferencing(ctx, TCPRoute_MeshServiceIndex, HTTPRoute_MeshServiceIndex, key)
}

// transformConsulGateway will return a list of gateways that this corresponds to.
func (r *GatewayController) transformConsulGateway(entry api.ConfigEntry) []types.NamespacedName {
	return []types.NamespacedName{common.EntryToNamespacedName(entry)}
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
				gateways = append(gateways, common.EntryToNamespacedName(gateway))
			}
		}
		return gateways
	}
}

// transformGatewayPolicy will return a list of all gateways that need to be reconcilled.
func (r *GatewayController) transformGatewayPolicy(ctx context.Context, o client.Object) []reconcile.Request {
	gatewayPolicy := o.(*v1alpha1.GatewayPolicy)
	gwNamespace := gatewayPolicy.Spec.TargetRef.Namespace
	if gwNamespace == "" {
		gwNamespace = gatewayPolicy.Namespace
	}
	gatewayRef := types.NamespacedName{
		Namespace: gwNamespace,
		Name:      gatewayPolicy.Spec.TargetRef.Name,
	}
	return []reconcile.Request{
		{
			NamespacedName: gatewayRef,
		},
	}
}

// transformRouteRetryFilter will return a list of routes that need to be reconciled.
func (r *GatewayController) transformRouteRetryFilter(ctx context.Context, o client.Object) []reconcile.Request {
	return r.gatewaysForRoutesReferencing(ctx, "", HTTPRoute_RouteRetryFilterIndex, client.ObjectKeyFromObject(o).String())
}

// transformTimeoutRetryFilter will return a list of routes that need to be reconciled.
func (r *GatewayController) transformRouteTimeoutFilter(ctx context.Context, o client.Object) []reconcile.Request {
	return r.gatewaysForRoutesReferencing(ctx, "", HTTPRoute_RouteTimeoutFilterIndex, client.ObjectKeyFromObject(o).String())
}

func (r *GatewayController) transformRouteAuthFilter(ctx context.Context, o client.Object) []reconcile.Request {
	return r.gatewaysForRoutesReferencing(ctx, "", HTTPRoute_RouteAuthFilterIndex, client.ObjectKeyFromObject(o).String())
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
				gateways = append(gateways, common.EntryToNamespacedName(gateway))
			}
		}
		return gateways
	}
}

func (r *GatewayController) transformConsulFileSystemCertificate(ctx context.Context) func(entry api.ConfigEntry) []types.NamespacedName {
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
				gateways = append(gateways, common.EntryToNamespacedName(gateway))
			}
		}

		return gateways
	}
}

func (r *GatewayController) transformConsulJWTProvider(ctx context.Context) func(entry api.ConfigEntry) []types.NamespacedName {
	return func(entry api.ConfigEntry) []types.NamespacedName {
		var gateways []types.NamespacedName

		jwtEntry := entry.(*api.JWTProviderConfigEntry)
		r.Log.Info("gatewaycontroller", "gateway items", r.cache.List(api.APIGateway))
		for _, gwEntry := range r.cache.List(api.APIGateway) {
			gateway := gwEntry.(*api.APIGatewayConfigEntry)
		LISTENER_LOOP:
			for _, listener := range gateway.Listeners {

				r.Log.Info("override names", "listener", fmt.Sprintf("%#v", listener))
				if listener.Override != nil && listener.Override.JWT != nil {
					for _, provider := range listener.Override.JWT.Providers {
						r.Log.Info("override names", "provider", provider.Name, "entry", jwtEntry.Name)
						if provider.Name == jwtEntry.Name {
							gateways = append(gateways, common.EntryToNamespacedName(gateway))
							continue LISTENER_LOOP
						}
					}
				}

				if listener.Default != nil && listener.Default.JWT != nil {
					for _, provider := range listener.Default.JWT.Providers {
						if provider.Name == jwtEntry.Name {
							gateways = append(gateways, common.EntryToNamespacedName(gateway))
							continue LISTENER_LOOP
						}
					}
				}
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

func (r *GatewayController) transformPods(ctx context.Context, o client.Object) []reconcile.Request {
	pod := o.(*corev1.Pod)

	if gateway, managed := common.GatewayFromPod(pod); managed {
		return []reconcile.Request{
			{NamespacedName: gateway},
		}
	}

	return nil
}

// transformEndpoints will return a list of gateways that are referenced
// by a TCPRoute or HTTPRoute that references the service.
func (r *GatewayController) transformEndpoints(ctx context.Context, o client.Object) []reconcile.Request {
	key := client.ObjectKeyFromObject(o)
	endpoints := o.(*corev1.Endpoints)

	if shouldIgnore(key.Namespace, r.denyK8sNamespacesSet, r.allowK8sNamespacesSet) || isLabeledIgnore(endpoints.Labels) {
		return nil
	}

	return r.gatewaysForRoutesReferencing(ctx, TCPRoute_ServiceIndex, HTTPRoute_ServiceIndex, key.String())
}

// gatewaysForRoutesReferencing returns a mapping of all gateways that are referenced by routes that
// have a backend associated with the given key and index.
func (r *GatewayController) gatewaysForRoutesReferencing(ctx context.Context, tcpIndex, httpIndex, key string) []reconcile.Request {
	requestSet := make(map[types.NamespacedName]struct{})

	if tcpIndex != "" {
		tcpRouteList := &gwv1alpha2.TCPRouteList{}
		if err := r.Client.List(ctx, tcpRouteList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(tcpIndex, key),
		}); err != nil {
			r.Log.Error(err, "unable to list TCPRoutes")
		}
		for _, route := range tcpRouteList.Items {
			for _, ref := range common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, route.Spec.ParentRefs) {
				requestSet[ref] = struct{}{}
			}
		}
	}

	httpRouteList := &gwv1beta1.HTTPRouteList{}
	if err := r.Client.List(ctx, httpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(httpIndex, key),
	}); err != nil {
		r.Log.Error(err, "unable to list HTTPRoutes")
	}
	for _, route := range httpRouteList.Items {
		for _, ref := range common.ParentRefs(common.BetaGroup, common.KindGateway, route.Namespace, route.Spec.ParentRefs) {
			requestSet[ref] = struct{}{}
		}
	}

	requests := []reconcile.Request{}
	for request := range requestSet {
		requests = append(requests, reconcile.Request{NamespacedName: request})
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

func (c *GatewayController) getReferenceGrants(ctx context.Context) ([]gwv1beta1.ReferenceGrant, error) {
	var list gwv1beta1.ReferenceGrantList

	if err := c.Client.List(ctx, &list); err != nil {
		return nil, err
	}

	return list.Items, nil
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
	labels := common.LabelsForGateway(&gateway)

	var list corev1.PodList

	if err := c.Client.List(ctx, &list, client.MatchingLabels(labels)); err != nil {
		return nil, err
	}

	return list.Items, nil
}

func (c *GatewayController) getRelatedHTTPRoutes(ctx context.Context, gateway types.NamespacedName, resources *common.ResourceMap) ([]gwv1beta1.HTTPRoute, error) {
	var list gwv1beta1.HTTPRouteList

	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(HTTPRoute_GatewayIndex, gateway.String()),
	}); err != nil {
		return nil, err
	}

	for _, route := range list.Items {
		resources.ReferenceCountHTTPRoute(route)

		_, err := c.getExternalFiltersForHTTPRoute(ctx, route, resources)
		if err != nil {
			c.Log.Error(err, "unable to list HTTPRoute ExternalFilters")
			return nil, err
		}
	}

	return list.Items, nil
}

func (c *GatewayController) getExternalFiltersForHTTPRoute(ctx context.Context, route gwv1beta1.HTTPRoute, resources *common.ResourceMap) ([]interface{}, error) {
	var externalFilters []interface{}
	for _, rule := range route.Spec.Rules {
		ruleFilters, err := c.filterFiltersForExternalRefs(ctx, route, rule.Filters, resources)
		if err != nil {
			return nil, err
		}
		externalFilters = append(externalFilters, ruleFilters...)

		for _, backendRef := range rule.BackendRefs {
			backendRefFilter, err := c.filterFiltersForExternalRefs(ctx, route, backendRef.Filters, resources)
			if err != nil {
				return nil, err
			}

			externalFilters = append(externalFilters, backendRefFilter...)
		}
	}

	return externalFilters, nil
}

func (c *GatewayController) filterFiltersForExternalRefs(ctx context.Context, route gwv1beta1.HTTPRoute, filters []gwv1beta1.HTTPRouteFilter, resources *common.ResourceMap) ([]interface{}, error) {
	var externalFilters []interface{}

	for _, filter := range filters {
		var externalFilter client.Object

		// check to see if we need to grab this filter
		if filter.ExtensionRef == nil {
			continue
		}
		switch kind := filter.ExtensionRef.Kind; kind {
		case v1alpha1.RouteRetryFilterKind:
			externalFilter = &v1alpha1.RouteRetryFilter{}
		case v1alpha1.RouteTimeoutFilterKind:
			externalFilter = &v1alpha1.RouteTimeoutFilter{}
		case v1alpha1.RouteAuthFilterKind:
			externalFilter = &v1alpha1.RouteAuthFilter{}
		default:
			continue
		}

		// get object from API
		err := c.Client.Get(ctx, client.ObjectKey{
			Name:      string(filter.ExtensionRef.Name),
			Namespace: route.Namespace,
		}, externalFilter)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				c.Log.Info(fmt.Sprintf("externalref %s:%s not found: %v", filter.ExtensionRef.Kind, filter.ExtensionRef.Name, err))
				// ignore, the validation call should mark this route as error
				continue
			} else {
				return nil, err
			}
		}

		// add external ref (or error) to resource map for this route
		resources.AddExternalFilter(externalFilter)
		externalFilters = append(externalFilters, externalFilter)
	}
	return externalFilters, nil
}

func (c *GatewayController) getRelatedGatewayPolicies(ctx context.Context, gateway types.NamespacedName, resources *common.ResourceMap) ([]v1alpha1.GatewayPolicy, error) {
	var list v1alpha1.GatewayPolicyList

	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Gatewaypolicy_GatewayIndex, gateway.String()),
	}); err != nil {
		return nil, err
	}

	// add all policies to the resourcemap
	for _, policy := range list.Items {
		resources.AddGatewayPolicy(&policy)
	}

	return list.Items, nil
}

func (c *GatewayController) getJWTProviders(ctx context.Context, resources *common.ResourceMap) ([]v1alpha1.JWTProvider, error) {
	var list v1alpha1.JWTProviderList

	if err := c.Client.List(ctx, &list, &client.ListOptions{}); err != nil {
		return nil, err
	}

	// add all policies to the resourcemap
	for _, provider := range list.Items {
		resources.AddJWTProvider(&provider)
	}

	return list.Items, nil
}

func (c *GatewayController) getRelatedTCPRoutes(ctx context.Context, gateway types.NamespacedName, resources *common.ResourceMap) ([]gwv1alpha2.TCPRoute, error) {
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
			gatewayClassConfig.Spec.ControllerName != common.GatewayClassControllerName {
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
		return nil, client.IgnoreNotFound(err)
	}
	return &gatewayClass, nil
}

// resource map construction routines

func (c *GatewayController) fetchControlledGateways(ctx context.Context, resources *common.ResourceMap) error {
	set := mapset.NewSet()

	list := gwv1beta1.GatewayClassList{}
	if err := c.Client.List(ctx, &list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(GatewayClass_ControllerNameIndex, common.GatewayClassControllerName),
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

func (c *GatewayController) fetchCertificatesForGateway(ctx context.Context, resources *common.ResourceMap, gateway gwv1beta1.Gateway) error {
	certificates := mapset.NewSet()

	for _, listener := range gateway.Spec.Listeners {
		if listener.TLS != nil {
			for _, cert := range listener.TLS.CertificateRefs {
				if common.NilOrEqual(cert.Group, "") && common.NilOrEqual(cert.Kind, common.KindSecret) {
					certificates.Add(common.IndexedNamespacedNameWithDefault(cert.Name, cert.Namespace, gateway.Namespace))
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

func (c *GatewayController) fetchSecret(ctx context.Context, resources *common.ResourceMap, key types.NamespacedName) error {
	var secret corev1.Secret
	if err := c.Client.Get(ctx, key, &secret); err != nil {
		return client.IgnoreNotFound(err)
	}

	resources.ReferenceCountCertificate(secret)

	return nil
}

func (c *GatewayController) fetchServicesForRoutes(ctx context.Context, resources *common.ResourceMap, tcpRoutes []gwv1alpha2.TCPRoute, httpRoutes []gwv1beta1.HTTPRoute) error {
	serviceBackends := mapset.NewSet()
	meshServiceBackends := mapset.NewSet()

	for _, route := range httpRoutes {
		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				if common.DerefEqual(backend.Group, v1alpha1.ConsulHashicorpGroup) &&
					common.DerefEqual(backend.Kind, v1alpha1.MeshServiceKind) {
					meshServiceBackends.Add(common.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				} else if common.NilOrEqual(backend.Group, "") && common.NilOrEqual(backend.Kind, "Service") {
					serviceBackends.Add(common.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				}
			}
		}
	}

	for _, route := range tcpRoutes {
		for _, rule := range route.Spec.Rules {
			for _, backend := range rule.BackendRefs {
				if common.DerefEqual(backend.Group, v1alpha1.ConsulHashicorpGroup) &&
					common.DerefEqual(backend.Kind, v1alpha1.MeshServiceKind) {
					meshServiceBackends.Add(common.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
				} else if common.NilOrEqual(backend.Group, "") && common.NilOrEqual(backend.Kind, "Service") {
					serviceBackends.Add(common.IndexedNamespacedNameWithDefault(backend.Name, backend.Namespace, route.Namespace))
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

func (c *GatewayController) fetchMeshService(ctx context.Context, resources *common.ResourceMap, key types.NamespacedName) error {
	var service v1alpha1.MeshService
	if err := c.Client.Get(ctx, key, &service); err != nil {
		return client.IgnoreNotFound(err)
	}

	resources.AddMeshService(service)

	return nil
}

func (c *GatewayController) fetchServicesForEndpoints(ctx context.Context, resources *common.ResourceMap, key types.NamespacedName) error {
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

func (c *GatewayController) fetchConsulHTTPRoutes(ref api.ResourceReference, resources *common.ResourceMap) {
	for _, route := range configEntriesTo[*api.HTTPRouteConfigEntry](c.cache.List(api.HTTPRoute)) {
		if routeReferencesGateway(route.Namespace, ref, route.Parents) {
			resources.ReferenceCountConsulHTTPRoute(*route)
		}
	}
}

func (c *GatewayController) fetchConsulTCPRoutes(ref api.ResourceReference, resources *common.ResourceMap) {
	for _, route := range configEntriesTo[*api.TCPRouteConfigEntry](c.cache.List(api.TCPRoute)) {
		if routeReferencesGateway(route.Namespace, ref, route.Parents) {
			resources.ReferenceCountConsulTCPRoute(*route)
		}
	}
}

func (c *GatewayController) fetchConsulFileSystemCertificates(resources *common.ResourceMap) {
	for _, cert := range configEntriesTo[*api.FileSystemCertificateConfigEntry](c.cache.List(api.FileSystemCertificate)) {
		resources.ReferenceCountConsulCertificate(*cert)
	}
}

func routeReferencesGateway(namespace string, ref api.ResourceReference, refs []api.ResourceReference) bool {
	// we don't need to check partition here since they're all in the same partition
	if namespace == "" {
		namespace = "default"
	}

	for _, parent := range refs {
		if common.EmptyOrEqual(parent.Kind, api.APIGateway) {
			if common.DefaultOrEqual(parent.Namespace, namespace, ref.Namespace) &&
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
