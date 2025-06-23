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
		err := r.updateACls(log, termGW)
		if err != nil {
			log.Error(err, "error updating terminating-gateway roles")
			r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
			return ctrl.Result{}, err
		}

		termGW.SetACLStatusConditon(corev1.ConditionTrue, "", "")
		err = r.UpdateStatus(ctx, termGW)
		if err != nil {
			log.Error(err, "error updating terminating-gateway status")
			return ctrl.Result{}, err
		}
	}

	return r.ConfigEntryController.ReconcileEntry(ctx, r, req, termGW)
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

func (r *TerminatingGatewayController) UpdateStatusFailedToSetACLs(ctx context.Context, termGW *consulv1alpha1.TerminatingGateway, err error) {
	termGW.SetSyncedCondition(corev1.ConditionFalse, consulv1alpha1.TerminatingGatewayFailedToSetACLs, err.Error())
	termGW.SetACLStatusConditon(corev1.ConditionFalse, consulv1alpha1.TerminatingGatewayFailedToSetACLs, err.Error())
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

func (r *TerminatingGatewayController) partitionsEnabled() (bool, error) {
	state, err := r.ConfigEntryController.ConsulServerConnMgr.State()
	if err != nil {
		return false, err
	}
	return state.Token != "", nil
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
		return errors.New("terminating gateway role not found")
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

	termGWPoliciesToKeep = append(termGWPoliciesToKeep, terminatingGatewayPolicy)
	terminatingGatewayRole.Policies = termGWPoliciesToKeep

	_, _, err = connMgrClient.ACL().RoleUpdate(terminatingGatewayRole, nil)
	if err != nil {
		return err
	}

	err = r.conditionallyDeletePolicies(log, connMgrClient, termGWPoliciesToRemove, termGW.Name)
	if err != nil {
		return err
	}

	return nil
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
		existingPolicy, _, err := client.ACL().PolicyReadByName(servicePolicyName(service.Name, defaultIfEmpty(service.Namespace)), &capi.QueryOptions{})
		if err != nil {
			log.Error(err, "error reading policy")
			return nil, nil, err
		}

		if existingPolicy == nil {
			policyTemplate := getPolicyTemplateFor(service.Name)
			var data bytes.Buffer
			if err := policyTemplate.Execute(&data, templateArgs{
				EnableNamespaces: r.NamespacesEnabled,
				EnablePartitions: r.PartitionsEnabled,
				Namespace:        defaultIfEmpty(service.Namespace),
				Partition:        defaultIfEmpty(r.ConfigEntryController.ConsulPartition),
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
				return nil, nil, err
			}
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
			log.Error(err, "failed to lookup terminating gateway list for service", serviceName)
			mErr = errors.Join(mErr, fmt.Errorf("failed to lookup terminating gateway list for service %q: %w", serviceName, err))
			continue
		}
		if len(termGWList.Items) == 0 {
			policiesToDelete = append(policiesToDelete, policy)
		}
	}

	for _, policy := range policiesToDelete {
		// don't delete the policy for the gateway itself
		if policy.Name == fmt.Sprintf("%s-policy", termGWName) {
			continue
		}

		policy, _, err := consulClient.ACL().PolicyReadByName(policy.Name, nil)
		if err != nil {
			log.Error(err, "failed to lookup policy by name from consul", policy.Name)
			mErr = errors.Join(mErr, fmt.Errorf("error reading policy %q: %w", policy.Name, err))
			continue
		}

		_, err = consulClient.ACL().PolicyDelete(policy.ID, nil)
		if err != nil {
			log.Error(err, "failed to delete policy from consul", policy.Name)
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
