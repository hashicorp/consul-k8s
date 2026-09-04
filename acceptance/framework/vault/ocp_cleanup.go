package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/helm"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (v *VaultCluster) uninstallReleaseNoHooks(t *testing.T, releaseName string) error {
	_, err := helm.RunHelmCommandAndGetOutputE(t, v.helmOptions,
		"uninstall", releaseName,
		"--no-hooks",
		"--timeout", "30s",
	)
	return err
}

func (v *VaultCluster) cleanupOpenShiftBeforeInstall(t *testing.T) {
	t.Helper()

	namespace := v.helmOptions.KubectlOptions.Namespace
	v.logger.Logf(t, "Cleaning stale Vault resources before install in OpenShift namespace %s", namespace)
	v.cleanupBrokenInjectorWebhooks(t, namespace)
	v.cleanupStaleVaultReleases(t)
}

func (v *VaultCluster) cleanupBrokenInjectorWebhooks(t *testing.T, namespace string) {
	t.Helper()

	client := v.kubernetesAPIClient(t)
	webhookConfigs, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		v.logger.Logf(t, "Unable to list mutating webhook configurations during stale webhook cleanup: %v", err)
		return
	}

	for _, cfg := range webhookConfigs.Items {
		if !strings.Contains(cfg.Name, "consul-connect-injector") {
			continue
		}

		shouldDelete := false
		for _, wh := range cfg.Webhooks {
			if wh.ClientConfig.Service == nil {
				continue
			}
			if wh.ClientConfig.Service.Namespace != namespace {
				continue
			}

			serviceName := wh.ClientConfig.Service.Name
			_, svcErr := client.CoreV1().Services(namespace).Get(context.Background(), serviceName, metav1.GetOptions{})
			if k8serrors.IsNotFound(svcErr) {
				v.logger.Logf(t, "Deleting stale mutating webhook configuration %s because target service %s/%s does not exist", cfg.Name, namespace, serviceName)
				shouldDelete = true
				break
			}
			if svcErr != nil {
				v.logger.Logf(t, "Unable to validate webhook target service %s/%s for configuration %s: %v", namespace, serviceName, cfg.Name, svcErr)
			}
		}

		if !shouldDelete {
			continue
		}

		delErr := client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(context.Background(), cfg.Name, metav1.DeleteOptions{})
		if delErr != nil && !k8serrors.IsNotFound(delErr) {
			require.NoError(t, delErr)
		}
	}
}

func (v *VaultCluster) releaseLabelSelectorFor(releaseName string) string {
	return fmt.Sprintf("%s=%s", releaseLabel, releaseName)
}

