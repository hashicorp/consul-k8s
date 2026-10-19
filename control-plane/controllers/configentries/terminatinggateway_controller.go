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

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"

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

	// ReleaseName and ReleaseNamespace identify the Consul Helm release. The release
	// namespace is used to scope the terminating gateway ACL binding rule.
	ReleaseName      string
	ReleaseNamespace string
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
		// The binding rule can only be scoped to the release namespace when it is known.
		if r.ReleaseNamespace != "" {
			helmValues, err := helmvalues.GetHelmValues(ctx, r.Client, r.ReleaseName, r.ReleaseNamespace)
			if err != nil {
				log.Error(err, "failed to get Helm values")
				r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
				return ctrl.Result{}, err
			}
			if err := r.ensureTerminatingGatewayACLBootstrap(log, termGW, helmValues); err != nil {
				log.Error(err, "error bootstrapping terminating-gateway ACLs")
				r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
				return ctrl.Result{}, err
			}
		}

		err = r.updateACls(log, termGW)
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

// ensureTerminatingGatewayACLBootstrap creates or updates the ACL policy, role and binding rule
// for a terminating gateway. The binding rule is restricted to the Consul release namespace so
// that a same-named ServiceAccount in another namespace cannot log in and receive the role.
func (r *TerminatingGatewayController) ensureTerminatingGatewayACLBootstrap(
	log logr.Logger,
	termGW *consulv1alpha1.TerminatingGateway,
	helmValues *helmvalues.HelmValues,
) error {
	if helmValues == nil {
		return fmt.Errorf("helm values are nil")
	}
	if !helmValues.Global.ACLs.ManageSystemACLs {
		return nil
	}

	consulClient, err := consul.NewClientFromConnMgr(
		r.ConfigEntryController.ConsulClientConfig,
		r.ConfigEntryController.ConsulServerConnMgr,
	)
	if err != nil {
		return fmt.Errorf("create consul client: %w", err)
	}

	fullName := helmvalues.ConsulFullName(helmValues)
	gatewayName := termGW.Name
	serviceAccountName := fmt.Sprintf("%s-%s", fullName, gatewayName)
	authMethodName := helmvalues.LocalComponentAuthMethodName(helmValues)

	consulNamespace := r.terminatingGatewayConsulNamespace(termGW, helmValues)

	rules, err := r.renderTerminatingGatewayACLRules(gatewayName, consulNamespace)
	if err != nil {
		return fmt.Errorf("render terminating gateway ACL rules: %w", err)
	}

	policyName := fmt.Sprintf("%s-policy", gatewayName)

	var datacenters []string
	if r.ConfigEntryController != nil && r.ConfigEntryController.DatacenterName != "" {
		datacenters = []string{r.ConfigEntryController.DatacenterName}
	}

	desiredPolicy := &capi.ACLPolicy{
		Name:        policyName,
		Description: fmt.Sprintf("%s Token Policy", policyName),
		Rules:       rules,
		Datacenters: datacenters,
	}

	existingPolicy, _, err := consulClient.ACL().PolicyReadByName(policyName, nil)
	if err != nil {
		return fmt.Errorf("read ACL policy %q: %w", policyName, err)
	}

	if existingPolicy == nil {
		log.Info("creating terminating gateway ACL policy", "policy", policyName)
		if _, _, err := consulClient.ACL().PolicyCreate(desiredPolicy, nil); err != nil {
			return fmt.Errorf("create ACL policy %q: %w", policyName, err)
		}
	} else {
		existingPolicy.Description = desiredPolicy.Description
		existingPolicy.Rules = desiredPolicy.Rules
		existingPolicy.Datacenters = desiredPolicy.Datacenters
		log.Info("updating terminating gateway ACL policy", "policy", policyName)
		if _, _, err := consulClient.ACL().PolicyUpdate(existingPolicy, nil); err != nil {
			return fmt.Errorf("update ACL policy %q: %w", policyName, err)
		}
	}

	roleName := fmt.Sprintf("%s-%s-acl-role", fullName, gatewayName)
	rolePolicyLink := &capi.ACLRolePolicyLink{Name: policyName}

	existingRole, _, err := consulClient.ACL().RoleReadByName(roleName, nil)
	if err != nil {
		return fmt.Errorf("read ACL role %q: %w", roleName, err)
	}

	if existingRole == nil {
		role := &capi.ACLRole{
			Name:        roleName,
			Description: fmt.Sprintf("ACL Role for %s", serviceAccountName),
			Policies:    []*capi.ACLRolePolicyLink{rolePolicyLink},
		}
		log.Info("creating terminating gateway ACL role", "role", roleName)
		if _, _, err := consulClient.ACL().RoleCreate(role, nil); err != nil {
			return fmt.Errorf("create ACL role %q: %w", roleName, err)
		}
	} else {
		found := false
		for _, p := range existingRole.Policies {
			if p != nil && p.Name == policyName {
				found = true
				break
			}
		}
		if !found {
			existingRole.Policies = append(existingRole.Policies, rolePolicyLink)
		}
		existingRole.Description = fmt.Sprintf("ACL Role for %s", serviceAccountName)
		log.Info("updating terminating gateway ACL role", "role", roleName)
		if _, _, err := consulClient.ACL().RoleUpdate(existingRole, nil); err != nil {
			return fmt.Errorf("update ACL role %q: %w", roleName, err)
		}
	}

	bindingRule := &capi.ACLBindingRule{
		Description: fmt.Sprintf("Binding Rule for %s", serviceAccountName),
		AuthMethod:  authMethodName,
		Selector: fmt.Sprintf(
			"serviceaccount.name==%q and serviceaccount.namespace==%q",
			serviceAccountName,
			r.ReleaseNamespace,
		),
		BindType: capi.BindingRuleBindTypeRole,
		BindName: roleName,
	}

	existingRules, _, err := consulClient.ACL().BindingRuleList(authMethodName, &capi.QueryOptions{})
	if err != nil {
		return fmt.Errorf("list binding rules for auth method %q: %w", authMethodName, err)
	}

	var matchingRule *capi.ACLBindingRule
	for _, rule := range existingRules {
		if rule.BindName == bindingRule.BindName && rule.Description == bindingRule.Description {
			matchingRule = rule
			break
		}
	}

	if matchingRule == nil {
		log.Info("creating terminating gateway ACL binding rule", "authMethod", authMethodName, "serviceAccount", serviceAccountName)
		if _, _, err := consulClient.ACL().BindingRuleCreate(bindingRule, nil); err != nil {
			return fmt.Errorf("create binding rule for auth method %q: %w", authMethodName, err)
		}
	} else {
		bindingRule.ID = matchingRule.ID
		log.Info("updating terminating gateway ACL binding rule", "authMethod", authMethodName, "serviceAccount", serviceAccountName)
		if _, _, err := consulClient.ACL().BindingRuleUpdate(bindingRule, nil); err != nil {
			return fmt.Errorf("update binding rule for auth method %q: %w", authMethodName, err)
		}
	}

	return nil
}

