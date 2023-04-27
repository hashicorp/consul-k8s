package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
)

const (
	gatewayFinalizer = "gateway-finalizer.consul.hashicorp.com"

	kindGateway = "Gateway"
)

// GatewayController reconciles a Gateway object.
// The GatewayClass is responsible for defining the behavior of API gateways.
type GatewayController struct {
	Log logr.Logger
	client.Client
}

// Reconcile handles the reconciliation loop for Gateway objects.
func (r *GatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gatewayClass", req.NamespacedName)
	log.Info("Reconciling the Gateway in the GatewayController", "name", req.Name)

	// If gateway doesn't exist log an error.
	gw := &gwv1beta1.Gateway{}
	err := r.Client.Get(ctx, req.NamespacedName, gw)
	if err != nil {
		log.Error(err, "unable to get Gateway")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If gateway class on the gateway does not exist, log an error.
	gwc := &gwv1beta1.GatewayClass{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}, gwc)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, client.IgnoreNotFound(err)
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
		// We have a deletion request. Ensure we are not in use.
		used, err := r.isGatewayInUse(ctx, gw)
		if err != nil {
			log.Error(err, "unable to check if GatewayClass is in use")
			return ctrl.Result{}, err
		}
		if used {
			log.Info("GatewayClass is in use, cannot delete")
			return ctrl.Result{}, nil
		}
		// Remove our finalizer.
		if _, err := RemoveFinalizer(ctx, r.Client, gwc, gatewayFinalizer); err != nil {
			log.Error(err, "unable to remove finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	//TODO: serialize gatewayClassConfig onto Gateway.

	didUpdate, err := EnsureFinalizer(ctx, r.Client, gw, gatewayFinalizer)
	if err != nil {
		log.Error(err, "unable to add finalizer")
		return ctrl.Result{}, err
	}
	if didUpdate {
		// We updated the Gateway, requeue to avoid another update.
		return ctrl.Result{}, nil
	}

	//TODO: Handle reconciliation.

	return ctrl.Result{}, nil
}

// isGatewayClassInUse returns true if the given GatewayClass is referenced by any Gateway objects.
func (r *GatewayController) isGatewayInUse(ctx context.Context, g *gwv1beta1.Gateway) (bool, error) {
	list := &gwv1beta1.GatewayList{}
	if err := r.Client.List(ctx, list, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Gateway_GatewayClassIndex, g.Name),
	}); err != nil {
		return false, err
	}

	return len(list.Items) != 0, nil
}

// SetupWithManager registers the controller with the given manager.
func (r *GatewayController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
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
		).Watches(
		source.NewKindWithCache(&gwv1alpha2.TCPRoute{}, mgr.GetCache()),
		handler.EnqueueRequestsFromMapFunc(r.transformTCPRoute(ctx)),
	).Watches(
		source.NewKindWithCache(&corev1.Secret{}, mgr.GetCache()),
		handler.EnqueueRequestsFromMapFunc(r.transformSecret(ctx)),
	).Watches(
		source.NewKindWithCache(&gwv1beta1.ReferenceGrant{}, mgr.GetCache()),
		handler.EnqueueRequestsFromMapFunc(r.transformReferenceGrant(ctx)),
	).
		// TODO: Watches for consul resources.
		Complete(r)
}

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

func (r *GatewayController) transformHTTPRoute(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		route := o.(*gwv1beta1.HTTPRoute)
		return refsToRequests(parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs))
	}
}

func (r *GatewayController) transformTCPRoute(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		route := o.(*gwv1alpha2.TCPRoute)
		return refsToRequests(parentRefs(gwv1beta1.GroupVersion.Group, kindGateway, route.Namespace, route.Spec.ParentRefs))
	}
}

func (r *GatewayController) transformSecret(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		secret := o.(*corev1.Secret)
		gatewayList := &gwv1alpha2.GatewayList{}
		if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(Secret_GatewayIndex, secret.Name),
		}); err != nil {
			return nil
		}
		return objectsToRequests(pointersOf(gatewayList.Items))
	}
}

func (r *GatewayController) transformReferenceGrant(ctx context.Context) func(o client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		// just reconcile all gateways within the namespace
		grant := o.(*gwv1alpha2.ReferenceGrant)
		gatewayList := &gwv1beta1.GatewayList{}
		if err := r.Client.List(ctx, gatewayList, &client.ListOptions{
			Namespace: grant.Namespace,
		}); err != nil {
			return nil
		}
		return objectsToRequests(pointersOf(gatewayList.Items))
	}
}

// objectsToRequests will take a list of objects and return a list of
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

func refsToRequests(objects []types.NamespacedName) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(objects))
	for _, object := range objects {
		requests = append(requests, reconcile.Request{
			NamespacedName: object,
		})
	}
	return requests
}

func parentRefs(group, kind, namespace string, refs []gwv1beta1.ParentReference) []types.NamespacedName {
	indexed := []types.NamespacedName{}
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
