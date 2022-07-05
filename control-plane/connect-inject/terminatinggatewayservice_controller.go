package connectinject

import (
	"context"
	"errors"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TerminatingGatewayServiceReconciler reconciles a TerminatingGatewayService object
type TerminatingGatewayServiceController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	context.Context
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggatewayservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggatewayservices/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TerminatingGatewayService object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.6.4/pkg/reconcile
func (r *TerminatingGatewayServiceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("recieved request for TerminatingGatewayService", "name", "ns", req.Namespace)

	// Get the TerminatingGatewayService resource
	terminatingGatewayService := &consulv1alpha1.TerminatingGatewayService{}
	err := r.Client.Get(ctx, req.NamespacedName, terminatingGatewayService)

	// This can be safely ignored as a resource will only ever be not found if it has never been reconciled
	// since we add finalizers to our resources.
	if k8serrors.IsNotFound(err) {
		r.Log.Info("TerminatingGatewayService resource not found. Ignoring resource", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "failed to get TerminatingGatewayService", "name", req.Name, "ns", req.Namespace)
		return ctrl.Result{}, err
	}

	spec := terminatingGatewayService.Spec

	// register external service with Consul

	catalogRegisteration := &api.CatalogRegistration{
		Node:    "legacy_node",
		Address: "10.20.10.22",
		Service: &api.AgentService{
			ID:      spec.Service.ID,
			Service: spec.Service.Service,
			Port:    spec.Service.Port}}

	_, err = r.ConsulClient.Catalog().Register(catalogRegisteration, &api.WriteOptions{})

	if err != nil {
		r.Log.Error(err, "Unable to register external service with Consul", "name", req.Name, "ns", req.Namespace)
		r.updateStatusError(ctx, terminatingGatewayService, err)
		return ctrl.Result{}, err
	}

	// Update the terminating gateway ACL token

	// update existing role to include new policy
	aclPolicy := &api.ACLPolicy{
		Name:  spec.Service.Service + "-write-policy",
		Rules: "service \"" + spec.Service.Service + "\" {policy = \"write\"}"}

	_, _, err = r.ConsulClient.ACL().PolicyCreate(aclPolicy, &api.WriteOptions{})
	if err != nil {
		r.Log.Error(err, "Unable to create new policy", "name", req.Name, "ns", req.Namespace)
		r.updateStatusError(ctx, terminatingGatewayService, err)
		return ctrl.Result{}, err
	}

	// Fetch ID of the terminating gateway token

	terminatingGatewayACLTokenFound := false

	aclRoleList, _, err := r.ConsulClient.ACL().RoleList(&api.QueryOptions{})
	if err != nil {
		r.Log.Error(err, "Error listing all ACL Roles", "name", req.Name, "ns", req.Namespace)
		r.updateStatusError(ctx, terminatingGatewayService, err)
		return ctrl.Result{}, err
	}

	matchedRole, err := terminatingGatewayACLRole(aclRoleList)
	if err != nil {
		r.Log.Error(err, "Terminating Gateway ACL Role not found", "name", req.Name, "ns", req.Namespace)
		r.updateStatusError(ctx, terminatingGatewayService, err)
		return ctrl.Result{}, err
	} else {
		terminatingGatewayACLTokenFound = true
	}

	// Update terminating Gateway ACL token with new policy
	if terminatingGatewayACLTokenFound {
		_, _, err = r.ConsulClient.ACL().RoleUpdate(matchedRole, &api.WriteOptions{})
		if err != nil {
			r.Log.Error(err, "Error updating terminating Gateway ACL token with new policy", "name", req.Name, "ns", req.Namespace)
			r.updateStatusError(ctx, terminatingGatewayService, err)
			return ctrl.Result{}, err
		}
	}

	// Store the state in the status
	err = r.updateStatus(ctx, terminatingGatewayService)
	return ctrl.Result{}, err
}

func terminatingGatewayACLRole(aclRoleList []*api.ACLRole) (*api.ACLRole, error) {
	strToFind := "- RELEASE_NAME-terminating-gateway-policy"

	result := &api.ACLRole{}
	roleFound := false

	for _, aclRole := range aclRoleList {
		if strings.Contains(aclRole.ID, strToFind) || strings.Contains(aclRole.Name, strToFind) || strings.Contains(aclRole.Description, strToFind) {
			roleFound = true
			result = aclRole
			break
		}
		// search policies
		for _, aclRolePolicyLink := range aclRole.Policies {
			if strings.Contains(aclRolePolicyLink.ID, strToFind) || strings.Contains(aclRolePolicyLink.Name, strToFind) {
				roleFound = true
				result = aclRole
				break
			}
		}
	}

	if !roleFound {
		return result, errors.New("Terminating Gateway ACL Role not found")
	}
	return result, nil
}

// updateStatus updates the terminatingGatewayService's ReconcileError in the status
func (r *TerminatingGatewayServiceController) updateStatus(ctx context.Context, terminatingGatewayService *consulv1alpha1.TerminatingGatewayService) error {
	terminatingGatewayService.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	terminatingGatewayService.Status.ReconcileError = &consulv1alpha1.ReconcileErrorStatus{
		Error:   pointerToBool(false),
		Message: pointerToString(""),
	}
	err := r.Status().Update(ctx, terminatingGatewayService)
	if err != nil {
		r.Log.Error(err, "failed to update TerminatingGatewayService status", "name", terminatingGatewayService.Name, terminatingGatewayService.Namespace)
	}
	return err
}

// updateStatusError updates the terminatingGatewayService's ReconcileError in the status.
func (r *TerminatingGatewayServiceController) updateStatusError(ctx context.Context, terminatingGatewayService *consulv1alpha1.TerminatingGatewayService, reconcileErr error) {
	terminatingGatewayService.Status.ReconcileError = &consulv1alpha1.ReconcileErrorStatus{
		Error:   pointerToBool(true),
		Message: pointerToString(reconcileErr.Error()),
	}

	terminatingGatewayService.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	err := r.Status().Update(ctx, terminatingGatewayService)
	if err != nil {
		r.Log.Error(err, "failed to update TerminatingGatewayService status", "name", terminatingGatewayService.Name, terminatingGatewayService.Namespace)
	}

}

// SetupWithManager sets up the controller with the Manager.
func (r *TerminatingGatewayServiceController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.TerminatingGatewayService{}).
		Complete(r)
}
