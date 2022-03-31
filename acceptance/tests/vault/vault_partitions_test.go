package vault

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVault_Partitions(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()
	serverClusterCtx := env.DefaultContext(t)
	clientClusterCtx := env.Context(t, environment.SecondaryContextName)
	ns := serverClusterCtx.KubectlOptions(t).Namespace

	const secondaryPartition = "secondary"

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
	serverClusterVault.Create(t, serverClusterCtx)

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
	secondaryVaultCluster.Create(t, clientClusterCtx)

	vaultClient := serverClusterVault.VaultClient(t)

	// Configure Vault Kubernetes auth method for the secondary cluster.
	{
		// Create auth method service account and ClusterRoleBinding. The Vault server
		// in the primary cluster will use this service account token to talk to the secondary
		// Kubernetes cluster.
		// This ClusterRoleBinding is adapted from the Vault server's role:
		// https://github.com/hashicorp/vault-helm/blob/b0528fce49c529f2c37953ea3a14f30ed651e0d6/templates/server-clusterrolebinding.yaml

		// Use a single name for all RBAC objects.
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
		_, err = clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Create(context.Background(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name: authMethodRBACName,
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		t.Cleanup(func() {
			clientClusterCtx.KubernetesClient(t).RbacV1().ClusterRoleBindings().Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
			clientClusterCtx.KubernetesClient(t).CoreV1().ServiceAccounts(ns).Delete(context.Background(), authMethodRBACName, metav1.DeleteOptions{})
		})

		// Figure out the host for the Kubernetes API. This needs to be reachable from the Vault server
		// in the primary cluster.
		k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

		// Now, configure the auth method in Vault.
		secondaryVaultCluster.ConfigureAuthMethod(t, vaultClient, "kubernetes-"+secondaryPartition, k8sAuthMethodHost, authMethodRBACName, ns)
	}

	vault.ConfigureGossipVaultSecret(t, vaultClient)
	vault.CreateConnectCAPolicy(t, vaultClient, "dc1")
	vault.ConfigureEnterpriseLicenseVaultSecret(t, vaultClient, cfg)
	vault.ConfigureACLTokenVaultSecret(t, vaultClient, "bootstrap")
	vault.ConfigureACLTokenVaultSecret(t, vaultClient, "partition")

	serverPolicies := "gossip,license,connect-ca-dc1,server-cert-dc1,bootstrap-token"
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server", serverPolicies)
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "client", "gossip")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server-acl-init", "bootstrap-token,partition-token")
	vault.ConfigureConsulCAKubernetesAuthRole(t, vaultClient, ns, "kubernetes")

	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes-"+secondaryPartition, "client", "gossip")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes-"+secondaryPartition, "server-acl-init", "partition-token")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes-"+secondaryPartition, "partition-init", "partition-token")
	vault.ConfigureConsulCAKubernetesAuthRole(t, vaultClient, ns, "kubernetes-"+secondaryPartition)
	vault.ConfigurePKICA(t, vaultClient)
	certPath := vault.ConfigurePKICertificates(t, vaultClient, consulReleaseName, ns, "dc1")

	vaultCASecretName := vault.CASecretName(vaultReleaseName)

	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled": "true",

		"global.enableConsulNamespaces": "true",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulClientRole":     "client",
		"global.secretsBackend.vault.consulCARole":         "consul-ca",
		"global.secretsBackend.vault.manageSystemACLsRole": "server-acl-init",

		"global.secretsBackend.vault.ca.secretName": vaultCASecretName,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.acls.manageSystemACLs": "true",

		"global.tls.enabled":           "true",
		"global.tls.enableAutoEncrypt": "true",
		"global.tls.caCert.secretName": "pki/cert/ca",

		"global.gossipEncryption.secretName": "consul/data/secret/gossip",
		"global.gossipEncryption.secretKey":  "gossip",

		"global.enterpriseLicense.secretName": "consul/data/secret/license",
		"global.enterpriseLicense.secretKey":  "license",
	}

	serverHelmValues := map[string]string{
		"global.secretsBackend.vault.consulServerRole":              "server",
		"global.secretsBackend.vault.connectCA.address":             serverClusterVault.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         "connect_root",
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": "dc1/connect_inter",

		"global.acls.bootstrapToken.secretName": "consul/data/secret/bootstrap",
		"global.acls.bootstrapToken.secretKey":  "token",
		"global.acls.partitionToken.secretName": "consul/data/secret/partition",
		"global.acls.partitionToken.secretKey":  "token",

		"server.exposeGossipAndRPCPorts": "true",
		"server.serverCert.secretName":   certPath,

		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecretName,
		"server.extraVolumes[0].load": "false",
	}

	// On Kind, there are no load balancers but since all clusters
	// share the same node network (docker bridge), we can use
	// a NodePort service so that we can access node(s) in a different Kind cluster.
	if cfg.UseKind {
		serverHelmValues["global.adminPartitions.service.type"] = "NodePort"
		serverHelmValues["global.adminPartitions.service.nodePort.https"] = "30000"
		serverHelmValues["meshGateway.service.type"] = "NodePort"
		serverHelmValues["meshGateway.service.nodePort"] = "30100"
	}

	helpers.MergeMaps(serverHelmValues, commonHelmValues)

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, serverHelmValues, serverClusterCtx, cfg, consulReleaseName)
	consulCluster.Create(t)

	partitionServiceName := fmt.Sprintf("%s-consul-partition", consulReleaseName)
	partitionSvcAddress := k8s.ServiceHost(t, cfg, serverClusterCtx, partitionServiceName)

	k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, clientClusterCtx)

	// Move Vault CA secret from primary to secondary so that we can mount it to pods in the
	// secondary cluster.
	logger.Logf(t, "retrieving Vault CA secret %s from the primary cluster and applying to the secondary", vaultCASecretName)
	vaultCASecret, err := serverClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Get(context.Background(), vaultCASecretName, metav1.GetOptions{})
	vaultCASecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Create(context.Background(), vaultCASecret, metav1.CreateOptions{})
	require.NoError(t, err)
	t.Cleanup(func() {
		clientClusterCtx.KubernetesClient(t).CoreV1().Secrets(ns).Delete(context.Background(), vaultCASecretName, metav1.DeleteOptions{})
	})

	// Create client cluster.
	clientHelmValues := map[string]string{
		"global.enabled": "false",

		"global.adminPartitions.name": secondaryPartition,

		"global.acls.bootstrapToken.secretName": "consul/data/secret/partition",
		"global.acls.bootstrapToken.secretKey":  "token",

		"global.secretsBackend.vault.agentAnnotations":    fmt.Sprintf("vault.hashicorp.com/tls-server-name: %s-vault", vaultReleaseName),
		"global.secretsBackend.vault.adminPartitionsRole": "partition-init",

		"externalServers.enabled":           "true",
		"externalServers.hosts[0]":          partitionSvcAddress,
		"externalServers.tlsServerName":     "server.dc1.consul",
		"externalServers.k8sAuthMethodHost": k8sAuthMethodHost,

		"client.enabled":           "true",
		"client.exposeGossipPorts": "true",
		"client.join[0]":           partitionSvcAddress,
	}

	if cfg.UseKind {
		clientHelmValues["externalServers.httpsPort"] = "30000"
		clientHelmValues["meshGateway.service.type"] = "NodePort"
		clientHelmValues["meshGateway.service.nodePort"] = "30100"
	}

	helpers.MergeMaps(clientHelmValues, commonHelmValues)

	// Install the consul cluster without servers in the client cluster kubernetes context.
	clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterCtx, cfg, consulReleaseName)
	clientConsulCluster.Create(t)

	// Ensure consul clients are created.
	agentPodList, err := clientClusterCtx.KubernetesClient(t).CoreV1().Pods(clientClusterCtx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{LabelSelector: "app=consul,component=client"})
	require.NoError(t, err)
	require.NotEmpty(t, agentPodList.Items)

	output, err := k8s.RunKubectlAndGetOutputE(t, clientClusterCtx.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "consul", "-n", clientClusterCtx.KubectlOptions(t).Namespace)
	require.NoError(t, err)
	require.Contains(t, output, "Partition: 'secondary'")
}
