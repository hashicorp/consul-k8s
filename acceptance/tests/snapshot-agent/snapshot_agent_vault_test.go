package snapshotagent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestSnapshotAgent_Vault installs snapshot agent config with an embedded token as a Vault secret.
// It then installs Consul with Vault as a secrets backend and verifies that snapshot files
// are generated.
// Currently, the token needs to be embedded in the snapshot agent config due to a Consul
// bug that does not recognize the token for snapshot command being configured via
// a command line arg or an environment variable.
func TestSnapshotAgent_Vault(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace

	consulReleaseName := helpers.RandomName()
	vaultReleaseName := helpers.RandomName()

	vaultCluster := vault.NewVaultCluster(t, ctx, cfg, vaultReleaseName, nil)
	vaultCluster.Create(t, ctx, "")
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	// -------------------------
	// PKI
	// -------------------------
	// Configure Service Mesh CA
	connectCAPolicy := "connect-ca-dc1"
	connectCARootPath := "connect_root"
	connectCAIntermediatePath := "dc1/connect_inter"
	// Configure Policy for Connect CA
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
		AuthMethodPath:      "kubernetes",
	}
	serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// -------------------------
	// KV2 secrets
	// -------------------------
	// Gossip key
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	gossipSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// License
	licenseSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise {
		licenseSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
	}

	// Bootstrap Token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// Snapshot Agent config
	snapshotAgentConfig := generateSnapshotAgentConfig(t, bootstrapToken)
	require.NoError(t, err)
	snapshotAgentConfigSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/snapshot-agent-config",
		Key:        "config",
		Value:      snapshotAgentConfig,
		PolicyName: "snapshot-agent-config",
	}
	snapshotAgentConfigSecret.SaveSecretAndAddReadPolicy(t, vaultClient)

	// -------------------------
	// Additional Auth Roles
	// -------------------------
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}

	// server
	consulServerRole := "server"
	srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	}
	srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// client
	consulClientRole := "client"
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "client")
	clientAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	}
	clientAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// manageSystemACLs
	manageSystemACLsRole := "server-acl-init"
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "server-acl-init")
	aclAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            manageSystemACLsRole,
		PolicyNames:         bootstrapTokenSecret.PolicyName,
	}
	aclAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// allow all components to access server ca
	srvCAAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// snapshot agent config
	snapAgentRole := "snapshot-agent"
	snapAgentServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "snapshot-agent")
	saAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  snapAgentServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            snapAgentRole,
		PolicyNames:         fmt.Sprintf("%s,%s", licenseSecret.PolicyName, snapshotAgentConfigSecret.PolicyName),
	}
	saAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     consulServerRole,
		"global.secretsBackend.vault.consulClientRole":     consulClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.secretsBackend.vault.connectCA.address":             vaultCluster.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         connectCARootPath,
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": connectCAIntermediatePath,

		"global.acls.manageSystemACLs":          "true",
		"global.acls.bootstrapToken.secretName": bootstrapTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  bootstrapTokenSecret.Key,
		"global.tls.enabled":                    "true",

		"server.serverCert.secretName": serverPKIConfig.CertPath,
		"global.tls.caCert.secretName": serverPKIConfig.CAPath,
		"global.tls.enableAutoEncrypt": "true",

		"client.snapshotAgent.enabled":                        "true",
		"client.snapshotAgent.configSecret.secretName":        snapshotAgentConfigSecret.Path,
		"client.snapshotAgent.configSecret.secretKey":         snapshotAgentConfigSecret.Key,
		"global.secretsBackend.vault.consulSnapshotAgentRole": snapAgentRole,
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		consulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Validate that consul snapshot agent is running correctly and is generating snapshot files
	logger.Log(t, "Confirming that Consul Snapshot Agent is generating snapshot files")
	// Create k8s client from kubectl options.
	client := environment.KubernetesClientFromOptions(t, kubectlOptions)
	podList, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(),
		metav1.ListOptions{LabelSelector: fmt.Sprintf("app=consul,component=client-snapshot-agent,release=%s", consulReleaseName)})
	require.NoError(t, err)
	require.True(t, len(podList.Items) > 0)

	// Wait for 10 seconds to allow snapshot to write.
	time.Sleep(10 * time.Second)

	// Loop through snapshot agents.  Only one will be the leader and have the snapshot files.
	hasSnapshots := false
	for _, pod := range podList.Items {
		snapshotFileListOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(t, kubectlOptions, terratestLogger.Discard, "exec", pod.Name, "-c", "consul-snapshot-agent", "--", "ls", "/")
		logger.Logf(t, "Snapshot: \n%s", snapshotFileListOutput)
		require.NoError(t, err)
		if strings.Contains(snapshotFileListOutput, ".snap") {
			logger.Logf(t, "Agent pod contains snapshot files")
			hasSnapshots = true
			break
		} else {
			logger.Logf(t, "Agent pod does not contain snapshot files")
		}
	}
	require.True(t, hasSnapshots)
}
