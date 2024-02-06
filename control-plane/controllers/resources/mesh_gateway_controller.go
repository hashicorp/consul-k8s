// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/gateways"
)

// errResourceNotOwned indicates that a resource the controller would have
// updated or deleted does not have an owner reference pointing to the MeshGateway.
var errResourceNotOwned = errors.New("existing resource not owned by controller")

// MeshGatewayController reconciles a MeshGateway object.
type MeshGatewayController struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Controller    *ConsulResourceController
	GatewayConfig gateways.GatewayConfig
}

// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=meshgateway,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mesh.consul.hashicorp.com,resources=meshgateway/status,verbs=get;update;patch

func (r *MeshGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger(req.NamespacedName)

	// Fetch the resource being reconciled
	resource := &meshv2beta1.MeshGateway{}
	if err := r.Get(ctx, req.NamespacedName, resource); k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Call hooks
	if !resource.GetDeletionTimestamp().IsZero() {
		logger.Info("deletion event")

		if err := r.onDelete(ctx, req, resource); err != nil {
			return ctrl.Result{}, err
		}
	} else {

		// Fetch GatewayClassConfig for the gateway
		gcc, err := getGatewayClassConfigForGateway(ctx, r.Client, resource.Spec.GatewayClassName)
		if err != nil {
			r.Log.Error(err, "unable to get gatewayclassconfig for gateway: %s gatewayclass: %s", resource.Name, resource.Spec.GatewayClassName)
			return ctrl.Result{}, err
		}

		if err := onCreateUpdate(ctx, r.Client, configs{
			gcc:           gcc,
			gatewayConfig: gateways.GatewayConfig{},
		}, resource); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.Controller.ReconcileResource(ctx, r, req, &meshv2beta1.MeshGateway{})
}

func (r *MeshGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *MeshGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *MeshGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupGatewayControllerWithManager[*meshv2beta1.MeshGatewayList](mgr, &meshv2beta1.MeshGateway{}, r.Client, r)
}

type gateway interface {
	*meshv2beta1.MeshGateway | *meshv2beta1.APIGateway
}

type configs struct {
	gcc           *meshv2beta1.GatewayClassConfig
	gatewayConfig gateways.GatewayConfig
}

// onCreateUpdate is responsible for creating/updating all K8s resources that
// are required in order to run a meshv2beta1.MeshGateway. These are created/updated
// in dependency order.
//  1. ServiceAccount
//  2. Deployment
//  3. Service
//  4. Role
//  5. RoleBinding
func onCreateUpdate[T gateways.Gateway](ctx context.Context, k8sClient client.Client, cfg configs, resource T) error {
	builder := gateways.NewGatewayBuilder[T](resource, cfg.gatewayConfig, cfg.gcc)

	// Create ServiceAccount
	desiredAccount := builder.ServiceAccount()
	existingAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: desiredAccount.Namespace, Name: desiredAccount.Name}}

	upsertOp := func(ctx context.Context, _, object client.Object) error {
		_, err := controllerutil.CreateOrUpdate(ctx, k8sClient, object, func() error { return nil })
		return err
	}

	err := opIfNewOrOwned(ctx, resource, k8sClient, existingAccount, desiredAccount, upsertOp)
	if err != nil {
		return fmt.Errorf("unable to create service account: %w", err)
	}

	// Create Role
	desiredRole := builder.Role()
	existingRole := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: desiredRole.Namespace, Name: desiredRole.Name}}

	err = opIfNewOrOwned(ctx, resource, k8sClient, existingRole, desiredRole, upsertOp)
	if err != nil {
		return fmt.Errorf("unable to create role: %w", err)
	}

	// Create RoleBinding
	desiredBinding := builder.RoleBinding()
	existingBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: desiredBinding.Namespace, Name: desiredBinding.Name}}

	err = opIfNewOrOwned(ctx, resource, k8sClient, existingBinding, desiredBinding, upsertOp)
	if err != nil {
		return fmt.Errorf("unable to create role binding: %w", err)
	}

	// Create Service
	desiredService := builder.Service()
	existingService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: desiredService.Namespace, Name: desiredService.Name}}

	mergeServiceOp := func(ctx context.Context, existingObj, desiredObj client.Object) error {
		existing := existingObj.(*corev1.Service)
		desired := desiredObj.(*corev1.Service)

		_, err := controllerutil.CreateOrUpdate(ctx, k8sClient, existing, func() error {
			gateways.MergeService(existing, desired)
			return nil
		})
		return err
	}

	err = opIfNewOrOwned(ctx, resource, k8sClient, existingService, desiredService, mergeServiceOp)
	if err != nil {
		return fmt.Errorf("unable to create service: %w", err)
	}

	// Create Deployment
	desiredDeployment, err := builder.Deployment()
	if err != nil {
		return fmt.Errorf("unable to create deployment: %w", err)
	}
	existingDeployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: desiredDeployment.Namespace, Name: desiredDeployment.Name}}

	mergeDeploymentOp := func(ctx context.Context, existingObj, desiredObj client.Object) error {
		existing := existingObj.(*appsv1.Deployment)
		desired := desiredObj.(*appsv1.Deployment)

		_, err = controllerutil.CreateOrUpdate(ctx, k8sClient, existing, func() error {
			gateways.MergeDeployment(existing, desired)
			return nil
		})
		return err
	}

	err = opIfNewOrOwned(ctx, resource, k8sClient, existingDeployment, desiredDeployment, mergeDeploymentOp)
	if err != nil {
		return fmt.Errorf("unable to create deployment: %w", err)
	}

	return nil
}

