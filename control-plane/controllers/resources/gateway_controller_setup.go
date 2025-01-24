// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/fields"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
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
			&meshv2beta1.GatewayClass{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
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
			&meshv2beta1.GatewayClassConfig{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
				gcc := o.(*meshv2beta1.GatewayClassConfig)
				if gcc == nil {
					return nil
				}

				classes, err := getGatewayClassesReferencingGatewayClassConfig(ctx, k8sClient, gcc.Name)
				if err != nil {
					return nil
				}

				var requests []reconcile.Request
				for _, class := range classes.Items {
					if class == nil {
						continue
					}

					gateways, err := getGatewaysReferencingGatewayClass[L](ctx, k8sClient, class.Name, index)
					if err != nil {
						continue
					}

					requests = append(requests, gateways.ReconcileRequests()...)
				}

				return requests
			})).
		Complete(gwc)
}

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
// and returns any that reference the given GatewayClass by name.
func getGatewaysReferencingGatewayClass[T gatewayList](ctx context.Context, k8sClient client.Client, className string, index indexName) (T, error) {
	var allGateways T
	if err := k8sClient.List(ctx, allGateways, &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(string(index), className),
	}); err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return allGateways, nil
}
