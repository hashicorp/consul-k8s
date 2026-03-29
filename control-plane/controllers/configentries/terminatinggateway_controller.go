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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"encoding/json"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	consulv1alpha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/helmvalues"
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
	ReleaseName           string
	ReleaseNamespace      string
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
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

func (r *TerminatingGatewayController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.V(1).WithValues("terminating-gateway", req.NamespacedName)
	log.Info("Reconciling TerminatingGateway")

	// Get Helm values from ConfigMap
	helmValues, err := helmvalues.GetHelmValues(ctx, r.Client, r.ReleaseName, r.ReleaseNamespace)
	if err != nil {
		log.Error(err, "failed to get Helm values")
		return ctrl.Result{}, err
	}

	// Access values
	log.Info("Retrieved Helm values",
		"datacenter", helmValues.Global.Datacenter,
		"enableConsulNamespaces", helmValues.Global.EnableConsulNamespaces,
		"defaultReplicas", helmValues.TerminatingGateways.Defaults.Replicas,
		"defaultAnnotations", helmValues.TerminatingGateways.Defaults.Annotations,
	)

	// Use the values in your reconciliation logic
	defaults := helmValues.TerminatingGateways.Defaults
	_ = defaults.Replicas
	_ = defaults.Annotations
	_ = defaults.ConsulNamespace

	termGW := &consulv1alpha1.TerminatingGateway{}
	// get the registration
	if err := r.Client.Get(ctx, req.NamespacedName, termGW); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(err, "unable to get terminating-gateway")
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	//print full termGW
	termGWBytes, err := json.MarshalIndent(termGW, "", "  ")
	if err != nil {
		log.Error(err, "failed to marshal TerminatingGateway")
	} else {
		log.Info("Loaded TerminatingGateway", "termGW", string(termGWBytes))
	}

	aclsEnabled, err := r.aclsEnabled()
	if err != nil {
		log.Error(err, "error checking if ACLs are enabled")
		return ctrl.Result{}, err
	}

	if aclsEnabled && helmValues.Global.ACLs.ManageSystemACLs {
		if err := r.ensureTerminatingGatewayACLBootstrap(log, termGW, helmValues); err != nil {
			log.Error(err, "error creating terminating gateway ACL role/policy/binding rule")
			r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
			return ctrl.Result{}, err
		}

		if err := r.updateACls(log, termGW); err != nil {
			log.Error(err, "error updating terminating gateway linked-service ACL policies")
			r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
			return ctrl.Result{}, err
		}

		termGW.SetACLStatusCondition(corev1.ConditionTrue, "", "")
		if err := r.UpdateStatus(ctx, termGW); err != nil {
			log.Error(err, "error updating terminating gateway ACL status")
			return ctrl.Result{}, err
		}
	}

	// Reconcile Consul config entry first.
	result, err := r.ConfigEntryController.ReconcileEntry(ctx, r, req, termGW)
	if err != nil {
		log.Error(err, "error reconciling terminating gateway config entry")
		termGW.SetSyncedCondition(corev1.ConditionFalse, "FailedToReconcileConfigEntry", err.Error())
		if statusErr := r.UpdateStatus(ctx, termGW); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return result, err
	}

	// If the resource is being deleted, ConfigEntryController already handled
	// finalizer removal / Consul cleanup. Do not continue with deployment or
	// status updates because the object may already be gone.
	if !termGW.GetDeletionTimestamp().IsZero() {
		return result, nil
	}

	if deployErr := r.deployTerminatingGatewayDeployment(ctx, log, termGW, helmValues); deployErr != nil {
		log.Error(deployErr, "error deploying terminating gateway pod")
		termGW.SetSyncedCondition(corev1.ConditionFalse, "FailedToDeployPod", deployErr.Error())
		if statusErr := r.UpdateStatus(ctx, termGW); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, deployErr
	}

	termGW.SetSyncedCondition(corev1.ConditionTrue, "DeploymentReady", "Deployment ready")
	if err := r.UpdateStatus(ctx, termGW); err != nil {
		return ctrl.Result{}, err
	}

	return result, nil
}

func (r *TerminatingGatewayController) Logger(name types.NamespacedName) logr.Logger {
	return r.Log.WithValues("request", name)
}

func (r *TerminatingGatewayController) UpdateStatus(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	desired, ok := obj.(*consulv1alpha1.TerminatingGateway)
	if !ok {
		return fmt.Errorf("expected *consulv1alpha1.TerminatingGateway, got %T", obj)
	}

	key := types.NamespacedName{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &consulv1alpha1.TerminatingGateway{}
		if err := r.Client.Get(ctx, key, latest); err != nil {
			return err
		}

		mergeTerminatingGatewayStatus(&latest.Status, desired.Status)

		return r.Status().Update(ctx, latest, opts...)
	})
}

