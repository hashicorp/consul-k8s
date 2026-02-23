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
			r.UpdateStatusFailedToSetACLs(ctx, termGW, err)
			return ctrl.Result{}, err
		}

		termGW.SetACLStatusCondition(corev1.ConditionTrue, "", "")
		err = r.UpdateStatus(ctx, termGW)
		if err != nil {
			log.Error(err, "error updating terminating-gateway status")
			return ctrl.Result{}, err
		}
	}

	// Deploy pod based on CRD spec
	if err := r.deployTerminatingGatewayDeployment(ctx, log, termGW, helmValues); err != nil {
		log.Error(err, "error deploying terminating gateway pod")
		termGW.SetSyncedCondition(corev1.ConditionFalse, "FailedToDeployPod", err.Error())
		err := r.UpdateStatus(ctx, termGW)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, err
	}

	termGW.SetSyncedCondition(corev1.ConditionTrue, "DeploymentReady", "Deployment ready")
	err = r.UpdateStatus(ctx, termGW)
	if err != nil {
		return ctrl.Result{}, err
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

func (r *TerminatingGatewayController) constructDeploymentFromCRD(termGW *consulv1alpha1.TerminatingGateway, helmConfigValues *helmvalues.HelmValues) *appsv1.Deployment {

	fullName := helmvalues.ConsulFullName(helmConfigValues)
	terminatingGatewayServiceAccountName := fmt.Sprintf("%s-%s", fullName, termGW.Spec.Deployment.GatewayName)
	baseLabels := map[string]string{
		"app":                      helmvalues.ConsulName(helmConfigValues),
		"chart":                    helmvalues.ConsulChart(),
		"heritage":                 helmConfigValues.Release.Service,
		"release":                  helmConfigValues.Release.Name,
		"component":                "terminating-gateway",
		"terminating-gateway-name": terminatingGatewayServiceAccountName,
	}
	// Add extra labels from helmConfigValues.Global.ExtraLabels
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

	podSpec := corev1.PodSpec{
		TerminationGracePeriodSeconds: int64Ptr(10),
		ServiceAccountName:            terminatingGatewayServiceAccountName,
		Affinity:                      termGW.Spec.Deployment.Affinity,
		Tolerations:                   termGW.Spec.Deployment.Tolerations,
		TopologySpreadConstraints:     termGW.Spec.Deployment.TopologySpreadConstraints,
		NodeSelector:                  termGW.Spec.Deployment.NodeSelector,
		PriorityClassName:             termGW.Spec.Deployment.PriorityClassName,
	}

	// Add volumes
	for _, vol := range termGW.Spec.Deployment.ExtraVolumes {
		volume := corev1.Volume{
			Name: vol.Name,
		}
		if vol.Type == "configMap" {
			volume.VolumeSource = corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: vol.Name,
					},
				},
			}
			if len(vol.Items) > 0 {
				items := make([]corev1.KeyToPath, len(vol.Items))
				for i, item := range vol.Items {
					items[i] = corev1.KeyToPath{Key: item.Key, Path: item.Path}
				}
				volume.ConfigMap.Items = items
			}
		} else if vol.Type == "secret" {
			volume.VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: vol.Name,
				},
			}
			if len(vol.Items) > 0 {
				items := make([]corev1.KeyToPath, len(vol.Items))
				for i, item := range vol.Items {
					items[i] = corev1.KeyToPath{Key: item.Key, Path: item.Path}
				}
				volume.Secret.Items = items
			}
		}
		podSpec.Volumes = append(podSpec.Volumes, volume)
	}

	// Init container for gateway registration
	initContainer := corev1.Container{
		Name:            "terminating-gateway-init",
		Image:           helmConfigValues.Global.ImageK8S,
		SecurityContext: restrictedSecurityContext(helmConfigValues),
		// In constructDeploymentFromCRD, for the init container Env field:
		Env: append([]corev1.EnvVar{
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
		}, r.consulK8sConsulServerEnvVars(helmConfigValues)...),
		Command: []string{"/bin/sh", "-ec"},
		Args: []string{
			fmt.Sprintf(`exec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${NAMESPACE} \
    -gateway-kind="terminating-gateway" \
    -proxy-id-file=/consul/service/proxy-id \
    -service-name=%s \
    -log-level=%s \
    -log-json=%v`, termGW.Spec.Deployment.GatewayName, termGW.Spec.Deployment.LogLevel, *termGW.Spec.Deployment.LogJSON),
		},
		VolumeMounts: func() []corev1.VolumeMount {
			mounts := []corev1.VolumeMount{
				{
					Name:      "tmp",
					MountPath: "/tmp",
				},
				{
					Name:      "consul-service",
					MountPath: "/consul/service",
				},
			}
			// TLS CA cert mount - add if TLS enabled and not using external servers with system roots or Vault
			if helmConfigValues.Global.TLS.Enabled {
				if !(helmConfigValues.ExternalServers.Enabled && helmConfigValues.ExternalServers.UseSystemRoots) && !helmConfigValues.Global.SecretsBackend.Vault.Enabled {
					mounts = append(mounts, corev1.VolumeMount{
						Name:      "consul-ca-cert",
						MountPath: "/consul/tls/ca",
						ReadOnly:  true,
					})
				}
			}
			return mounts
		}(),

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

	podSpec.InitContainers = []corev1.Container{initContainer}

	// Main container
	mainContainer := corev1.Container{
		Name:            "terminating-gateway",
		Image:           helmConfigValues.Global.ImageConsulDataplane,
		Resources:       termGW.Spec.Deployment.Resources,
		ImagePullPolicy: getImagePullPolicy(helmConfigValues.Global.ImagePullPolicy),
		SecurityContext: restrictedSecurityContext(helmConfigValues),
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

	podSpec.Containers = []corev1.Container{mainContainer}

	// inside constructDeploymentFromCRD, before creating the Deployment
	annotations := map[string]string{
		"consul.hashicorp.com/connect-inject":              "false",
		"consul.hashicorp.com/mesh-inject":                 "false",
		"consul.hashicorp.com/gateway-kind":                "terminating-gateway",
		"consul.hashicorp.com/gateway-consul-service-name": termGW.Spec.Deployment.GatewayName,
	}

	// Add gateway-namespace if namespaces are enabled
	if helmConfigValues.Global.EnableConsulNamespaces {
		ns := defaultIfEmpty(termGW.Spec.Deployment.ConsulNamespace, helmConfigValues.TerminatingGateways.Defaults.ConsulNamespace)
		if ns != "" {
			annotations["consul.hashicorp.com/gateway-namespace"] = ns
		}
	}

	// Add Vault annotations if Vault is enabled
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
		// Add agent annotations if present
		for k, v := range helmConfigValues.Global.SecretsBackend.Vault.AgentAnnotations {
			annotations[k] = v
		}
	}

	// Add Prometheus annotations if metrics are enabled
	if helmConfigValues.Global.Metrics.Enabled && helmConfigValues.Global.Metrics.EnableGatewayMetrics {
		annotations["prometheus.io/scrape"] = "true"
		if _, exists := annotations["prometheus.io/path"]; !exists {
			annotations["prometheus.io/path"] = "/metrics"
		}
		annotations["prometheus.io/port"] = "20200"
	}

	// Add default annotations from Helm values
	for k, v := range parseAnnotationsString(helmConfigValues.TerminatingGateways.Defaults.Annotations) {
		annotations[k] = v
	}

	// Add gateway-specific annotations (these override defaults)
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

	err := ctrl.SetControllerReference(termGW, deployment, r.Scheme)
	if err != nil {
		return nil
	}
	return deployment
}

