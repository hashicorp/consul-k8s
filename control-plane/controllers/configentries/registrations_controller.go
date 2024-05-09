// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-multierror"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const RegistrationFinalizer = "registration.finalizers.consul.hashicorp.com"

// Status Reasons
const (
	ConsulErrorRegistration   = "ConsulErrorRegistration"
	ConsulErrorDeregistration = "ConsulErrorDeregistration"
	ConsulErrorACL            = "ConsulErrorACL"
)

// RegistrationsController is the controller for Registrations resources.
type RegistrationsController struct {
	client.Client
	FinalizerPatcher
	Scheme              *runtime.Scheme
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

	client, err := consul.NewClientFromConnMgr(r.ConsulClientConfig, r.ConsulServerConnMgr)
	if err != nil {
		log.Error(err, "error initializing consul client")
		return ctrl.Result{}, err
	}

	// deletion request
	if !registration.ObjectMeta.DeletionTimestamp.IsZero() {
		err := r.handleDeletion(ctx, log, client, registration)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// registration request
	err = r.handleRegistration(ctx, log, client, registration)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *RegistrationsController) handleRegistration(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	log.Info("Registering service")

	patch := r.AddFinalizersPatch(registration, RegistrationFinalizer)
	err := r.Patch(ctx, registration, patch)
	if err != nil {
		return err
	}

	err = r.registerService(log, client, registration)
	if err != nil {
		r.updateStatusError(ctx, log, registration, ConsulErrorRegistration, err)
		return err
	}
	if r.ConsulClientConfig.APIClientConfig.Token != "" || r.ConsulClientConfig.APIClientConfig.TokenFile != "" {
		err = r.updateTermGWACLRole(ctx, log, client, registration)
		if err != nil {
			r.updateStatusError(ctx, log, registration, ConsulErrorACL, err)
			return err
		}
	}
	err = r.updateStatus(ctx, log, registration.NamespacedName())
	if err != nil {
		return err
	}
	return nil
}

func (r *RegistrationsController) registerService(log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	regReq, err := registration.ToCatalogRegistration()
	if err != nil {
		return err
	}

	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return err
	}

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)
	return nil
}

func termGWContainsService(registration *v1alpha1.Registration) func(v1alpha1.LinkedService) bool {
	return func(svc v1alpha1.LinkedService) bool {
		return svc.Name == registration.Spec.Service.Name
	}
}