// onDelete is responsible for cleaning up any side effects of onCreateUpdate.
// We only clean up side effects because all resources that we create explicitly
// have an owner reference and will thus be cleaned up by the K8s garbage collector
// once the owning meshv2beta1.MeshGateway is deleted.
func (r *MeshGatewayController) onDelete(ctx context.Context, req ctrl.Request, resource *meshv2beta1.MeshGateway) error {
	// TODO NET-6392 NET-6393
	return nil
}

// ownedObjectOp represents an operation that needs to be applied
// only if the newObject does not yet exist or if the existingObject
// has an owner reference pointing to the MeshGateway being reconciled.
//
// The existing and new object are available in case any merging needs
// to occur, such as unknown annotations and values from the existing object
// that need to be carried forward onto the new object.
type ownedObjectOp func(ctx context.Context, existing, desired client.Object) error

// opIfNewOrOwned runs a given ownedObjectOp to create, update, or delete a resource.
// The purpose of opIfNewOrOwned is to ensure that we aren't updating or deleting a
// resource that was not created by us. If this scenario is encountered, we error.
func opIfNewOrOwned(ctx context.Context, gateway client.Object, k8sClient client.Client, existing, desired client.Object, op ownedObjectOp) error {
	// Ensure owner reference is always set on objects that we write
	if err := ctrl.SetControllerReference(gateway, desired, k8sClient.Scheme()); err != nil {
		return err
	}

	key := client.ObjectKey{
		Namespace: existing.GetNamespace(),
		Name:      existing.GetName(),
	}

	exists := false
	if err := k8sClient.Get(ctx, key, existing); err != nil {
		// We failed to fetch the object in a way that doesn't tell us about its existence
		if !k8serr.IsNotFound(err) {
			return err
		}
	} else {
		// We successfully fetched the object, so it exists
		exists = true
	}

	// None exists, so we need only execute the operation
	if !exists {
		return op(ctx, existing, desired)
	}

	// Ensure the existing object was put there by us so that we don't overwrite random objects
	owned := false
	for _, reference := range existing.GetOwnerReferences() {
		if reference.UID == gateway.GetUID() && reference.Name == gateway.GetName() {
			owned = true
			break
		}
	}
	if !owned {
		return errResourceNotOwned
	}
	return op(ctx, existing, desired)
}
