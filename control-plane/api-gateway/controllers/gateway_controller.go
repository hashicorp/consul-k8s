package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	log.Info("------------Reconciling the Gateway GatewayController", "name", req.Name)

	gw := &gwv1beta1.Gateway{}
	err := r.Client.Get(ctx, req.NamespacedName, gw)
	if err != nil {
		log.Error(err, "unable to get Gateway")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// if gw class doesn't exist log an error
	gwc := &gwv1beta1.GatewayClass{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: string(gw.Spec.GatewayClassName)}, gwc)
	if err != nil {
		log.Error(err, "unable to get GatewayClass")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if string(gwc.Spec.ControllerName) != GatewayClassControllerName || !gw.ObjectMeta.DeletionTimestamp.IsZero() {
		// This Gateway is not for this controller or the gateway is being deleted
		// TODO: Cleanup Consul resources
		log.Info("This Gateway is not for this controller or the gateway is being deleted")
		_, err := RemoveFinalizer(ctx, r.Client, gw, gatewayFinalizer)
		if err != nil {
			log.Error(err, "unable to remove finalizer")
		}

		return ctrl.Result{}, err
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

	//TODO: Handle reconcilation.
	// do the tcp routes

	// do the gateway

	return ctrl.Result{}, nil
}

// SetupWithManager registers the controller with the given manager.
func (r *GatewayController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwv1beta1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Watches(
			source.NewKindWithCache(&gwv1alpha2.TCPRoute{}, mgr.GetCache()), r.tcpRouteFieldIndexEventHandler(ctx),
		).
		Watches(
			source.NewKindWithCache(&gwv1alpha2.HTTPRoute{}, mgr.GetCache()), r.tcpRouteFieldIndexEventHandler(ctx),
		).
		//Watches(
		//	&source.Kind{Type: &corev1.Pod{}},
		//	handler.EnqueueRequestsFromMapFunc(podToGatewayRequest),
		//	builder.WithPredicates(predicate),
		//).
		//Watches(
		//	&source.Kind{Type: &gwv1alpha2.ReferenceGrant{}},
		//	handler.EnqueueRequestsFromMapFunc(r.referenceGrantToGatewayRequests),
		//).
		//Watches(
		//	&source.Kind{Type: &gwv1alpha2.ReferencePolicy{}},
		//	handler.EnqueueRequestsFromMapFunc(r.referencePolicyToGatewayRequests),
		//).
		// TODO: Watches for consul resources.
		Complete(r)
}

// gatewayClassConfigFieldIndexEventHandler returns an EventHandler that will enqueue
// reconcile.Requests for GatewayClass objects that reference the GatewayClassConfig
// object that triggered the event.
func (r *GatewayController) tcpRouteFieldIndexEventHandler(ctx context.Context) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
		// Get all GatewayClass objects from the field index of the GatewayClassConfig which triggered the event.
		var gList gwv1beta1.GatewayList
		err := r.Client.List(ctx, &gList, &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(TCPRoute_GatewayIndex, o.GetName()),
		})
		if err != nil {
			r.Log.Error(err, "unable to list gateway")
		}

		return makeListOfRequestsToReconcile(gList)
	})
}

// TODO: Melisa think about efficiency
// TODO: Is this where we want this to live
// makeListOfRequestsToReconcile will take a list of Gateways and return a list of
// reconcile Requests.
func makeListOfRequestsToReconcile(gateways gwv1beta1.GatewayList) []reconcile.Request {
	requests := make([]reconcile.Request, 0, len(gateways.Items))
	for _, gw := range gateways.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      gw.Name,
				Namespace: gw.Namespace,
			},
		})
	}
	return requests
}