func (r *RegistrationsController) updateTermGWACLRole(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	termGWList := &v1alpha1.TerminatingGatewayList{}
	err := r.Client.List(ctx, termGWList)
	if err != nil {
		log.Error(err, "error listing terminating gateways")
		return err
	}

	termGWsToUpdate := make([]v1alpha1.TerminatingGateway, 0, len(termGWList.Items))
	for _, termGW := range termGWList.Items {
		if slices.ContainsFunc(termGW.Spec.Services, termGWContainsService(registration)) {
			termGWsToUpdate = append(termGWsToUpdate, termGW)
		}
	}

	if len(termGWsToUpdate) == 0 {
		log.Info("terminating gateway not found")
		return nil
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		log.Error(err, "error reading role list")
		return err
	}

	policy := &capi.ACLPolicy{
		Name:        servicePolicyName(registration.Spec.Service.Name),
		Description: "Write policy for terminating gateways for external service",
		Rules:       fmt.Sprintf(`service %q { policy = "write" }`, registration.Spec.Service.Name),
		Datacenters: []string{registration.Spec.Datacenter},
		Namespace:   registration.Spec.Service.Namespace,
		Partition:   registration.Spec.Service.Partition,
	}

	existingPolicy, _, err := client.ACL().PolicyReadByName(policy.Name, nil)
	if err != nil {
		log.Error(err, "error reading policy")
		return err
	}

	if existingPolicy == nil {
		policy, _, err = client.ACL().PolicyCreate(policy, nil)
		if err != nil {
			return fmt.Errorf("error creating policy: %w", err)
		}
	} else {
		policy = existingPolicy
	}

	mErr := &multierror.Error{}

	for _, termGW := range termGWsToUpdate {
		var role *capi.ACLRole
		for _, r := range roles {
			if strings.HasSuffix(r.Name, fmt.Sprintf("-%s-acl-role", termGW.Name)) {
				role = r
				break
			}
		}

		if role == nil {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = multierror.Append(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		role.Policies = append(role.Policies, &capi.ACLRolePolicyLink{Name: policy.Name, ID: policy.ID})

		_, _, err = client.ACL().RoleUpdate(role, nil)
		if err != nil {
			log.Error(err, "error updating role", "roleName", role.Name)
			mErr = multierror.Append(mErr, fmt.Errorf("error updating role %q", role.Name))
			continue
		}
	}

	return mErr.ErrorOrNil()
}

func (r *RegistrationsController) handleDeletion(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	log.Info("Deregistering service")
	err := r.deregisterService(log, client, registration)
	if err != nil {
		r.updateStatusError(ctx, log, registration, ConsulErrorDeregistration, err)
		return err
	}
	if r.ConsulClientConfig.APIClientConfig.Token != "" || r.ConsulClientConfig.APIClientConfig.TokenFile != "" {
		err = r.removeTermGWACLRole(ctx, log, client, registration)
		if err != nil {
			r.updateStatusError(ctx, log, registration, ConsulErrorACL, err)
			return err
		}
	}
	patch := r.RemoveFinalizersPatch(registration, RegistrationFinalizer)
	err = r.Patch(ctx, registration, patch)
	if err != nil {
		return err
	}

	return nil
}

func (r *RegistrationsController) deregisterService(log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	deRegReq := registration.ToCatalogDeregistration()

	_, err := client.Catalog().Deregister(deRegReq, nil)
	if err != nil {
		log.Error(err, "error deregistering service", "svcID", deRegReq.ServiceID)
		return err
	}

	log.Info("Successfully deregistered service", "svcID", deRegReq.ServiceID)
	return nil
}

func (r *RegistrationsController) removeTermGWACLRole(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	termGWList := &v1alpha1.TerminatingGatewayList{}
	err := r.Client.List(ctx, termGWList)
	if err != nil {
		return err
	}

	termGWsToUpdate := make([]v1alpha1.TerminatingGateway, 0, len(termGWList.Items))
	for _, termGW := range termGWList.Items {
		if slices.ContainsFunc(termGW.Spec.Services, termGWContainsService(registration)) {
			termGWsToUpdate = append(termGWsToUpdate, termGW)
		}
	}

	if len(termGWsToUpdate) == 0 {
		log.Info("terminating gateway not found")
		return nil
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	mErr := &multierror.Error{}
	for _, termGW := range termGWsToUpdate {
		var role *capi.ACLRole
		for _, r := range roles {
			if strings.HasSuffix(r.Name, fmt.Sprintf("-%s-acl-role", termGW.Name)) {
				role = r
				break
			}
		}

		if role == nil {
			log.Info("terminating gateway role not found", "terminatingGatewayName", termGW.Name)
			mErr = multierror.Append(mErr, fmt.Errorf("terminating gateway role not found for %q", termGW.Name))
			continue
		}

		var policyID string

		expectedPolicyName := servicePolicyName(registration.Spec.Service.Name)
		role.Policies = slices.DeleteFunc(role.Policies, func(i *capi.ACLRolePolicyLink) bool {
			if i.Name == expectedPolicyName {
				policyID = i.ID
				return true
			}
			return false
		})

		if policyID == "" {
			log.Info("policy not found on terminating gateway role", "policyName", expectedPolicyName, "terminatingGatewayName", termGW.Name)
			continue
		}

		_, _, err = client.ACL().RoleUpdate(role, nil)
		if err != nil {
			log.Error(err, "error updating role", "roleName", role.Name)
			mErr = multierror.Append(mErr, fmt.Errorf("error updating role %q", role.Name))
			continue
		}

		_, err = client.ACL().PolicyDelete(policyID, nil)
		if err != nil {
			log.Error(err, "error deleting service policy", "policyID", policyID, "policyName", expectedPolicyName)
			mErr = multierror.Append(mErr, fmt.Errorf("error deleting service ACL policy %q", policyID))
			continue
		}
	}

	return mErr.ErrorOrNil()
}

func servicePolicyName(name string) string {
	return fmt.Sprintf("%s-write-policy", name)
}

func (r *RegistrationsController) updateStatusError(ctx context.Context, log logr.Logger, registration *v1alpha1.Registration, reason string, reconcileErr error) {
	registration.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
	registration.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}

	err := r.Status().Update(ctx, registration)
	if err != nil {
		log.Error(err, "failed to update Registration status", "name", registration.Name, "namespace", registration.Namespace)
	}
}

func (r *RegistrationsController) updateStatus(ctx context.Context, log logr.Logger, req types.NamespacedName) error {
	registration := &v1alpha1.Registration{}

	err := r.Get(ctx, req, registration)
	if err != nil {
		return err
	}

	registration.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	registration.SetSyncedCondition(corev1.ConditionTrue, "", "")

	err = r.Status().Update(ctx, registration)
	if err != nil {
		log.Error(err, "failed to update Registration status", "name", registration.Name, "namespace", registration.Namespace)
		return err
	}
	return nil
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *RegistrationsController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.Registration{}, "registrationName", indexerFn); err != nil {
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
		// lookup registrationList by service name add add it to the reconcile request
		registrationList := &v1alpha1.RegistrationList{}

		err := r.Client.List(ctx, registrationList, client.MatchingFields{"registrationName": svc.Name})
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