func (v *VaultCluster) cleanupOpenShiftResourcesByRelease(t *testing.T, releaseName string) {
	t.Helper()

	client := v.kubernetesAPIClient(t)
	selector := v.releaseLabelSelectorFor(releaseName)
	listOptions := metav1.ListOptions{LabelSelector: selector}

	deleteList := func(err error) {
		if err != nil && !k8serrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	}

	var gracePeriod int64 = 0

	pods, err := client.CoreV1().Pods("").List(context.Background(), listOptions)
	deleteList(err)
	for _, pod := range pods.Items {
		err := client.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
		deleteList(err)
	}

	statefulSets, err := client.AppsV1().StatefulSets("").List(context.Background(), listOptions)
	deleteList(err)
	for _, statefulSet := range statefulSets.Items {
		err := client.AppsV1().StatefulSets(statefulSet.Namespace).Delete(context.Background(), statefulSet.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	deployments, err := client.AppsV1().Deployments("").List(context.Background(), listOptions)
	deleteList(err)
	for _, deployment := range deployments.Items {
		err := client.AppsV1().Deployments(deployment.Namespace).Delete(context.Background(), deployment.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	replicaSets, err := client.AppsV1().ReplicaSets("").List(context.Background(), listOptions)
	deleteList(err)
	for _, replicaSet := range replicaSets.Items {
		err := client.AppsV1().ReplicaSets(replicaSet.Namespace).Delete(context.Background(), replicaSet.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	persistentVolumeClaims, err := client.CoreV1().PersistentVolumeClaims("").List(context.Background(), listOptions)
	deleteList(err)
	for _, persistentVolumeClaim := range persistentVolumeClaims.Items {
		err := client.CoreV1().PersistentVolumeClaims(persistentVolumeClaim.Namespace).Delete(context.Background(), persistentVolumeClaim.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	serviceAccounts, err := client.CoreV1().ServiceAccounts("").List(context.Background(), listOptions)
	deleteList(err)
	for _, serviceAccount := range serviceAccounts.Items {
		err := client.CoreV1().ServiceAccounts(serviceAccount.Namespace).Delete(context.Background(), serviceAccount.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	roles, err := client.RbacV1().Roles("").List(context.Background(), listOptions)
	deleteList(err)
	for _, role := range roles.Items {
		err := client.RbacV1().Roles(role.Namespace).Delete(context.Background(), role.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	roleBindings, err := client.RbacV1().RoleBindings("").List(context.Background(), listOptions)
	deleteList(err)
	for _, roleBinding := range roleBindings.Items {
		err := client.RbacV1().RoleBindings(roleBinding.Namespace).Delete(context.Background(), roleBinding.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	services, err := client.CoreV1().Services("").List(context.Background(), listOptions)
	deleteList(err)
	for _, service := range services.Items {
		err := client.CoreV1().Services(service.Namespace).Delete(context.Background(), service.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	clusterRoleBindings, err := client.RbacV1().ClusterRoleBindings().List(context.Background(), listOptions)
	deleteList(err)
	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		err := client.RbacV1().ClusterRoleBindings().Delete(context.Background(), clusterRoleBinding.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	clusterRoles, err := client.RbacV1().ClusterRoles().List(context.Background(), listOptions)
	deleteList(err)
	for _, clusterRole := range clusterRoles.Items {
		err := client.RbacV1().ClusterRoles().Delete(context.Background(), clusterRole.Name, metav1.DeleteOptions{})
		deleteList(err)
	}

	for _, secretName := range []string{
		certSecretName(releaseName),
		CASecretName(releaseName),
		fmt.Sprintf("%s-vault-root-token", releaseName),
		fmt.Sprintf("%s-vault-token", releaseName),
	} {
		secrets, err := client.CoreV1().Secrets("").List(context.Background(), metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", secretName)})
		deleteList(err)
		for _, secret := range secrets.Items {
			err := client.CoreV1().Secrets(secret.Namespace).Delete(context.Background(), secret.Name, metav1.DeleteOptions{})
			deleteList(err)
		}
	}
}

func (v *VaultCluster) cleanupStaleVaultReleases(t *testing.T) {
	t.Helper()

	helmListOutput, err := helm.RunHelmCommandAndGetOutputE(t, v.helmOptions, "list", "--output", "json")
	if err != nil {
		v.logger.Logf(t, "Unable to list Helm releases for stale Vault cleanup: %v", err)
		return
	}

	var installedReleases []map[string]string
	if err := json.Unmarshal([]byte(helmListOutput), &installedReleases); err != nil {
		v.logger.Logf(t, "Unable to parse Helm release list for stale Vault cleanup: %v", err)
		return
	}

	for _, release := range installedReleases {
		// if !strings.Contains(release["chart"], "vault-") {
		// 	continue
		// }

		releaseName := release["name"]
		if releaseName == "" {
			continue
		}

		v.logger.Logf(t, "Found stale Vault release %s (chart %s), uninstalling before fresh test install", releaseName, release["chart"])
		if err := v.uninstallReleaseNoHooks(t, releaseName); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "already deleted") {
			v.logger.Logf(t, "Unable to uninstall stale Vault release %s: %v", releaseName, err)
		}
		if v.isOpenShift {
			v.cleanupOpenShiftResourcesByRelease(t, releaseName)
		}
	}
}
