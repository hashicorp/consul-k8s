package connectinject

import (
	"context"
	"errors"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// PeeringDialerController reconciles a PeeringDialer object.
type PeeringDialerController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringdialers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peeringdialers/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PeeringDialerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("received request for PeeringDialer:", "name", req.Name, "ns", req.Namespace)

	// Get the PeeringDialer resource.
	peeringDialer := &consulv1alpha1.PeeringDialer{}
	err := r.Client.Get(ctx, req.NamespacedName, peeringDialer)

	// If the PeeringDialer resource has been deleted (and we get an IsNotFound
	// error), we need to delete it in Consul.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("PeeringDialer was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
		err := r.deletePeering(ctx, req.Name)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringDialer", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// Get the status secret and the spec secret.
	// Cases need to handle statusSecretSet, existingStatusSecret, specSecretSet, existingSpecSecret.
	// no specSecretSet --> error bc spec needs to be set.
	// no existingSpecSecret --> error bc waiting for spec secret to exist.
	// no statusSecretSet, yes specSecretSet, no existingStatusSecret, yes existingSpecSecret --> initiate peering.
	// yes statusSecretSet, yes specSecretSet, no existingStatusSecret, yes existingSpecSecret --> initiate peering.
	// yes statusSecretSet, yes specSecretSet, yes existingStatusSecret, yes existingSpecSecret --> compare contents, if
	// different initiate peering.

	// Get the status secret and the spec secret.
	statusSecretSet := false
	if peeringDialer.Status.SecretRef != nil {
		statusSecretSet = true
	}
	// TODO(peering): remove this once CRD validation exists.
	specSecretSet := false
	if peeringDialer.Spec.Peer != nil {
		if peeringDialer.Spec.Peer.Secret != nil {
			specSecretSet = true
		}
	}
	if !specSecretSet {
		err = errors.New("PeeringDialer spec.peer.secret was not set")
		_ = r.updateStatusError(ctx, peeringDialer, err)
		return ctrl.Result{}, err
	}

	// existingStatusSecret will be nil if the secret specified by the status doesn't exist.
	var existingStatusSecret *corev1.Secret
	if statusSecretSet {
		_, existingStatusSecret, err = r.getExistingSecret(ctx, peeringDialer.Status.SecretRef.Name, peeringDialer.Namespace)
		if err != nil {
			_ = r.updateStatusError(ctx, peeringDialer, err)
			return ctrl.Result{}, err
		}
	}

	// existingSpecSecret will be nil if the secret specified by the spec doesn't exist.
	var existingSpecSecret *corev1.Secret
	if specSecretSet {
		_, existingSpecSecret, err = r.getExistingSecret(ctx, peeringDialer.Spec.Peer.Secret.Name, peeringDialer.Namespace)
		if err != nil {
			_ = r.updateStatusError(ctx, peeringDialer, err)
			return ctrl.Result{}, err
		}
	}

	// If spec secret doesn't exist, error because we can only initiate peering if we have a token to initiate with.
	if existingSpecSecret == nil {
		err = errors.New("PeeringDialer spec.peer.secret does not exist")
		_ = r.updateStatusError(ctx, peeringDialer, err)
		return ctrl.Result{}, err
	}

	// Read the peering from Consul.
	// TODO(peering): do we need to pass in partition?
	r.Log.Info("reading peering from Consul", "name", peeringDialer.Name)
	peering, _, err := r.ConsulClient.Peerings().Read(ctx, peeringDialer.Name, nil)
	if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}
	peeringExists := peering != nil
	// TODO(peering): Verify that the existing peering in Consul is an dialer peer. If it is an acceptor peer, an error should be thrown.

	// At this point, we know the spec secret exists. If the status secret doesn't
	// exist, then we want to initiate peering and update the status with the secret for the token being used.
	if existingStatusSecret == nil {
		// Whether the peering exists in Consul or not we want to initiate the peering so the status can reflect the
		// correct secret specified in the spec.
		r.Log.Info("status.secret doesn't exist or wasn't set, establishing peering with the existing spec.peer.secret", "secret-name", peeringDialer.Spec.Peer.Secret.Name, "secret-namespace", peeringDialer.Namespace)
		peeringToken := existingSpecSecret.Data[peeringDialer.Spec.Peer.Secret.Key]
		_, err := r.initiatePeering(ctx, peeringDialer.Name, string(peeringToken))
		if err != nil {
			_ = r.updateStatusError(ctx, peeringDialer, err)
			return ctrl.Result{}, err
		} else {
			err := r.updateStatus(ctx, peeringDialer, existingSpecSecret.ResourceVersion)
			return ctrl.Result{}, err
		}
	} else {
		// At this point, the status secret does exist.
		// If the peering in Consul does not exist, initiate peering.
		if !peeringExists {
			r.Log.Info("status.secret exists, but the peering doesn't exist in Consul; establishing peering with the existing spec.peer.secret", "secret-name", peeringDialer.Spec.Peer.Secret.Name, "secret-namespace", peeringDialer.Namespace)
			peeringToken := existingSpecSecret.Data[peeringDialer.Spec.Peer.Secret.Key]
			_, err := r.initiatePeering(ctx, peeringDialer.Name, string(peeringToken))
			if err != nil {
				_ = r.updateStatusError(ctx, peeringDialer, err)
				return ctrl.Result{}, err
			} else {
				err := r.updateStatus(ctx, peeringDialer, existingSpecSecret.ResourceVersion)
				return ctrl.Result{}, err
			}
		}

		// Or, if the peering in Consul does exist, compare it to the contents of the spec's secret. If there's any
		// differences, initiate peering.
		if r.specStatusSecretsDifferent(peeringDialer, existingSpecSecret) {
			r.Log.Info("status.secret exists and is different from spec.peer.secret; establishing peering with the existing spec.peer.secret", "secret-name", peeringDialer.Spec.Peer.Secret.Name, "secret-namespace", peeringDialer.Namespace)
			peeringToken := existingSpecSecret.Data[peeringDialer.Spec.Peer.Secret.Key]
			_, err := r.initiatePeering(ctx, peeringDialer.Name, string(peeringToken))
			if err != nil {
				_ = r.updateStatusError(ctx, peeringDialer, err)
				return ctrl.Result{}, err
			} else {
				err := r.updateStatus(ctx, peeringDialer, existingSpecSecret.ResourceVersion)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *PeeringDialerController) specStatusSecretsDifferent(peeringDialer *consulv1alpha1.PeeringDialer, existingSpecSecret *corev1.Secret) bool {
	if peeringDialer.Status.SecretRef.Name != peeringDialer.Spec.Peer.Secret.Name {
		return true
	}
	if peeringDialer.Status.SecretRef.Key != peeringDialer.Spec.Peer.Secret.Key {
		return true
	}
	if peeringDialer.Status.SecretRef.Backend != peeringDialer.Spec.Peer.Secret.Backend {
		return true
	}
	existingSpecSecretResourceVersion := existingSpecSecret.ResourceVersion
	return existingSpecSecretResourceVersion != peeringDialer.Status.SecretRef.ResourceVersion
}

func (r *PeeringDialerController) updateStatus(ctx context.Context, peeringDialer *consulv1alpha1.PeeringDialer, resourceVersion string) error {
	peeringDialer.Status.SecretRef = &consulv1alpha1.SecretRefStatus{
		Name:    peeringDialer.Spec.Peer.Secret.Name,
		Key:     peeringDialer.Spec.Peer.Secret.Key,
		Backend: peeringDialer.Spec.Peer.Secret.Backend,
	}

	peeringDialer.Status.SecretRef.ResourceVersion = resourceVersion

	peeringDialer.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	err := r.Status().Update(ctx, peeringDialer)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringDialer status", "name", peeringDialer.Name, "namespace", peeringDialer.Namespace)
	}
	return err
}

func (r *PeeringDialerController) updateStatusError(ctx context.Context, peeringDialer *consulv1alpha1.PeeringDialer, reconcileErr error) error {
	peeringDialer.Status.ReconcileError = &consulv1alpha1.ReconcileErrorStatus{
		Error:   pointerToBool(true),
		Message: pointerToString(reconcileErr.Error()),
	}

	peeringDialer.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	err := r.Status().Update(ctx, peeringDialer)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringDialer status", "name", peeringDialer.Name, "namespace", peeringDialer.Namespace)
	}
	return err
}

