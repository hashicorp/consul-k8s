// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// PeeringDialerController reconciles a PeeringDialer object.
type PeeringDialerController struct {
	client.Client
	// ConsulClientConfig is the config to create a Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// Log is the logger for this controller.
	Log logr.Logger
	// Scheme is the API scheme that this controller should have.
	Scheme *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringdialers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringdialers/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PeeringDialerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("received request for PeeringDialer:", "name", req.Name, "ns", req.Namespace)

	// Get the PeeringDialer resource.
	dialer := &consulv1alpha1.PeeringDialer{}
	err := r.Client.Get(ctx, req.NamespacedName, dialer)

	// This can be safely ignored as a resource will only ever be not found if it has never been reconciled
	// since we add finalizers to our resources.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("PeeringDialer resource not found. Ignoring resource", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringDialer", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// Create Consul client for this reconcile.
	serverState, err := r.ConsulServerConnMgr.State()
	if err != nil {
		r.Log.Error(err, "failed to get Consul server state", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}
	apiClient, err := consul.NewClientFromConnMgrState(r.ConsulClientConfig, serverState)
	if err != nil {
		r.Log.Error(err, "failed to create Consul API client", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// The DeletionTimestamp is zero when the object has not been marked for deletion. The finalizer is added
	// in case it does not exist to all resources. If the DeletionTimestamp is non-zero, the object has been
	// marked for deletion and goes into the deletion workflow.
	if dialer.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(dialer, finalizerName) {
			controllerutil.AddFinalizer(dialer, finalizerName)
			if err := r.Update(ctx, dialer); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if containsString(dialer.Finalizers, finalizerName) {
			r.Log.Info("PeeringDialer was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
			if err := r.deletePeering(ctx, apiClient, req.Name); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(dialer, finalizerName)
			err := r.Update(ctx, dialer)
			return ctrl.Result{}, err
		}
	}

	// specSecret will be nil if the secret specified by the spec doesn't exist.
	var specSecret *corev1.Secret
	specSecret, err = r.getSecret(ctx, dialer.Secret().Name, dialer.Namespace)
	if err != nil {
		r.updateStatusError(ctx, dialer, kubernetesError, err)
		return ctrl.Result{}, err
	}

	// If specSecret doesn't exist, error because we can only initiate peering if we have a token to initiate with.
	if specSecret == nil {
		err = errors.New("PeeringDialer spec.peer.secret does not exist")
		r.updateStatusError(ctx, dialer, internalError, err)
		return ctrl.Result{}, err
	}

	// Check if the status has a secretRef.
	secretRefSet := false
	if dialer.SecretRef() != nil {
		secretRefSet = true
	}

	// statusSecret will be nil if the secret specified by the status doesn't exist.
	var statusSecret *corev1.Secret
	if secretRefSet {
		statusSecret, err = r.getSecret(ctx, dialer.SecretRef().Name, dialer.Namespace)
		if err != nil {
			r.updateStatusError(ctx, dialer, kubernetesError, err)
			return ctrl.Result{}, err
		}
	}

	// At this point, we know the spec secret exists. If the status secret doesn't
	// exist, then we want to initiate peering and update the status with the secret for the token being used.
	if statusSecret == nil {
		// Whether the peering exists in Consul or not we want to initiate the peering so the status can reflect the
		// correct secret specified in the spec.
		r.Log.Info("the secret in status.secretRef doesn't exist or wasn't set, establishing peering with the existing spec.peer.secret", "secret-name", dialer.Secret().Name, "secret-namespace", dialer.Namespace)
		peeringToken := specSecret.Data[dialer.Secret().Key]
		if err := r.establishPeering(ctx, apiClient, dialer.Name, string(peeringToken)); err != nil {
			r.updateStatusError(ctx, dialer, consulAgentError, err)
			return ctrl.Result{}, err
		} else {
			err := r.updateStatus(ctx, req.NamespacedName, specSecret.ResourceVersion)
			return ctrl.Result{}, err
		}
	} else {
		// At this point, the status secret does exist.
		// If the peering in Consul does not exist, initiate peering.

		// Read the peering from Consul.
		r.Log.Info("reading peering from Consul", "name", dialer.Name)
		peering, _, err := apiClient.Peerings().Read(ctx, dialer.Name, nil)
		if err != nil {
			r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
			return ctrl.Result{}, err
		}
		// TODO(peering): Verify that the existing peering in Consul is an dialer peer. If it is an acceptor peer, an error should be thrown.

		if peering == nil {
			r.Log.Info("status.secret exists, but the peering doesn't exist in Consul; establishing peering with the existing spec.peer.secret", "secret-name", dialer.Secret().Name, "secret-namespace", dialer.Namespace)
			peeringToken := specSecret.Data[dialer.Secret().Key]
			if err := r.establishPeering(ctx, apiClient, dialer.Name, string(peeringToken)); err != nil {
				r.updateStatusError(ctx, dialer, consulAgentError, err)
				return ctrl.Result{}, err
			} else {
				err := r.updateStatus(ctx, req.NamespacedName, specSecret.ResourceVersion)
				return ctrl.Result{}, err
			}
		}

		// Or, if the peering in Consul does exist, compare it to the spec's secret. If there's any
		// differences, initiate peering.
		if r.specStatusSecretsDifferent(dialer, specSecret) {
			r.Log.Info("the spec.peer.secret is different from the status secret, re-establishing peering", "secret-name", dialer.Secret().Name, "secret-namespace", dialer.Namespace)
			peeringToken := specSecret.Data[dialer.Secret().Key]
			if err := r.establishPeering(ctx, apiClient, dialer.Name, string(peeringToken)); err != nil {
				r.updateStatusError(ctx, dialer, consulAgentError, err)
				return ctrl.Result{}, err
			} else {
				err := r.updateStatus(ctx, req.NamespacedName, specSecret.ResourceVersion)
				return ctrl.Result{}, err
			}
		}

		if updated, err := r.versionAnnotationUpdated(dialer); err == nil && updated {
			r.Log.Info("the version annotation was incremented; re-establishing peering with spec.peer.secret", "secret-name", dialer.Secret().Name, "secret-namespace", dialer.Namespace)
			peeringToken := specSecret.Data[dialer.Secret().Key]
			if err := r.establishPeering(ctx, apiClient, dialer.Name, string(peeringToken)); err != nil {
				r.updateStatusError(ctx, dialer, consulAgentError, err)
				return ctrl.Result{}, err
			} else {
				err := r.updateStatus(ctx, req.NamespacedName, specSecret.ResourceVersion)
				return ctrl.Result{}, err
			}
		} else if err != nil {
			r.updateStatusError(ctx, dialer, internalError, err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *PeeringDialerController) specStatusSecretsDifferent(dialer *consulv1alpha1.PeeringDialer, existingSpecSecret *corev1.Secret) bool {
	if dialer.SecretRef().Name != dialer.Secret().Name {
		return true
	}
	if dialer.SecretRef().Key != dialer.Secret().Key {
		return true
	}
	if dialer.SecretRef().Backend != dialer.Secret().Backend {
		return true
	}
	return dialer.SecretRef().ResourceVersion != existingSpecSecret.ResourceVersion
}

func (r *PeeringDialerController) updateStatus(ctx context.Context, dialerObjKey types.NamespacedName, resourceVersion string) error {
	dialer := &consulv1alpha1.PeeringDialer{}
	if err := r.Client.Get(ctx, dialerObjKey, dialer); err != nil {
		return fmt.Errorf("error fetching dialer resource before status update: %w", err)
	}
	dialer.Status.SecretRef = &consulv1alpha1.SecretRefStatus{
		Secret:          *dialer.Spec.Peer.Secret,
		ResourceVersion: resourceVersion,
	}
	dialer.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	dialer.SetSyncedCondition(corev1.ConditionTrue, "", "")
	if peeringVersionString, ok := dialer.Annotations[constants.AnnotationPeeringVersion]; ok {
		peeringVersion, err := strconv.ParseUint(peeringVersionString, 10, 64)
		if err != nil {
			r.Log.Error(err, "failed to update PeeringDialer status", "name", dialer.Name, "namespace", dialer.Namespace)
			return err
		}
		if dialer.Status.LatestPeeringVersion == nil || *dialer.Status.LatestPeeringVersion < peeringVersion {
			dialer.Status.LatestPeeringVersion = pointer.Uint64(peeringVersion)
		}
	}
	err := r.Status().Update(ctx, dialer)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringDialer status", "name", dialer.Name, "namespace", dialer.Namespace)
	}
	return err
}

func (r *PeeringDialerController) updateStatusError(ctx context.Context, dialer *consulv1alpha1.PeeringDialer, reason string, reconcileErr error) {
	dialer.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
	err := r.Status().Update(ctx, dialer)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringDialer status", "name", dialer.Name, "namespace", dialer.Namespace)
	}
}

func (r *PeeringDialerController) getSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	namespacedName := types.NamespacedName{Name: name, Namespace: namespace}
	err := r.Client.Get(ctx, namespacedName, secret)
	if k8serrors.IsNotFound(err) {
		// The secret was deleted.
		return nil, nil
	} else if err != nil {
		r.Log.Error(err, "couldn't get secret", "name", name, "namespace", namespace)
		return nil, err
	}
	return secret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PeeringDialerController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.PeeringDialer{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForPeeringTokens),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.filterPeeringDialers)),
		).Complete(r)
}

// establishPeering is a helper function that calls the Consul api to generate a token for the peer.
func (r *PeeringDialerController) establishPeering(ctx context.Context, apiClient *api.Client, peerName string, peeringToken string) error {
	req := api.PeeringEstablishRequest{
		PeerName:     peerName,
		PeeringToken: peeringToken,
	}
	_, _, err := apiClient.Peerings().Establish(ctx, req, nil)
	if err != nil {
		r.Log.Error(err, "failed to initiate peering", "err", err)
		return err
	}
	return nil
}

// deletePeering is a helper function that calls the Consul api to delete a peering.
func (r *PeeringDialerController) deletePeering(ctx context.Context, apiClient *api.Client, peerName string) error {
	_, err := apiClient.Peerings().Delete(ctx, peerName, nil)
	if err != nil {
		r.Log.Error(err, "failed to delete Peering from Consul", "name", peerName)
		return err
	}
	return nil
}

func (r *PeeringDialerController) versionAnnotationUpdated(dialer *consulv1alpha1.PeeringDialer) (bool, error) {
	if peeringVersionString, ok := dialer.Annotations[constants.AnnotationPeeringVersion]; ok {
		peeringVersion, err := strconv.ParseUint(peeringVersionString, 10, 64)
		if err != nil {
			return false, err
		}
		if dialer.Status.LatestPeeringVersion == nil || *dialer.Status.LatestPeeringVersion < peeringVersion {
			return true, nil
		}
	}
	return false, nil
}

// requestsForPeeringTokens creates a slice of requests for the peering dialer controller.
// It enqueues a request for each dialer that needs to be reconciled. It iterates through
// the list of dialers and creates a request for the dialer that has the same secret as it's
// secret and that of the updated secret that is being watched.
// We compare it to the secret in the spec as the resource is dependent on the secret.
func (r *PeeringDialerController) requestsForPeeringTokens(ctx context.Context, object client.Object) []reconcile.Request {
	r.Log.Info("received update for Peering Token Secret", "name", object.GetName(), "namespace", object.GetNamespace())

	// Get the list of all dialers.
	var dialerList consulv1alpha1.PeeringDialerList
	if err := r.Client.List(ctx, &dialerList); err != nil {
		r.Log.Error(err, "failed to list PeeringDialers")
		return []ctrl.Request{}
	}
	for _, dialer := range dialerList.Items {
		if dialer.Secret().Backend == "kubernetes" {
			if dialer.Secret().Name == object.GetName() && dialer.Namespace == object.GetNamespace() {
				return []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: dialer.Namespace, Name: dialer.Name}}}
			}
		}
	}
	return []ctrl.Request{}
}

// filterPeeringDialers receives meta and object information for Kubernetes resources that are being watched,
// which in this case are Secrets. It only returns true if the Secret is a Peering Token Secret. It reads the labels
// from the meta of the resource and uses the values of the "consul.hashicorp.com/peering-token" label to validate that
// the Secret is a Peering Token Secret.
func (r *PeeringDialerController) filterPeeringDialers(object client.Object) bool {
	secretLabels := object.GetLabels()
	isPeeringToken, ok := secretLabels[constants.LabelPeeringToken]
	if !ok {
		return false
	}
	return isPeeringToken == "true"
}
