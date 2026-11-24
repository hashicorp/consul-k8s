// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package configentries

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

var _ Controller = (*TerminatingGatewayController)(nil)

const terminatingGatewayByLinkedServiceName = "linkedServiceName"

// terminatingGatewayACLResyncPeriod is how often the controller reconciles a
// TerminatingGateway again when ACLs are enabled. server-acl-init also writes
// the terminating gateway role, and Consul has no compare-and-set for roles.
// Thus, a role update by server-acl-init that is based on an earlier read can
// remove a linked-service policy link that the controller added. The resync
// links it again.
const terminatingGatewayACLResyncPeriod = 5 * time.Minute

// errGatewayPolicyNotLinked means that the terminating gateway role has no
// link to the gateway policy. server-acl-init creates this link.
var errGatewayPolicyNotLinked = errors.New("terminating gateway policy is not linked to the terminating gateway role")

// TerminatingGatewayController is the controller for TerminatingGateway resources.
type TerminatingGatewayController struct {
	client.Client
	FinalizerPatcher

	NamespacesEnabled bool
	PartitionsEnabled bool

	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

func init() {
	servicePolicyTpl = template.Must(template.New("root").Parse(strings.TrimSpace(servicePolicyRulesTpl)))
	wildcardPolicyTpl = template.Must(template.New("root").Parse(strings.TrimSpace(wildcardPolicyRulesTpl)))
}

type templateArgs struct {
	Namespace        string
	Partition        string
	ServiceName      string
	EnableNamespaces bool
	EnablePartitions bool
}

var (
	servicePolicyTpl      *template.Template
	servicePolicyRulesTpl = `
{{- if .EnablePartitions }}
partition "{{.Partition}}" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace "{{.Namespace}}" {
{{- end }}
    service "{{.ServiceName}}" {
      policy    = "write"
      intention = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	wildcardPolicyTpl      *template.Template
	wildcardPolicyRulesTpl = `
{{- if .EnablePartitions }}
partition "{{.Partition}}" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace "{{.Namespace}}" {
{{- end }}
    service_prefix "" {
      policy    = "write"
      intention = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`
)

// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=consul.hashicorp.com,resources=terminatinggateways/status,verbs=get;update;patch

func (r *TerminatingGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.V(1).WithValues("terminating-gateway", req.NamespacedName)
	log.Info("Reconciling TerminatingGateway")
	termGW := &consulv1alpha1.TerminatingGateway{}
	// get the registration
	if err := r.Client.Get(ctx, req.NamespacedName, termGW); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get terminating-gateway")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// creation/modification
	enabled, err := r.aclsEnabled()
	if err != nil {
		log.Error(err, "error checking if acls are enabled")
		return ctrl.Result{}, err
	}

	if enabled {
		err = r.updateACls(log, termGW)
		if err != nil {
			log.Error(err, "error updating terminating-gateway roles")
			reason := consulv1alpha1.TerminatingGatewayFailedToSetACLs
			if errors.Is(err, errGatewayPolicyNotLinked) {
				reason = consulv1alpha1.TerminatingGatewayGatewayPolicyNotLinked
			}
			r.UpdateStatusFailedToSetACLs(ctx, termGW, reason, err)
			return ctrl.Result{}, err
		}

		termGW.SetACLStatusConditon(corev1.ConditionTrue, "", "")
		err = r.UpdateStatus(ctx, termGW)
		if err != nil {
			log.Error(err, "error updating terminating-gateway status")
			return ctrl.Result{}, err
		}
	}

	result, err := r.ConfigEntryController.ReconcileEntry(ctx, r, req, termGW)
	if err == nil && enabled && termGW.ObjectMeta.DeletionTimestamp.IsZero() && !result.Requeue && result.RequeueAfter == 0 {
		result.RequeueAfter = terminatingGatewayACLResyncPeriod
	}
	return result, err
}

func (r *TerminatingGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *TerminatingGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return r.Status().Update(ctx, obj, opts...)
}

func (r *TerminatingGatewayController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// setup the index to lookup registrations by service name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &v1alpha1.TerminatingGateway{}, terminatingGatewayByLinkedServiceName, termGWLinkedServiceIndexer); err != nil {
		return err
	}

	return setupWithManager(mgr, &consulv1alpha1.TerminatingGateway{}, r)
}

func termGWLinkedServiceIndexer(o client.Object) []string {
	termGW := o.(*v1alpha1.TerminatingGateway)
	names := make([]string, 0, len(termGW.Spec.Services))
	for _, service := range termGW.Spec.Services {
		names = append(names, service.Name)
	}

	return names
}

func (r *TerminatingGatewayController) UpdateStatusFailedToSetACLs(ctx context.Context, termGW *consulv1alpha1.TerminatingGateway, reason string, err error) {
	termGW.SetSyncedCondition(corev1.ConditionFalse, reason, err.Error())
	termGW.SetACLStatusConditon(corev1.ConditionFalse, reason, err.Error())
	if err := r.UpdateStatus(ctx, termGW); err != nil {
		r.Log.Error(err, "error updating status")
	}
}

func (r *TerminatingGatewayController) aclsEnabled() (bool, error) {
	state, err := r.ConfigEntryController.ConsulServerConnMgr.State()
	if err != nil {
		return false, err
	}
	return state.Token != "", nil
}

func (r *TerminatingGatewayController) adminPartition() string {
	if r.ConfigEntryController == nil {
		return common.DefaultConsulPartition
	}
	return defaultIfEmpty(r.ConfigEntryController.ConsulPartition)
}

func (r *TerminatingGatewayController) updateACls(log logr.Logger, termGW *consulv1alpha1.TerminatingGateway) error {
	connMgrClient, err := consul.NewClientFromConnMgr(r.ConfigEntryController.ConsulClientConfig, r.ConfigEntryController.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	roles, _, err := connMgrClient.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	terminatingGatewayRoleID := ""
	for _, role := range roles {
		// terminating gateway roles are always of the form ${INSTALL_NAME}-consul-${GATEWAY_NAME}-acl-role
		if strings.HasSuffix(role.Name, fmt.Sprintf("%s-acl-role", termGW.Name)) {
			terminatingGatewayRoleID = role.ID
			break
		}
	}

	if terminatingGatewayRoleID == "" {
		return fmt.Errorf("terminating gateway role ending in %q not found for gateway %q; server-acl-init creates this role",
			fmt.Sprintf("%s-acl-role", termGW.Name), termGW.Name)
	}

	terminatingGatewayRole, _, err := connMgrClient.ACL().RoleRead(terminatingGatewayRoleID, nil)
	if err != nil {
		return err
	}

	var terminatingGatewayPolicy *capi.ACLRolePolicyLink

	for _, policy := range terminatingGatewayRole.Policies {
		// terminating gateway policies are always of the form ${GATEWAY_NAME}-policy
		if policy.Name == fmt.Sprintf("%s-policy", termGW.Name) {
			terminatingGatewayPolicy = policy
			break
		}
	}

	var termGWPoliciesToKeep []*capi.ACLRolePolicyLink
	var termGWPoliciesToRemove []*capi.ACLRolePolicyLink

	existingTermGWPolicies := mapset.NewSet[string]()

	for _, policy := range terminatingGatewayRole.Policies {
		existingTermGWPolicies.Add(policy.Name)
	}

	if termGW.ObjectMeta.DeletionTimestamp.IsZero() {
		termGWPoliciesToKeep, termGWPoliciesToRemove, err = r.handleModificationForPolicies(log, connMgrClient, existingTermGWPolicies, termGW.Spec.Services)
		if err != nil {
			return err
		}
	} else {
		termGWPoliciesToKeep, termGWPoliciesToRemove = handleDeletionForPolicies(termGW.Spec.Services)
	}

	// Do not write the role when the gateway policy link is missing.
	// server-acl-init can link the gateway policy at the same time, and Consul
	// has no compare-and-set for roles, so a write based on this read could
	// remove that link again.
	if terminatingGatewayPolicy == nil {
		if !termGW.ObjectMeta.DeletionTimestamp.IsZero() {
			// Do not block the deletion of the TerminatingGateway. The deleted
			// service policies are no longer granted through the role.
			return r.conditionallyDeletePolicies(log, connMgrClient, termGWPoliciesToRemove, termGW.Name)
		}
		return fmt.Errorf("%w: role %q has no link to policy %q; server-acl-init creates this link. "+
			"Link the policy to the role, or run server-acl-init again",
			errGatewayPolicyNotLinked, terminatingGatewayRole.Name, fmt.Sprintf("%s-policy", termGW.Name))
	}
	termGWPoliciesToKeep = append(termGWPoliciesToKeep, terminatingGatewayPolicy)

	keptTermGWPolicies := mapset.NewSet[string]()
	for _, policy := range termGWPoliciesToKeep {
		keptTermGWPolicies.Add(policy.Name)
	}

	// Skip the write when the links do not change, so that a periodic resync
	// does not write the role each time.
	if !keptTermGWPolicies.Equal(existingTermGWPolicies) || len(termGWPoliciesToKeep) != len(terminatingGatewayRole.Policies) {
		terminatingGatewayRole.Policies = termGWPoliciesToKeep
		_, _, err = connMgrClient.ACL().RoleUpdate(terminatingGatewayRole, nil)
		if err != nil {
			return err
		}
	}

	return r.conditionallyDeletePolicies(log, connMgrClient, termGWPoliciesToRemove, termGW.Name)
}

func handleDeletionForPolicies(services []v1alpha1.LinkedService) ([]*capi.ACLRolePolicyLink, []*capi.ACLRolePolicyLink) {
	var termGWPoliciesToRemove []*capi.ACLRolePolicyLink
	for _, service := range services {
		termGWPoliciesToRemove = append(termGWPoliciesToRemove, &capi.ACLRolePolicyLink{Name: servicePolicyName(service.Name, defaultIfEmpty(service.Namespace))})
	}
	return nil, termGWPoliciesToRemove
}

func (r *TerminatingGatewayController) handleModificationForPolicies(log logr.Logger, client *capi.Client, existingTermGWPolicies mapset.Set[string], services []v1alpha1.LinkedService) ([]*capi.ACLRolePolicyLink, []*capi.ACLRolePolicyLink, error) {
	// add one to length to include the terminating-gateway policy for itself
	termGWPoliciesToKeep := make([]*capi.ACLRolePolicyLink, 0, len(services)+1)
	termGWPoliciesToRemove := make([]*capi.ACLRolePolicyLink, 0, len(services))

	termGWPoliciesToKeepNames := mapset.NewSet[string]()
	for _, service := range services {
		log.Info("Checking for existing policies", "policy", servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)))
		existingPolicy, _, err := client.ACL().PolicyReadByName(servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)), &capi.QueryOptions{})
		if err != nil {
			log.Error(err, "error reading policy")
			return nil, nil, err
		}

		if existingPolicy == nil {
			log.Info("No existing ACL Policies Found", "policy", servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)))
			policyTemplate := getPolicyTemplateFor(service.Name)
			policyNamespace := defaultIfEmpty(service.Namespace)
			policyAdminPartition := r.adminPartition()
			log.Info("Templating new ACL Policy", "Service", service.Name, "Namespace", policyNamespace, "Partition", policyAdminPartition)
			var data bytes.Buffer
			if err := policyTemplate.Execute(&data, templateArgs{
				EnableNamespaces: r.NamespacesEnabled,
				EnablePartitions: r.PartitionsEnabled,
				Namespace:        policyNamespace,
				Partition:        policyAdminPartition,
				ServiceName:      service.Name,
			}); err != nil {
				// just panic if we can't compile the simple template
				// as it means something else is going severly wrong.
				panic(err)
			}

			_, _, err = client.ACL().PolicyCreate(&capi.ACLPolicy{
				Name:  servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)),
				Rules: data.String(),
			}, nil)
			if err != nil {
				log.Error(err, "error creating policy")
				return nil, nil, err
			} else {
				log.Info("Created new ACL Policy", "Service", service.Name, "Namespace", policyNamespace, "Partition", policyAdminPartition)
			}
		} else {
			log.Info("Found for existing policies", "policy", existingPolicy.Name, "ID", existingPolicy.ID)
		}

		termGWPoliciesToKeep = append(termGWPoliciesToKeep, &capi.ACLRolePolicyLink{Name: servicePolicyName(service.Name, defaultIfEmpty(service.Namespace))})
		termGWPoliciesToKeepNames.Add(servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)))
	}

	for _, policy := range existingTermGWPolicies.Difference(termGWPoliciesToKeepNames).ToSlice() {
		termGWPoliciesToRemove = append(termGWPoliciesToRemove, &capi.ACLRolePolicyLink{Name: policy})
	}

	return termGWPoliciesToKeep, termGWPoliciesToRemove, nil
}

func (r *TerminatingGatewayController) conditionallyDeletePolicies(log logr.Logger, consulClient *capi.Client, policies []*capi.ACLRolePolicyLink, termGWName string) error {
	policiesToDelete := make([]*capi.ACLRolePolicyLink, 0, len(policies))
	var mErr error
	for _, policy := range policies {
		termGWList := &v1alpha1.TerminatingGatewayList{}
		serviceName := serviceNameFromPolicy(policy.Name)

		if err := r.Client.List(context.Background(), termGWList, client.MatchingFields{terminatingGatewayByLinkedServiceName: serviceName}); err != nil {
			log.Error(err, "failed to lookup terminating gateway list for service", "service", serviceName)
			mErr = errors.Join(mErr, fmt.Errorf("failed to lookup terminating gateway list for service %q: %w", serviceName, err))
			continue
		}
		if len(termGWList.Items) == 0 {
			policiesToDelete = append(policiesToDelete, policy)
		}
	}

	for _, policy := range policiesToDelete {
		// don't delete the policy for the gateway itself
		policyName := policy.Name

		if policyName == fmt.Sprintf("%s-policy", termGWName) {
			continue
		}

		policy, _, err := consulClient.ACL().PolicyReadByName(policyName, nil)
		if err != nil {
			log.Error(err, "failed to lookup policy by name from consul", "policy", policyName)
			mErr = errors.Join(mErr, fmt.Errorf("error reading policy %q: %w", policyName, err))
			continue
		}

		if policy == nil {
			log.Info("unable to find policy by name", "policy", policyName)
			continue
		}

		_, err = consulClient.ACL().PolicyDelete(policy.ID, nil)
		if err != nil {
			log.Error(err, "failed to delete policy from consul", "policy", policy.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error delete policy %q: %w", policy.Name, err))
		}
	}

	return mErr
}

func getPolicyTemplateFor(service string) *template.Template {
	if service == "*" {
		return wildcardPolicyTpl
	}
	return servicePolicyTpl
}

func defaultIfEmpty(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func servicePolicyName(name, namespace string) string {
	if name == "*" {
		return fmt.Sprintf("%s-wildcard-write-policy", namespace)
	}

	return fmt.Sprintf("%s-%s-write-policy", namespace, name)
}

func serviceNameFromPolicy(policyName string) string {
	// remove the namespace from the beginning of the string
	_, n, _ := strings.Cut(policyName, "-")

	// remove the write policy suffix
	return strings.TrimSuffix(n, "-write-policy")
}
