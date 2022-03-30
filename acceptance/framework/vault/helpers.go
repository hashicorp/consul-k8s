package vault

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/go-uuid"
	vapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
)

const (
	gossipPolicy = `
path "consul/data/secret/gossip" {
  capabilities = ["read"]
}`

	tokenPolicyTemplate = `
path "consul/data/secret/%s" {
  capabilities = ["read"]
}`

	enterpriseLicensePolicy = `
path "consul/data/secret/license" {
  capabilities = ["read"]
}`

	// connectCAPolicy allows Consul to bootstrap all certificates for the service mesh in Vault.
	// Adapted from https://www.consul.io/docs/connect/ca/vault#consul-managed-pki-paths.
	connectCAPolicyTemplate = `
path "/sys/mounts" {
  capabilities = [ "read" ]
}

path "/sys/mounts/connect_root" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/sys/mounts/%s/connect_inter" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/connect_root/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}

path "/%s/connect_inter/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
`
	caPolicy = `
path "pki/cert/ca" {
  capabilities = ["read"]
}`

	snapshotAgentPolicy = `
path "consul/data/secret/snapshot-agent-config" {
  capabilities = ["read"]
}`
)

// GenerateGossipSecret generates a random 32 byte secret returned as a base64 encoded string.
func GenerateGossipSecret() (string, error) {
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

// ConfigureGossipVaultSecret generates a gossip encryption key,
// stores it in Vault as a secret and configures a policy to access it.
func ConfigureGossipVaultSecret(t *testing.T, vaultClient *vapi.Client) string {
	// Create the Vault Policy for the gossip key.
	logger.Log(t, "Creating gossip policy")
	err := vaultClient.Sys().PutPolicy("gossip", gossipPolicy)
	require.NoError(t, err)

	// Generate the gossip secret.
	gossipKey, err := GenerateGossipSecret()
	require.NoError(t, err)

	// Create the gossip secret.
	logger.Log(t, "Creating the gossip secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"gossip": gossipKey,
		},
	}
	_, err = vaultClient.Logical().Write("consul/data/secret/gossip", params)
	require.NoError(t, err)

	return gossipKey
}

// ConfigureEnterpriseLicenseVaultSecret stores it in Vault as a secret and configures a policy to access it.
func ConfigureEnterpriseLicenseVaultSecret(t *testing.T, vaultClient *vapi.Client, cfg *config.TestConfig) {
	// Create the enterprise license secret.
	logger.Log(t, "Creating the Enterprise License secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"license": cfg.EnterpriseLicense,
		},
	}
	_, err := vaultClient.Logical().Write("consul/data/secret/license", params)
	require.NoError(t, err)

	err = vaultClient.Sys().PutPolicy("license", enterpriseLicensePolicy)
	require.NoError(t, err)
}

// ConfigureSnapshotAgentSecret stores it in Vault as a secret and configures a policy to access it.
func ConfigureSnapshotAgentSecret(t *testing.T, vaultClient *vapi.Client, cfg *config.TestConfig, config string) {
	logger.Log(t, "Creating the Snapshot Agent Config secret in Vault")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"config": config,
		},
	}
	_, err := vaultClient.Logical().Write("consul/data/secret/snapshot-agent-config", params)
	require.NoError(t, err)

	err = vaultClient.Sys().PutPolicy("snapshot-agent-config", snapshotAgentPolicy)
	require.NoError(t, err)
}

// ConfigureKubernetesAuthRole configures a role in Vault for the component for the Kubernetes auth method
// that will be used by the test Helm chart installation.
func ConfigureKubernetesAuthRole(t *testing.T, vaultClient *vapi.Client, consulReleaseName, ns, authPath, component, policies string) {
	componentServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, component)

	// Create the Auth Roles for the component.
	// Auth roles bind policies to Kubernetes service accounts, which
	// then enables the Vault agent init container to call 'vault login'
	// with the Kubernetes auth method to obtain a Vault token.
	// Please see https://www.vaultproject.io/docs/auth/kubernetes#configuration
	// for more details.
	logger.Logf(t, "Creating the %q", componentServiceAccountName)
	params := map[string]interface{}{
		"bound_service_account_names":      componentServiceAccountName,
		"bound_service_account_namespaces": ns,
		"policies":                         policies,
		"ttl":                              "24h",
	}
	_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/%s", authPath, component), params)
	require.NoError(t, err)
}

