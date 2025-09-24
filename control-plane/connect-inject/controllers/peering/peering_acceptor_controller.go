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
	"k8s.io/utils/ptr"
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

// AcceptorController reconciles a PeeringAcceptor object.
type AcceptorController struct {
	client.Client
	// ConsulClientConfig is the config to create a Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	// ExposeServersServiceName is the Kubernetes service name that the Consul servers are using.
	ExposeServersServiceName string
	// ReleaseNamespace is the namespace where this controller is deployed.
	ReleaseNamespace string
	// Log is the logger for this controller
	Log logr.Logger
	// Scheme is the API scheme that this controller should have.
	Scheme *runtime.Scheme
	context.Context
}

const (
	finalizerName    = "finalizers.consul.hashicorp.com"
	consulAgentError = "consulAgentError"
	internalError    = "internalError"
	kubernetesError  = "kubernetesError"
)

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringacceptors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringacceptors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=secrets/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// PeeringAcceptor resources determine whether to generate a new peering token in Consul and store it in the backend
// specified in the spec.
//   - If the resource doesn't exist, the peering should be deleted in Consul.
//   - If the resource exists, and a peering doesn't exist in Consul, it should be created.
//   - If the resource exists, and a peering does exist in Consul, it should be reconciled.
//   - If the status of the resource does not match the current state of the specified secret, generate a new token
//     and store it according to the spec.
//
// NOTE: It is possible that Reconcile is called multiple times concurrently because we're watching
// two different resource kinds. As a result, we need to make sure that the code in this method
// is thread-safe. For example, we may need to fetch the resource again before writing because another
// call to Reconcile could have modified it, and so we need to make sure that we're updating the latest version.
func (r *AcceptorController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("received request for PeeringAcceptor", "name", req.Name, "ns", req.Namespace)

	// Get the PeeringAcceptor resource.
	acceptor := &consulv1alpha1.PeeringAcceptor{}
	err := r.Client.Get(ctx, req.NamespacedName, acceptor)

	// This can be safely ignored as a resource will only ever be not found if it has never been reconciled
	// since we add finalizers to our resources.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("PeeringAcceptor resource not found. Ignoring resource", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringAcceptor", "name", req.Name, "ns", req.Namespace)
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
	if acceptor.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(acceptor, finalizerName) {
			controllerutil.AddFinalizer(acceptor, finalizerName)
			if err := r.Update(ctx, acceptor); err != nil {
				return ctrl.Result{}, err
			}
			// Return to ensure that rest of the reconcile is done in a subsequent call with updated resource version.
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if containsString(acceptor.Finalizers, finalizerName) {
			r.Log.Info("PeeringAcceptor was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
			err := r.deletePeering(ctx, apiClient, req.Name)
			if acceptor.Secret().Backend == "kubernetes" {
				err = r.deleteK8sSecret(ctx, acceptor.Secret().Name, acceptor.Namespace)
			}
			if err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(acceptor, finalizerName)
			err = r.Update(ctx, acceptor)
			return ctrl.Result{}, err
		}
	}

	// existingSecret will be nil if it doesn't exist, and have the contents of the secret if it does exist.
	existingSecret, err := r.getExistingSecret(ctx, acceptor.Secret().Name, acceptor.Namespace)
	if err != nil {
		r.Log.Error(err, "error retrieving existing secret", "name", acceptor.Secret().Name)
		r.updateStatusError(ctx, acceptor, kubernetesError, err)
		return ctrl.Result{}, err
	}

	// Read the peering from Consul.
	peering, _, err := apiClient.Peerings().Read(ctx, acceptor.Name, nil)
	if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}

	// If the peering doesn't exist in Consul, generate a new token, and store it in the specified backend. Store the
	// current state in the status.
	if peering == nil {
		r.Log.Info("peering doesn't exist in Consul; creating new peering", "name", acceptor.Name)

		if acceptor.SecretRef() != nil {
			r.Log.Info("stale secret in status; deleting stale secret", "name", acceptor.Name, "secret-name", acceptor.SecretRef().Name)
			if err := r.deleteK8sSecret(ctx, acceptor.SecretRef().Name, acceptor.Namespace); err != nil {
				r.updateStatusError(ctx, acceptor, kubernetesError, err)
				return ctrl.Result{}, err
			}
		}
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, apiClient, acceptor.Name); err != nil {
			r.updateStatusError(ctx, acceptor, consulAgentError, err)
			return ctrl.Result{}, err
		}
		if acceptor.Secret().Backend == "kubernetes" {
			if err := r.createOrUpdateK8sSecret(ctx, acceptor, resp); err != nil {
				r.updateStatusError(ctx, acceptor, kubernetesError, err)
				return ctrl.Result{}, err
			}
		}
		// Store the state in the status.
		err := r.updateStatus(ctx, req.NamespacedName)
		return ctrl.Result{}, err
	}

	// TODO(peering): Verify that the existing peering in Consul is an acceptor peer. If it is a dialing peer, an error should be thrown.

	r.Log.Info("peering exists in Consul")

	// If the peering does exist in Consul, figure out whether to generate and store a new token.
	shouldGenerate, nameChanged, err := shouldGenerateToken(acceptor, existingSecret)
	if err != nil {
		r.updateStatusError(ctx, acceptor, internalError, err)
		return ctrl.Result{}, err
	}

	if shouldGenerate {
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		r.Log.Info("generating new token for an existing peering")
		if resp, err = r.generateToken(ctx, apiClient, acceptor.Name); err != nil {
			return ctrl.Result{}, err
		}
		if acceptor.Secret().Backend == "kubernetes" {
			if err = r.createOrUpdateK8sSecret(ctx, acceptor, resp); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Delete the existing secret if the name changed. This needs to come before updating the status if we do generate a new token.
		if nameChanged && acceptor.SecretRef() != nil {
			r.Log.Info("stale secret in status; deleting stale secret", "name", acceptor.Name, "secret-name", acceptor.SecretRef().Name)
			if err = r.deleteK8sSecret(ctx, acceptor.SecretRef().Name, acceptor.Namespace); err != nil {
				r.updateStatusError(ctx, acceptor, kubernetesError, err)
				return ctrl.Result{}, err
			}
		}

		// Store the state in the status.
		err := r.updateStatus(ctx, req.NamespacedName)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// shouldGenerateToken returns whether a token should be generated, and whether the name of the secret has changed. It
// compares the spec secret's name/key/backend and resource version with the name/key/backend and resource version of the status secret's.
func shouldGenerateToken(acceptor *consulv1alpha1.PeeringAcceptor, existingSecret *corev1.Secret) (shouldGenerate bool, nameChanged bool, err error) {
	if acceptor.SecretRef() != nil {
		// Compare the existing name, key, and backend.
		if acceptor.SecretRef().Name != acceptor.Secret().Name {
			return true, true, nil
		}
		if acceptor.SecretRef().Key != acceptor.Secret().Key {
			return true, false, nil
		}
		// TODO(peering): remove this when validation webhook exists.
		if acceptor.SecretRef().Backend != acceptor.Secret().Backend {
			return false, false, errors.New("PeeringAcceptor backend cannot be changed")
		}
		if peeringVersionString, ok := acceptor.Annotations[constants.AnnotationPeeringVersion]; ok {
			peeringVersion, err := strconv.ParseUint(peeringVersionString, 10, 64)
			if err != nil {
				return false, false, err
			}
			if acceptor.Status.LatestPeeringVersion == nil || *acceptor.Status.LatestPeeringVersion < peeringVersion {
				return true, false, nil
			}
		}
	}

	if existingSecret == nil {
		return true, false, nil
	}

	return false, false, nil
}

// updateStatus updates the peeringAcceptor's secret in the status.
func (r *AcceptorController) updateStatus(ctx context.Context, acceptorObjKey types.NamespacedName) error {
	// Get the latest resource before we update it.
	acceptor := &consulv1alpha1.PeeringAcceptor{}
	if err := r.Client.Get(ctx, acceptorObjKey, acceptor); err != nil {
		return fmt.Errorf("error fetching acceptor resource before status update: %w", err)
	}
	acceptor.Status.SecretRef = &consulv1alpha1.SecretRefStatus{
		Secret: *acceptor.Secret(),
	}
	acceptor.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	acceptor.SetSyncedCondition(corev1.ConditionTrue, "", "")
	if peeringVersionString, ok := acceptor.Annotations[constants.AnnotationPeeringVersion]; ok {
		peeringVersion, err := strconv.ParseUint(peeringVersionString, 10, 64)
		if err != nil {
			r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
			return err
		}
		if acceptor.Status.LatestPeeringVersion == nil || *acceptor.Status.LatestPeeringVersion < peeringVersion {
			acceptor.Status.LatestPeeringVersion = ptr.To(uint64(peeringVersion))
		}
	}
	err := r.Status().Update(ctx, acceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
	}
	return err
}

// updateStatusError updates the peeringAcceptor's ReconcileError in the status.
func (r *AcceptorController) updateStatusError(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, reason string, reconcileErr error) {
	acceptor.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
	err := r.Status().Update(ctx, acceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
	}
}

// getExistingSecret gets the K8s secret specified, and either returns the existing secret or nil if it doesn't exist.
func (r *AcceptorController) getExistingSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
	existingSecret := &corev1.Secret{}
	namespacedName := types.NamespacedName{Name: name, Namespace: namespace}
	err := r.Client.Get(ctx, namespacedName, existingSecret)
	if k8serrors.IsNotFound(err) {
		// The secret was deleted.
		return nil, nil
	} else if err != nil {
		r.Log.Error(err, "couldn't get secret", "name", name, "namespace", namespace)
		return nil, err
	}
	return existingSecret, nil
}

// createOrUpdateK8sSecret creates a secret and uses the controller's K8s client to apply the secret. It checks if
// there's an existing secret with the same name and makes sure to update the existing secret if so.
func (r *AcceptorController) createOrUpdateK8sSecret(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, resp *api.PeeringGenerateTokenResponse) error {
	secretName := acceptor.Secret().Name
	secretNamespace := acceptor.Namespace
	secret := createSecret(secretName, secretNamespace, acceptor.Secret().Key, resp.PeeringToken)
	existingSecret, err := r.getExistingSecret(ctx, secretName, secretNamespace)
	if err != nil {
		return err
	}
	if existingSecret != nil {
		if err := r.Client.Update(ctx, secret); err != nil {
			return err
		}

	} else {
		if err := r.Client.Create(ctx, secret); err != nil {
			return err
		}
	}
	return nil
}

func (r *AcceptorController) deleteK8sSecret(ctx context.Context, name, namespace string) error {
	existingSecret, err := r.getExistingSecret(ctx, name, namespace)
	if err != nil {
		return err
	}
	if existingSecret != nil {
		if err := r.Client.Delete(ctx, existingSecret); err != nil {
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AcceptorController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.PeeringAcceptor{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.requestsForPeeringTokens),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.filterPeeringAcceptors)),
		).Complete(r)
}

