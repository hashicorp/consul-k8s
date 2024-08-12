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

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

var _ Controller = (*TerminatingGatewayController)(nil)

// TerminatingGatewayController is the controller for TerminatingGateway resources.
type TerminatingGatewayController struct {
	client.Client
	FinalizerPatcher

	NamespacesEnabled bool

	Log                   logr.Logger
	Scheme                *runtime.Scheme
	ConfigEntryController *ConfigEntryController
}

func init() {
	servicePolicyTpl = template.Must(template.New("root").Parse(strings.TrimSpace(servicePolicyRulesTpl)))
}

type templateArgs struct {
	Namespace        string
	ServiceName      string
	EnableNamespaces bool
}

var (
	servicePolicyTpl      *template.Template
	servicePolicyRulesTpl = `
{{- if .EnableNamespaces }}
namespace "{{.Namespace}}" {
{{- end }}
  service "{{.ServiceName}}" { 
    policy = "write" 
  }
{{- if .EnableNamespaces }}
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

func (r *TerminatingGatewayController) SetupWithManager(mgr ctrl.Manager) error {
	return setupWithManager(mgr, &consulv1alpha1.TerminatingGateway{}, r)
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

func (r *TerminatingGatewayController) updateACls(log logr.Logger, termGW *consulv1alpha1.TerminatingGateway) error {
	client, err := consul.NewClientFromConnMgr(r.ConfigEntryController.ConsulClientConfig, r.ConfigEntryController.ConsulServerConnMgr)
	if err != nil {
		return err
	}

	roles, _, err := client.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	terminatingGatewayRoleID := ""
	for _, role := range roles {
		if strings.Contains(role.Name, termGW.Name) {
			terminatingGatewayRoleID = role.ID
			break
			// }
		}
	}

	if terminatingGatewayRoleID == "" {
		return errors.New("terminating gateway role not found")
	}

	termGwRole, _, err := client.ACL().RoleRead(terminatingGatewayRoleID, nil)
	if err != nil {
		return err
	}

	var termGWPolicy *capi.ACLRolePolicyLink

	for _, policy := range termGwRole.Policies {
		if strings.Contains(policy.Name, termGW.Name) {
			termGWPolicy = policy
			break
		}
	}

	// add one to length to include the terminating-gateway policy for itself
	termGWPolicies := make([]*capi.ACLRolePolicyLink, 0, len(termGW.Spec.Services)+1)
	termGWPolicies = append(termGWPolicies, termGWPolicy)

	for _, service := range termGW.Spec.Services {
		var data bytes.Buffer
		if err := servicePolicyTpl.Execute(&data, templateArgs{
			EnableNamespaces: r.NamespacesEnabled,
			Namespace:        defaultIfEmpty(service.Namespace),
			ServiceName:      service.Name,
		}); err != nil {
			// just panic if we can't compile the simple template
			// as it means something else is going severly wrong.
			panic(err)
		}

		existingPolicy, _, err := client.ACL().PolicyReadByName(servicePolicyName(service.Name), &capi.QueryOptions{})
		if err != nil {
			log.Error(err, "error reading policy")
			return err
		}

		if existingPolicy == nil {
			_, _, err = client.ACL().PolicyCreate(&capi.ACLPolicy{
				Name:  servicePolicyName(service.Name),
				Rules: data.String(),
			}, nil)
			if err != nil {
				return err
			}
		}

		termGWPolicies = append(termGWPolicies, &capi.ACLRolePolicyLink{Name: servicePolicyName(service.Name)})
	}

	termGwRole.Policies = termGWPolicies

	_, _, err = client.ACL().RoleUpdate(termGwRole, nil)
	if err != nil {
		return err
	}

	log.Info("finished updating acl roles")
	return nil
}

func defaultIfEmpty(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func servicePolicyName(name string) string {
	return fmt.Sprintf("%s-write-policy", name)
}