func mergeTerminatingGatewayStatus(dst *consulv1alpha1.Status, src consulv1alpha1.Status) {
	if src.LastSyncedTime != nil {
		t := *src.LastSyncedTime
		dst.LastSyncedTime = &t
	}

	for _, cond := range src.Conditions {
		updated := false
		for i := range dst.Conditions {
			if dst.Conditions[i].Type == cond.Type {
				dst.Conditions[i] = cond
				updated = true
				break
			}
		}
		if !updated {
			dst.Conditions = append(dst.Conditions, cond)
		}
	}
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
	termGW.SetACLStatusCondition(corev1.ConditionFalse, consulv1alpha1.TerminatingGatewayFailedToSetACLs, err.Error())
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

	gatewayName := defaultIfEmpty(termGW.Spec.Deployment.GatewayName, termGW.Name)

	roles, _, err := connMgrClient.ACL().RoleList(nil)
	if err != nil {
		return err
	}

	terminatingGatewayRoleID := ""
	for _, role := range roles {
		// terminating gateway roles are of the form <consul.fullname>-<gatewayName>-acl-role
		if strings.HasSuffix(role.Name, fmt.Sprintf("%s-acl-role", gatewayName)) {
			terminatingGatewayRoleID = role.ID
			break
		}
	}

	if terminatingGatewayRoleID == "" {
		return fmt.Errorf("terminating gateway role not found for gateway %q", gatewayName)
	}

	terminatingGatewayRole, _, err := connMgrClient.ACL().RoleRead(terminatingGatewayRoleID, nil)
	if err != nil {
		return err
	}

	var terminatingGatewayPolicy *capi.ACLRolePolicyLink

	for _, policy := range terminatingGatewayRole.Policies {
		// terminating gateway policy is always of the form <gatewayName>-policy
		if policy.Name == fmt.Sprintf("%s-policy", gatewayName) {
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

	if terminatingGatewayPolicy != nil {
		termGWPoliciesToKeep = append(termGWPoliciesToKeep, terminatingGatewayPolicy)
	}

	terminatingGatewayRole.Policies = termGWPoliciesToKeep

	_, _, err = connMgrClient.ACL().RoleUpdate(terminatingGatewayRole, nil)
	if err != nil {
		return err
	}

	err = r.conditionallyDeletePolicies(log, connMgrClient, termGWPoliciesToRemove, gatewayName)
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

func defaultIfEmpty(s string, defaultVal ...string) string {
	if s != "" {
		return s
	}
	if len(defaultVal) > 0 {
		return defaultVal[0]
	}
	return "default"
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

func (r *TerminatingGatewayController) constructDeploymentFromCRD(
	termGW *consulv1alpha1.TerminatingGateway,
	helmConfigValues *helmvalues.HelmValues,
) (*appsv1.Deployment, error) {
	if termGW == nil {
		return nil, fmt.Errorf("terminating gateway is nil")
	}
	if helmConfigValues == nil {
		return nil, fmt.Errorf("helm values are nil")
	}

	fullName := helmvalues.ConsulFullName(helmConfigValues)
	gatewayName := defaultIfEmpty(termGW.Spec.Deployment.GatewayName, termGW.Name)
	if gatewayName == "" {
		return nil, fmt.Errorf("spec.deployment.gatewayName must be set")
	}

	if helmConfigValues.ExternalServers.Enabled && len(helmConfigValues.ExternalServers.Hosts) == 0 {
		return nil, fmt.Errorf("externalServers.enabled is true but no hosts are configured")
	}

	terminatingGatewayServiceAccountName := fmt.Sprintf("%s-%s", fullName, gatewayName)

	baseLabels := map[string]string{
		"app":                      helmvalues.ConsulName(helmConfigValues),
		"chart":                    helmvalues.ConsulChart(),
		"heritage":                 helmConfigValues.Release.Service,
		"release":                  helmConfigValues.Release.Name,
		"component":                "terminating-gateway",
		"terminating-gateway-name": terminatingGatewayServiceAccountName,
		"consul.hashicorp.com/connect-inject-managed-by": "consul-k8s-endpoints-controller",
	}

	labels := make(map[string]string, len(baseLabels)+len(helmConfigValues.Global.ExtraLabels))
	for k, v := range baseLabels {
		labels[k] = v
	}
	for k, v := range helmConfigValues.Global.ExtraLabels {
		labels[k] = v
	}

	replicas := int32(helmConfigValues.TerminatingGateways.Defaults.Replicas)
	if termGW.Spec.Deployment.Replicas != nil {
		replicas = *termGW.Spec.Deployment.Replicas
	}

	consulNamespace := defaultIfEmpty(
		termGW.Spec.Deployment.ConsulNamespace,
		helmConfigValues.TerminatingGateways.Defaults.ConsulNamespace,
	)

	logLevel := termGW.Spec.Deployment.LogLevel
	if logLevel == "" {
		if helmConfigValues.TerminatingGateways.LogLevel != "" {
			logLevel = helmConfigValues.TerminatingGateways.LogLevel
		} else {
			logLevel = helmConfigValues.Global.LogLevel
		}
	}
	logJSON := ptr.Deref(termGW.Spec.Deployment.LogJSON, helmConfigValues.Global.LogJSON)
	imagePullPolicy := getImagePullPolicy(helmConfigValues.Global.ImagePullPolicy)

	setOrAppendEnv := func(envs []corev1.EnvVar, name, value string) []corev1.EnvVar {
		for i := range envs {
			if envs[i].Name == name {
				envs[i].Value = value
				envs[i].ValueFrom = nil
				return envs
			}
		}
		return append(envs, corev1.EnvVar{Name: name, Value: value})
	}

	volumes := []corev1.Volume{
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "consul-service",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	if helmConfigValues.Global.TLS.Enabled &&
		!(helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.UseSystemRoots) &&
		!helmConfigValues.Global.SecretsBackend.Vault.Enabled {
		secretName := helmConfigValues.Global.TLS.CACert.SecretName
		if secretName == "" {
			secretName = fmt.Sprintf("%s-ca-cert", fullName)
		}
		secretKey := defaultIfEmpty(helmConfigValues.Global.TLS.CACert.SecretKey, "tls.crt")

		volumes = append(volumes, corev1.Volume{
			Name: "consul-ca-cert",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items: []corev1.KeyToPath{
						{
							Key:  secretKey,
							Path: "tls.crt",
						},
					},
				},
			},
		})
	}

	mainVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "tmp",
			MountPath: "/tmp",
		},
		{
			Name:      "consul-service",
			MountPath: "/consul/service",
			ReadOnly:  true,
		},
	}

	initVolumeMounts := []corev1.VolumeMount{
		{
			Name:      "tmp",
			MountPath: "/tmp",
		},
		{
			Name:      "consul-service",
			MountPath: "/consul/service",
		},
	}

	if helmConfigValues.Global.TLS.Enabled &&
		!(helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.UseSystemRoots) &&
		!helmConfigValues.Global.SecretsBackend.Vault.Enabled {
		caMount := corev1.VolumeMount{
			Name:      "consul-ca-cert",
			MountPath: "/consul/tls/ca",
			ReadOnly:  true,
		}
		initVolumeMounts = append(initVolumeMounts, caMount)
		mainVolumeMounts = append(mainVolumeMounts, caMount)
	}

	for _, vol := range termGW.Spec.Deployment.ExtraVolumes {
		volumeName := fmt.Sprintf("userconfig-%s", vol.Name)

		volume := corev1.Volume{Name: volumeName}
		switch vol.Type {
		case "configMap":
			volume.VolumeSource = corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: vol.Name},
				},
			}
			if len(vol.Items) > 0 {
				items := make([]corev1.KeyToPath, len(vol.Items))
				for i, item := range vol.Items {
					items[i] = corev1.KeyToPath{
						Key:  item.Key,
						Path: item.Path,
					}
				}
				volume.ConfigMap.Items = items
			}
		case "secret":
			volume.VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: vol.Name,
				},
			}
			if len(vol.Items) > 0 {
				items := make([]corev1.KeyToPath, len(vol.Items))
				for i, item := range vol.Items {
					items[i] = corev1.KeyToPath{
						Key:  item.Key,
						Path: item.Path,
					}
				}
				volume.Secret.Items = items
			}
		default:
			return nil, fmt.Errorf("unsupported extra volume type %q for volume %q", vol.Type, vol.Name)
		}

		volumes = append(volumes, volume)
		mainVolumeMounts = append(mainVolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: fmt.Sprintf("/consul/userconfig/%s", vol.Name),
			ReadOnly:  true,
		})
	}

	initEnv := append([]corev1.EnvVar{
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
	}, r.consulK8sConsulServerEnvVars(helmConfigValues)...)

	if !helmConfigValues.ExternalServers.Enabled {
		initEnv = setOrAppendEnv(
			initEnv,
			"CONSUL_ADDRESSES",
			fmt.Sprintf("%s-server.%s.svc", fullName, helmConfigValues.Release.Namespace),
		)
	}
	if helmConfigValues.Global.EnableConsulNamespaces {
		initEnv = setOrAppendEnv(initEnv, "CONSUL_NAMESPACE", consulNamespace)
	}
	if helmConfigValues.Global.ACLs.ManageSystemACLs {
		initEnv = setOrAppendEnv(
			initEnv,
			"CONSUL_LOGIN_AUTH_METHOD",
			fmt.Sprintf("%s-k8s-component-auth-method", fullName),
		)
	}

	initContainer := corev1.Container{
		Name:            "terminating-gateway-init",
		Image:           helmConfigValues.Global.ImageK8S,
		ImagePullPolicy: imagePullPolicy,
		SecurityContext: restrictedSecurityContext(helmConfigValues),
		Env:             initEnv,
		Command:         []string{"/bin/sh", "-ec"},
		Args: []string{
			fmt.Sprintf(`exec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${NAMESPACE} \
    -gateway-kind="terminating-gateway" \
    -proxy-id-file=/consul/service/proxy-id \
    -service-name=%s \
    -log-level=%s \
    -log-json=%v`, gatewayName, logLevel, logJSON),
		},
		VolumeMounts: initVolumeMounts,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("50Mi"),
				corev1.ResourceCPU:    resource.MustParse("50m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("50Mi"),
				corev1.ResourceCPU:    resource.MustParse("50m"),
			},
		},
	}

	dataplaneArgs := []string{}
	if helmConfigValues.ExternalServers.Enabled {
		dataplaneArgs = append(dataplaneArgs,
			fmt.Sprintf("-addresses=%s", helmConfigValues.ExternalServers.Hosts[0]),
			fmt.Sprintf("-grpc-port=%d", helmConfigValues.ExternalServers.GRPCPort),
		)
	} else {
		dataplaneArgs = append(dataplaneArgs,
			fmt.Sprintf("-addresses=%s-server.%s.svc", fullName, helmConfigValues.Release.Namespace),
			"-grpc-port=8502",
		)
	}

	dataplaneArgs = append(dataplaneArgs, "-proxy-service-id-path=/consul/service/proxy-id")

	if helmConfigValues.Global.EnableConsulNamespaces {
		dataplaneArgs = append(dataplaneArgs, fmt.Sprintf("-service-namespace=%s", consulNamespace))
	}

	if helmConfigValues.Global.TLS.Enabled {
		if !(helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.UseSystemRoots) {
			if helmConfigValues.Global.SecretsBackend.Vault.Enabled {
				dataplaneArgs = append(dataplaneArgs, "-ca-certs=/vault/secrets/serverca.crt")
			} else {
				dataplaneArgs = append(dataplaneArgs, "-ca-certs=/consul/tls/ca/tls.crt")
			}
		}
		if helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.TLSServerName != "" {
			dataplaneArgs = append(dataplaneArgs, fmt.Sprintf("-tls-server-name=%s", helmConfigValues.ExternalServers.TLSServerName))
		}
	} else {
		dataplaneArgs = append(dataplaneArgs, "-tls-disabled")
	}

	if helmConfigValues.Global.ACLs.ManageSystemACLs {
		dataplaneArgs = append(dataplaneArgs,
			"-credential-type=login",
			"-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token",
			fmt.Sprintf("-login-auth-method=%s-k8s-component-auth-method", fullName),
		)
		if helmConfigValues.Global.AdminPartitions.Enabled {
			dataplaneArgs = append(dataplaneArgs, fmt.Sprintf("-login-partition=%s", helmConfigValues.Global.AdminPartitions.Name))
		}
	}

	if helmConfigValues.Global.AdminPartitions.Enabled {
		dataplaneArgs = append(dataplaneArgs, fmt.Sprintf("-service-partition=%s", helmConfigValues.Global.AdminPartitions.Name))
	}

	dataplaneArgs = append(dataplaneArgs,
		fmt.Sprintf("-log-level=%s", logLevel),
		fmt.Sprintf("-log-json=%v", logJSON),
	)

	if helmConfigValues.Global.Metrics.Enabled && helmConfigValues.Global.Metrics.EnableGatewayMetrics {
		dataplaneArgs = append(dataplaneArgs, "-telemetry-prom-scrape-path=/metrics")
	}

	if helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.SkipServerWatch {
		dataplaneArgs = append(dataplaneArgs, "-server-watch-disabled=true")
	}

	dataplaneArgs = append(dataplaneArgs,
		"-envoy-admin-bind-address=127.0.0.1",
		"-xds-bind-addr=127.0.0.1",
		"-graceful-addr=127.0.0.1",
		"-consul-dns-bind-addr=127.0.0.1",
	)

	mainContainer := corev1.Container{
		Name:            "terminating-gateway",
		Image:           helmConfigValues.Global.ImageConsulDataplane,
		ImagePullPolicy: imagePullPolicy,
		SecurityContext: restrictedSecurityContext(helmConfigValues),
		Command:         []string{"consul-dataplane"},
		Args:            dataplaneArgs,
		Resources:       termGW.Spec.Deployment.Resources,
		VolumeMounts:    mainVolumeMounts,
		Ports: []corev1.ContainerPort{
			{Name: "gateway", ContainerPort: 8443},
		},
		Env: []corev1.EnvVar{
			{
				Name: "NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
				},
			},
			{
				Name:  "DP_CREDENTIAL_LOGIN_META1",
				Value: "pod=$(NAMESPACE)/$(POD_NAME)",
			},
			{
				Name:  "DP_CREDENTIAL_LOGIN_META2",
				Value: "component=terminating-gateway",
			},
			{
				Name:  "DP_SERVICE_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
			},
			{
				Name: "DP_ENVOY_READY_BIND_ADDRESS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8443)},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(8443)},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
	}

	podSpec := corev1.PodSpec{
		TerminationGracePeriodSeconds: int64Ptr(10),
		ServiceAccountName:            terminatingGatewayServiceAccountName,
		Affinity:                      termGW.Spec.Deployment.Affinity,
		Tolerations:                   termGW.Spec.Deployment.Tolerations,
		TopologySpreadConstraints:     termGW.Spec.Deployment.TopologySpreadConstraints,
		NodeSelector:                  termGW.Spec.Deployment.NodeSelector,
		PriorityClassName:             termGW.Spec.Deployment.PriorityClassName,
		Volumes:                       volumes,
		InitContainers:                []corev1.Container{initContainer},
		Containers:                    []corev1.Container{mainContainer},
	}

	annotations := map[string]string{
		"consul.hashicorp.com/connect-inject":              "false",
		"consul.hashicorp.com/mesh-inject":                 "false",
		"consul.hashicorp.com/gateway-kind":                "terminating-gateway",
		"consul.hashicorp.com/gateway-consul-service-name": gatewayName,
	}

	if helmConfigValues.Global.EnableConsulNamespaces && consulNamespace != "" {
		annotations["consul.hashicorp.com/gateway-namespace"] = consulNamespace
	}

	if helmConfigValues.Global.SecretsBackend.Vault.Enabled {
		annotations["vault.hashicorp.com/agent-init-first"] = "true"
		annotations["vault.hashicorp.com/agent-inject"] = "true"
		annotations["vault.hashicorp.com/role"] = helmConfigValues.Global.SecretsBackend.Vault.ConsulCARole
		annotations["vault.hashicorp.com/agent-inject-secret-serverca.crt"] = helmConfigValues.Global.TLS.CACert.SecretName
		annotations["vault.hashicorp.com/agent-inject-template-serverca.crt"] = consulServerTLSCATemplate(helmConfigValues.Global.TLS.CACert.SecretName)

		if helmConfigValues.Global.SecretsBackend.Vault.CA.SecretName != "" && helmConfigValues.Global.SecretsBackend.Vault.CA.SecretKey != "" {
			annotations["vault.hashicorp.com/agent-extra-secret"] = helmConfigValues.Global.SecretsBackend.Vault.CA.SecretName
			annotations["vault.hashicorp.com/ca-cert"] = fmt.Sprintf("/vault/custom/%s", helmConfigValues.Global.SecretsBackend.Vault.CA.SecretKey)
		}
		for k, v := range helmConfigValues.Global.SecretsBackend.Vault.AgentAnnotations {
			annotations[k] = v
		}
	}

	if helmConfigValues.Global.Metrics.Enabled && helmConfigValues.Global.Metrics.EnableGatewayMetrics {
		annotations["prometheus.io/scrape"] = "true"
		if _, exists := annotations["prometheus.io/path"]; !exists {
			annotations["prometheus.io/path"] = "/metrics"
		}
		annotations["prometheus.io/port"] = "20200"
	}

	for k, v := range helmConfigValues.TerminatingGateways.Defaults.Annotations {
		annotations[k] = v
	}
	for k, v := range termGW.Spec.Deployment.Annotations {
		annotations[k] = v
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      terminatingGatewayServiceAccountName,
			Namespace: helmConfigValues.Release.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: baseLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: podSpec,
			},
		},
	}

	if err := ctrl.SetControllerReference(termGW, deployment, r.Scheme); err != nil {
		return nil, fmt.Errorf("set controller reference on deployment %s/%s: %w",
			deployment.Namespace, deployment.Name, err)
	}

	return deployment, nil
}

