// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package registration

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
)

const RegistrationFinalizer = "registration.finalizers.consul.hashicorp.com"

var (
	ErrRegisteringService   = fmt.Errorf("error registering service")
	ErrDeregisteringService = fmt.Errorf("error deregistering service")
	ErrUpdatingACLRoles     = fmt.Errorf("error updating ACL roles")
	ErrRemovingACLRoles     = fmt.Errorf("error removing ACL roles")
)

// RegistrationsController is the controller for Registrations resources.
type RegistrationsController struct {
	client.Client
	configentries.FinalizerPatcher
	Scheme              *runtime.Scheme
	Cache               *RegistrationCache
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	Log                 logr.Logger
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

	log.Info("reg status", "status", registration.Status)

	client, err := consul.NewClientFromConnMgr(r.ConsulClientConfig, r.ConsulServerConnMgr)
	if err != nil {
		log.Error(err, "error initializing consul client")
		return ctrl.Result{}, err
	}

	// deletion request
	if !registration.ObjectMeta.DeletionTimestamp.IsZero() {

		result := r.handleDeletion(ctx, log, client, registration)

		if result.hasErrors() {
			r.UpdateStatus(ctx, log, registration, result)
			return ctrl.Result{}, result.errors()
		}
		return ctrl.Result{}, nil
	}

	// registration request
	result := r.handleRegistration(ctx, log, client, registration)
	r.UpdateStatus(ctx, log, registration, result)
	if result.hasErrors() {
		return ctrl.Result{}, result.errors()
	}

	return ctrl.Result{}, nil
}

func (c *RegistrationsController) watchForDeregistrations(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case svc := <-c.Cache.UpdateChan:
			// get all registrations for the service
			regList := &v1alpha1.RegistrationList{}
			err := c.Client.List(context.Background(), regList, client.MatchingFields{registrationByServiceNameIndex: svc})
			if err != nil {
				c.Log.Error(err, "error listing registrations by service name", "serviceName", svc)
				continue
			}
			for _, reg := range regList.Items {
				c.UpdateStatus(context.Background(), c.Log, &reg, Result{Registration: fmt.Errorf("service %q was deregistered by consul", reg.Spec.Service.Name)})
			}
		}
	}
}

func (r *RegistrationsController) handleRegistration(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) Result {
	log.Info("Registering service")

	patch := r.AddFinalizersPatch(registration, RegistrationFinalizer)
	err := r.Patch(ctx, registration, patch)
	if err != nil {
		err = fmt.Errorf("error adding finalizer: %w", err)
		return Result{Finalizer: err}
	}

	err = r.Cache.RegisterService(ctx, log, registration)
	if err != nil {
		return Result{Sync: err, Registration: fmt.Errorf("%w: %s", ErrRegisteringService, err)}
	}

	if r.Cache.ACLsEnabled() {
		termGWsToUpdate, err := r.terminatingGatewaysToUpdate(ctx, log, registration)
		if err != nil {
			return Result{Sync: err, ACLUpdate: fmt.Errorf("%w: %s", ErrUpdatingACLRoles, err)}
		}

		err = r.Cache.updateTermGWACLRole(log, client, registration, termGWsToUpdate)
		if err != nil {
			return Result{Sync: err, ACLUpdate: fmt.Errorf("%w: %s", ErrUpdatingACLRoles, err)}
		}
	}
	return Result{}
}

func (r *RegistrationsController) terminatingGatewaysToUpdate(ctx context.Context, log logr.Logger, registration *v1alpha1.Registration) ([]v1alpha1.TerminatingGateway, error) {
	termGWList := &v1alpha1.TerminatingGatewayList{}
	err := r.Client.List(ctx, termGWList)
	if err != nil {
		log.Error(err, "error listing terminating gateways")
		return nil, err
	}

	termGWsToUpdate := make([]v1alpha1.TerminatingGateway, 0, len(termGWList.Items))
	for _, termGW := range termGWList.Items {
		if slices.ContainsFunc(termGW.Spec.Services, termGWContainsService(registration)) {
			termGWsToUpdate = append(termGWsToUpdate, termGW)
		}
	}

	return termGWsToUpdate, nil
}

func termGWContainsService(registration *v1alpha1.Registration) func(v1alpha1.LinkedService) bool {
	return func(svc v1alpha1.LinkedService) bool {
		return svc.Name == registration.Spec.Service.Name
	}
}

func (r *RegistrationsController) handleDeletion(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) Result {
	log.Info("Deregistering service")
	err := r.Cache.DeregisterService(ctx, log, registration)
	if err != nil {
		return Result{Sync: err, Registration: fmt.Errorf("%w: %s", ErrDeregisteringService, err)}
	}

	if r.Cache.ACLsEnabled() {
		termGWsToUpdate, err := r.terminatingGatewaysToUpdate(ctx, log, registration)
		if err != nil {
			return Result{Sync: err, ACLUpdate: fmt.Errorf("%w: %s", ErrRemovingACLRoles, err)}
		}

		err = r.Cache.removeTermGWACLRole(log, client, registration, termGWsToUpdate)
		if err != nil {
			return Result{Sync: err, ACLUpdate: fmt.Errorf("%w: %s", ErrRemovingACLRoles, err)}
		}
	}

	patch := r.RemoveFinalizersPatch(registration, RegistrationFinalizer)
	err = r.Patch(ctx, registration, patch)
	if err != nil {
		return Result{Finalizer: err}
	}

	return Result{}
}

func (r *RegistrationsController) UpdateStatus(ctx context.Context, log logr.Logger, registration *v1alpha1.Registration, result Result) error {
	registration.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	registration.Status.Conditions = v1alpha1.Conditions{
		syncedCondition(result),
		registrationCondition(result),
	}

	if r.Cache.ACLsEnabled() {
		registration.Status.Conditions = append(registration.Status.Conditions, aclCondition(result))
	}

	err := r.Status().Update(ctx, registration)
	if err != nil {
		log.Error(err, "failed to update Registration status", "name", registration.Name, "namespace", registration.Namespace)
		return err
	}
	return nil
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

const registrationByServiceNameIndex = "registrationName"

func (r *RegistrationsController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// setup the cache
	go r.Cache.Run(ctx, r.Log)
	r.Cache.WaitSynced(ctx)

	go r.watchForDeregistrations(ctx)

	// setup the index to lookup registrations by service name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Registration{}, registrationByServiceNameIndex, indexerFn); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Registration{}).
		Watches(&v1alpha1.TerminatingGateway{}, handler.EnqueueRequestsFromMapFunc(r.transformTerminatingGateways)).
		Complete(r)
}

func indexerFn(o client.Object) []string {
	reg := o.(*v1alpha1.Registration)
	return []string{reg.Spec.Service.Name}
}

func (r *RegistrationsController) transformTerminatingGateways(ctx context.Context, o client.Object) []reconcile.Request {
	termGW := o.(*v1alpha1.TerminatingGateway)
	reqs := make([]reconcile.Request, 0, len(termGW.Spec.Services))
	for _, svc := range termGW.Spec.Services {
		// lookup registrationList by service name and add it to the reconcile request
		registrationList := &v1alpha1.RegistrationList{}

		err := r.Client.List(ctx, registrationList, client.MatchingFields{registrationByServiceNameIndex: svc.Name})
		if err != nil {
			r.Log.Error(err, "error listing registrations by service name", "serviceName", svc.Name)
			continue
		}

		for _, reg := range registrationList.Items {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      reg.Name,
					Namespace: reg.Namespace,
				},
			})
		}
	}
	return reqs
}
