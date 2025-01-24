// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serviceaccount

import (
	"context"

	"github.com/go-logr/logr"
	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	defaultServiceAccountName = "default"
)

type Controller struct {
	client.Client
	// ConsulServerConnMgr is the watcher for the Consul server addresses used to create Consul API v2 clients.
	ConsulServerConnMgr consul.ServerConnectionManager
	// K8sNamespaceConfig manages allow/deny Kubernetes namespaces.
	common.K8sNamespaceConfig
	// ConsulTenancyConfig manages settings related to Consul namespaces and partitions.
	common.ConsulTenancyConfig

	Log logr.Logger

	Scheme *runtime.Scheme
	context.Context
}

func (r *Controller) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ServiceAccount{}).
		Complete(r)
}

// Reconcile reads the state of a ServiceAccount object for a Kubernetes namespace and reconciles the corresponding
// Consul WorkloadIdentity.
func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var serviceAccount corev1.ServiceAccount

	// Ignore the request if the namespace of the service account is not allowed.
	if inject.ShouldIgnore(req.Namespace, r.DenyK8sNamespacesSet, r.AllowK8sNamespacesSet) {
		return ctrl.Result{}, nil
	}

	// Create Consul resource service client for this reconcile.
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		r.Log.Error(err, "failed to create Consul resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	state, err := r.ConsulServerConnMgr.State()
	if err != nil {
		r.Log.Error(err, "failed to query Consul client state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	if state.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-consul-token", state.Token)
	}

	// We don't allow the default service account synced to prevent unintended TrafficPermissions
	if req.Name == defaultServiceAccountName {
		r.Log.Info("Not syncing default Kubernetes service account", "namespace", req.Namespace)
		return ctrl.Result{}, nil
	}

	// If the ServiceAccount object has been deleted (and we get an IsNotFound error),
	// we need to deregister that WorkloadIdentity from Consul.
	err = r.Client.Get(ctx, req.NamespacedName, &serviceAccount)
	if k8serrors.IsNotFound(err) {
		err = r.deregisterWorkloadIdentity(ctx, resourceClient, req.Name, r.getConsulNamespace(req.Namespace), r.getConsulPartition())
		return ctrl.Result{}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get ServiceAccount", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	r.Log.Info("retrieved ServiceAccount", "name", req.Name, "ns", req.Namespace)

	// Ensure the WorkloadIdentity exists.
	workloadIdentityResource := r.getWorkloadIdentityResource(
		serviceAccount.Name, // Consul and Kubernetes service account name will always match
		r.getConsulNamespace(serviceAccount.Namespace),
		r.getConsulPartition(),
		map[string]string{
			constants.MetaKeyKubeNS:                 serviceAccount.Namespace,
			constants.MetaKeyKubeServiceAccountName: serviceAccount.Name,
			constants.MetaKeyManagedBy:              constants.ManagedByServiceAccountValue,
		},
	)

	r.Log.Info("registering workload identity with Consul", getLogFieldsForResource(workloadIdentityResource.Id)...)
	// We currently blindly write these records as changes to service accounts and resulting reconciles should be rare,
	// and there's no data to conflict with in the payload.
	if _, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: workloadIdentityResource}); err != nil {
		// We could be racing with the namespace controller.
		// Requeue (which includes backoff) to try again.
		if inject.ConsulNamespaceIsNotFound(err) {
			r.Log.Info("Consul namespace not found; re-queueing request",
				"service-account", serviceAccount.Name, "ns", serviceAccount.Namespace,
				"consul-ns", workloadIdentityResource.GetId().GetTenancy().GetNamespace(), "err", err.Error())
			return ctrl.Result{Requeue: true}, nil
		}

		r.Log.Error(err, "failed to register workload identity", getLogFieldsForResource(workloadIdentityResource.Id)...)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// deregisterWorkloadIdentity deletes the WorkloadIdentity resource corresponding to the given name and namespace from
// Consul. This operation is idempotent and can be executed for non-existent service accounts.
func (r *Controller) deregisterWorkloadIdentity(ctx context.Context, resourceClient pbresource.ResourceServiceClient, name, namespace, partition string) error {
	_, err := resourceClient.Delete(ctx, &pbresource.DeleteRequest{
		Id: getWorkloadIdentityID(name, namespace, partition),
	})
	return err
}

// getWorkloadIdentityResource converts the given Consul WorkloadIdentity and metadata to a Consul resource API record.
func (r *Controller) getWorkloadIdentityResource(name, namespace, partition string, meta map[string]string) *pbresource.Resource {
	return &pbresource.Resource{
		Id: getWorkloadIdentityID(name, namespace, partition),
		// WorkloadIdentity is currently an empty message.
		Data:     inject.ToProtoAny(&pbauth.WorkloadIdentity{}),
		Metadata: meta,
	}
}

func getWorkloadIdentityID(name, namespace, partition string) *pbresource.ID {
	return &pbresource.ID{
		Name: name,
		Type: pbauth.WorkloadIdentityType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
	}
}

// getConsulNamespace returns the Consul destination namespace for a provided Kubernetes namespace
// depending on Consul Namespaces being enabled and the value of namespace mirroring.
func (r *Controller) getConsulNamespace(kubeNamespace string) string {
	ns := namespaces.ConsulNamespace(
		kubeNamespace,
		r.EnableConsulNamespaces,
		r.ConsulDestinationNamespace,
		r.EnableNSMirroring,
		r.NSMirroringPrefix,
	)

	// TODO: remove this if and when the default namespace of resources is no longer required to be set explicitly.
	if ns == "" {
		ns = constants.DefaultConsulNS
	}
	return ns
}

func (r *Controller) getConsulPartition() string {
	if !r.EnableConsulPartitions || r.ConsulPartition == "" {
		return constants.DefaultConsulPartition
	}
	return r.ConsulPartition
}

func getLogFieldsForResource(id *pbresource.ID) []any {
	return []any{
		"name", id.Name,
		"ns", id.Tenancy.Namespace,
		"partition", id.Tenancy.Partition,
	}
}