func (r *PeeringDialerController) getExistingSecret(ctx context.Context, name string, namespace string) (bool, *corev1.Secret, error) {
	existingSecret := &corev1.Secret{}
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	err := r.Client.Get(ctx, namespacedName, existingSecret)
	if k8serrors.IsNotFound(err) {
		// The secret was deleted.
		return false, nil, nil
	} else if err != nil {
		r.Log.Error(err, "couldn't get secret", "name", name, "namespace", namespace)
		return false, nil, err
	}
	return true, existingSecret, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PeeringDialerController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.PeeringDialer{}).
		Complete(r)
}

// initiatePeering is a helper function that calls the Consul api to generate a token for the peer.
func (r *PeeringDialerController) initiatePeering(ctx context.Context, peerName string, peeringToken string) (*api.PeeringInitiateResponse, error) {
	req := api.PeeringInitiateRequest{
		PeerName:     peerName,
		PeeringToken: peeringToken,
	}
	resp, _, err := r.ConsulClient.Peerings().Initiate(ctx, req, nil)
	if err != nil {
		r.Log.Error(err, "failed to initiate peering", "err", err)
		return nil, err
	}
	return resp, nil
}

// deletePeering is a helper function that calls the Consul api to delete a peering.
func (r *PeeringDialerController) deletePeering(ctx context.Context, peerName string) error {
	_, err := r.ConsulClient.Peerings().Delete(ctx, peerName, nil)
	if err != nil {
		r.Log.Error(err, "failed to delete Peering from Consul", "name", peerName)
		return err
	}
	return nil
}
