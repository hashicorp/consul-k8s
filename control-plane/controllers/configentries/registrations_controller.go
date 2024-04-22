// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capi "github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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
		Node:            "node-virtual",
		Address:         "127.0.0.1",
		TaggedAddresses: map[string]string{},
		NodeMeta:        map[string]string{},
		Datacenter:      "",
		Service: &capi.AgentService{
			ID:      fmt.Sprintf("%s-1234", registration.Spec.Service.Name),
			Service: registration.Spec.Service.Name,
			Address: registration.Spec.Service.Address,
			Port:    registration.Spec.Service.Port,
		},
		SkipNodeUpdate: false,
		Partition:      "",
		Locality:       &capi.Locality{},
	}

	_, err = client.Catalog().Register(regReq, nil)
	if err != nil {
		log.Error(err, "error registering service", "svcName", regReq.Service.Service)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RegistrationsController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *RegistrationsController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *RegistrationsController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.Registration{}, r)
}
