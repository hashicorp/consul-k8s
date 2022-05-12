package vault

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
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

// ConfigurePKICerts configures roles in Vault so
// that controller webhook TLS certificates can be issued by Vault.
func ConfigurePKICerts(t *testing.T,
	vaultClient *vapi.Client, baseUrl, allowedSubdomain, roleName, ns, datacenter,
	maxTTL string) string {
	allowedDomains := fmt.Sprintf("%s.consul,%s,%s.%s,%s.%s.svc", datacenter,
		allowedSubdomain, allowedSubdomain, ns, allowedSubdomain, ns)
	params := map[string]interface{}{
		"allowed_domains":    allowedDomains,
		"allow_bare_domains": "true",
		"allow_localhost":    "true",
		"allow_subdomains":   "true",
		"generate_lease":     "true",
		"max_ttl":            maxTTL,
	}

	_, err := vaultClient.Logical().Write(
		fmt.Sprintf("%s/roles/%s", baseUrl, roleName), params)
	require.NoError(t, err)

	certificateIssuePath := fmt.Sprintf("%s/issue/%s", baseUrl, roleName)
	policy := fmt.Sprintf(`
		path %q {
		capabilities = ["create", "update"]
		}`, certificateIssuePath)

	// Create the server policy.
	err = vaultClient.Sys().PutPolicy(roleName, policy)
	require.NoError(t, err)

	return certificateIssuePath
}

// ConfigurePKI generates a CA in Vault at a given path with a given policyName.
func ConfigurePKI(t *testing.T, vaultClient *vapi.Client, baseUrl, policyName, commonName string, skipMountPKIEngine bool) {
	if !skipMountPKIEngine {
		// Mount the PKI Secrets engine at the baseUrl.
		mountError := vaultClient.Sys().Mount(baseUrl, &vapi.MountInput{
			Type:   "pki",
			Config: vapi.MountConfigInput{},
		})
		require.NoError(t, mountError)
		// Create root CA to issue Consul server certificates and the `consul-server` PKI role.
		// See https://learn.hashicorp.com/tutorials/consul/vault-pki-consul-secure-tls.
		// Generate the root CA.
		params := map[string]interface{}{
			"common_name": commonName,
			"ttl":         "24h",
		}
		_, err := vaultClient.Logical().Write(fmt.Sprintf("%s/root/generate/internal", baseUrl), params)
		require.NoError(t, err)
	}
	policy := fmt.Sprintf(`path "%s/cert/ca" {
		capabilities = ["read"]
	  }`, baseUrl)
	err := vaultClient.Sys().PutPolicy(policyName, policy)
	require.NoError(t, err)
}

type KubernetesAuthRoleConfiguration struct {
	ServiceAccountName  string
	KubernetesNamespace string
	PolicyNames         string
	AuthMethodPath      string
	RoleName            string
}

// ConfigureKubernetesAuthRole configures a role in Vault for the component for the Kubernetes auth method
// that will be used by the test Helm chart installation.
func ConfigureK8SAuthRole(t *testing.T, vaultClient *vapi.Client, config *KubernetesAuthRoleConfiguration) {
	// Create the Auth Roles for the component.
	// Auth roles bind policies to Kubernetes service accounts, which
	// then enables the Vault agent init container to call 'vault login'
	// with the Kubernetes auth method to obtain a Vault token.
	// Please see https://www.vaultproject.io/docs/auth/kubernetes#configuration
	// for more details.
	logger.Logf(t, "Creating the %q", config.ServiceAccountName)
	params := map[string]interface{}{
		"bound_service_account_names":      config.ServiceAccountName,
		"bound_service_account_namespaces": config.KubernetesNamespace,
		"policies":                         config.PolicyNames,
		"ttl":                              "24h",
	}
	_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/%s/role/%s", config.AuthMethodPath, config.RoleName), params)
	require.NoError(t, err)
}

type PKIAndAuthRoleConfiguration struct {
	ServiceAccountName  string
	BaseURL             string
	PolicyName          string
	RoleName            string
	CommonName          string
	CAPath              string
	CertPath            string
	KubernetesNamespace string
	DataCenter          string
	MaxTTL              string
	AuthMethodPath      string
	AllowedSubdomain    string
	SkipMountPKIEngine  bool
}

func ConfigurePKIAndAuthRole(t *testing.T, vaultClient *vapi.Client, config *PKIAndAuthRoleConfiguration) {
	config.CAPath = fmt.Sprintf("%s/cert/ca", config.BaseURL)
	// Configure role with read access to <baseURL>/cert/ca
	ConfigurePKI(t, vaultClient, config.BaseURL, config.PolicyName,
		config.CommonName, config.SkipMountPKIEngine)
	// Configure role with create and update access to issue certs at
	// <baseURL>/issue/<roleName>
	config.CertPath = ConfigurePKICerts(t, vaultClient, config.BaseURL,
		config.AllowedSubdomain, config.PolicyName, config.KubernetesNamespace,
		config.DataCenter, config.MaxTTL)
	// Configure AuthMethodRole that will map the service account name
	// to the Vault role
	authMethodRoleConfig := &KubernetesAuthRoleConfiguration{
		ServiceAccountName:  config.ServiceAccountName,
		KubernetesNamespace: config.KubernetesNamespace,
		AuthMethodPath:      config.AuthMethodPath,
		RoleName:            config.RoleName,
		PolicyNames:         config.PolicyName,
	}
	ConfigureK8SAuthRole(t, vaultClient, authMethodRoleConfig)
}

type SaveVaultSecretConfiguration struct {
	Path       string
	Key        string
	PolicyName string
	Value      string
}

func SaveSecret(t *testing.T, vaultClient *vapi.Client, config *SaveVaultSecretConfiguration) {
	policy := fmt.Sprintf(`
	path "%s" {
	  capabilities = ["read"]
	}`, config.Path)
	// Create the Vault Policy for the gossip key.
	logger.Log(t, "Creating policy")
	err := vaultClient.Sys().PutPolicy(config.PolicyName, policy)
	require.NoError(t, err)

	// Create the gossip secret.
	logger.Log(t, "Creating the gossip secret")
	params := map[string]interface{}{
		"data": map[string]interface{}{
			config.Key: config.Value,
		},
	}
	_, err = vaultClient.Logical().Write(config.Path, params)
	require.NoError(t, err)
}

// CreateConnectCAPolicyForDatacenter creates the Vault Policy for the connect-ca in a given datacenter.
func CreateConnectCARootAndIntermediatePKIPolicy(t *testing.T, vaultClient *vapi.Client, policyName, rootPath, intermediatePath string) {
	// connectCAPolicy allows Consul to bootstrap all certificates for the service mesh in Vault.
	// Adapted from https://www.consul.io/docs/connect/ca/vault#consul-managed-pki-paths.
	connectCAPolicyTemplate := `
path "/sys/mounts" {
  capabilities = [ "read" ]
}
path "/sys/mounts/%s" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
path "/sys/mounts/%s" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
path "/%s/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
path "/%s/*" {
  capabilities = [ "create", "read", "update", "delete", "list" ]
}
`
	err := vaultClient.Sys().PutPolicy(
		policyName,
		fmt.Sprintf(connectCAPolicyTemplate, rootPath, intermediatePath, rootPath, intermediatePath))
	require.NoError(t, err)
}