func (r *TerminatingGatewayController) constructServiceFromCRD(
	termGW *consulv1alpha1.TerminatingGateway,
	helmConfigValues *helmvalues.HelmValues,
) (*corev1.Service, error) {
	if termGW == nil {
		return nil, fmt.Errorf("terminating gateway is nil")
	}
	if helmConfigValues == nil {
		return nil, fmt.Errorf("helm values are nil")
	}

	fullName := helmvalues.ConsulFullName(helmConfigValues)
	gatewayName := defaultIfEmpty(termGW.Spec.Deployment.GatewayName, termGW.Name)
	if gatewayName == "" {
		return nil, fmt.Errorf("spec.deployment.gatewayName must be set")
	}

	serviceName := fmt.Sprintf("%s-%s", fullName, gatewayName)

	labels := map[string]string{
		"app":                      helmvalues.ConsulName(helmConfigValues),
		"chart":                    helmvalues.ConsulChart(),
		"heritage":                 helmConfigValues.Release.Service,
		"release":                  helmConfigValues.Release.Name,
		"component":                "terminating-gateway",
		"terminating-gateway-name": serviceName,
	}
	for k, v := range helmConfigValues.Global.ExtraLabels {
		labels[k] = v
	}

	// Use a narrow selector so each Service selects only its own gateway pod.
	selector := map[string]string{
		"app":                      helmvalues.ConsulName(helmConfigValues),
		"release":                  helmConfigValues.Release.Name,
		"component":                "terminating-gateway",
		"terminating-gateway-name": serviceName,
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: helmConfigValues.Release.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt32(8443),
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(termGW, svc, r.Scheme); err != nil {
		return nil, fmt.Errorf("set controller reference on service %s/%s: %w",
			svc.Namespace, svc.Name, err)
	}

	return svc, nil
}

func (r *TerminatingGatewayController) deployTerminatingGatewayDeployment(
	ctx context.Context,
	log logr.Logger,
	termGW *consulv1alpha1.TerminatingGateway,
	helmValues *helmvalues.HelmValues,
) error {
	if termGW.Spec.Deployment.EnableDeployment == nil || !*termGW.Spec.Deployment.EnableDeployment {
		log.Info("terminating gateway deployment is disabled, returning")
		return nil
	}

	if termGW.Namespace != helmValues.Release.Namespace {
		return fmt.Errorf(
			"terminatinggateway %q is in namespace %q, but Consul Helm release namespace is %q; the TerminatingGateway resource must be created in the same namespace as the Consul installation",
			termGW.Name,
			termGW.Namespace,
			helmValues.Release.Namespace,
		)
	}

	deployment, err := r.constructDeploymentFromCRD(termGW, helmValues)
	if err != nil {
		return fmt.Errorf("construct deployment from CRD: %w", err)
	}

	// Ensure the referenced ServiceAccount exists before the Deployment is created/updated.
	if saName := deployment.Spec.Template.Spec.ServiceAccountName; saName != "" {
		desiredSA := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:        saName,
				Namespace:   deployment.Namespace,
				Labels:      deployment.Labels,
				Annotations: termGW.Spec.Deployment.ServiceAccount.Annotations,
			},
		}

		if err := ctrl.SetControllerReference(termGW, desiredSA, r.Scheme); err != nil {
			return fmt.Errorf("set controller reference on serviceaccount %s/%s: %w",
				desiredSA.Namespace, desiredSA.Name, err)
		}

		existingSA := &corev1.ServiceAccount{}
		err := r.Client.Get(ctx, client.ObjectKey{Name: desiredSA.Name, Namespace: desiredSA.Namespace}, existingSA)
		switch {
		case k8serrors.IsNotFound(err):
			log.Info("Creating terminating gateway service account", "serviceAccount", desiredSA.Name)
			if err := r.Client.Create(ctx, desiredSA); err != nil {
				return fmt.Errorf("create serviceaccount %s/%s: %w", desiredSA.Namespace, desiredSA.Name, err)
			}
		case err != nil:
			return fmt.Errorf("get serviceaccount %s/%s: %w", desiredSA.Namespace, desiredSA.Name, err)
		default:
			existingSA.Labels = desiredSA.Labels
			existingSA.Annotations = desiredSA.Annotations
			if err := ctrl.SetControllerReference(termGW, existingSA, r.Scheme); err != nil {
				return fmt.Errorf("set controller reference on existing serviceaccount %s/%s: %w",
					existingSA.Namespace, existingSA.Name, err)
			}
			log.Info("Updating terminating gateway service account", "serviceAccount", existingSA.Name)
			if err := r.Client.Update(ctx, existingSA); err != nil {
				return fmt.Errorf("update serviceaccount %s/%s: %w", existingSA.Namespace, existingSA.Name, err)
			}
		}
	}

	serviceObj, err := r.constructServiceFromCRD(termGW, helmValues)
	if err != nil {
		return fmt.Errorf("construct service from CRD: %w", err)
	}

	existingService := &corev1.Service{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: serviceObj.Name, Namespace: serviceObj.Namespace}, existingService)
	switch {
	case err != nil && !k8serrors.IsNotFound(err):
		return fmt.Errorf("get service %s/%s: %w", serviceObj.Namespace, serviceObj.Name, err)
	case k8serrors.IsNotFound(err):
		log.Info("Creating terminating gateway service", "service", serviceObj.Name)
		if err := r.Client.Create(ctx, serviceObj); err != nil {
			return fmt.Errorf("create service %s/%s: %w", serviceObj.Namespace, serviceObj.Name, err)
		}
	default:
		log.Info("Updating terminating gateway service", "service", serviceObj.Name)

		// Preserve cluster-assigned fields.
		serviceObj.ResourceVersion = existingService.ResourceVersion
		serviceObj.Spec.ClusterIP = existingService.Spec.ClusterIP
		serviceObj.Spec.ClusterIPs = existingService.Spec.ClusterIPs
		serviceObj.Spec.IPFamilies = existingService.Spec.IPFamilies
		serviceObj.Spec.IPFamilyPolicy = existingService.Spec.IPFamilyPolicy
		serviceObj.Spec.InternalTrafficPolicy = existingService.Spec.InternalTrafficPolicy

		if err := r.Client.Update(ctx, serviceObj); err != nil {
			return fmt.Errorf("update service %s/%s: %w", serviceObj.Namespace, serviceObj.Name, err)
		}
	}

	deploymentSpecBytes, printErr := json.MarshalIndent(deployment.Spec, "", "  ")
	if printErr != nil {
		log.Error(printErr, "failed to marshal deployment spec for logging")
	} else {
		log.Info("Constructed Deployment spec", "spec", string(deploymentSpecBytes))
	}

	existingDeployment := &appsv1.Deployment{}
	err = r.Client.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment)

	switch {
	case err != nil && !k8serrors.IsNotFound(err):
		return fmt.Errorf("get deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
	case k8serrors.IsNotFound(err):
		log.Info("Creating terminating gateway deployment", "deployment", deployment.Name)
		if err := r.Client.Create(ctx, deployment); err != nil {
			return fmt.Errorf("create deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
		}
	default:
		log.Info("Updating terminating gateway deployment", "deployment", deployment.Name)
		deployment.ResourceVersion = existingDeployment.ResourceVersion
		if err := r.Client.Update(ctx, deployment); err != nil {
			return fmt.Errorf("update deployment %s/%s: %w", deployment.Namespace, deployment.Name, err)
		}
	}

	return nil
}

// int64Ptr returns a pointer to an int64 value.
func int64Ptr(v int64) *int64 {
	return &v
}

// consulServerTLSCATemplate mirrors Helm helper `consul.serverTLSCATemplate` (helpers.tpl):
// It generates a Vault agent template string for fetching the server TLS CA certificate.
func consulServerTLSCATemplate(secretName string) string {
	return fmt.Sprintf(` |
            {{- with secret "%s" -}}
            {{- .Data.certificate -}}
            {{- end -}}`, secretName)
}

// consulK8sConsulServerEnvVars returns the environment variables for Consul server connection
// matching the consul.consulK8sConsulServerEnvVars Helm template helper.
func (r *TerminatingGatewayController) consulK8sConsulServerEnvVars(helmConfigValues *helmvalues.HelmValues) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name: "CONSUL_ADDRESSES",
			Value: func() string {
				if helmConfigValues.ExternalServers.Enabled {
					if len(helmConfigValues.ExternalServers.Hosts) > 0 {
						return helmConfigValues.ExternalServers.Hosts[0]
					}
					return ""
				}
				return fmt.Sprintf("%s-server.%s.svc", helmConfigValues.Global.Name, r.ReleaseNamespace)
			}(),
		},
		{
			Name: "CONSUL_GRPC_PORT",
			Value: func() string {
				if helmConfigValues.ExternalServers.Enabled {
					return fmt.Sprintf("%d", helmConfigValues.ExternalServers.GRPCPort)
				}
				return "8502"
			}(),
		},
		{
			Name: "CONSUL_HTTP_PORT",
			Value: func() string {
				if helmConfigValues.ExternalServers.Enabled {
					return fmt.Sprintf("%d", helmConfigValues.ExternalServers.HTTPSPort)
				}
				if helmConfigValues.Global.TLS.Enabled {
					return "8501"
				}
				return "8500"
			}(),
		},
		{
			Name:  "CONSUL_DATACENTER",
			Value: helmConfigValues.Global.Datacenter,
		},
		{
			Name:  "CONSUL_API_TIMEOUT",
			Value: helmConfigValues.Global.ConsulAPITimeout,
		},
	}

	// Admin partitions
	if helmConfigValues.Global.AdminPartitions.Enabled {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "CONSUL_PARTITION",
			Value: helmConfigValues.Global.AdminPartitions.Name,
		})
		if helmConfigValues.Global.ACLs.ManageSystemACLs {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "CONSUL_LOGIN_PARTITION",
				Value: helmConfigValues.Global.AdminPartitions.Name,
			})
		}
	}

	// TLS configuration
	if helmConfigValues.Global.TLS.Enabled {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "CONSUL_USE_TLS",
			Value: "true",
		})
		if !(helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.UseSystemRoots) {
			caCertFile := "/consul/tls/ca/tls.crt"
			if helmConfigValues.Global.SecretsBackend.Vault.Enabled {
				caCertFile = "/vault/secrets/serverca.crt"
			}
			envVars = append(envVars, corev1.EnvVar{
				Name:  "CONSUL_CACERT_FILE",
				Value: caCertFile,
			})
		}
		if helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.TLSServerName != "" {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "CONSUL_TLS_SERVER_NAME",
				Value: helmConfigValues.ExternalServers.TLSServerName,
			})
		}
	}

	// Skip server watch
	if helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.SkipServerWatch {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "CONSUL_SKIP_SERVER_WATCH",
			Value: "true",
		})
	}

	// Consul namespace
	if helmConfigValues.Global.EnableConsulNamespaces {
		consulNamespace := helmConfigValues.TerminatingGateways.Defaults.ConsulNamespace
		if consulNamespace == "" {
			consulNamespace = "default"
		}
		envVars = append(envVars, corev1.EnvVar{
			Name:  "CONSUL_NAMESPACE",
			Value: consulNamespace,
		})
	}

	// ACL configuration
	if helmConfigValues.Global.ACLs.ManageSystemACLs {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_AUTH_METHOD",
				Value: fmt.Sprintf("%s-k8s-component-auth-method", helmvalues.ConsulFullName(helmConfigValues)),
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_DATACENTER",
				Value: helmConfigValues.Global.Datacenter,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_META",
				Value: "component=terminating-gateway,pod=$(NAMESPACE)/$(POD_NAME)",
			},
		)
	}

	// Node name
	envVars = append(envVars, corev1.EnvVar{
		Name:  "CONSUL_NODE_NAME",
		Value: "$(NODE_NAME)-virtual",
	})

	return envVars
}

