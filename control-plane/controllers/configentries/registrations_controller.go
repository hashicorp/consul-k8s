// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

var _ Controller = (*RegistrationsController)(nil)

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

	var registration v1alpha1.Registration
	// get the gateway
	if err := r.Client.Get(ctx, req.NamespacedName, &registration); err != nil {
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

	fmt.Println(regReq)
	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return ctrl.Result{}, err
	}

	log.Info("Successfully registered service", "svcName", regReq.Service.Service)
	return ctrl.Result{}, nil
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

func (r *RegistrationsController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *RegistrationsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &v1alpha1.Registration{}, r)
}
