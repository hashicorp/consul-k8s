// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"errors"
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

	capi "github.com/hashicorp/consul/api"

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

func (r *RegistrationsController) handleDeletion(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	log.Info("Deregistering service")
	err := r.deregisterService(log, client, registration)
	if err != nil {
		r.updateStatusError(ctx, log, registration, ConsulErrorDeregistration, err)
		return err
	}
	if r.ConsulClientConfig.APIClientConfig.Token != "" || r.ConsulClientConfig.APIClientConfig.TokenFile != "" {
		err = r.removeTermGWACLRole(log, client, registration)
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

func (r *RegistrationsController) handleRegistration(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	log.Info("Registering service")
	err := r.registerService(log, client, registration)
	if err != nil {
		r.updateStatusError(ctx, log, registration, ConsulErrorRegistration, err)
		return err
	}
	if r.ConsulClientConfig.APIClientConfig.Token != "" || r.ConsulClientConfig.APIClientConfig.TokenFile != "" {
		err = r.updateTermGWACLRole(log, client, registration)
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

func (r *RegistrationsController) updateTermGWACLRole(log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	var role *capi.ACLRole
	for _, r := range roles {
		if strings.HasSuffix(r.Name, "terminating-gateway-acl-role") {
			role = r
			break
		}
	}

	if role == nil {
		log.Info("terminating gateway role not found")
		return errors.New("terminating gateway role not found")
	}

	policy := &capi.ACLPolicy{
		Name:        servicePolicyName(registration.Spec.Service.Name),
		Description: "Write policy for terminating gateways for external service",
		Rules:       `service "zoidberg" { policy = "write" }`,
		Datacenters: []string{registration.Spec.Datacenter},
		Namespace:   registration.Spec.Service.Namespace,
		Partition:   registration.Spec.Service.Partition,
	}

	existingPolicy, _, err := client.ACL().PolicyReadByName(policy.Name, nil)
	if err != nil {
		log.Error(err, "error reading policy")
		return err
	}

	// existingPolicy will never be nil beause of how PolicyReadByName works so we need to check if the ID is empty
	if existingPolicy.ID == "" {
		policy, _, err = client.ACL().PolicyCreate(policy, nil)
		if err != nil {
			return fmt.Errorf("error creating policy: %w", err)
		}
	} else {
		policy = existingPolicy
	}

	role.Policies = append(role.Policies, &capi.ACLRolePolicyLink{Name: policy.Name, ID: policy.ID})

	_, _, err = client.ACL().RoleUpdate(role, nil)
	if err != nil {
		log.Error(err, "error updating role")
		return err
	}

	return nil
}

func (r *RegistrationsController) removeTermGWACLRole(log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	var role *capi.ACLRole
	for _, r := range roles {
		if strings.HasSuffix(r.Name, "terminating-gateway-acl-role") {
			fmt.Printf("Role: %v\n", r)
			role = r
			break
		}
	}

	if role == nil {
		log.Info("terminating gateway role not found")
		return nil
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
		log.Info("policy not found on terminating gateway role", "policyName", expectedPolicyName)
		return nil
	}

	_, _, err = client.ACL().RoleUpdate(role, nil)
	if err != nil {
		log.Error(err, "error updating role")
		return err
	}

	_, err = client.ACL().PolicyDelete(policyID, nil)
	if err != nil {
		log.Error(err, "error deleting service policy")
		return err
	}

	return nil
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
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

func (r *RegistrationsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &v1alpha1.Registration{}, r)
}

func servicePolicyName(name string) string {
	return fmt.Sprintf("%s-write-policy", name)
}
