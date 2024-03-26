// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	tenancy "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	FinalizerName                = "finalizers.consul.hashicorp.com"
	ConsulAgentError             = "ConsulAgentError"
	ExternallyManagedConfigError = "ExternallyManagedConfigError"
)

// ResourceController is implemented by resources syncing Consul Resources from their CRD counterparts.
// It is used by ConsulResourceController to abstract CRD-specific Consul Resources.
type ResourceController interface {
	// Update updates the state of the whole object.
	Update(context.Context, client.Object, ...client.UpdateOption) error
	// UpdateStatus updates the state of just the object's status.
	UpdateStatus(context.Context, client.Object, ...client.SubResourceUpdateOption) error
	// Get retrieves an object for the given object key from the Kubernetes Cluster.
	// obj must be a struct pointer so that obj can be updated with the response
	// returned by the Server.
	Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	// Logger returns a logger with values added for the specific controller
	// and request name.
	Logger(types.NamespacedName) logr.Logger
}

// ConsulResourceController is a generic controller that is used to reconcile
// all Consul Resource types, e.g. TrafficPermissions, ProxyConfiguration, etc., since
// they share the same reconcile behaviour.
type ConsulResourceController struct {
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config

	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager

	common.ConsulTenancyConfig
}

// ReconcileResource reconciles an update to a resource. CRD-specific controller's
// call this function because it handles reconciliation of config entries
// generically.
// CRD-specific controller should pass themselves in as updater since we
// need to call back into their own update methods to ensure they update their
// internal state.
func (r *ConsulResourceController) ReconcileResource(ctx context.Context, crdCtrl ResourceController, req ctrl.Request, resource common.ConsulResource) (ctrl.Result, error) {
	logger := crdCtrl.Logger(req.NamespacedName)
	err := crdCtrl.Get(ctx, req.NamespacedName, resource)
	if k8serr.IsNotFound(err) {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	} else if err != nil {
		logger.Error(err, "retrieving resource")
		return ctrl.Result{}, err
	}

	// Create Consul resource service client for this reconcile.
	resourceClient, err := consul.NewResourceServiceClient(r.ConsulServerConnMgr)
	if err != nil {
		logger.Error(err, "failed to create Consul resource client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	state, err := r.ConsulServerConnMgr.State()
	if err != nil {
		logger.Error(err, "failed to query Consul client state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	if state.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "x-consul-token", state.Token)
	}

	if resource.GetDeletionTimestamp().IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then let's add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !slices.Contains(resource.GetFinalizers(), FinalizerName) {
			resource.AddFinalizer(FinalizerName)
			if err := r.syncUnknown(ctx, crdCtrl, resource); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	if !resource.GetDeletionTimestamp().IsZero() {
		if slices.Contains(resource.GetFinalizers(), FinalizerName) {
			// The object is being deleted
			logger.Info("deletion event")
			// Check to see if consul has config entry with the same name
			res, err := resourceClient.Read(ctx, &pbresource.ReadRequest{Id: resource.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})

			// Ignore the error where the resource isn't found in Consul.
			// It is indicative of desired state.
			if err != nil && !isNotFoundErr(err) {
				return ctrl.Result{}, fmt.Errorf("getting resource from Consul: %w", err)
			}

			// In the case this resource was created outside of consul, skip the deletion process and continue
			if !managedByConsulResourceController(res.GetResource()) {
				logger.Info("resource in Consul was created outside of Kubernetes - skipping delete from Consul")
			}

			if err == nil && managedByConsulResourceController(res.GetResource()) {
				_, err := resourceClient.Delete(ctx, &pbresource.DeleteRequest{Id: resource.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})
				if err != nil {
					return r.syncFailed(ctx, logger, crdCtrl, resource, ConsulAgentError,
						fmt.Errorf("deleting resource from Consul: %w", err))
				}
				logger.Info("deletion from Consul successful")
			}
			// remove our finalizer from the list and update it.
			resource.RemoveFinalizer(FinalizerName)
			if err := crdCtrl.Update(ctx, resource); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("finalizer removed")
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Check to see if consul has config entry with the same name
	res, err := resourceClient.Read(ctx, &pbresource.ReadRequest{Id: resource.ResourceID(r.consulNamespace(req.Namespace), r.getConsulPartition())})

	// In the case the namespace doesn't exist in Consul yet, assume we are racing with the namespace controller
	// and requeue.
	if tenancy.ConsulNamespaceIsNotFound(err) {
		logger.Info("Consul namespace not found; re-queueing request",
			"name", req.Name, "ns", req.Namespace, "consul-ns",
			r.consulNamespace(req.Namespace), "err", err.Error())
		return ctrl.Result{Requeue: true}, nil
	}

	// If resource with this name does not exist
	if isNotFoundErr(err) {
		logger.Info("resource not found in Consul")

		// Create the config entry
		_, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: resource.Resource(r.consulNamespace(req.Namespace), r.getConsulPartition())})
		if err != nil {
			return r.syncFailed(ctx, logger, crdCtrl, resource, ConsulAgentError,
				fmt.Errorf("writing resource to Consul: %w", err))
		}

		logger.Info("resource created")
		return r.syncSuccessful(ctx, crdCtrl, resource)
	}

	// If there is an error when trying to get the resource from the api server,
	// fail the reconcile.
	if err != nil {
		return r.syncFailed(ctx, logger, crdCtrl, resource, ConsulAgentError, err)
	}

	// TODO: consider the case where we want to migrate a resource existing into Consul to a CRD with an annotation
	if !managedByConsulResourceController(res.Resource) {
		return r.syncFailed(ctx, logger, crdCtrl, resource, ExternallyManagedConfigError,
			fmt.Errorf("resource already exists in Consul"))
	}

	if !resource.MatchesConsul(res.Resource, r.consulNamespace(req.Namespace), r.getConsulPartition()) {
		logger.Info("resource does not match Consul")
		_, err := resourceClient.Write(ctx, &pbresource.WriteRequest{Resource: resource.Resource(r.consulNamespace(req.Namespace), r.getConsulPartition())})
		if err != nil {
			return r.syncUnknownWithError(ctx, logger, crdCtrl, resource, ConsulAgentError,
				fmt.Errorf("updating resource in Consul: %w", err))
		}
		logger.Info("resource updated")
		return r.syncSuccessful(ctx, crdCtrl, resource)
	} else if resource.SyncedConditionStatus() != corev1.ConditionTrue {
		return r.syncSuccessful(ctx, crdCtrl, resource)
	}

	return ctrl.Result{}, nil
}

// setupWithManager sets up the controller manager for the given resource
// with our default options.
func setupWithManager(mgr ctrl.Manager, resource client.Object, reconciler reconcile.Reconciler) error {
	options := controller.Options{
		// Taken from https://github.com/kubernetes/client-go/blob/master/util/workqueue/default_rate_limiters.go#L39
		// and modified from a starting backoff of 5ms and max of 1000s to a
		// starting backoff of 200ms and a max of 5s to better fit our most
		// common error cases and performance characteristics.
		//
		// One common error case is that a resource is applied that requires
		// a protocol like http or grpc. Often the user will apply a new resource
		// to set the protocol in a minute or two. During this time, the
		// default backoff could then be set up to 5m or more which means the
		// original resource takes a long time to re-sync.
		//
		// In terms of performance, Consul servers can handle tens of thousands
		// of writes per second, so retrying at max every 5s isn't an issue and
		// provides a better UX.
		RateLimiter: workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(200*time.Millisecond, 5*time.Second),
			// 10 qps, 100 bucket size.  This is only for retry speed, and it's only the overall factor (not per item)
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		),
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(resource).
		WithOptions(options).
		Complete(reconciler)
}

func (r *ConsulResourceController) syncFailed(ctx context.Context, logger logr.Logger, updater ResourceController, resource common.ConsulResource, errType string, err error) (ctrl.Result, error) {
	resource.SetSyncedCondition(corev1.ConditionFalse, errType, err.Error())
	if updateErr := updater.UpdateStatus(ctx, resource); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise, the original error would be lost.
		logger.Error(err, "sync failed")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

func (r *ConsulResourceController) syncSuccessful(ctx context.Context, updater ResourceController, resource common.ConsulResource) (ctrl.Result, error) {
	resource.SetSyncedCondition(corev1.ConditionTrue, "", "")
	timeNow := metav1.NewTime(time.Now())
	resource.SetLastSyncedTime(&timeNow)
	return ctrl.Result{}, updater.UpdateStatus(ctx, resource)
}

func (r *ConsulResourceController) syncUnknown(ctx context.Context, updater ResourceController, resource common.ConsulResource) error {
	resource.SetSyncedCondition(corev1.ConditionUnknown, "", "")
	return updater.Update(ctx, resource)
}

func (r *ConsulResourceController) syncUnknownWithError(ctx context.Context,
	logger logr.Logger,
	updater ResourceController,
	resource common.ConsulResource,
	errType string,
	err error,
) (ctrl.Result, error) {
	resource.SetSyncedCondition(corev1.ConditionUnknown, errType, err.Error())
	if updateErr := updater.UpdateStatus(ctx, resource); updateErr != nil {
		// Log the original error here because we are returning the updateErr.
		// Otherwise, the original error would be lost.
		logger.Error(err, "sync status unknown")
		return ctrl.Result{}, updateErr
	}
	return ctrl.Result{}, err
}

// isNotFoundErr checks the grpc response code for "NotFound".
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return codes.NotFound == s.Code()
}

func (r *ConsulResourceController) consulNamespace(namespace string) string {
	ns := namespaces.ConsulNamespace(
		namespace,
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

func (r *ConsulResourceController) getConsulPartition() string {
	if !r.EnableConsulPartitions || r.ConsulPartition == "" {
		return constants.DefaultConsulPartition
	}
	return r.ConsulPartition
}

func managedByConsulResourceController(resource *pbresource.Resource) bool {
	if resource == nil {
		return false
	}

	consulMeta := resource.GetMetadata()
	if consulMeta == nil {
		return false
	}

	if val, ok := consulMeta[common.SourceKey]; ok && val == common.SourceValue {
		return true
	}
	return false
}
