package connectinject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
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

// PeeringController reconciles a Peering object
type PeeringController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peerings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peerings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=secrets/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PeeringController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("peering", req.NamespacedName)

	r.Log.Info("received request for PeeringAcceptor:", "name", req.Name, "ns", req.Namespace)

	// Get the PeeringAcceptor resource.
	peeringAcceptor := &consulv1alpha1.Peering{}
	err := r.Client.Get(ctx, req.NamespacedName, peeringAcceptor)

	// If the PeeringAcceptor resource has been deleted (and we get an IsNotFound
	// error), we need to delete it in Consul.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("PeeringAcceptor was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
		deleteReq := api.PeeringRequest{
			Name: req.Name,
		}
		if _, _, err := r.ConsulClient.Peerings().Delete(ctx, deleteReq, nil); err != nil {
			r.Log.Error(err, "failed to delete Peering from Consul", "name", req.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringAcceptor", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// Read the peering from Consul.
	// Todo(peering) do we need to pass in partition?
	peering, _, err := r.ConsulClient.Peerings().Read(ctx, peeringAcceptor.Name, nil)
	var statusErr api.StatusError

	// If the peering doesn't exist in Consul, generate a new token, and store it in the specified backend. Store the
	// current state in the status.
	if errors.As(err, &statusErr) && statusErr.Code == http.StatusNotFound && peering == nil {
		r.Log.Info("peering doesn't exist in Consul", "name", peeringAcceptor.Name)

		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, peeringAcceptor.Name); err != nil {
			return ctrl.Result{}, err
		}
		if peeringAcceptor.Spec.Peer.Secret.Backend == "kubernetes" {
			if err := r.createK8sPeeringTokenSecretWithOwner(ctx, peeringAcceptor, resp); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Store the state in the status.
		err := r.updateStatus(ctx, peeringAcceptor, resp)
		return ctrl.Result{}, err
	} else if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}

	// If the peering does exist in Consul, compare the existing status to the spec, and decide whether to make updates.
	shouldGenerate, err := r.shouldGenerateToken(ctx, peeringAcceptor)
	if shouldGenerate {
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, peeringAcceptor.Name); err != nil {
			return ctrl.Result{}, err
		}
		if peeringAcceptor.Spec.Peer.Secret.Backend == "kubernetes" {
			if err := r.createK8sPeeringTokenSecretWithOwner(ctx, peeringAcceptor, resp); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Store the state in the status.
		err := r.updateStatus(ctx, peeringAcceptor, resp)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
func (r *PeeringController) shouldGenerateToken(ctx context.Context, peeringAcceptor *consulv1alpha1.Peering) (bool, error) {
	if peeringAcceptor.Status.Secret == nil || peeringAcceptor.Status.LastReconcileTime == nil {
		return false, errors.New("shouldGenerateToken was called with an empty fields in the existing status")
	}
	// Compare the existing name, key, and backend.
	if peeringAcceptor.Status.Secret.Name != peeringAcceptor.Spec.Peer.Secret.Name {
		return true, nil
	}
	if peeringAcceptor.Status.Secret.Key != peeringAcceptor.Spec.Peer.Secret.Key {
		return true, nil
	}
	// TODO(peering): remove this when validation webhook exists.
	if peeringAcceptor.Status.Secret.Backend != peeringAcceptor.Spec.Peer.Secret.Backend {
		return false, errors.New("PeeringAcceptor backend cannot be changed")
	}
	// Compare the existing secret hash.
	// Get the secret specified by the status, make sure it matches the status' secret.latestHash.
	secret := &corev1.Secret{}
	namespacedName := types.NamespacedName{
		Name:      peeringAcceptor.Status.Secret.Name,
		Namespace: peeringAcceptor.Namespace,
	}
	err := r.Client.Get(ctx, namespacedName, secret)
	if k8serrors.IsNotFound(err) {
		// The secret was deleted, so this is a case to generate a new token.
		return true, nil
	} else if err != nil {
		r.Log.Error(err, "couldn't get secret", "name", peeringAcceptor.Status.Secret.Name, "namespace", peeringAcceptor.Namespace)
		return false, err
	}
	existingSecretHashBytes := sha256.Sum256(secret.Data[peeringAcceptor.Status.Secret.Key])
	existingSecretHash := hex.EncodeToString(existingSecretHashBytes[:])
	if existingSecretHash != peeringAcceptor.Status.Secret.LatestHash {
		r.Log.Info("secret doesn't match status.secret.latestHash, should generate new token")
		return true, nil
	}
	return false, nil
}
func (r *PeeringController) updateStatus(ctx context.Context, peeringAcceptor *consulv1alpha1.Peering, resp *api.PeeringGenerateTokenResponse) error {
	peeringAcceptor.Status.Secret = &consulv1alpha1.SecretStatus{
		Name:    peeringAcceptor.Spec.Peer.Secret.Name,
		Key:     peeringAcceptor.Spec.Peer.Secret.Key,
		Backend: peeringAcceptor.Spec.Peer.Secret.Backend,
	}

	peeringTokenHash := sha256.Sum256([]byte(resp.PeeringToken))
	peeringAcceptor.Status.Secret.LatestHash = hex.EncodeToString(peeringTokenHash[:])

	peeringAcceptor.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	err := r.Status().Update(ctx, peeringAcceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", peeringAcceptor.Name, "namespace", peeringAcceptor.Namespace)
	}
	return err
}

func (r *PeeringController) generateToken(ctx context.Context, peerName string) (*api.PeeringGenerateTokenResponse, error) {
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

func (r *PeeringController) createK8sPeeringTokenSecretWithOwner(ctx context.Context, peeringAcceptor *consulv1alpha1.Peering, resp *api.PeeringGenerateTokenResponse) error {
	secret := createSecret(peeringAcceptor.Spec.Peer.Secret.Name, peeringAcceptor.Namespace, peeringAcceptor.Spec.Peer.Secret.Key, resp.PeeringToken)
	if err := controllerutil.SetControllerReference(peeringAcceptor, secret, r.Scheme); err != nil {
		return err
	}
	if err := r.Client.Create(ctx, secret); err != nil {
		return err
	}
	return nil
}

func createSecret(name, namespace, key, value string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		StringData: map[string]string{
			key: value,
		},
	}
	return secret
}

// SetupWithManager sets up the controller with the Manager.
func (r *PeeringController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.Peering{}).
		Complete(r)
}
