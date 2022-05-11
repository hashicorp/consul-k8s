package connectinject

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
)

// PeeringController reconciles a Peering object
type PeeringController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	// ConsulClientCfg is the client config used by the ConsulClient when calling NewClient().
	ConsulClientCfg *api.Config
	// ConsulScheme is the scheme to use when making API calls to Consul,
	// i.e. "http" or "https".
	ConsulScheme string
	// ConsulPort is the port to make HTTP API calls to Consul agents on.
	ConsulPort string
	Log        logr.Logger
	Scheme     *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peerings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=peerings/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Peering object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.6.4/pkg/reconcile
func (r *PeeringController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("peering", req.NamespacedName)

	r.Log.Info("received request for PeeringAcceptor:", "name", req.Name, "ns", req.Namespace)

	token := &consulv1alpha1.Peering{}
	err := r.Client.Get(ctx, req.NamespacedName, token)
	// If the PeeringAcceptor object has been deleted (and we get an IsNotFound
	// error), we need to delete it in Consul.
	if k8serrors.IsNotFound(err) {
		// TODO(peering): currently deletion doesn't work because token.Name is empty when deleted. Do I need to list and figure out what to delete?
		deleteReq := api.PeeringRequest{
			Name: token.Name,
		}
		_, _, err := r.ConsulClient.Peerings().Delete(ctx, deleteReq, nil)
		if err != nil {
			r.Log.Error(err, "failed to delete Peering from Consul", "name", req.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		r.Log.Error(err, "failed to get PeeringAcceptor", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	// Read the peering from Consul.
	// Todo(peering) do we need to pass in partition?
	peering, _, err := r.ConsulClient.Peerings().Read(ctx, token.Name, nil)
	var statusErr api.StatusError
	if errors.As(err, &statusErr) && statusErr.Code == http.StatusNotFound {
		r.Log.Info("peering doesn't exist in Consul", "name", token.Name)
	} else if err != nil {
		r.Log.Error(err, "failed to get Peering from Consul", "name", req.Name)
		return ctrl.Result{}, err
	}

	// If the peering doesn't exist in Consul, we should generate a new token.
	if peering == nil {
		r.Log.Info("peering doesn't exist in Consul", "name", token.Name)
		req := api.PeeringGenerateTokenRequest{
			PeerName: token.Name,
		}
		resp, _, err := r.ConsulClient.Peerings().GenerateToken(ctx, req, nil)
		if err != nil {
			r.Log.Error(err, "failed to get generate token", "err", err)
			return ctrl.Result{}, err
		}

		if token.Spec.Peer.Secret.Backend == "kubernetes" {
			secret := createSecret(token.Spec.Peer.Secret.Name, token.Namespace, token.Spec.Peer.Secret.Key, resp.PeeringToken)
			err = r.Client.Create(ctx, secret)
		}
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	r.Log.Info("found token:", "token", token.Name)

	return ctrl.Result{}, nil
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

//// remoteConsulClient returns an *api.Client that points at the consul agent local to the pod for a provided namespace.
//func (r *PeeringController) remoteConsulClient(ip string, namespace string) (*api.Client, error) {
//	newAddr := fmt.Sprintf("%s://%s:%s", r.ConsulScheme, ip, r.ConsulPort)
//	localConfig := r.ConsulClientCfg
//	localConfig.Address = newAddr
//	localConfig.Namespace = namespace
//	return consul.NewClient(localConfig)
//}