// getImagePullPolicy converts the helm imagePullPolicy string to corev1.PullPolicy
// Valid values are: IfNotPresent, Always, Never, or empty.
func getImagePullPolicy(policy string) corev1.PullPolicy {
	switch policy {
	case "IfNotPresent":
		return corev1.PullIfNotPresent
	case "Always":
		return corev1.PullAlways
	case "Never":
		return corev1.PullNever
	default:
		return "" // Empty lets Kubernetes use default behavior
	}
}

// restrictedSecurityContext matches charts/consul/templates/_helpers.tpl:consul.restrictedSecurityContext.
func restrictedSecurityContext(helmConfigValues *helmvalues.HelmValues) *corev1.SecurityContext {
	// Helm: {{- if not .Values.global.enablePodSecurityPolicies -}}
	// If PSPs are enabled, the helper emits nothing.
	if helmConfigValues.Global.EnablePodSecurityPolicies {
		return nil
	}

	sc := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}

	// Helm: {{- if not .Values.global.openshift.enabled -}} runAsUser: 100 {{- end -}}
	if !helmConfigValues.Global.OpenShiftEnabled {
		sc.RunAsUser = ptr.To(int64(100))
	}

	return sc
}

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
	gatewayName := defaultIfEmpty(termGW.Spec.Deployment.GatewayName, termGW.Name)
	serviceAccountName := fmt.Sprintf("%s-%s", fullName, gatewayName)
	authMethodName := fmt.Sprintf("%s-k8s-component-auth-method", fullName)

	consulNamespace := defaultIfEmpty(
		termGW.Spec.Deployment.ConsulNamespace,
		helmValues.TerminatingGateways.Defaults.ConsulNamespace,
	)

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
		Selector:    fmt.Sprintf("serviceaccount.name==%q", serviceAccountName),
		BindType:    capi.BindingRuleBindTypeRole,
		BindName:    roleName,
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