// renderTerminatingGatewayACLRules renders the ACL policy rules for a terminating gateway service.
func (r *TerminatingGatewayController) renderTerminatingGatewayACLRules(gatewayName, consulNamespace string) (string, error) {
	const tpl = `
{{- if .EnablePartitions }}
partition "{{ .Partition }}" {
{{- end }}
{{- if .EnableNamespaces }}
  namespace "{{ .Namespace }}" {
{{- end }}
    service "{{ .GatewayName }}" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
{{- if .EnableNamespaces }}
  }
{{- end }}
{{- if .EnablePartitions }}
}
{{- end }}
`

	data := struct {
		GatewayName      string
		Namespace        string
		Partition        string
		EnableNamespaces bool
		EnablePartitions bool
	}{
		GatewayName:      gatewayName,
		Namespace:        defaultIfEmpty(consulNamespace),
		Partition:        r.adminPartition(),
		EnableNamespaces: r.NamespacesEnabled,
		EnablePartitions: r.PartitionsEnabled,
	}

	var out bytes.Buffer
	t, err := template.New("terminating-gateway-acl-rules").Parse(strings.TrimSpace(tpl))
	if err != nil {
		return "", err
	}
	if err := t.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// terminatingGatewayConsulNamespace resolves the Consul namespace of the terminating gateway
// service. Resolution order:
//  1. Per-gateway consulNamespace from Helm values.
//  2. terminatingGateways.defaults.consulNamespace from Helm values (a bare "default" is
//     treated as unset when Consul namespaces are enabled so mirroring can take precedence).
//  3. The mirrored namespace derived from the gateway's K8s namespace, when mirroring is enabled.
//  4. "default".
func (r *TerminatingGatewayController) terminatingGatewayConsulNamespace(
	termGW *consulv1alpha1.TerminatingGateway,
	helmValues *helmvalues.HelmValues,
) string {
	if termGW == nil || helmValues == nil {
		return defaultIfEmpty("")
	}

	for _, gw := range helmValues.TerminatingGateways.Gateways {
		if gw.Name == termGW.Name && gw.ConsulNamespace != "" {
			return gw.ConsulNamespace
		}
	}

	namespacesEnabled := helmValues.Global.EnableConsulNamespaces
	mirroring := false
	mirroringPrefix := ""
	if r != nil && r.ConfigEntryController != nil {
		namespacesEnabled = r.ConfigEntryController.EnableConsulNamespaces
		mirroring = r.ConfigEntryController.EnableNSMirroring
		mirroringPrefix = r.ConfigEntryController.NSMirroringPrefix
	}

	defaultsNS := helmValues.TerminatingGateways.Defaults.ConsulNamespace
	if defaultsNS != "" && !(defaultsNS == "default" && namespacesEnabled) {
		return defaultsNS
	}

	if namespacesEnabled && mirroring {
		return mirroringPrefix + termGW.Namespace
	}

	return defaultIfEmpty(defaultsNS)
}
