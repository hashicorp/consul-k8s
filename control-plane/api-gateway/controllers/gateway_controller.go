// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/binding"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/gatekeeper"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/cache"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
)

const (
	gatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	kindGateway = "Gateway"
)

// GatewayControllerConfig holds the values necessary for configuring the GatewayController.
type GatewayControllerConfig struct {
	HelmConfig          apigateway.HelmConfig
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	NamespacesEnabled   bool
	Partition           string
}

// GatewayController reconciles a Gateway object.
// The Gateway is responsible for defining the behavior of API gateways.
type GatewayController struct {
	HelmConfig apigateway.HelmConfig
	Log        logr.Logger
	Translator translation.K8sToConsulTranslator
	cache      *cache.Cache
	client.Client
}

func buildOpts(ref api.ConfigEntry) cmp.Option {
	switch v := ref.(type) {
	case *api.APIGatewayConfigEntry:
		return cmpopts.IgnoreFields(api.APIGatewayConfigEntry{}, "Status", "ModifyIndex", "CreateIndex")
	case *api.HTTPRouteConfigEntry:
		return cmpopts.IgnoreFields(api.HTTPRouteConfigEntry{}, "Status", "ModifyIndex", "CreateIndex")
	case *api.TCPRouteConfigEntry:
		return cmpopts.IgnoreFields(api.TCPRouteConfigEntry{}, "Status", "ModifyIndex", "CreateIndex")
	case *api.InlineCertificateConfigEntry:
		return cmpopts.IgnoreFields(api.InlineCertificateConfigEntry{}, "Status", "ModifyIndex", "CreateIndex")
	default:
		panic(fmt.Sprintf("type is not known: %+v", v))
	}
}

// Reconcile handles the reconciliation loop for Gateway objects.
func (r *GatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)
<<<<<<< HEAD
	log.Info("Reconciling the Gateway: ", "gatewayName", req.Name)
=======
	log.Info("Reconciling the Gateway: ")
