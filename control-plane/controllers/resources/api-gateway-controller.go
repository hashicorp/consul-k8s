// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"

	"github.com/go-logr/logr"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
)

// APIGatewayController reconciles a APIGateway object.
type APIGatewayController struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Controller    *ConsulResourceController
	GatewayConfig gateways.GatewayConfig
}

// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=tcproute,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=tcproute/status,verbs=get;update;patch

func (r *APIGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger(req.NamespacedName)
	logger.Info("Reconciling APIGateway")

	resource := &meshv2beta1.APIGateway{}
	if err := r.Get(ctx, req.NamespacedName, resource); k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Call hooks
	if !resource.GetDeletionTimestamp().IsZero() {
		logger.Info("deletion event")

		if err := onDelete(ctx, req, r.Client, resource); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// Fetch GatewayClassConfig for the gateway
		if resource.Namespace == "" {
			resource.Namespace = "default"
		}

		gcc, err := getGatewayClassConfigByGatewayClassName(ctx, r.Client, resource.Spec.GatewayClassName)
		if err != nil {
			r.Log.Error(err, "unable to get gatewayclassconfig for gateway: %s gatewayclass: %s", resource.Name, resource.Spec.GatewayClassName)
			return ctrl.Result{}, err
		}

		if err := onCreateUpdate(ctx, r.Client, gatewayConfigs{
			gcc:           gcc,
			gatewayConfig: r.GatewayConfig,
		}, resource, gateways.APIGatewayAnnotationKind); err != nil {
			logger.Error(err, "unable to create/update gateway")
			return ctrl.Result{}, err
		}
	}

	return r.Controller.ReconcileResource(ctx, r, req, &meshv2beta1.APIGateway{})
}

func (r *APIGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *APIGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *APIGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupGatewayControllerWithManager[*meshv2beta1.APIGatewayList](mgr, &meshv2beta1.APIGateway{}, r.Client, r, APIGateway_GatewayClassIndex)
}