func (r *TerminatingGatewayController) deployTerminatingGatewayDeployment(ctx context.Context, log logr.Logger, termGW *consulv1alpha1.TerminatingGateway, helmValues *helmvalues.HelmValues) error {
	if termGW.Spec.Deployment.Enabled == nil || !*termGW.Spec.Deployment.Enabled {
		return nil
	}

	deployment := r.constructDeploymentFromCRD(termGW, helmValues)
	if deployment == nil {
		return fmt.Errorf("failed to construct deployment for terminating gateway")
	}

	// printing deployment spec for debugging purposes
	deploymentSpecBytes, printErr := json.MarshalIndent(deployment.Spec, "", "  ")
	if printErr != nil {
		log.Error(printErr, "failed to marshal deployment spec for logging")
	} else {
		log.Info("Constructed Deployment spec", "spec", string(deploymentSpecBytes))
	}

	existingDeployment := &appsv1.Deployment{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: deployment.Name, Namespace: deployment.Namespace}, existingDeployment)

	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if k8serrors.IsNotFound(err) {
		log.Info("Creating terminating gateway deployment", "deployment", deployment.Name)
		if err := r.Client.Create(ctx, deployment); err != nil {
			return err
		}
	} else {
		log.Info("Updating terminating gateway deployment", "deployment", deployment.Name)
		deployment.ResourceVersion = existingDeployment.ResourceVersion
		if err := r.Client.Update(ctx, deployment); err != nil {
			return err
		}
	}

	return nil
}

// int64Ptr returns a pointer to an int64 value
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

// parseAnnotationsString parses a YAML-formatted annotation string (format: "'key': value" per line)
// and returns a map of key-value pairs.
func parseAnnotationsString(annotationsStr string) map[string]string {
	result := make(map[string]string)
	if annotationsStr == "" {
		return result
	}

	lines := strings.Split(annotationsStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on first colon
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.Trim(strings.TrimSpace(parts[0]), "'\"")
			value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
			result[key] = value
		}
	}
	return result
}

// consulK8sConsulServerEnvVars returns the environment variables for Consul server connection
// matching the consul.consulK8sConsulServerEnvVars Helm template helper
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
				Value: fmt.Sprintf("%s-k8s-component-auth-method", helmConfigValues.Global.Name),
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
// Valid values are: IfNotPresent, Always, Never, or empty
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

// restrictedSecurityContext matches charts/consul/templates/_helpers.tpl:consul.restrictedSecurityContext
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