>>>>>>> 1cc80777 (still has some print statements, seeing issues with updates)

	// If gateway does not exist, log an error.
	var gw gwv1beta1.Gateway
	err := r.Client.Get(ctx, req.NamespacedName, &gw)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to get Gateway")
		return ctrl.Result{}, err
	}

	// If gateway class on the gateway does not exist, log an error.
	gwc := &gwv1beta1.GatewayClass{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}, gwc)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get GatewayClass")
			return ctrl.Result{}, err
		}
		gwc = nil
	}

	gwcc, err := getConfigForGatewayClass(ctx, r.Client, gwc)
	if err != nil {
		log.Error(err, "error fetching the gateway class config")
		return ctrl.Result{}, err
	}

	// fetch all namespaces
	namespaceList := &corev1.NamespaceList{}
	if err := r.Client.List(ctx, namespaceList); err != nil {
		log.Error(err, "unable to list Namespaces")
		return ctrl.Result{}, err
	}
	namespaces := map[string]corev1.Namespace{}
	for _, namespace := range namespaceList.Items {
		namespaces[namespace.Name] = namespace
	}

	// fetch all gateways we control for reference counting
	gwcList := &gwv1beta1.GatewayClassList{}
	if err := r.Client.List(ctx, gwcList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(GatewayClass_ControllerNameIndex, GatewayClassControllerName),
	}); err != nil {
		log.Error(err, "unable to list GatewayClasses")
		return ctrl.Result{}, err
	}

	gwList := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, gwList); err != nil {
		log.Error(err, "unable to list Gateways")
		return ctrl.Result{}, err
	}

	controlled := map[types.NamespacedName]gwv1beta1.Gateway{}
	for _, gwc := range gwcList.Items {
		for _, gw := range gwList.Items {
			if string(gw.Spec.GatewayClassName) == gwc.Name {
				controlled[types.NamespacedName{Namespace: gw.Namespace, Name: gw.Name}] = gw
			}
		}
	}

	// fetch all secrets referenced by this gateway
	secretList := &corev1.SecretList{}
	if err := r.Client.List(ctx, secretList); err != nil {
		log.Error(err, "unable to list Secrets")
		return ctrl.Result{}, err
	}

	listenerCerts := make(map[types.NamespacedName]struct{})
	for _, listener := range gw.Spec.Listeners {
		if listener.TLS != nil {
			for _, ref := range listener.TLS.CertificateRefs {
				if nilOrEqual(ref.Group, "") && nilOrEqual(ref.Kind, "Secret") {
					listenerCerts[indexedNamespacedNameWithDefault(ref.Name, ref.Namespace, gw.Namespace)] = struct{}{}
				}
			}
		}
	}

	filteredSecrets := []corev1.Secret{}
	for _, secret := range secretList.Items {
		namespacedName := types.NamespacedName{Namespace: secret.Namespace, Name: secret.Name}
		if _, ok := listenerCerts[namespacedName]; ok {
			filteredSecrets = append(filteredSecrets, secret)
		}
	}

	// fetch all http routes referencing this gateway
	httpRouteList := &gwv1beta1.HTTPRouteList{}
	if err := r.Client.List(ctx, httpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(HTTPRoute_GatewayIndex, req.String()),
	}); err != nil {
		log.Error(err, "unable to list HTTPRoutes")
		return ctrl.Result{}, err
	}

	// fetch all tcp routes referencing this gateway
	tcpRouteList := &gwv1alpha2.TCPRouteList{}
	if err := r.Client.List(ctx, tcpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(TCPRoute_GatewayIndex, req.String()),
	}); err != nil {
		log.Error(err, "unable to list TCPRoutes")
		return ctrl.Result{}, err
	}

	httpRoutes := r.cache.List(api.HTTPRoute)
	tcpRoutes := r.cache.List(api.TCPRoute)
	inlineCertificates := r.cache.List(api.InlineCertificate)
	services := r.cache.ListServices()

	binder := binding.NewBinder(binding.BinderConfig{
		Translator:               r.Translator,
		ControllerName:           GatewayClassControllerName,
		GatewayClassConfig:       gwcc,
		GatewayClass:             gwc,
		Gateway:                  gw,
		HTTPRoutes:               httpRouteList.Items,
		TCPRoutes:                tcpRouteList.Items,
		Secrets:                  filteredSecrets,
		ConsulHTTPRoutes:         derefAll(configEntriesTo[*api.HTTPRouteConfigEntry](httpRoutes)),
		ConsulTCPRoutes:          derefAll(configEntriesTo[*api.TCPRouteConfigEntry](tcpRoutes)),
		ConsulInlineCertificates: derefAll(configEntriesTo[*api.InlineCertificateConfigEntry](inlineCertificates)),
		ConnectInjectedServices:  services,
		Namespaces:               namespaces,
		ControlledGateways:       controlled,
	})

	updates := binder.Snapshot()

	fmt.Println("--------------------------------------\n", updates)

	if updates.UpsertGatewayDeployment {
		log.Info("updating gatekeeper")
		err := r.updateGatekeeperResources(ctx, log, &gw, gwcc)
		if err != nil {
			log.Error(err, "unable to update gateway resources")
			return ctrl.Result{}, err
		}
	} else {
		log.Info("deleting gatekeeper")
		err := r.deleteGatekeeperResources(ctx, log, &gw)
		if err != nil {
			log.Error(err, "unable to delete gateway resources")
			return ctrl.Result{}, err
		}
	}

	for _, deletion := range updates.Consul.Deletions {
		log.Info("deleting from Consul", "kind", deletion.Kind, "namespace", deletion.Namespace, "name", deletion.Name)
		err = r.cache.Delete(deletion)
		if err != nil {
			log.Error(err, "failed to delete entry from consul", "kind", deletion.Kind, "namespace", deletion.Namespace, "name", deletion.Name)
		}

	}

	for _, update := range updates.Consul.Updates {
		log.Info("updating in Consul", "kind", update.GetKind(), "namespace", update.GetNamespace(), "name", update.GetName())
		ref := translation.EntryToReference(update)
		old := r.cache.Get(ref)
		if cmp.Equal(old, update, buildOpts(update)) {
			continue
		}

		err := r.cache.Write(update)
		if err != nil {
			log.Error(err, "error updating config entry in consul")
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

	/* TODO:
	1.Pull in the deployments that have been created through Gatekeeper previously to check their statuses.
		Leverage health-checking.
			OG impl: Any state change for the deployment/pods we subscribe to and set statuses on the gateway.
			Do a health check on the service.
	2.ReferenceGrants
	  Error out if someone uses an unsupported feature:
			- TLS mode type pass through
	3. Run Gatekeeper Upsert with the GW, GWCC, HelmConfig.
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
	e := make([]T, len(vs))
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
	logger := mgr.GetLogger()

	c := cache.New(cache.Config{
		ConsulClientConfig:  config.ConsulClientConfig,
		ConsulServerConnMgr: config.ConsulServerConnMgr,
		NamespacesEnabled:   config.NamespacesEnabled,
		Partition:           config.Partition,
		Logger:              mgr.GetLogger(),
	})

	translator := translation.NewConsulToNamespaceNameTranslator(c)

	r := &GatewayController{
		Client:     mgr.GetClient(),
		HelmConfig: config.HelmConfig,
		cache:      c,
		Log:        mgr.GetLogger(),
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
			// Subscribe to changes from Consul Connect Services
			&source.Channel{Source: c.SubscribeServices(ctx, func(service *api.CatalogService) []types.NamespacedName {
				nsn := serviceToNamespacedName(service)

				if nsn.Namespace != "" && nsn.Name != "" {
					key := nsn.String()

					requestSet := make(map[types.NamespacedName]struct{})
					tcpRouteList := &gwv1alpha2.TCPRouteList{}
					if err := r.Client.List(ctx, tcpRouteList, &client.ListOptions{
						FieldSelector: fields.OneTermEqualSelector(TCPRoute_ServiceIndex, key),
					}); err != nil {
						logger.Error(err, "unable to list TCPRoutes")
					}
					for _, route := range tcpRouteList.Items {
						for _, ref := range parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs) {
							requestSet[ref] = struct{}{}
						}
					}

					httpRouteList := &gwv1alpha2.HTTPRouteList{}
					if err := r.Client.List(ctx, httpRouteList, &client.ListOptions{
						FieldSelector: fields.OneTermEqualSelector(HTTPRoute_ServiceIndex, key),
					}); err != nil {
						logger.Error(err, "unable to list HTTPRoutes")
					}
					for _, route := range httpRouteList.Items {
						for _, ref := range parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs) {
							requestSet[ref] = struct{}{}
						}
					}

					requests := []types.NamespacedName{}
					for request := range requestSet {
						requests = append(requests, request)
					}
					return requests
				}

				return nil
			}).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for APIGateways
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, translator.BuildConsulGatewayTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for HTTPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.HTTPRoute, translator.BuildConsulHTTPRouteTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for TCPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.TCPRoute, translator.BuildConsulTCPRouteTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for InlineCertificates
			&source.Channel{Source: c.Subscribe(ctx, api.InlineCertificate, translator.BuildConsulInlineCertificateTranslator(ctx, r.transformSecret)).Events()},
			&handler.EnqueueRequestForObject{},
		).Complete(r)
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

func (r *GatewayController) getAllRefsForGateway(ctx context.Context, gw *gwv1beta1.Gateway) ([]metav1.Object, error) {
	objs := make([]metav1.Object, 0)

	// handle http routes
	httpRouteList := &gwv1beta1.HTTPRouteList{}
	err := r.Client.List(ctx, httpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(HTTPRoute_GatewayIndex, types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}.String()),
	})
	if err != nil {
		return nil, err
	}
	for _, route := range httpRouteList.Items {
		objs = append(objs, &route)
	}
	// handle tcp routes
	tcpRouteList := &v1alpha2.TCPRouteList{}
	err = r.Client.List(ctx, tcpRouteList, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(TCPRoute_GatewayIndex, types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}.String()),
	})
	if err != nil {
		return nil, err
	}
	for _, route := range tcpRouteList.Items {
		objs = append(objs, &route)
	}

	// handle secrets
	for _, listener := range gw.Spec.Listeners {
		for _, secret := range listener.TLS.CertificateRefs {
			secretObj := &corev1.Secret{}
			err = r.Client.Get(ctx, indexedNamespacedNameWithDefault(secret.Name, secret.Namespace, gw.Namespace), secretObj)
			if err != nil {
				continue
			}
			objs = append(objs, secretObj)
		}
	}

	return objs, nil
}

// getConfigForGatewayClass returns the relevant GatewayClassConfig for the GatewayClass.
func getConfigForGatewayClass(ctx context.Context, client client.Client, gwc *gwv1beta1.GatewayClass) (*v1alpha1.GatewayClassConfig, error) {
	if gwc == nil {
		// if we don't have a gateway class we can't fetch the corresponding config
		return nil, nil
	}

	config := &v1alpha1.GatewayClassConfig{}
	if ref := gwc.Spec.ParametersRef; ref != nil {
		if string(ref.Group) != v1alpha1.GroupVersion.Group ||
			ref.Kind != v1alpha1.GatewayClassConfigKind ||
			gwc.Spec.ControllerName != GatewayClassControllerName {
			// we don't have supported params, so return nil
			return nil, nil
		}

		err := client.Get(ctx, types.NamespacedName{Name: ref.Name}, config)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
	}
	return config, nil
}
