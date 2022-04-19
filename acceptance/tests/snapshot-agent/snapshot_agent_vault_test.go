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
	vaultCluster.Create(t, ctx)
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	vault.CreateConnectCAPolicy(t, vaultClient, "dc1")
	if cfg.EnableEnterprise {
		vault.ConfigureEnterpriseLicenseVaultSecret(t, vaultClient, cfg)
	}

	bootstrapToken := vault.ConfigureACLTokenVaultSecret(t, vaultClient, "bootstrap")

	config := generateSnapshotAgentConfig(t, bootstrapToken)
	vault.ConfigureSnapshotAgentSecret(t, vaultClient, cfg, config)

	serverPolicies := "gossip,connect-ca-dc1,server-cert-dc1,bootstrap-token"
	if cfg.EnableEnterprise {
		serverPolicies += ",license"
	}
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server", serverPolicies)
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "client", "gossip")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server-acl-init", "bootstrap-token")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "snapshot-agent", "snapshot-agent-config,license")
	vault.ConfigureConsulCAKubernetesAuthRole(t, vaultClient, ns, "kubernetes")

	vault.ConfigurePKICA(t, vaultClient)
	certPath := vault.ConfigurePKICertificates(t, vaultClient, consulReleaseName, ns, "dc1", "1h")

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     "server",
		"global.secretsBackend.vault.consulClientRole":     "client",
		"global.secretsBackend.vault.consulCARole":         "consul-ca",
		"global.secretsBackend.vault.manageSystemACLsRole": "server-acl-init",

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.secretsBackend.vault.connectCA.address":             vaultCluster.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         "connect_root",
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": "dc1/connect_inter",

		"global.acls.manageSystemACLs":          "true",
		"global.acls.bootstrapToken.secretName": "consul/data/secret/bootstrap",
		"global.acls.bootstrapToken.secretKey":  "token",
		"global.tls.enabled":                    "true",

		"server.serverCert.secretName": certPath,
		"global.tls.caCert.secretName": "pki/cert/ca",
		"global.tls.enableAutoEncrypt": "true",

		"client.snapshotAgent.enabled":                        "true",
		"client.snapshotAgent.configSecret.secretName":        "consul/data/secret/snapshot-agent-config",
		"client.snapshotAgent.configSecret.secretKey":         "config",
		"global.secretsBackend.vault.consulSnapshotAgentRole": "snapshot-agent",
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = "consul/data/secret/license"
		consulHelmValues["global.enterpriseLicense.secretKey"] = "license"
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

	// Wait for 10seconds to allow snapsot to write.
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
