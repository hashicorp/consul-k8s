package connectinject

import (
	"context"
	"errors"
	"fmt"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TerminatingGatewayServiceController reconciles a TerminatingGatewayService object.
type TerminatingGatewayServiceController struct {
	client.Client
	// ConsulClient points at the agent local to the connect-inject deployment pod.
	ConsulClient *api.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	context.Context
	AclEnabled bool
}

//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggatewayservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggatewayservices/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.6.4/pkg/reconcile
func (r *TerminatingGatewayServiceController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("received request for TerminatingGatewayService", "name", "ns", req.Namespace)

	// Get the TerminatingGatewayService resource.
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

	// The DeletionTimestamp is zero when the object has not been marked for deletion. The finalizer is added
	// in case it does not exist to all resources. If the DeletionTimestamp is non-zero, the object has been
	// marked for deletion and goes into the deletion workflow.
	if terminatingGatewayService.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(terminatingGatewayService, FinalizerName) {
			controllerutil.AddFinalizer(terminatingGatewayService, FinalizerName)
			if err := r.Update(ctx, terminatingGatewayService); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if containsString(terminatingGatewayService.Finalizers, FinalizerName) {
			r.Log.Info("TerminatingGatewayService was deleted, deleting from consul", "name", req.Name, "ns", req.Namespace)
			_, err := r.deleteService(spec.Service.ServiceName)
			if err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(terminatingGatewayService, FinalizerName)
			err = r.Update(ctx, terminatingGatewayService)
			return ctrl.Result{}, err
		}
	}

	err = r.createOrUpdateService(terminatingGatewayService, ctx)
	if err != nil {
		r.Log.Error(err, "Unable to create or update service", "name", req.Name, "ns", req.Namespace)

		err = r.updateStatusError(ctx, terminatingGatewayService, err)
		if err != nil {
			r.Log.Error(err, "failed to update TerminatingGatewayService status", "name", terminatingGatewayService.Name, terminatingGatewayService.Namespace)
		}
	}

	return ctrl.Result{}, err
}

// terminatingGatewayACLRole returns the ACL role of the running terminating gateway.
func terminatingGatewayACLRole(aclRoleList []*api.ACLRole) (*api.ACLRole, error) {
	strToFind := "terminating-gateway"

	result := &api.ACLRole{}
	roleFound := false

	for _, role := range aclRoleList {
		if strings.Contains(role.Name, strToFind) {
			result = role
			roleFound = true
			break
		}
	}

	if !roleFound {
		return result, errors.New("terminating Gateway ACL Role not found")
	}
	return result, nil
}

// updateStatus updates the terminatingGatewayService's information in the status.
func (r *TerminatingGatewayServiceController) updateStatus(ctx context.Context, terminatingGatewayService *consulv1alpha1.TerminatingGatewayService) error {

	policyName := ""
	if r.AclEnabled {
		policyName = fmt.Sprintf("%s-write-policy", terminatingGatewayService.Spec.Service.ServiceName)
	}

	terminatingGatewayService.Status.LastSyncedTime = &metav1.Time{Time: time.Now()}
	terminatingGatewayService.SetSyncedCondition(corev1.ConditionTrue, "", "")

	terminatingGatewayService.Status.ServiceInfoRef = &consulv1alpha1.ServiceInfoRefStatus{
		ServiceName: terminatingGatewayService.Spec.Service.ServiceName,
		PolicyName:  policyName,
	}

	err := r.Status().Update(ctx, terminatingGatewayService)
	if err != nil {
		err = fmt.Errorf("failed to update TerminatingGatewayService status: %v", err)
	}
	return err
}

// updateStatusError updates the terminatingGatewayService's Condition in the status.
func (r *TerminatingGatewayServiceController) updateStatusError(ctx context.Context, terminatingGatewayService *consulv1alpha1.TerminatingGatewayService, reconcileErr error) error {
	terminatingGatewayService.SetSyncedCondition(corev1.ConditionFalse, "Error updating status", reconcileErr.Error())

	err := r.Status().Update(ctx, terminatingGatewayService)
	if err != nil {
		err = fmt.Errorf("failed to update TerminatingGatewayService status: %v", err)
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *TerminatingGatewayServiceController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&consulv1alpha1.TerminatingGatewayService{}).
		Complete(r)
}

// createOrUpdateService creates or updates a service in Consul.
func (r *TerminatingGatewayServiceController) createOrUpdateService(terminatingGatewayService *consulv1alpha1.TerminatingGatewayService, ctx context.Context) error {
	spec := terminatingGatewayService.Spec

	service, serviceExists, err := r.serviceFound(spec.Service.ServiceName)
	if err != nil {
		return fmt.Errorf("error obtaining existing services: %v", err)
	}

	if !serviceExists {
		// register external service with Consul.
		serviceID := ""
		if spec.Service.ServiceID == "" {
			serviceID = spec.Service.ServiceName
		} else {
			serviceID = spec.Service.ServiceID
		}

		catalogRegistration := &api.CatalogRegistration{
			Node:    spec.Service.Node,
			Address: spec.Service.Address,
			Service: &api.AgentService{
				ID:      serviceID,
				Service: spec.Service.ServiceName,
				Port:    spec.Service.ServicePort}}

		_, err = r.ConsulClient.Catalog().Register(catalogRegistration, nil)
		if err != nil {
			return fmt.Errorf("unable to register external service with Consul: %v", err)
		}

		if r.AclEnabled {
			err = r.updateTerminatingGatewayTokenWithWritePolicy(terminatingGatewayService)
			if err != nil {
				return fmt.Errorf("unable to update the terminating gateway ACL token with new write policy: %v", err)
			}
		}

		// Store the state in the status.
		err = r.updateStatus(ctx, terminatingGatewayService)
		return err
	}

	err = r.updateServiceIfDifferent(service, terminatingGatewayService, ctx)
	return err
}

// updateTerminatingGatewayTokenWithWritePolicy updates the terminating gateway token with the "write" policy for a service.
func (r *TerminatingGatewayServiceController) updateTerminatingGatewayTokenWithWritePolicy(terminatingGatewayService *consulv1alpha1.TerminatingGatewayService) error {
	spec := terminatingGatewayService.Spec
	// Update the terminating gateway ACL token.

	// create a new policy that includes write permissions.
	// update existing role to include new policy.
	matchedRole, tokenFound, err := r.fetchTerminatingGatewayToken()
	if err != nil {
		return fmt.Errorf("error fetching terminating gateway token: %v", err)
	} else if !tokenFound {
		return fmt.Errorf("failed to find terminating gateway token: %v", err)
	}

	aclPolicy := &api.ACLPolicy{
		Name:  spec.Service.ServiceName + "-write-policy",
		Rules: fmt.Sprintf(`service "%s" {policy = "write"}`, spec.Service.ServiceName)}

	allConsulPolicies, _, err := r.ConsulClient.ACL().PolicyList(nil)
	if err != nil {
		return fmt.Errorf("unable to list exisiting policies: %v", err)
	}
	_, policyAlreadyExists := findConsulPolicy(aclPolicy.Name, allConsulPolicies)

	if !policyAlreadyExists {
		_, _, err = r.ConsulClient.ACL().PolicyCreate(aclPolicy, nil)
		if err != nil {
			return fmt.Errorf("unable to create new policy: %v", err)
		}

		aclRolePolicyLink := &api.ACLRolePolicyLink{
			Name: aclPolicy.Name,
		}

		termGwRole, _, err := r.ConsulClient.ACL().RoleRead(matchedRole.ID, nil)
		if err != nil {
			return fmt.Errorf("error reading terminating gateway role: %v", err)
		}

		termGwRole.Policies = append(termGwRole.Policies, aclRolePolicyLink)
		_, _, err = r.ConsulClient.ACL().RoleUpdate(termGwRole, nil)
		if err != nil {
			return fmt.Errorf("error updating terminating Gateway ACL token with new policy: %v", err)
		}
	}
	return nil
}

// serviceFound specifies whether a service was found.
func (r *TerminatingGatewayServiceController) serviceFound(serviceName string) (*api.CatalogService, bool, error) {
	result := &api.CatalogService{}
	serviceExists := false

	services, _, err := r.ConsulClient.Catalog().Service(serviceName, "", nil)
	length := len(services)
	if err != nil {
		return result, serviceExists, err
	}
	if length > 1 {
		err = errors.New("multiple services found with the same serviceName")
	} else if length == 0 {
		err = errors.New("no service found")
	} else {
		result = services[0]
		serviceExists = true
	}
	return result, serviceExists, err
}

// updateServiceIfDifferent updates information about a service if the CRD specification changes.
func (r *TerminatingGatewayServiceController) updateServiceIfDifferent(service *api.CatalogService, terminatingGatewayService *consulv1alpha1.TerminatingGatewayService, ctx context.Context) error {
	spec := terminatingGatewayService.Spec

	updatedCatalogRegisteration := &api.CatalogRegistration{
		Node:    spec.Service.Node,
		Address: spec.Service.Address,

		Service: &api.AgentService{
			Service: spec.Service.ServiceName,
			ID:      spec.Service.ServiceID,
		},
	}

	if service.Node != spec.Service.Node {
		updatedCatalogRegisteration.Node = spec.Service.Node
	}

	if service.Datacenter != spec.Service.Datacenter {
		updatedCatalogRegisteration.Datacenter = spec.Service.Datacenter
	}

	if service.ServiceAddress != spec.Service.ServiceAddress {
		updatedCatalogRegisteration.Service.Address = spec.Service.ServiceAddress
	}

	if service.ServicePort != spec.Service.ServicePort {
		updatedCatalogRegisteration.Service.Port = spec.Service.ServicePort
	}

	if service.ServiceEnableTagOverride != spec.Service.ServiceEnableTagOverride {
		updatedCatalogRegisteration.Service.EnableTagOverride = spec.Service.ServiceEnableTagOverride
	}

	// Delete old service.
	_, err := r.onlyDeleteServiceEntry(service.ServiceName)
	if err != nil {
		return fmt.Errorf("error deleting stale service entry: %v", err)
	}

	// Register updated service.
	_, err = r.ConsulClient.Catalog().Register(updatedCatalogRegisteration, nil)
	if err != nil {
		return fmt.Errorf("unable to update TerminatingGatewayService status: %v", err)
	}

	// Check if write policy needs to be created.
	if terminatingGatewayService.Status.ServiceInfoRef.PolicyName == "" && r.AclEnabled {
		// ACLs have just been enabled. Thus, update TerminatingGatewayToken with write policy.
		err := r.updateTerminatingGatewayTokenWithWritePolicy(terminatingGatewayService)
		if err != nil {
			return fmt.Errorf("unable to update the terminating gateway ACL token with new write policy: %v", err)
		}
	}

	// Store the state in the status.
	err = r.updateStatus(ctx, terminatingGatewayService)
	return err
}

// deleteService deletes a service and its associated "write" policy.
func (r *TerminatingGatewayServiceController) deleteService(serviceName string) (bool, error) {
	serviceDeleted := false
	// Search for service.
	service, serviceExists, err := r.serviceFound(serviceName)
	if err != nil {
		err = fmt.Errorf("error finding service to delete: %v", err)
		return serviceDeleted, err
	}
	if serviceExists {

		serviceDeleted, err = r.onlyDeleteServiceEntry(service.ServiceName)
		if err != nil {
			err = fmt.Errorf("error deleting service entry: %v", err)
			return serviceDeleted, err
		} else {
			serviceDeleted = true
		}

		if r.AclEnabled {
			err = r.deleteTerminatingGatewayTokenWritePolicy(serviceName)
			if err != nil {
				err = fmt.Errorf("unable to delete terminating gateway token's write policy: %v", err)
				serviceDeleted = false
				return serviceDeleted, err
			}
		}
	}
	return serviceDeleted, nil
}

// onlyDeleteServiceEntry de-registers a service.
func (r *TerminatingGatewayServiceController) onlyDeleteServiceEntry(serviceName string) (bool, error) {
	serviceDeleted := false
	service, serviceExists, _ := r.serviceFound(serviceName)

	if serviceExists {
		catalogDeregistration := &api.CatalogDeregistration{
			Node:       service.Node,
			Address:    service.Address,
			ServiceID:  service.ServiceID,
			Datacenter: service.Datacenter,
		}
		_, err := r.ConsulClient.Catalog().Deregister(catalogDeregistration, nil)
		if err != nil {
			err = fmt.Errorf("error deleting service: %v", err)
			return serviceDeleted, err
		} else {
			serviceDeleted = true
		}
	}

	return serviceDeleted, nil
}

// deleteTerminatingGatewayTokenWritePolicy deletes the "write" policy under the terminating gateway.
func (r *TerminatingGatewayServiceController) deleteTerminatingGatewayTokenWritePolicy(serviceName string) error {
	// Search for policy.
	terminatingGatewayToken, _, err := r.fetchTerminatingGatewayToken()
	if err != nil {
		return fmt.Errorf("unable to fetch terminating gateway token: %v", err)
	}

	policyName := fmt.Sprintf("%s-write-policy", serviceName)
	policies := terminatingGatewayToken.Policies
	indexToFind, policyFound := findAclPolicy(policyName, policies)

	if policyFound {
		// Delete actual policy.
		_, err = r.ConsulClient.ACL().PolicyDelete(policies[indexToFind].ID, nil)
		if err != nil {
			return fmt.Errorf("error deleting write policy: %v", err)
		}

		// Remove policy from policies.
		policies[indexToFind] = policies[len(policies)-1]
		policies[len(policies)-1] = &api.ACLRolePolicyLink{}
		policies = policies[:len(policies)-1]

	} else {
		return fmt.Errorf("error finding write  policy: %v", err)
	}

	updatedRole := &api.ACLRole{
		ID:       terminatingGatewayToken.ID,
		Name:     terminatingGatewayToken.Name,
		Policies: policies,
	}

	// Delete policy from terminating gateway's policies.
	_, _, err = r.ConsulClient.ACL().RoleUpdate(updatedRole, nil)
	if err != nil {
		return fmt.Errorf("error updating terminating Gateway ACL token with deleted policy: %v", err)
	}
	return nil
}

// fetchTerminatingGatewayToken returns the terminating gateway token.
func (r *TerminatingGatewayServiceController) fetchTerminatingGatewayToken() (*api.ACLRole, bool, error) {
	var matchedRole *api.ACLRole
	terminatingGatewayACLTokenFound := false

	aclRoleList, _, err := r.ConsulClient.ACL().RoleList(nil)
	if err != nil {
		err = fmt.Errorf("error Listing all ACL Roles: %v", err)
		return matchedRole, terminatingGatewayACLTokenFound, err
	}

	matchedRole, err = terminatingGatewayACLRole(aclRoleList)
	if err != nil {
		err = fmt.Errorf("terminating Gateway ACL Role not found: %v", err)
		return matchedRole, terminatingGatewayACLTokenFound, err
	} else {
		terminatingGatewayACLTokenFound = true
	}
	return matchedRole, terminatingGatewayACLTokenFound, nil
}

// serviceFound specifies whether a service was found.
// It is different from the serviceFound controller method because it does not require a controller.
func serviceFound(serviceName string, consulClient *api.Client) (*api.CatalogService, bool) {
	result := &api.CatalogService{}
	serviceExists := false

	services, _, err := consulClient.Catalog().Service(serviceName, "", nil)
	if err != nil {
		return result, serviceExists
	}
	if len(services) == 1 {
		result = services[0]
		serviceExists = true
	}
	return result, serviceExists
}

// fetchTerminatingGatewayToken returns the terminating gateway token.
// It is different from the fetchTerminatingGatewayToken controller method because it does not require a controller.
func fetchTerminatingGatewayToken(consulClient *api.Client) (*api.ACLRole, bool) {
	var matchedRole *api.ACLRole
	terminatingGatewayACLTokenFound := false

	aclRoleList, _, err := consulClient.ACL().RoleList(nil)
	if err != nil {
		return matchedRole, terminatingGatewayACLTokenFound
	}

	matchedRole, err = terminatingGatewayACLRole(aclRoleList)
	if err == nil {
		terminatingGatewayACLTokenFound = true
	}
	return matchedRole, terminatingGatewayACLTokenFound
}

// findAclPolicy returns the index of the acl policy to locate. It also specifies if the policy was found.
func findAclPolicy(policyName string, allPolicies []*api.ACLRolePolicyLink) (int, bool) {
	indexToFind := -1
	found := false

	for i, policy := range allPolicies {
		if strings.Contains(policy.Name, policyName) {
			indexToFind = i
			found = true
			break
		}
	}
	return indexToFind, found
}

// findConsulPolicy returns the index of the Consul policy to locate. It also specifies if the policy was found.
func findConsulPolicy(policyName string, allPolicies []*api.ACLPolicyListEntry) (int, bool) {
	indexToFind := -1
	found := false

	for i, policy := range allPolicies {
		if strings.Contains(policy.Name, policyName) {
			indexToFind = i
			found = true
			break
		}
	}
	return indexToFind, found
}
