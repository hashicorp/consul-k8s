// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"maps"
	"slices"
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

const registrationFinalizer = "registration.finalizers.consul.hashicorp.com"

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
	// get the gateway
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

	log.Info("Registration", "registration", registration)
	// deletion request
	if !registration.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Deregistering service")
		err = r.deregisterService(ctx, log, client, registration)
		if err != nil {
			return ctrl.Result{}, err
		}
		r.updateStatusError(ctx, registration, "ConsulErrorDeregistration", err)
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
	patch := r.AddFinalizersPatch(registration, registrationFinalizer)
	err := r.Patch(ctx, registration, patch)
	if err != nil {
		return err
	}

	regReq := &capi.CatalogRegistration{
		ID:              registration.Spec.ID,
		Node:            registration.Spec.Node,
		Address:         registration.Spec.Address,
		TaggedAddresses: maps.Clone(registration.Spec.TaggedAddresses),
		NodeMeta:        maps.Clone(registration.Spec.NodeMeta),
		Datacenter:      registration.Spec.Datacenter,
		Service: &capi.AgentService{
			ID:                registration.Spec.Service.ID,
			Service:           registration.Spec.Service.Name,
			Tags:              slices.Clone(registration.Spec.Service.Tags),
			Meta:              maps.Clone(registration.Spec.Service.Meta),
			Port:              registration.Spec.Service.Port,
			Address:           registration.Spec.Service.Address,
			SocketPath:        registration.Spec.Service.SocketPath,
			TaggedAddresses:   copyTaggedAddresses(registration.Spec.Service.TaggedAddresses),
			Weights:           capi.AgentWeights(registration.Spec.Service.Weights),
			EnableTagOverride: registration.Spec.Service.EnableTagOverride,
			Namespace:         registration.Spec.Service.Namespace,
			Partition:         registration.Spec.Service.Partition,
			Locality:          copyLocality(registration.Spec.Service.Locality),
		},
		Check:          copyHealthCheck(registration.Spec.HealthCheck),
		SkipNodeUpdate: registration.Spec.SkipNodeUpdate,
		Partition:      registration.Spec.Partition,
	}

	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return err
	}

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)
	return nil
}

func (r *RegistrationsController) deregisterService(ctx context.Context, log logr.Logger, client *capi.Client, registration *v1alpha1.Registration) error {
	patch := r.RemoveFinalizersPatch(registration, registrationFinalizer)
	if err := r.Patch(ctx, registration, patch); err != nil {
		return err
	}

	deregReq := &capi.CatalogDeregistration{
		Node:       registration.Spec.Node,
		Address:    registration.Spec.Address,
		Datacenter: registration.Spec.Datacenter,
		ServiceID:  registration.Spec.Service.ID,
		CheckID:    registration.Spec.HealthCheck.CheckID,
		Namespace:  registration.Spec.Service.Namespace,
		Partition:  registration.Spec.Service.Partition,
	}
	_, err := client.Catalog().Deregister(deregReq, nil)
	if err != nil {
		log.Error(err, "error deregistering service", "svcID", deregReq.ServiceID)
		return err
	}

	log.Info("Successfully deregistered service", "svcID", deregReq.ServiceID)
	return nil
}

func copyTaggedAddresses(taggedAddresses map[string]v1alpha1.ServiceAddress) map[string]capi.ServiceAddress {
	if taggedAddresses == nil {
		return nil
	}
	result := make(map[string]capi.ServiceAddress, len(taggedAddresses))
	for k, v := range taggedAddresses {
		result[k] = capi.ServiceAddress(v)
	}
	return result
}

func copyLocality(locality *v1alpha1.Locality) *capi.Locality {
	if locality == nil {
		return nil
	}
	return &capi.Locality{
		Region: locality.Region,
		Zone:   locality.Zone,
	}
}

func copyHealthCheck(healthCheck *v1alpha1.HealthCheck) *capi.AgentCheck {
	if healthCheck == nil {
		return nil
	}

	// TODO: handle error
	intervalDuration, _ := time.ParseDuration(healthCheck.Definition.IntervalDuration)
	timeoutDuration, _ := time.ParseDuration(healthCheck.Definition.TimeoutDuration)
	deregisterAfter, _ := time.ParseDuration(healthCheck.Definition.DeregisterCriticalServiceAfterDuration)

	return &capi.AgentCheck{
		CheckID:   healthCheck.CheckID,
		Name:      healthCheck.Name,
		Type:      healthCheck.Type,
		Status:    healthCheck.Status,
		ServiceID: healthCheck.ServiceID,
		Output:    healthCheck.Output,
		Namespace: healthCheck.Namespace,
		Definition: capi.HealthCheckDefinition{
			HTTP:                                   healthCheck.Definition.HTTP,
			TCP:                                    healthCheck.Definition.TCP,
			GRPC:                                   healthCheck.Definition.GRPC,
			GRPCUseTLS:                             healthCheck.Definition.GRPCUseTLS,
			Method:                                 healthCheck.Definition.Method,
			Header:                                 healthCheck.Definition.Header,
			Body:                                   healthCheck.Definition.Body,
			TLSServerName:                          healthCheck.Definition.TLSServerName,
			TLSSkipVerify:                          healthCheck.Definition.TLSSkipVerify,
			OSService:                              healthCheck.Definition.OSService,
			IntervalDuration:                       intervalDuration,
			TimeoutDuration:                        timeoutDuration,
			DeregisterCriticalServiceAfterDuration: deregisterAfter,
		},
	}
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *RegistrationsController) updateStatusError(ctx context.Context, registration *v1alpha1.Registration, reason string, reconcileErr error) {
	registration.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
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
