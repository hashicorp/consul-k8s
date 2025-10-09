// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package snapshotagent

import (
	"context"
	"fmt"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/go-version"
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
	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set and snapshot agent is already tested with regular tproxy")
	}
	ctx := suite.Environment().DefaultContext(t)
	kubectlOptions := ctx.KubectlOptions(t)
	ns := kubectlOptions.Namespace

	ver, err := version.NewVersion("1.12.0")
	require.NoError(t, err)
	if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
		t.Skipf("skipping this test because vault secrets backend is not supported in version %v", cfg.ConsulVersion.String())
	}

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
	snapshotAgentConfig := generateSnapshotAgentConfig(t)
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
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName, snapshotAgentConfigSecret.PolicyName)
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

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"connectInject.enabled":  "false",
		"connectInject.replicas": "1",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     consulServerRole,
		"global.secretsBackend.vault.manageSystemACLsRole": manageSystemACLsRole,

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.acls.manageSystemACLs":          "true",
		"global.acls.bootstrapToken.secretName": bootstrapTokenSecret.Path,
		"global.acls.bootstrapToken.secretKey":  bootstrapTokenSecret.Key,
		"global.tls.enabled":                    "true",

		"server.serverCert.secretName": serverPKIConfig.CertPath,
		"global.tls.caCert.secretName": serverPKIConfig.CAPath,
		"global.tls.enableAutoEncrypt": "true",

		"server.snapshotAgent.enabled":                 "true",
		"server.snapshotAgent.configSecret.secretName": snapshotAgentConfigSecret.Path,
		"server.snapshotAgent.configSecret.secretKey":  snapshotAgentConfigSecret.Key,
		"global.dualStack.defaultEnabled":              cfg.GetDualStack(),
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
		metav1.ListOptions{LabelSelector: fmt.Sprintf("app=consul,component=server,release=%s", consulReleaseName)})
	require.NoError(t, err)
	require.Len(t, podList.Items, 1, "expected to find only 1 consul server instance")

	// We need to give some extra time for ACLs to finish bootstrapping and for servers to come up.
	timer := &retry.Timer{Timeout: 1 * time.Minute, Wait: 1 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		// Loop through snapshot agents.  Only one will be the leader and have the snapshot files.
		pod := podList.Items[0]
		snapshotFileListOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(r, kubectlOptions, terratestLogger.Discard, "exec", pod.Name, "-c", "consul-snapshot-agent", "--", "ls", "/tmp")
		require.NoError(r, err)
		logger.Logf(r, "Snapshot: \n%s", snapshotFileListOutput)
		require.Contains(r, snapshotFileListOutput, ".snap", "Agent pod does not contain snapshot files")
	})
}
