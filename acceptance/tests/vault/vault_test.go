// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

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
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-uuid"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StaticClientName         = "static-client"
	KubernetesAuthMethodPath = "kubernetes"
	ManageSystemACLsRole     = "server-acl-init"
	ClientRole               = "client"
	ServerRole               = "server"
)

// TestVault installs Vault, bootstraps it with secrets, policies, and Kube Auth Method.
// It then configures Consul to use vault as the backend and checks that it works.
func TestVault(t *testing.T) {
	cases := map[string]struct {
		autoBootstrap bool
	}{
		"manual ACL bootstrap": {},
		"automatic ACL bootstrap": {
			autoBootstrap: true,
		},
	}
	for name, c := range cases {
		c := c
		t.Run(name, func(t *testing.T) {
			testVault(t, c.autoBootstrap)
		})
	}
}

// testVault is the implementation for TestVault:
//
//   - testAutoBootstrap = false. Test when ACL bootstrapping has already occurred.
//     The test pre-populates a Vault secret with the bootstrap token.
//   - testAutoBootstrap = true. Test that server-acl-init automatically ACL bootstraps
//     consul and writes the bootstrap token to Vault.
func testVault(t *testing.T, testAutoBootstrap bool) {
	cfg := suite.Config()
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

	// Initially tried to set the expiration to 5-20s to keep the test as short running as possible,
	// but at those levels, the pods would fail to start becuase the certs had expired and would throw errors.
	// 30s seconds seemed to consistently clear this issue and not have startup problems.
	// If trying to go lower, be sure to run this several times in CI to ensure that there are little issues.
	// If wanting to make this higher, there is no problem except for consideration of how long the test will
	// take to complete.
	expirationInSeconds := 30

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
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, ServerRole),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, ServerRole),
		MaxTTL:              fmt.Sprintf("%ds", expirationInSeconds),
		AuthMethodPath:      KubernetesAuthMethodPath,
	}
	serverPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

	// Configure connect injector webhook PKI
	connectInjectorWebhookPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "connect",
		PolicyName:          "connect-ca-policy",
		RoleName:            "connect-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "connect-injector"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "connect-injector"),
		MaxTTL:              fmt.Sprintf("%ds", expirationInSeconds),
		AuthMethodPath:      KubernetesAuthMethodPath,
	}
	connectInjectorWebhookPKIConfig.ConfigurePKIAndAuthRole(t, vaultClient)

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

	bootstrapTokenSecret := &vault.KV2Secret{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      "",
		PolicyName: "bootstrap",
	}
	if testAutoBootstrap {
		bootstrapTokenSecret.SaveSecretAndAddUpdatePolicy(t, vaultClient)
	} else {
		id, err := uuid.GenerateUUID()
		require.NoError(t, err)
		bootstrapTokenSecret.Value = id
		bootstrapTokenSecret.SaveSecretAndAddReadPolicy(t, vaultClient)
	}

	bootstrapToken := bootstrapTokenSecret.Value

	// -------------------------
	// Additional Auth Roles
	// -------------------------
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}

	// server
	consulServerRole := ServerRole
	srvAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	}
	srvAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// client
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

	// manageSystemACLs
	manageSystemACLsRole := ManageSystemACLsRole
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, ManageSystemACLsRole)
	aclAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            manageSystemACLsRole,
		PolicyNames:         bootstrapTokenSecret.PolicyName,
	}
	aclAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	// allow all components to access server ca
	srvCAAuthRoleConfig := &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      KubernetesAuthMethodPath,
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	}
	srvCAAuthRoleConfig.ConfigureK8SAuthRole(t, vaultClient)

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"global.secretsBackend.vault.connectInject.tlsCert.secretName": connectInjectorWebhookPKIConfig.CertPath,
		"global.secretsBackend.vault.connectInject.caCert.secretName":  connectInjectorWebhookPKIConfig.CAPath,

		"global.secretsBackend.vault.enabled":              "true",
		"global.secretsBackend.vault.consulServerRole":     consulServerRole,
		"global.secretsBackend.vault.consulClientRole":     consulClientRole,
		"global.secretsBackend.vault.consulCARole":         serverPKIConfig.RoleName,
		"global.secretsBackend.vault.connectInjectRole":    connectInjectorWebhookPKIConfig.RoleName,
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
		"global.gossipEncryption.secretName":    gossipSecret.Path,
		"global.gossipEncryption.secretKey":     gossipSecret.Key,

		"ingressGateways.enabled":               "true",
		"ingressGateways.defaults.replicas":     "1",
		"terminatingGateways.enabled":           "true",
		"terminatingGateways.defaults.replicas": "1",

		"server.serverCert.secretName": serverPKIConfig.CertPath,
		"global.tls.caCert.secretName": serverPKIConfig.CAPath,
		"global.tls.enableAutoEncrypt": "true",

		// For sync catalog, it is sufficient to check that the deployment is running and ready
		// because we only care that get-auto-encrypt-client-ca init container was able
		// to talk to the Consul server using the CA from Vault. For this reason,
		// we don't need any services to be synced in either direction.
		"syncCatalog.enabled":  "true",
		"syncCatalog.toConsul": "false",
		"syncCatalog.toK8S":    "false",

		// Enable clients to make sure vault integration still works.
		"client.enabled": "true",
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		consulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Portforward to connect injector pod and get cert
	client := environment.KubernetesClientFromOptions(t, kubectlOptions)
	podList, err := client.CoreV1().Pods(kubectlOptions.Namespace).List(context.Background(),
		metav1.ListOptions{LabelSelector: fmt.Sprintf("app=consul,component=connect-injector,release=%s", consulReleaseName)})
	require.NoError(t, err)
	require.NotEmpty(t, podList.Items)
	connectInjectorPodName := podList.Items[0].Name
	connectInjectorPodAddress := portforward.CreateTunnelToResourcePort(t, connectInjectorPodName, 8080, kubectlOptions, terratestLogger.Discard)
	connectInjectorCert, err := getCertificate(t, connectInjectorPodAddress)
	require.NoError(t, err)
	logger.Logf(t, "Connect Inject Webhook Cert expiry: %s \n", connectInjectorCert.NotAfter.String())

	logger.Logf(t, "Wait %d seconds for certificates to rotate....", expirationInSeconds)
	time.Sleep(time.Duration(expirationInSeconds) * time.Second)

	if testAutoBootstrap {
		logger.Logf(t, "Validating the ACL bootstrap token was stored in Vault.")
		timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 1 * time.Second}
		retry.RunWith(timer, t, func(r *retry.R) {
			secret, err := vaultClient.Logical().Read("consul/data/secret/bootstrap")
			require.NoError(r, err)

			data, ok := secret.Data["data"].(map[string]interface{})
			require.True(r, ok)
			require.NotNil(r, data)

			tok, ok := data["token"].(string)
			require.True(r, ok)
			require.NotEmpty(r, tok)

			// Set bootstrapToken for subsequent validations.
			bootstrapToken = tok
		})
	}

	// Validate that the gossip encryption key is set correctly.
	logger.Log(t, "Validating the gossip key has been set correctly.")
	consulCluster.ACLToken = bootstrapToken
	consulClient, _ := consulCluster.SetupConsulClient(t, true)
	keys, err := consulClient.Operator().KeyringList(nil)
	require.NoError(t, err)
	// There are two identical keys for LAN and WAN since there is only 1 dc.
	require.Len(t, keys, 2)
	require.Equal(t, 1, keys[0].PrimaryKeys[gossipKey])

	// Confirm that the Vault Connect CA has been bootstrapped correctly.
	caConfig, _, err := consulClient.Connect().CAGetConfig(nil)
	require.NoError(t, err)
	require.Equal(t, caConfig.Provider, "vault")

	// Validate that consul sever is running correctly and the consul members command works
	logger.Log(t, "Confirming that we can run Consul commands when exec'ing into server container")
	membersOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(t, ctx.KubectlOptions(t), terratestLogger.Discard, "exec", fmt.Sprintf("%s-consul-server-0", consulReleaseName), "-c", "consul", "--", "sh", "-c", fmt.Sprintf("CONSUL_HTTP_TOKEN=%s consul members", bootstrapToken))
	logger.Logf(t, "Members: \n%s", membersOutput)
	require.NoError(t, err)
	require.Contains(t, membersOutput, fmt.Sprintf("%s-consul-server-0", consulReleaseName))

	if cfg.EnableEnterprise {
		// Validate that the enterprise license is set correctly.
		logger.Log(t, "Validating the enterprise license has been set correctly.")
		license, licenseErr := consulClient.Operator().LicenseGet(nil)
		require.NoError(t, licenseErr)
		require.True(t, license.Valid)
	}

	// Deploy two services and check that they can talk to each other.
	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")
	})
	k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")

	logger.Log(t, "checking that connection is successful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), StaticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
	}

	connectInjectorCert2, err := getCertificate(t, connectInjectorPodAddress)
	require.NoError(t, err)
	// verify that a previous cert expired and that a new one has been issued
	// by comparing the NotAfter on the two certs.
	require.NotEqual(t, connectInjectorCert.NotAfter, connectInjectorCert2.NotAfter)
}
