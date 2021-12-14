package vault

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/stretchr/testify/require"
)

const (
	gossipPolicy = `
path "consul/data/secret/gossip" {
  capabilities = ["read"]
}`

	// connectCAPolicy allows Consul to bootstrap all certificates for the service mesh in Vault.
	// Adapted from https://www.consul.io/docs/connect/ca/vault#consul-managed-pki-paths.
	connectCAPolicy = `
path "/sys/mounts" {
  capabilities = [ "read" ]
}

path "/sys/mounts/connect_root" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/sys/mounts/connect_inter" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/connect_root/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/connect_inter/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
`
	serverTLSPolicy = `
path "pki/issue/consul-server" {
  capabilities = ["create", "update"]
}`
	caPolicy = `
path "pki/cert/ca" {
  capabilities = ["read"]
}`
)

// TestVault installs Vault, bootstraps it with secrets, policies, and Kube Auth Method.
// It then configures Consul to use vault as the backend and checks that it works.
func TestVault(t *testing.T) {
	cfg := suite.Config()
	if cfg.EnablePodSecurityPolicies {
		t.Skipf("skipping this test because PSPs don't yet work with the acceptance test Vault installation")
	}
	ctx := suite.Environment().DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

	consulReleaseName := helpers.RandomName()
	vaultReleaseName := helpers.RandomName()
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-client", consulReleaseName)
	consulServerServiceAccountName := fmt.Sprintf("%s-consul-server", consulReleaseName)

	vaultCluster := vault.NewVaultCluster(t, ctx, cfg, vaultReleaseName)
	vaultCluster.Create(t, ctx)
	// Vault is now installed in the cluster.

	// Now fetch the Vault client so we can create the policies and secrets.
	vaultClient := vaultCluster.VaultClient(t)

	// Create the Vault Policy for the gossip key.
	logger.Log(t, "Creating policies")
	err := vaultClient.Sys().PutPolicy("consul-gossip", gossipPolicy)
	require.NoError(t, err)

	// Create the Vault Policy for the connect-ca.
	err = vaultClient.Sys().PutPolicy("connect-ca", connectCAPolicy)
	require.NoError(t, err)

	// Create the Auth Roles for consul-server and consul-client.
	// Auth roles bind policies to Kubernetes service accounts, which
	// then enables the Vault agent init container to call 'vault login'
	// with the Kubernetes auth method to obtain a Vault token.
	// Please see https://www.vaultproject.io/docs/auth/kubernetes#configuration
	// for more details.
	logger.Log(t, "Creating the consul-server and consul-client roles")
	params := map[string]interface{}{
		"bound_service_account_names":      consulClientServiceAccountName,
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-gossip",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write("auth/kubernetes/role/consul-client", params)
	require.NoError(t, err)

	params = map[string]interface{}{
		"bound_service_account_names":      consulServerServiceAccountName,
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-gossip,connect-ca,consul-server",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write("auth/kubernetes/role/consul-server", params)
	require.NoError(t, err)

	// Create the CA role that all components will use to fetch the Server CA certs.
	params = map[string]interface{}{
		"bound_service_account_names":      "*",
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-ca",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write("auth/kubernetes/role/consul-ca", params)
	require.NoError(t, err)

	// Generate the gossip secret.
	gossipKey, err := generateGossipSecret()
	require.NoError(t, err)

	// Create the gossip secret.
	logger.Log(t, "Creating the gossip secret")
	params = map[string]interface{}{
		"data": map[string]interface{}{
			"gossip": gossipKey,
		},
	}
	_, err = vaultClient.Logical().Write("consul/data/secret/gossip", params)
	require.NoError(t, err)

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	// Bootstrap TLS by creating the CA infrastructure required for Consul server TLS and also create the `consul-server` PKI role.
	// Using https://learn.hashicorp.com/tutorials/consul/vault-pki-consul-secure-tls.
	// Generate the root CA.
	params = map[string]interface{}{
		"common_name": "dc1.consul",
		"ttl":         "24h",
	}
	_, err = vaultClient.Logical().Write("pki/root/generate/internal", params)
	require.NoError(t, err)

	// Create the Vault PKI Role.
	name := consulReleaseName + "-consul"
	allowedDomains := fmt.Sprintf("dc1.consul,%s-server,%s-server.%s,%s-server.%s.svc", name, name, ns, name, ns)
	params = map[string]interface{}{
		"allowed_domains":    allowedDomains,
		"allow_bare_domains": "true",
		"allow_localhost":    "true",
		"allow_subdomains":   "true",
		"generate_lease":     "true",
		"max_ttl":            "1h",
	}
	_, err = vaultClient.Logical().Write("pki/roles/consul-server", params)
	require.NoError(t, err)

	// Create the server and ca policies
	err = vaultClient.Sys().PutPolicy("consul-server", serverTLSPolicy)
	require.NoError(t, err)
	err = vaultClient.Sys().PutPolicy("consul-ca", caPolicy)
	require.NoError(t, err)

	consulHelmValues := map[string]string{
		// TODO: Update the global image once 1.11 is GA.
		"global.image": "docker.mirror.hashicorp.services/hashicorpdev/consul:latest",

		"server.enabled":              "true",
		"server.replicas":             "1",
		"server.extraVolumes[0].type": "secret",
		"server.extraVolumes[0].name": vaultCASecret,
		"server.extraVolumes[0].load": "false",
		"global.datacenter":           "dc1",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

		"global.secretsBackend.vault.enabled":          "true",
		"global.secretsBackend.vault.consulServerRole": "consul-server",
		"global.secretsBackend.vault.consulClientRole": "consul-client",
		"global.secretsBackend.vault.consulCARole":     "consul-ca",

		"global.secretsBackend.vault.ca.secretName": vaultCASecret,
		"global.secretsBackend.vault.ca.secretKey":  "tls.crt",

		"global.secretsBackend.vault.connectCA.address":             vaultCluster.Address(),
		"global.secretsBackend.vault.connectCA.rootPKIPath":         "connect_root",
		"global.secretsBackend.vault.connectCA.intermediatePKIPath": "connect_inter",

		"global.acls.manageSystemACLs":       "true",
		"global.tls.enabled":                 "true",
		"global.gossipEncryption.secretName": "consul/data/secret/gossip",
		"global.gossipEncryption.secretKey":  "gossip",

		"ingressGateways.enabled":               "true",
		"ingressGateways.defaults.replicas":     "1",
		"terminatingGateways.enabled":           "true",
		"terminatingGateways.defaults.replicas": "1",

		"server.serverCert.secretName": "pki/issue/consul-server",
		"global.tls.caCert.secretName": "pki/cert/ca",
		"global.tls.httpsOnly":         "false",
		"global.tls.enableAutoEncrypt": "true",

		// For sync catalog, it is sufficient to check that the deployment is running and ready
		// because we only care that get-auto-encrypt-client-ca init container was able
		// to talk to the Consul server using the CA from Vault. For this reason,
		// we don't need any services to be synced in either direction.
		"syncCatalog.enabled":  "true",
		"syncCatalog.toConsul": "false",
		"syncCatalog.toK8S":    "false",
	}
	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Validate that the gossip encryption key is set correctly.
	logger.Log(t, "Validating the gossip key has been set correctly.")
	consulClient := consulCluster.SetupConsulClient(t, true)
	keys, err := consulClient.Operator().KeyringList(nil)
	require.NoError(t, err)
	// There are two identical keys for LAN and WAN since there is only 1 dc.
	require.Len(t, keys, 2)
	require.Equal(t, 1, keys[0].PrimaryKeys[gossipKey])

	// Confirm that the Vault Connect CA has been bootstrapped correctly.
	caConfig, _, err := consulClient.Connect().CAGetConfig(nil)
	require.NoError(t, err)
	require.Equal(t, caConfig.Provider, "vault")

	// Deploy two services and check that they can talk to each other.
	logger.Log(t, "creating static-server and static-client deployments")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
	} else {
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")
	})
	k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/bases/intention")

	logger.Log(t, "checking that connection is successful")
	if cfg.EnableTransparentProxy {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), "http://localhost:1234")
	}
}

// generateGossipSecret generates a random 32 byte secret returned as a base64 encoded string.
func generateGossipSecret() (string, error) {
	// This code was copied from Consul's Keygen command:
	// https://github.com/hashicorp/consul/blob/d652cc86e3d0322102c2b5e9026c6a60f36c17a5/command/keygen/keygen.go

	key := make([]byte, 32)
	n, err := rand.Reader.Read(key)
	if err != nil {
		return "", fmt.Errorf("error reading random data: %s", err)
	}
	if n != 32 {
		return "", fmt.Errorf("couldn't read enough entropy")
	}

	return base64.StdEncoding.EncodeToString(key), nil
}
