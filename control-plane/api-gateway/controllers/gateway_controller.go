package controllers

import (
	"context"

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

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
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
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	NamespacesEnabled   bool
	Partition           string
}

// GatewayController reconciles a Gateway object.
// The Gateway is responsible for defining the behavior of API gateways.
type GatewayController struct {
	cache *cache.Cache
	Log   logr.Logger
	client.Client
}

// Reconcile handles the reconciliation loop for Gateway objects.
func (r *GatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)
	log.Info("Reconciling the Gateway: ", req.Name)

	// If gateway does not exist, log an error.
	gw := &gwv1beta1.Gateway{}
	err := r.Client.Get(ctx, req.NamespacedName, gw)
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
		if k8serrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, err
	}

	if string(gwc.Spec.ControllerName) != GatewayClassControllerName || !gw.ObjectMeta.DeletionTimestamp.IsZero() {
		// This Gateway is not for this controller or the gateway is being deleted.
		// TODO: Cleanup Consul resources.
		_, err := RemoveFinalizer(ctx, r.Client, gw, gatewayFinalizer)
		if err != nil {
			log.Error(err, "unable to remove finalizer")
		}

		return ctrl.Result{}, err
	}

	if !gw.ObjectMeta.DeletionTimestamp.IsZero() {
		// We have a deletion request. Remove our finalizer.
		if _, err := RemoveFinalizer(ctx, r.Client, gw, gatewayFinalizer); err != nil {
			log.Error(err, "unable to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// TODO: serialize gatewayClassConfig onto Gateway.

	didUpdate, err := EnsureFinalizer(ctx, r.Client, gw, gatewayFinalizer)
	if err != nil {
		log.Error(err, "unable to add finalizer")
		return ctrl.Result{}, err
	}
	if didUpdate {
		// We updated the Gateway, requeue to avoid another update.
		return ctrl.Result{}, nil
	}

	// TODO: Handle reconciliation.

	return ctrl.Result{}, nil
}

// SetupWithGatewayControllerManager registers the controller with the given manager.
func SetupGatewayControllerWithManager(ctx context.Context, mgr ctrl.Manager, config GatewayControllerConfig) (*cache.Cache, error) {
	c := cache.New(cache.Config{
		ConsulClientConfig:  config.ConsulClientConfig,
		ConsulServerConnMgr: config.ConsulServerConnMgr,
		NamespacesEnabled:   config.NamespacesEnabled,
		Partition:           config.Partition,
		Logger:              mgr.GetLogger(),
	})

	r := &GatewayController{
		Client: mgr.GetClient(),
		cache:  c,
		Log:    mgr.GetLogger(),
	}

	translator := translation.NewConsulToNamespaceNameTranslator(c)

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
			// Subscribe to changes from Consul for APIGateways
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, translator.BuildConsulGatewayTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for HTTPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, translator.BuildConsulHTTPRouteTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for TCPRoutes
			&source.Channel{Source: c.Subscribe(ctx, api.APIGateway, translator.BuildConsulTCPRouteTranslator(ctx)).Events()},
			&handler.EnqueueRequestForObject{},
		).
		Watches(
			// Subscribe to changes from Consul for InlineCertificates
			&source.Channel{Source: c.Subscribe(ctx, api.InlineCertificate, translator.BuildConsulInlineCertificateTranslator(ctx, r.transformSecret)).Events()},
			&handler.EnqueueRequestForObject{},
		).Complete(r)
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
