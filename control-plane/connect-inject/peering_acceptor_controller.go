package connectinject

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PeeringAcceptorController reconciles a PeeringAcceptor object.
type PeeringAcceptorController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringacceptors,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringacceptors/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=secrets/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// PeeringAcceptor resources determine whether to generate a new peering token in Consul and store it in the backend
// specified in the spec.
// - If the resource doesn't exist, the peering should be deleted in Consul.
// - If the resource exists, and a peering doesn't exist in Consul, it should be created.
// - If the resource exists, and a peering does exist in Consul, it should be reconciled.
// - If the status of the resource does not match the current state of the specified secret, generate a new token
//   and store it according to the spec.
func (r *PeeringAcceptorController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("received request for PeeringAcceptor", "name", req.Name, "ns", req.Namespace)

	// Get the PeeringAcceptor resource.
	acceptor := &consulv1alpha1.PeeringAcceptor{}
	err := r.Client.Get(ctx, req.NamespacedName, acceptor)

	// If the PeeringAcceptor resource has been deleted (and we get an IsNotFound
	// error), we need to delete it in Consul.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("PeeringAcceptor was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
		if err := r.deletePeering(ctx, req.Name); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringAcceptor", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	statusSecretSet := acceptor.SecretRef() != nil

	// existingStatusSecret will be nil if it doesn't exist, and have the contents of the secret if it does exist.
	var existingStatusSecret *corev1.Secret
	if statusSecretSet {
		existingStatusSecret, err = r.getExistingSecret(ctx, acceptor.SecretRef().Name, acceptor.Namespace)
		if err != nil {
			r.updateStatusError(ctx, acceptor, err)
			return ctrl.Result{}, err
		}
	}

	var secretResourceVersion string

	// Read the peering from Consul.
	peering, _, err := r.ConsulClient.Peerings().Read(ctx, acceptor.Name, nil)
	if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}

	// If the peering doesn't exist in Consul, generate a new token, and store it in the specified backend. Store the
	// current state in the status.
	if peering == nil {
		r.Log.Info("peering doesn't exist in Consul", "name", acceptor.Name)

		if statusSecretSet {
			if existingStatusSecret != nil {
				err := r.Client.Delete(ctx, existingStatusSecret)
				if err != nil {
					r.updateStatusError(ctx, acceptor, err)
					return ctrl.Result{}, err
				}
			}
		}
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, acceptor.Name); err != nil {
			r.updateStatusError(ctx, acceptor, err)
			return ctrl.Result{}, err
		}
		if acceptor.Secret().Backend == "kubernetes" {
			secretResourceVersion, err = r.createK8sPeeringTokenSecretWithOwner(ctx, acceptor, resp)
			if err != nil {
				r.updateStatusError(ctx, acceptor, err)
				return ctrl.Result{}, err
			}
		}
		// Store the state in the status.
		err := r.updateStatus(ctx, acceptor, secretResourceVersion)
		return ctrl.Result{}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}

	// TODO(peering): Verify that the existing peering in Consul is an acceptor peer. If it is a dialing peer, an error should be thrown.

	// If the peering does exist in Consul, figure out whether to generate and store a new token by comparing the secret
	// in the status to the actual contents of the secret. If no secret is specified in the status, shouldGenerate will
	// be set to true.
	var shouldGenerate bool
	var nameChanged bool
	if statusSecretSet {
		shouldGenerate, nameChanged, err = shouldGenerateToken(acceptor, existingStatusSecret)
		if err != nil {
			r.updateStatusError(ctx, acceptor, err)
			return ctrl.Result{}, err
		}
	} else {
		shouldGenerate = true
	}

	if shouldGenerate {
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, acceptor.Name); err != nil {
			return ctrl.Result{}, err
		}
		if acceptor.Secret().Backend == "kubernetes" {
			secretResourceVersion, err = r.createK8sPeeringTokenSecretWithOwner(ctx, acceptor, resp)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		// Delete the existing secret if the name changed. This needs to come before updating the status if we do generate a new token.
		if nameChanged {
			if existingStatusSecret != nil {
				err := r.Client.Delete(ctx, existingStatusSecret)
				if err != nil {
					r.updateStatusError(ctx, acceptor, err)
					return ctrl.Result{}, err
				}
			}
		}

		// Store the state in the status.
		err := r.updateStatus(ctx, acceptor, secretResourceVersion)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// shouldGenerateToken returns whether a token should be generated, and whether the name of the secret has changed. It
// compares the spec secret's name/key/backend and contents to the status secret's name/key/backend and contents. The
// contents are compared by taking a SHA256 sum of the secret.
func shouldGenerateToken(acceptor *consulv1alpha1.PeeringAcceptor, existingStatusSecret *corev1.Secret) (shouldGenerate bool, nameChanged bool, err error) {
	if acceptor.SecretRef() == nil {
		return false, false, errors.New("shouldGenerateToken was called with an empty fields in the existing status")
	}
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
	// Compare the existing secret hash.
	// Get the secret specified by the status, make sure it matches the status' secret.latestHash.
	if existingStatusSecret != nil {
		if existingStatusSecret.ResourceVersion != acceptor.SecretRef().ResourceVersion {
			return true, false, nil
		}

	} else {
		return true, false, nil
	}
	return false, false, nil
}

// updateStatus updates the peeringAcceptor's secret in the status.
func (r *PeeringAcceptorController) updateStatus(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, secretResourceVersion string) error {
	acceptor.Status.SecretRef = &consulv1alpha1.SecretRefStatus{
		Secret:          *acceptor.Secret(),
		ResourceVersion: secretResourceVersion,
	}
	acceptor.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	acceptor.Status.ReconcileError = &consulv1alpha1.ReconcileErrorStatus{
		Error:   pointerToBool(false),
		Message: pointerToString(""),
	}
	err := r.Status().Update(ctx, acceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
	}
	return err
}

// updateStatusError updates the peeringAcceptor's ReconcileError in the status.
func (r *PeeringAcceptorController) updateStatusError(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, reconcileErr error) {
	acceptor.Status.ReconcileError = &consulv1alpha1.ReconcileErrorStatus{
		Error:   pointerToBool(true),
		Message: pointerToString(reconcileErr.Error()),
	}

	acceptor.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	err := r.Status().Update(ctx, acceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
	}
}

// getExistingSecret gets the K8s secret specified, and either returns the existing secret or nil if it doesn't exist.
func (r *PeeringAcceptorController) getExistingSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
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

// createK8sPeeringTokenSecretWithOwner creates a secret and uses the controller's K8s client to apply the secret. It
// sets an owner reference to the PeeringAcceptor resource. It also checks if there's an existing secret with the same
// name and makes sure to update the existing secret if so.
func (r *PeeringAcceptorController) createK8sPeeringTokenSecretWithOwner(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, resp *api.PeeringGenerateTokenResponse) (string, error) {
	secretName := acceptor.Secret().Name
	secretNamespace := acceptor.Namespace
	secret := createSecret(secretName, secretNamespace, acceptor.Secret().Key, resp.PeeringToken)
	if err := controllerutil.SetControllerReference(acceptor, secret, r.Scheme); err != nil {
		return "", err
	}
	existingSecret, err := r.getExistingSecret(ctx, acceptor.Secret().Name, acceptor.Namespace)
	if err != nil {
		return "", err
	}
	if existingSecret != nil {
		if err := r.Client.Update(ctx, secret); err != nil {
			return "", err
		}

	} else {
		if err := r.Client.Create(ctx, secret); err != nil {
			return "", err
		}
	}
	// The newly created or updated secret should exist at this point, so we can get it and return the resourceVersion.
	newSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, newSecret); err != nil {
		return "", err
	}

	return newSecret.ResourceVersion, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PeeringAcceptorController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.PeeringAcceptor{}).
		Complete(r)
}

// generateToken is a helper function that calls the Consul api to generate a token for the peer.
func (r *PeeringAcceptorController) generateToken(ctx context.Context, peerName string) (*api.PeeringGenerateTokenResponse, error) {
	req := api.PeeringGenerateTokenRequest{
		PeerName: peerName,
	}
	resp, _, err := r.ConsulClient.Peerings().GenerateToken(ctx, req, nil)
	if err != nil {
		r.Log.Error(err, "failed to get generate token", "err", err)
		return nil, err
	}
	return resp, nil
}

// deletePeering is a helper function that calls the Consul api to delete a peering.
func (r *PeeringAcceptorController) deletePeering(ctx context.Context, peerName string) error {
	_, err := r.ConsulClient.Peerings().Delete(ctx, peerName, nil)
	if err != nil {
		r.Log.Error(err, "failed to delete Peering from Consul", "name", peerName)
		return err
	}
	return nil
}

// createSecret is a helper function that creates a corev1.SecretRef when provided inputs.
func createSecret(name, namespace, key, value string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		StringData: map[string]string{
			key: value,
		},
		Data: map[string][]byte{
			key: []byte(value),
		},
	}
	return secret
}

func pointerToString(s string) *string {
	return &s
}
