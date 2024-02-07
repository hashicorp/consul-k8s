package resources

import (
	"context"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
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

func setupGatewayControllerWithManager[L gatewayList](mgr ctrl.Manager, obj client.Object, k8sClient client.Client, gwc reconcile.Reconciler, index indexName) error {
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
				gc := o.(*meshv2beta1.GatewayClass)
				if gc == nil {
					return nil
				}

				gateways, err := getGatewaysReferencingGatewayClass[L](context.Background(), k8sClient, gc.Name, index)
				if err != nil {
					return nil
				}

				return gateways.ReconcileRequests()
			})).
		Watches(
			source.NewKindWithCache(&meshv2beta1.GatewayClassConfig{}, mgr.GetCache()),
			handler.EnqueueRequestsFromMapFunc(func(o client.Object) []reconcile.Request {
				gcc := o.(*meshv2beta1.GatewayClassConfig)
				if gcc == nil {
					return nil
				}

				classes, err := getGatewayClassesByGatewayClassConfigName(context.Background(), k8sClient, gcc.Name)
				if err != nil {
					return nil
				}

				var requests []reconcile.Request
				for _, class := range classes.Items {
					if class == nil {
						continue
					}

					gateways, err := getGatewaysReferencingGatewayClass[L](context.Background(), k8sClient, class.Name, index)
					if err != nil {
						continue
					}

					requests = append(requests, gateways.ReconcileRequests()...)
				}

				return requests
			})).
		Complete(gwc)
}

// TODO: uncomment when moving the CRUD hooks from mesh gateway controller
//func getGatewayClassConfigByGatewayClassName(ctx context.Context, k8sClient client.Client, className string) (*meshv2beta1.GatewayClassConfig, error) {
//	gatewayClass, err := getGatewayClassByName(ctx, k8sClient, className)
//	if err != nil {
//		return nil, err
//	}
//
//	if gatewayClass == nil {
//		return nil, nil
//	}
//
//	gatewayClassConfig := &meshv2beta1.GatewayClassConfig{}
//	if ref := gatewayClass.Spec.ParametersRef; ref != nil {
//		if ref.Group != meshv2beta1.MeshGroup || ref.Kind != v2beta1.KindGatewayClassConfig {
//			// TODO @Gateway-Management additionally check for controller name when available
//			return nil, nil
//		}
//
//		if err := k8sClient.Get(ctx, types.NamespacedName{Name: ref.Name}, gatewayClassConfig); err != nil {
//			return nil, client.IgnoreNotFound(err)
//		}
//	}
//	return gatewayClassConfig, nil
//}
//
//func getGatewayClassByName(ctx context.Context, k8sClient client.Client, className string) (*meshv2beta1.GatewayClass, error) {
//var gatewayClass meshv2beta1.GatewayClass
//
//	if err := k8sClient.Get(ctx, types.NamespacedName{Name: className}, &gatewayClass); err != nil {
//		return nil, client.IgnoreNotFound(err)
//	}
//	return &gatewayClass, nil
//}

// getGatewayClassesReferencingGatewayClassConfig queries all GatewayClass resources in the
// cluster and returns any that reference the given GatewayClassConfig by name.
func getGatewayClassesReferencingGatewayClassConfig(ctx context.Context, k8sClient client.Client, configName string) (*meshv2beta1.GatewayClassList, error) {
	allClasses := &meshv2beta1.GatewayClassList{}
	if err := k8sClient.List(ctx, allClasses, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(string(GatewayClass_GatewayClassConfigIndex), configName),
	}); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return allClasses, nil
}

// getGatewaysReferencingGatewayClass queries all xGateway resources in the cluster
// and returns any that reference the given GatewayClass.
func getGatewaysReferencingGatewayClass[T gatewayList](ctx context.Context, k8sClient client.Client, className string, index indexName) (T, error) {
	var allGateways T
	if err := k8sClient.List(ctx, allGateways, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(string(index), className),
	}); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return allGateways, nil
}
