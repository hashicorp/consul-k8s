package connectinject

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-logr/logr"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PeeringAcceptorController reconciles a PeeringAcceptor object.
type PeeringAcceptorController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient              *api.Client
	ExposeServersServiceName  string
	ReadServerExternalService bool
	TokenServerAddresses      []string
	ReleaseNamespace          string
	Log                       logr.Logger
	Scheme                    *runtime.Scheme
	context.Context
}

const (
	FinalizerName    = "finalizers.consul.hashicorp.com"
	ConsulAgentError = "ConsulAgentError"
	InternalError    = "InternalError"
	KubernetesError  = "KubernetesError"
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
func (r *PeeringAcceptorController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	// The DeletionTimestamp is zero when the object has not been marked for deletion. The finalizer is added
	// in case it does not exist to all resources. If the DeletionTimestamp is non-zero, the object has been
	// marked for deletion and goes into the deletion workflow.
	if acceptor.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(acceptor, FinalizerName) {
			controllerutil.AddFinalizer(acceptor, FinalizerName)
			if err := r.Update(ctx, acceptor); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if containsString(acceptor.Finalizers, FinalizerName) {
			r.Log.Info("PeeringAcceptor was deleted, deleting from Consul", "name", req.Name, "ns", req.Namespace)
			err := r.deletePeering(ctx, req.Name)
			if acceptor.Secret().Backend == "kubernetes" {
				err = r.deleteK8sSecret(ctx, acceptor.Secret().Name, acceptor.Namespace)
			}
			if err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(acceptor, FinalizerName)
			err = r.Update(ctx, acceptor)
			return ctrl.Result{}, err
		}
	}

	// Scrape the address of the server service
	var serverExternalAddresses []string
	if r.ReadServerExternalService {
		addrs, err := r.getExposeServersServiceAddresses()
		if err != nil {
			r.updateStatusError(ctx, acceptor, KubernetesError, err)
			return ctrl.Result{}, err
		}
		serverExternalAddresses = addrs
	} else if len(r.TokenServerAddresses) > 0 {
		serverExternalAddresses = r.TokenServerAddresses
	}

	// existingSecret will be nil if it doesn't exist, and have the contents of the secret if it does exist.
	existingSecret, err := r.getExistingSecret(ctx, acceptor.Secret().Name, acceptor.Namespace)
	if err != nil {
		r.Log.Error(err, "error retrieving existing secret", "name", acceptor.Secret().Name)
		r.updateStatusError(ctx, acceptor, KubernetesError, err)
		return ctrl.Result{}, err
	}

	// Read the peering from Consul.
	peering, _, err := r.ConsulClient.Peerings().Read(ctx, acceptor.Name, nil)
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
				r.updateStatusError(ctx, acceptor, KubernetesError, err)
				return ctrl.Result{}, err
			}
		}
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		if resp, err = r.generateToken(ctx, acceptor.Name, serverExternalAddresses); err != nil {
			r.updateStatusError(ctx, acceptor, ConsulAgentError, err)
			return ctrl.Result{}, err
		}
		if acceptor.Secret().Backend == "kubernetes" {
			if err := r.createOrUpdateK8sSecret(ctx, acceptor, resp); err != nil {
				r.updateStatusError(ctx, acceptor, KubernetesError, err)
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
		r.updateStatusError(ctx, acceptor, InternalError, err)
		return ctrl.Result{}, err
	}

	if shouldGenerate {
		// Generate and store the peering token.
		var resp *api.PeeringGenerateTokenResponse
		r.Log.Info("generating new token for an existing peering")
		if resp, err = r.generateToken(ctx, acceptor.Name, serverExternalAddresses); err != nil {
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
				r.updateStatusError(ctx, acceptor, KubernetesError, err)
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
		if peeringVersionString, ok := acceptor.Annotations[annotationPeeringVersion]; ok {
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
func (r *PeeringAcceptorController) updateStatus(ctx context.Context, acceptorObjKey types.NamespacedName) error {
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
	if peeringVersionString, ok := acceptor.Annotations[annotationPeeringVersion]; ok {
		peeringVersion, err := strconv.ParseUint(peeringVersionString, 10, 64)
		if err != nil {
			r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
			return err
		}
		if acceptor.Status.LatestPeeringVersion == nil || *acceptor.Status.LatestPeeringVersion < peeringVersion {
			acceptor.Status.LatestPeeringVersion = pointer.Uint64(peeringVersion)
		}
	}
	err := r.Status().Update(ctx, acceptor)
	if err != nil {
		r.Log.Error(err, "failed to update PeeringAcceptor status", "name", acceptor.Name, "namespace", acceptor.Namespace)
	}
	return err
}

// updateStatusError updates the peeringAcceptor's ReconcileError in the status.
func (r *PeeringAcceptorController) updateStatusError(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, reason string, reconcileErr error) {
	acceptor.SetSyncedCondition(corev1.ConditionFalse, reason, reconcileErr.Error())
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

// createOrUpdateK8sSecret creates a secret and uses the controller's K8s client to apply the secret. It checks if
// there's an existing secret with the same name and makes sure to update the existing secret if so.
func (r *PeeringAcceptorController) createOrUpdateK8sSecret(ctx context.Context, acceptor *consulv1alpha1.PeeringAcceptor, resp *api.PeeringGenerateTokenResponse) error {
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

func (r *PeeringAcceptorController) deleteK8sSecret(ctx context.Context, name, namespace string) error {
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
func (r *PeeringAcceptorController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.PeeringAcceptor{}).
		Watches(
			&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.requestsForPeeringTokens),
			builder.WithPredicates(predicate.NewPredicateFuncs(r.filterPeeringAcceptors)),
		).Complete(r)
}

// generateToken is a helper function that calls the Consul api to generate a token for the peer.
func (r *PeeringAcceptorController) generateToken(ctx context.Context, peerName string, serverExternalAddresses []string) (*api.PeeringGenerateTokenResponse, error) {
	req := api.PeeringGenerateTokenRequest{
		PeerName: peerName,
	}
	if len(serverExternalAddresses) > 0 {
		req.ServerExternalAddresses = serverExternalAddresses
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

// requestsForPeeringTokens creates a slice of requests for the peering acceptor controller.
// It enqueues a request for each acceptor that needs to be reconciled. It iterates through
// the list of acceptors and creates a request for the acceptor that has the same secret as it's
// secretRef and that of the updated secret that is being watched.
// We compare it to the secret in the status as the resource has created the secret.
func (r *PeeringAcceptorController) requestsForPeeringTokens(object client.Object) []reconcile.Request {
	r.Log.Info("received update for Peering Token Secret", "name", object.GetName(), "namespace", object.GetNamespace())

	// Get the list of all acceptors.
	var acceptorList consulv1alpha1.PeeringAcceptorList
	if err := r.Client.List(r.Context, &acceptorList); err != nil {
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

func (r *PeeringAcceptorController) getExposeServersServiceAddresses() ([]string, error) {
	r.Log.Info("getting external address from expose-servers service", "name", r.ExposeServersServiceName)
	var serverExternalAddresses []string

	serverService := &corev1.Service{}
	key := types.NamespacedName{
		Name:      r.ExposeServersServiceName,
		Namespace: r.ReleaseNamespace,
	}
	err := r.Client.Get(r.Context, key, serverService)
	if err != nil {
		return nil, err
	}
	switch serverService.Spec.Type {
	case corev1.ServiceTypeNodePort:
		nodes := corev1.NodeList{}
		err := r.Client.List(r.Context, &nodes)
		if err != nil {
			return nil, err
		}
		if len(nodes.Items) == 0 {
			return nil, fmt.Errorf("no nodes were found for scraping server addresses from expose-servers service")
		}
		var grpcNodePort int32
		for _, port := range serverService.Spec.Ports {
			if port.Name == "grpc" {
				grpcNodePort = port.NodePort
			}
		}
		if grpcNodePort == 0 {
			return nil, fmt.Errorf("no grpc port was found for expose-servers service")
		}
		for _, node := range nodes.Items {
			addrs := node.Status.Addresses
			for _, addr := range addrs {
				if addr.Type == corev1.NodeInternalIP {
					serverExternalAddresses = append(serverExternalAddresses, fmt.Sprintf("%s:%d", addr.Address, grpcNodePort))
				}
			}
		}
		if len(serverExternalAddresses) == 0 {
			return nil, fmt.Errorf("no server addresses were scraped from expose-servers service")
		}
		return serverExternalAddresses, nil
	case corev1.ServiceTypeLoadBalancer:
		lbAddrs := serverService.Status.LoadBalancer.Ingress
		if len(lbAddrs) < 1 {
			return nil, fmt.Errorf("unable to find load balancer address for %s service, retrying", r.ExposeServersServiceName)
		}
		for _, lbAddr := range lbAddrs {
			// When the service is of type load balancer, the grpc port is hardcoded to 8502.
			if lbAddr.IP != "" {
				serverExternalAddresses = append(serverExternalAddresses, fmt.Sprintf("%s:%s", lbAddr.IP, "8502"))
			}
			if lbAddr.Hostname != "" {
				serverExternalAddresses = append(serverExternalAddresses, fmt.Sprintf("%s:%s", lbAddr.Hostname, "8502"))
			}
		}
		if len(serverExternalAddresses) == 0 {
			return nil, fmt.Errorf("unable to find load balancer address for %s service, retrying", r.ExposeServersServiceName)
		}
	default:
		return nil, fmt.Errorf("only NodePort and LoadBalancer service types are supported")
	}
	return serverExternalAddresses, nil
}

// filterPeeringAcceptors receives meta and object information for Kubernetes resources that are being watched,
// which in this case are Secrets. It only returns true if the Secret is a Peering Token Secret. It reads the labels
// from the meta of the resource and uses the values of the "consul.hashicorp.com/peering-token" label to validate that
// the Secret is a Peering Token Secret.
func (r *PeeringAcceptorController) filterPeeringAcceptors(object client.Object) bool {
	secretLabels := object.GetLabels()
	isPeeringToken, ok := secretLabels[labelPeeringToken]
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
				labelPeeringToken: "true",
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
