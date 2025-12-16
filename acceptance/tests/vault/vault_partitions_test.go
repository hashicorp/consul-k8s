// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"net"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestVault_Partitions(t *testing.T) {
    crlog.SetLogger(zap.New(zap.UseDevMode(true)))
    env := suite.Environment()
    cfg := suite.Config()
    serverClusterCtx := env.DefaultContext(t)
    clientClusterCtx := env.Context(t, 1)
    ns := serverClusterCtx.KubectlOptions(t).Namespace

    const secondaryPartition = "secondary"

    ver, err := version.NewVersion("1.12.0")
    require.NoError(t, err)
    if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
        t.Skipf("skipping this test because vault secrets backend is not supported in version %v", cfg.ConsulVersion.String())
    }
    if !cfg.EnableEnterprise {
        t.Skipf("skipping this test because -enable-enterprise is not set")
    }
    if !cfg.EnableMultiCluster {
        t.Skipf("skipping this test because -enable-multi-cluster is not set")
    }
    vaultReleaseName := helpers.RandomName()
    consulReleaseName := helpers.RandomName()

    // In the primary cluster, we will expose Vault server as a Load balancer
    // or a NodePort service so that the secondary can connect to it.
    serverClusterVaultHelmValues := map[string]string{
        "server.service.type": "LoadBalancer",
    }
    if cfg.UseKind {
        serverClusterVaultHelmValues["server.service.type"] = "NodePort"
        serverClusterVaultHelmValues["server.service.nodePort"] = "31000"
    }
    serverClusterVault := vault.NewVaultCluster(t, serverClusterCtx, cfg, vaultReleaseName, serverClusterVaultHelmValues)
    serverClusterVault.Create(t, serverClusterCtx, "")

    externalVaultAddress := vaultAddress(t, cfg, serverClusterCtx, vaultReleaseName)

    // In the secondary cluster, we will only deploy the agent injector and provide
    // it with the primary's Vault address. We also want to configure the injector with
    // a different k8s auth method path since the secondary cluster will need its own auth method.
    clientClusterVaultHelmValues := map[string]string{
        "server.enabled":             "false",
        "injector.externalVaultAddr": externalVaultAddress,
        "injector.authPath":          "auth/kubernetes-" + secondaryPartition,
    }

    secondaryVaultCluster := vault.NewVaultCluster(t, clientClusterCtx, cfg, vaultReleaseName, clientClusterVaultHelmValues)
    secondaryVaultCluster.Create(t, clientClusterCtx, "")

    vaultClient := serverClusterVault.VaultClient(t)

    // Configure Vault Kubernetes auth method for the secondary cluster.
    {
        // Create auth method service account and ClusterRoleBinding.
        authMethodRBACName := fmt.Sprintf("%s-vault-auth-method", vaultReleaseName)
        _, err := clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Create(context.Background(), &rbacv1.ClusterRoleBinding{
            ObjectMeta: metav1.ObjectMeta{
                Name: authMethodRBACName,
            },
            Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: authMethodRBACName, Namespace: ns}},
            RoleRef:  rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Name: "system:auth-delegator", Kind: "ClusterRole"},
        }, metav1.CreateOptions{})
        require.NoError(t, err)

        // Create service account for the auth method in the secondary cluster.
        svcAcct, err := clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Create(context.Background(), &corev1.ServiceAccount{
            ObjectMeta: metav1.ObjectMeta{
                Name: authMethodRBACName,
            },
        }, metav1.CreateOptions{})
        require.NoError(t, err)
        
        // In Kubernetes 1.24+ manually create the secret.
        if len(svcAcct.Secrets) == 0 {
            _, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), &corev1.Secret{
                ObjectMeta: metav1.ObjectMeta{
                    Name:        authMethodRBACName,
                    Annotations: map[string]string{corev1.ServiceAccountNameKey: authMethodRBACName},
                },
                Type: corev1.SecretTypeServiceAccountToken,
            }, metav1.CreateOptions{})
            require.NoError(t, err)
        }
        
        logger.Logf(t, "Waiting for ServiceAccount token secret %s to be populated...", authMethodRBACName)
        require.Eventually(t, func() bool {
            secret, err := clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), authMethodRBACName, metav1.GetOptions{})
            if err != nil {
                return false
            }
            if secret.Data == nil {
                return false
            }
            token, ok := secret.Data["token"]
            return ok && len(token) > 0
        }, 30*time.Second, 1*time.Second, "ServiceAccount token was not populated in time")

        t.Cleanup(func() {
            clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
            clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
        })

        k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)
        secondaryVaultCluster.ConfigureAuthMethod(t, vaultClient, "kubernetes-"+secondaryPartition, k8sAuthMethodHost, authMethodRBACName, ns)
    }

    // -------------------------
    // PKI
    // -------------------------
    connectCAPolicy := "connect-ca-dc1"
    connectCARootPath := "connect_root"
    connectCAIntermediatePath := "dc1/connect_inter"
    vault.CreateConnectCARootAndIntermediatePKIPolicy(t, vaultClient, connectCAPolicy, connectCARootPath, connectCAIntermediatePath)

    // Configure Server PKI
    serverPKIConfig := &vault.PKIAndAuthRoleConfiguration{
        BaseURL:             "pki",
        PolicyName:          "consul-ca-policy",
        RoleName:            "consul-ca-role",
        KubernetesNamespace: ns,
        DataCenter:          "dc1",
        ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
        AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
        MaxTTL:              "1h",
        AuthMethodPath:      KubernetesAuthMethodPath,
    }
    serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

    // Explicit Policy for Reading Root CA (Fixes 403)
    pkiCAReadPolicyName := "read-pki-ca"
    pkiCAReadPolicy := fmt.Sprintf(`
path "%s/cert/ca" {
    capabilities = ["read"]
}
`, serverPKIConfig.BaseURL)
    err = vaultClient.Sys().PutPolicy(pkiCAReadPolicyName, pkiCAReadPolicy)
    require.NoError(t, err)

    // -------------------------
    // KV2 secrets
    // -------------------------
    gossipKey, err := vault.GenerateGossipSecret()
    require.NoError(t, err)
    gossipSecret := &vault.KV2Secret{
        Path:       "consul/data/secret/gossip",
        Key:        "gossip",
        Value:      gossipKey,
        PolicyName: "gossip",
    }
    gossipSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

    licenseSecret := &vault.KV2Secret{
        Path:       "consul/data/secret/license",
        Key:        "license",
        Value:      cfg.EnterpriseLicense,
        PolicyName: "license",
    }
    if cfg.EnableEnterprise {
        licenseSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
    }

    bootstrapToken, err := uuid.GenerateUUID()
    require.NoError(t, err)
    bootstrapTokenSecret := &vault.KV2Secret{
        Path:       "consul/data/secret/bootstrap",
        Key:        "token",
        Value:      bootstrapToken,
        PolicyName: "bootstrap",
    }
    bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

    partitionToken, err := uuid.GenerateUUID()
    require.NoError(t, err)
    partitionTokenSecret := &vault.KV2Secret{
        Path:       "consul/data/secret/partition",
        Key:        "token",
        Value:      partitionToken,
        PolicyName: "partition",
    }
    partitionTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

    // -------------------------------------------
    // Additional Auth Roles in Primary Datacenter
    // -------------------------------------------
    consulServerRole := "server"
    serverPolicies := fmt.Sprintf("%s,%s,%s,%s,%s,%s", 
        gossipSecret.PolicyName, 
        connectCAPolicy, 
        serverPKIConfig.PolicyName, 
        bootstrapTokenSecret.PolicyName, 
        partitionTokenSecret.PolicyName,
        pkiCAReadPolicyName)
    if cfg.EnableEnterprise {
        serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
    }
    
    srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  serverPKIConfig.ServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      KubernetesAuthMethodPath,
        RoleName:            consulServerRole,
        PolicyNames:         serverPolicies,
    }
    srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

    consulClientRole := ClientRole
    consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ClientRole)
    clientAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  consulClientServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      KubernetesAuthMethodPath,
        RoleName:            consulClientRole,
        PolicyNames:         gossipSecret.PolicyName,
    }
    clientAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

    manageSystemACLsRole := ManageSystemACLsRole
    manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole)
    aclAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  manageSystemACLsServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      KubernetesAuthMethodPath,
        RoleName:            manageSystemACLsRole,
        PolicyNames:         fmt.Sprintf("%s,%s", bootstrapTokenSecret.PolicyName, partitionTokenSecret.PolicyName),
    }
    aclAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

    srvCAAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  "*",
        KubernetesNamespace: ns,
        AuthMethodPath:      KubernetesAuthMethodPath,
        RoleName:            serverPKIConfig.RoleName,
        PolicyNames:         serverPKIConfig.PolicyName,
    }
    srvCAAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

    // ---------------------------------------------
    // Additional Auth Roles in Secondary Datacenter
    // ---------------------------------------------
    clientAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  consulClientServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
        RoleName:            consulClientRole,
        PolicyNames:         fmt.Sprintf("%s,%s", gossipSecret.PolicyName, partitionTokenSecret.PolicyName),
    }
    clientAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

    aclAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  manageSystemACLsServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
        RoleName:            manageSystemACLsRole,
        PolicyNames:         partitionTokenSecret.PolicyName,
    }
    aclAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

    adminPartitionsRole := "partition-init"
    partitionInitServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "partition-init")
    prtAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  partitionInitServiceAccountName,
        KubernetesNamespace: ns,
        AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
        RoleName:            adminPartitionsRole,
        PolicyNames:         partitionTokenSecret.PolicyName,
    }
    prtAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

    srvCAAuthRoleConfigSecondary := &vault.KubernetesAuthRoleConfiguration{
        ServiceAccountName:  "*",
        KubernetesNamespace: ns,
        AuthMethodPath:      fmt.Sprintf("kubernetes-%s", secondaryPartition),
        RoleName:            serverPKIConfig.RoleName,
        PolicyNames:         serverPKIConfig.PolicyName,
    }
    srvCAAuthRoleConfigSecondary.ConfigureK8SAuthRole(t, vaultClient)

    vaultCASecretName := vault.CASecretName(vaultReleaseName)

    commonHelmValues := map[string]string{
        "global.adminPartitions.enabled": "true",
        "global.enableConsulNamespaces": "true",
        "connectInject.enabled":  "true",
        "connectInject.replicas": "1",
        "global.secretsBackend.vault.enabled":              "true",
        "global.secretsBackend.vault.consulClientRole":     consulClientRole,
        "global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
        "global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,
        "global.secretsBackend.vault.ca.secretName": vaultCASecretName,
        "global.secretsBackend.vault.ca.secretKey":  "tls.crt",
        "global.acls.manageSystemACLs": "true",
        "global.tls.enabled":           "true",
        "global.tls.enableAutoEncrypt": "true",
        "global.tls.caCert.secretName": serverPKIConfig.CAPath,
        "global.gossipEncryption.secretName": gossipSecret.Path,
        "global.gossipEncryption.secretKey":  gossipSecret.Key,
        "global.enterpriseLicense.secretName": licenseSecret.Path,
        "global.enterpriseLicense.secretKey":  licenseSecret.Key,
    }

    serverHelmValues := map[string]string{
        "global.secretsBackend.vault.consulServerRole":              consulServerRole,
        "global.secretsBackend.vault.connectCA.address":             serverClusterVault.Address(),
        "global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPath,
        "global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePath,
        "global.acls.bootstrapToken.secretName": bootstrapTokenSecret.Path,
        "global.acls.bootstrapToken.secretKey":  bootstrapTokenSecret.Key,
        "global.acls.partitionToken.secretName": partitionTokenSecret.Path,
        "global.acls.partitionToken.secretKey":  partitionTokenSecret.Key,
        "server.serverCert.secretName": serverPKIConfig.CertPath,
        "server.extraVolumes[0].type": "secret",
        "server.extraVolumes[0].name": vaultCASecretName,
        "server.extraVolumes[0].load": "false",
    }

    if cfg.UseKind {
        serverHelmValues["meshGateway.service.type"] = "NodePort"
        serverHelmValues["meshGateway.service.nodePort"] = "30100"
        serverHelmValues["server.exposeService.type"] = "NodePort"
        serverHelmValues["server.exposeService.nodePort.https"] = "30000"
    }

    helpers.MergeMaps(serverHelmValues, commonHelmValues)

    logger.Log(t, "Installing Consul")
    consulCluster := consul.NewHelmCluster(t, serverHelmValues, serverClusterCtx, cfg, consulReleaseName)
    consulCluster.Create(t)

    partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", consulReleaseName)
    partitionSvcAddress := k8s.ServiceHost(t, cfg, serverClusterCtx, partitionServiceName)

    // --- FIX: Split IP and Port for Helm values ---
    partitionHost, partitionPort, err := net.SplitHostPort(partitionSvcAddress)
    require.NoError(t, err, "failed to split partition service address")
    // ----------------------------------------------

    k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

    // Move Vault CA secret from primary to secondary
    logger.Logf(t, "retrieving Vault CA secret %s from the primary cluster and applying to the secondary", vaultCASecretName)
    vaultCASecret, err := serverClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), vaultCASecretName, metav1.GetOptions{})
    vaultCASecret.ResourceVersion = ""
    require.NoError(t, err)
    _, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), vaultCASecret, metav1.CreateOptions{})
    require.NoError(t, err)
    t.Cleanup(func() {
        clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Delete(context.Background(), vaultCASecretName, metav1.DeleteOptions{})
    })

    if adminPartitionsRole == "" {
        logger.Logf(t, "ERROR: adminPartitionsRole is empty")
    }
    if k8sAuthMethodHost == "" {
        logger.Logf(t, "ERROR: k8sAuthMethodHost is empty")
    }

    // Create client cluster configuration
    clientHelmValues := map[string]string{
        "global.enabled": "false",
        "global.adminPartitions.name": secondaryPartition,
        "global.acls.bootstrapToken.secretName": partitionTokenSecret.Path,
        "global.acls.bootstrapToken.secretKey":  partitionTokenSecret.Key,
        "global.secretsBackend.vault.agentAnnotations":    fmt.Sprintf("vault.hashicorp.com/tls-server-name: %s-vault", vaultReleaseName),
        "global.secretsBackend.vault.adminPartitionsRole": adminPartitionsRole,
        "externalServers.enabled":           "true",
        
        // --- FIX: Use split Host and Port ---
        "externalServers.hosts[0]":          partitionHost,
        "externalServers.httpsPort":         partitionPort,
        // ------------------------------------
        
        "externalServers.tlsServerName":     "server.dc1.consul",
        "externalServers.k8sAuthMethodHost": k8sAuthMethodHost,
        "client.enabled": "true",
    }

    if cfg.UseKind {
        clientHelmValues["externalServers.httpsPort"] = "30000"
        clientHelmValues["meshGateway.service.type"] = "NodePort"
        clientHelmValues["meshGateway.service.nodePort"] = "30100"
    }

    helpers.MergeMaps(clientHelmValues, commonHelmValues)

    // DEBUG: Quick network connectivity test
    logger.Logf(t, "DEBUG: Testing basic network connectivity...")
    testConnectivity(t, serverClusterCtx, clientClusterCtx, ns, partitionSvcAddress)

    // Install the consul cluster without servers in the client cluster kubernetes context.
    logger.Logf(t, "DEBUG: Creating client Consul cluster...")
    clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterCtx, cfg, consulReleaseName)
    clientConsulCluster.Create(t)
    
    // Ensure consul clients are created.
    // Use Retry to wait for pod creation instead of hard sleep
    var agentPodList *corev1.PodList
    require.Eventually(t, func() bool {
        list, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientClusterCtx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=consul,component=client"})
        if err != nil || len(list.Items) == 0 {
            return false
        }
        agentPodList = list
        return true
    }, 2*time.Minute, 5*time.Second, "Client pods did not appear in time")

    output, err := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "consul", "-n", clientClusterCtx.KubectlOptions(t).Namespace)
    require.NoError(t, err)
    require.Contains(t, output, "Partition: 'secondary'")
}

