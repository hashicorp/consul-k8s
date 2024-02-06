package resources

import (
	"context"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type gatewayList interface {
	*meshv2beta1.MeshGatewayList | *meshv2beta1.APIGatewayList
	client.ObjectList
	ReconcileRequests() []reconcile.Request
}

func setupGatewayControllerWithManager[L gatewayList](mgr ctrl.Manager, obj client.Object, k8sClient client.Client, gwc reconcile.Reconciler) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Owns(&appsv1.Deployment{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Watches(
			source.NewKindWithCache(&meshv2beta1.GatewayClass{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				gateways, err := getGatewaysReferencingGatewayClass[L](context.Background(), k8sClient, o.(*meshv2beta1.GatewayClass))
				if err != nil {
					return nil
				}

				return gateways.ReconcileRequests()
			})).
		Watches(
			source.NewKindWithCache(&meshv2beta1.GatewayClassConfig{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				classes, err := getGatewayClassesReferencingGatewayClassConfig(context.Background(), k8sClient, o.(*meshv2beta1.GatewayClassConfig))
				if err != nil {
					return nil
				}

				var requests []reconcile.Request
				for _, class := range classes.Items {
					gateways, err := getGatewaysReferencingGatewayClass[L](context.Background(), k8sClient, class)
					if err != nil {
						continue
					}

					requests = append(requests, gateways.ReconcileRequests()...)
				}

				return requests
			})).
		Complete(gwc)
}

func getGatewayClassConfigForGatewayClass(ctx context.Context, k8sClient client.Client, gatewayClass *meshv2beta1.GatewayClass) (*meshv2beta1.GatewayClassConfig, error) {
	if gatewayClass == nil {
		// if we don't have a gateway class we can't fetch the corresponding config
		return nil, nil
	}

	config := &meshv2beta1.GatewayClassConfig{}
	if ref := gatewayClass.Spec.ParametersRef; ref != nil {
		if ref.Group != meshv2beta1.MeshGroup || ref.Kind != "GatewayClassConfig" {
			// TODO @Gateway-Management additionally check for controller name when available
			return nil, nil
		}

		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ref.Name}, config); err != nil {
			return nil, client.IgnoreNotFound(err)
		}
	}
	return config, nil
}

func getGatewayClassForGateway(ctx context.Context, k8sClient client.Client, className string) (*meshv2beta1.GatewayClass, error) {
	var gatewayClass meshv2beta1.GatewayClass

	if err := k8sClient.Get(ctx, types.NamespacedName{Name: className}, &gatewayClass); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &gatewayClass, nil
}

// getGatewayClassesReferencingGatewayClassConfig queries all GatewayClass resources in the
// cluster and returns any that reference the given GatewayClassConfig.
func getGatewayClassesReferencingGatewayClassConfig(ctx context.Context, k8sClient client.Client, config *meshv2beta1.GatewayClassConfig) (*meshv2beta1.GatewayClassList, error) {
	if config == nil {
		return nil, nil
	}

	allClasses := &meshv2beta1.GatewayClassList{}
	if err := k8sClient.List(ctx, allClasses, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(GatewayClass_GatewayClassConfigIndex, config.Name),
	}); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return allClasses, nil
}

// getGatewaysReferencingGatewayClass queries all MeshGateway resources in the cluster
// and returns any that reference the given GatewayClass.
func getGatewaysReferencingGatewayClass[T gatewayList](ctx context.Context, k8sClient client.Client, class *meshv2beta1.GatewayClass) (T, error) {
	if class == nil {
		return nil, nil
	}

	var allGateways T
	if err := k8sClient.List(ctx, allGateways, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(Gateway_GatewayClassIndex, class.Name),
	}); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return allGateways, nil
}

func getGatewayClassConfigForGateway(ctx context.Context, k8sClient client.Client, className string) (*meshv2beta1.GatewayClassConfig, error) {
	gatewayClass, err := getGatewayClassForGateway(ctx, k8sClient, className)
	if err != nil {
		return nil, err
	}

	gatewayClassConfig, err := getGatewayClassConfigForGatewayClass(ctx, k8sClient, gatewayClass)
	if err != nil {
		return nil, err
	}

	return gatewayClassConfig, nil
}
