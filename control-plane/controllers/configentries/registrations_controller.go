// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const RegistrationFinalizer = "registration.finalizers.consul.hashicorp.com"

// RegistrationsController is the controller for Registrations resources.
type RegistrationsController struct {
	client.Client
	FinalizerPatcher
	Log                 logr.Logger
	Scheme              *runtime.Scheme
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
}

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicerouters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=servicerouters/status,verbs=get;update;patch

func (r *RegistrationsController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.V(1).WithValues("registration", req.NamespacedName)
	log.Info("Reconciling Registaration")

	registration := &v1alpha1.Registration{}
	// get the registration
	if err := r.Client.Get(ctx, req.NamespacedName, registration); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get registration")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	client, err := consul.NewClientFromConnMgr(r.ConsulClientConfig, r.ConsulServerConnMgr)
	if err != nil {
		log.Error(err, "error initializing consul client")
		return ctrl.Result{}, err
	}

	// deletion request
	if !registration.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Deregistering service")
		err = r.deregisterService(ctx, log, client, registration)
		if err != nil {
			r.updateStatusError(ctx, registration, "ConsulErrorDeregistration", err)
			return ctrl.Result{}, err
		}
		err := r.updateStatus(ctx, req.NamespacedName)
		if err != nil {
			log.Error(err, "failed to update status")
		}

		return ctrl.Result{}, nil
	}

	log.Info("Registering service")
	err = r.registerService(ctx, log, client, registration)
	if err != nil {
		r.updateStatusError(ctx, registration, "ConsulErrorRegistration", err)
		return ctrl.Result{}, err
	}

	err = r.updateStatus(ctx, req.NamespacedName)
	if err != nil {
		log.Error(err, "failed to update status")
	}
	return ctrl.Result{}, err
}

func (r *RegistrationsController) registerService(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	patch := r.AddFinalizersPatch(registration, RegistrationFinalizer)

	err := r.Patch(ctx, registration, patch)
	if err != nil {
		return err
	}

	regReq := registration.ToCatalogRegistration()

	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return err
	}

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)
	return nil
}

func (r *RegistrationsController) deregisterService(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	deRegReq := registration.ToCatalogDeregistration()

	_, err := client.Catalog().Deregister(deRegReq, nil)
	if err != nil {
		log.Error(err, "error deregistering service", "svcID", deRegReq.ServiceID)
		return err
	}

	patch := r.RemoveFinalizersPatch(registration, RegistrationFinalizer)

	if err := r.Patch(ctx, registration, patch); err != nil {
		return err
	}
	log.Info("Successfully deregistered service", "svcID", deRegReq.ServiceID)
	return nil
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *RegistrationsController) updateStatusError(ctx context.Context, registration *v1alpha1.Registration, reason string, reconcileErr error) {
	registration.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
	registration.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}

	err := r.Status().Update(ctx, registration)
	if err != nil {
		r.Log.Error(err, "failed to update Registration status", "name", registration.Name, "namespace", registration.Namespace)
	}
}

func (r *RegistrationsController) updateStatus(ctx context.Context, req types.NamespacedName) error {
	registration := &v1alpha1.Registration{}

	err := r.Get(ctx, req, registration)
	if err != nil {
		return err
	}

	registration.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	registration.SetSyncedCondition(corev1.ConditionTrue, "", "")

	err = r.Status().Update(ctx, registration)
	if err != nil {
		r.Log.Error(err, "failed to update Registration status", "name", registration.Name, "namespace", registration.Namespace)
		return err
	}
	return nil
}

func (r *RegistrationsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &v1alpha1.Registration{}, r)
}