// ConfigureConsulCAKubernetesAuthRole configures a role in Vault that allows all service accounts
// within the installation namespace access to the Consul server CA.
func ConfigureConsulCAKubernetesAuthRole(t *testing.T, vaultClient *vapi.Client, ns, authPath string) {
	// Create the CA role that all components will use to fetch the Server CA certs.
	params := map[string]interface{}{
		"bound_service_account_names":      "*",
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-ca",
		"ttl":                              "24h",
	}
	_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/consul-ca", authPath), params)
	require.NoError(t, err)
}

// ConfigurePKICA generates a CA in Vault.
func ConfigurePKICA(t *testing.T, vaultClient *vapi.Client) {
	// Create root CA to issue Consul server certificates and the `consul-server` PKI role.
	// See https://learn.hashicorp.com/tutorials/consul/vault-pki-consul-secure-tls.
	// Generate the root CA.
	params := map[string]interface{}{
		"common_name": "Consul CA",
		"ttl":         "24h",
	}
	_, err := vaultClient.Logical().Write("pki/root/generate/internal", params)
	require.NoError(t, err)

	err = vaultClient.Sys().PutPolicy("consul-ca", caPolicy)
	require.NoError(t, err)
}

// ConfigurePKICertificates configures roles in Vault so that Consul server TLS certificates
// can be issued by Vault.
func ConfigurePKICertificates(t *testing.T, vaultClient *vapi.Client, consulReleaseName, ns, datacenter string) string {
	// Create the Vault PKI Role.
	consulServerDNSName := consulReleaseName + "-consul-server"
	allowedDomains := fmt.Sprintf("%s.consul,%s,%s.%s,%s.%s.svc", datacenter, consulServerDNSName, consulServerDNSName, ns, consulServerDNSName, ns)
	params := map[string]interface{}{
		"allowed_domains":    allowedDomains,
		"allow_bare_domains": "true",
		"allow_localhost":    "true",
		"allow_subdomains":   "true",
		"generate_lease":     "true",
		"max_ttl":            "1h",
	}

	pkiRoleName := fmt.Sprintf("server-cert-%s", datacenter)

	_, err := vaultClient.Logical().Write(fmt.Sprintf("pki/roles/%s", pkiRoleName), params)
	require.NoError(t, err)

	certificateIssuePath := fmt.Sprintf("pki/issue/%s", pkiRoleName)
	serverTLSPolicy := fmt.Sprintf(`
path %q {
  capabilities = ["create", "update"]
}`, certificateIssuePath)

	// Create the server policy.
	err = vaultClient.Sys().PutPolicy(pkiRoleName, serverTLSPolicy)
	require.NoError(t, err)

	return certificateIssuePath
}

// ConfigureACLTokenVaultSecret generates a token secret ID for a given name,
// stores it in Vault as a secret and configures a policy to access it.
func ConfigureACLTokenVaultSecret(t *testing.T, vaultClient *vapi.Client, tokenName string) string {
	// Create the Vault Policy for the token.
	logger.Logf(t, "Creating %s token policy", tokenName)
	policyName := fmt.Sprintf("%s-token", tokenName)
	tokenPolicy := fmt.Sprintf(tokenPolicyTemplate, tokenName)
	err := vaultClient.Sys().PutPolicy(policyName, tokenPolicy)
	require.NoError(t, err)

	// Generate the token secret.
	token, err := uuid.GenerateUUID()
	require.NoError(t, err)

	// Create the replication token secret.
	logger.Logf(t, "Creating the %s token secret", tokenName)
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"token": token,
		},
	}
	_, err = vaultClient.Logical().Write(fmt.Sprintf("consul/data/secret/%s", tokenName), params)
	require.NoError(t, err)

	return token
}

// CreateConnectCAPolicy creates the Vault Policy for the connect-ca in a given datacenter.
func CreateConnectCAPolicy(t *testing.T, vaultClient *vapi.Client, datacenter string) {
	err := vaultClient.Sys().PutPolicy(
		fmt.Sprintf("connect-ca-%s", datacenter),
		fmt.Sprintf(connectCAPolicyTemplate, datacenter, datacenter))
	require.NoError(t, err)
}
