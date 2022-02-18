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
	replicationTokenPolicy = `
path "consul/data/secret/replication" {
  capabilities = ["read", "update"]
}`

	enterpriseLicensePolicy = `
path "consul/data/secret/enterpriselicense" {
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
)

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

// configureGossipVaultSecret generates a gossip encryption key,
// stores it in vault as a secret and configures a policy to access it.
func configureGossipVaultSecret(t *testing.T, vaultClient *vapi.Client) string {
	// Create the Vault Policy for the gossip key.
	logger.Log(t, "Creating gossip policy")
	err := vaultClient.Sys().PutPolicy("consul-gossip", gossipPolicy)
	require.NoError(t, err)

	// Generate the gossip secret.
	gossipKey, err := generateGossipSecret()
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

// configureEnterpriseLicenseVaultSecret stores it in vault as a secret and configures a policy to access it.
func configureEnterpriseLicenseVaultSecret(t *testing.T, vaultClient *vapi.Client, cfg *config.TestConfig) {
	// Create the enterprise license secret.
	logger.Log(t, "Creating the Enterprise License secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"enterpriselicense": cfg.EnterpriseLicense,
		},
	}
	_, err := vaultClient.Logical().Write("consul/data/secret/enterpriselicense", params)
	require.NoError(t, err)

	// Create the Vault Policy for the consul-enterpriselicense.
	err = vaultClient.Sys().PutPolicy("consul-enterpriselicense", enterpriseLicensePolicy)
	require.NoError(t, err)
}

// configureKubernetesAuthRoles configures roles for the Kubernetes auth method
// that will be used by the test Helm chart installation.
func configureKubernetesAuthRoles(t *testing.T, vaultClient *vapi.Client, consulReleaseName, ns, authPath, datacenter string, cfg *config.TestConfig) {
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-client", consulReleaseName)
	consulServerServiceAccountName := fmt.Sprintf("%s-consul-server", consulReleaseName)
	sharedPolicies := "consul-gossip"
	if cfg.EnableEnterprise {
		sharedPolicies += ",consul-enterpriselicense"
	}

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
		"policies":                         sharedPolicies,
		"ttl":                              "24h",
	}
	_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/consul-client", authPath), params)
	require.NoError(t, err)

	params = map[string]interface{}{
		"bound_service_account_names":      consulServerServiceAccountName,
		"bound_service_account_namespaces": ns,
		"policies":                         fmt.Sprintf(sharedPolicies+",connect-ca-%s,consul-server-%s,consul-replication-token", datacenter, datacenter),
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/consul-server", authPath), params)
	require.NoError(t, err)

	// Create the CA role that all components will use to fetch the Server CA certs.
	params = map[string]interface{}{
		"bound_service_account_names":      "*",
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-ca",
		"ttl":                              "24h",
	}
	_, err = vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/consul-ca", authPath), params)
	require.NoError(t, err)
}

// configurePKICA generates a CA in Vault.
func configurePKICA(t *testing.T, vaultClient *vapi.Client) {
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

// configurePKICertificates configures roles so that Consul server TLS certificates
// can be issued by Vault.
func configurePKICertificates(t *testing.T, vaultClient *vapi.Client, consulReleaseName, ns, datacenter string) string {
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

	pkiRoleName := fmt.Sprintf("consul-server-%s", datacenter)

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

// configureReplicationTokenVaultSecret generates a replication token secret ID,
// stores it in vault as a secret and configures a policy to access it.
func configureReplicationTokenVaultSecret(t *testing.T, vaultClient *vapi.Client, consulReleaseName, ns string, authMethodPaths ...string) string {
	// Create the Vault Policy for the replication token.
	logger.Log(t, "Creating replication token policy")
	err := vaultClient.Sys().PutPolicy("consul-replication-token", replicationTokenPolicy)
	require.NoError(t, err)

	// Generate the token secret.
	token, err := uuid.GenerateUUID()
	require.NoError(t, err)

	// Create the replication token secret.
	logger.Log(t, "Creating the replication token secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			"replication": token,
		},
	}
	_, err = vaultClient.Logical().Write("consul/data/secret/replication", params)
	require.NoError(t, err)

	logger.Log(t, "Creating kubernetes auth role for the server-acl-init job")
	serverACLInitSAName := fmt.Sprintf("%s-consul-server-acl-init", consulReleaseName)
	params = map[string]interface{}{
		"bound_service_account_names":      serverACLInitSAName,
		"bound_service_account_namespaces": ns,
		"policies":                         "consul-replication-token",
		"ttl":                              "24h",
	}

	for _, authMethodPath := range authMethodPaths {
		_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/server-acl-init", authMethodPath), params)
		require.NoError(t, err)
	}

	return token
}

// createConnectCAPolicy creates the Vault Policy for the connect-ca in a given datacenter.
func createConnectCAPolicy(t *testing.T, vaultClient *vapi.Client, datacenter string) {
	err := vaultClient.Sys().PutPolicy(
		fmt.Sprintf("connect-ca-%s", datacenter),
		fmt.Sprintf(connectCAPolicyTemplate, datacenter, datacenter))
	require.NoError(t, err)
}