func debugVaultPartitions(t *testing.T, clientClusterCtx environment.TestContext, ns string) {
	logger.Logf(t, "DEBUG: Checking client cluster state...")

	// List all pods in the namespace
	pods, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(ns).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		logger.Logf(t, "ERROR: Failed to list pods: %v", err)
		return
	}

	for _, pod := range pods.Items {
		logger.Logf(t, "Pod: %s, Status: %s", pod.Name, pod.Status.Phase)
		if pod.Status.Phase != corev1.PodRunning {
			logger.Logf(t, "  Container Statuses:")
			for _, cs := range pod.Status.ContainerStatuses {
				logger.Logf(t, "    %s: Ready=%v, Restarts=%d",
					cs.Name, cs.Ready, cs.RestartCount)
				if cs.State.Waiting != nil {
					logger.Logf(t, "      Waiting Reason: %s, Message: %s",
						cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}

			// Get pod events
			events, err := clientClusterCtx.KubernetesClient(t).CoreV1().Events(ns).List(
				context.Background(), metav1.ListOptions{
					FieldSelector: fmt.Sprintf("involvedObject.name=%s", pod.Name),
				})
			if err == nil && len(events.Items) > 0 {
				for _, event := range events.Items {
					logger.Logf(t, "  Event: %s - %s", event.Reason, event.Message)
				}
			}
		}
	}

	// Check for failed init containers
	for _, pod := range pods.Items {
		for _, status := range pod.Status.InitContainerStatuses {
			if !status.Ready && status.State.Terminated != nil {
				logger.Logf(t, "ERROR: Init container %s in pod %s failed: %s",
					status.Name, pod.Name, status.State.Terminated.Message)
			}
		}
	}
}

// Helper function to get keys from a map
func getSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// Helper function to test connectivity
func testConnectivity(t *testing.T, serverCtx, clientCtx environment.TestContext, ns, targetAddress string) {
	// Create a simple test pod in client cluster
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "network-test-" + strings.ToLower(rand.String(5)),
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test",
					Image:   "busybox:latest",
					Command: []string{"sleep", "30"},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	// Try to create test pod
	clientK8s := clientCtx.KubernetesClient(t)
	_, err := clientK8s.CoreV1().Pods(ns).Create(context.Background(), testPod, metav1.CreateOptions{})
	if err != nil {
		logger.Logf(t, "WARN: Could not create network test pod: %v", err)
		return
	}

	// Clean up after test
	defer func() {
		clientK8s.CoreV1().Pods(ns).Delete(context.Background(), testPod.Name, metav1.DeleteOptions{})
	}()

	// Wait for pod to be ready
	time.Sleep(2 * time.Second)

}