// generateToken is a helper function that calls the Consul api to generate a token for the peer.
func (r *AcceptorController) generateToken(ctx context.Context, apiClient *api.Client, peerName string) (*api.PeeringGenerateTokenResponse, error) {
	req := api.PeeringGenerateTokenRequest{
		PeerName: peerName,
	}
	resp, _, err := apiClient.Peerings().GenerateToken(ctx, req, nil)
	if err != nil {
		r.Log.Error(err, "failed to get generate token", "err", err)
		return nil, err
	}
	return resp, nil
}

// deletePeering is a helper function that calls the Consul api to delete a peering.
func (r *AcceptorController) deletePeering(ctx context.Context, apiClient *api.Client, peerName string) error {
	_, err := apiClient.Peerings().Delete(ctx, peerName, nil)
	if err != nil {
		r.Log.Error(err, "failed to delete Peering from Consul", "name", peerName)
		return err
	}
	return nil
}

// requestsForPeeringTokens creates a slice of requests for the peering acceptor controller.
// It enqueues a request for each acceptor that needs to be reconciled. It iterates through
// the list of acceptors and creates a request for the acceptor that has the same secret as it's
// secretRef and that of the updated secret that is being watched.
// We compare it to the secret in the status as the resource has created the secret.
func (r *AcceptorController) requestsForPeeringTokens(ctx context.Context, object client.Object) []reconcile.Request {
	r.Log.Info("received update for Peering Token Secret", "name", object.GetName(), "namespace", object.GetNamespace())

	// Get the list of all acceptors.
	var acceptorList consulv1alpha1.PeeringAcceptorList
	if err := r.Client.List(ctx, &acceptorList); err != nil {
		r.Log.Error(err, "failed to list Peering Acceptors")
		return []ctrl.Request{}
	}
	for _, acceptor := range acceptorList.Items {
		if acceptor.SecretRef() != nil && acceptor.SecretRef().Backend == "kubernetes" {
			if acceptor.SecretRef().Name == object.GetName() && acceptor.Namespace == object.GetNamespace() {
				return []ctrl.Request{{NamespacedName: types.NamespacedName{Namespace: acceptor.Namespace, Name: acceptor.Name}}}
			}
		}
	}
	return []ctrl.Request{}
}

// filterPeeringAcceptors receives meta and object information for Kubernetes resources that are being watched,
// which in this case are Secrets. It only returns true if the Secret is a Peering Token Secret. It reads the labels
// from the meta of the resource and uses the values of the "consul.hashicorp.com/peering-token" label to validate that
// the Secret is a Peering Token Secret.
func (r *AcceptorController) filterPeeringAcceptors(object client.Object) bool {
	secretLabels := object.GetLabels()
	isPeeringToken, ok := secretLabels[constants.LabelPeeringToken]
	if !ok {
		return false
	}
	return isPeeringToken == "true"
}

// createSecret is a helper function that creates a corev1.Secret when provided inputs.
func createSecret(name, namespace, key, value string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constants.LabelPeeringToken: "true",
			},
		},
		Data: map[string][]byte{
			key: []byte(value),
		},
	}
	return secret
}

// containsString returns true if s is in slice.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
