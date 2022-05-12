package vault

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/vault"
	"github.com/hashicorp/go-uuid"
	"github.com/stretchr/testify/require"
)

// TestVault_TlsAutoReload installs Vault, bootstraps it with secrets, policies, and Kube Auth Method.
// It then gets certs for https and rpc on the server. It then waits for the certs to rotate and checks
// that certs have different expirations.
func TestVault_TlsAutoReload(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	ns := ctx.KubectlOptions(t).Namespace

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

	// Initially tried toset the expiration to 5-20s to keep the test as short running as possible,
	// but at those levels, the pods would fail to start becuase the certs had expired and would throw errors.
	// 30s seconds seemed to consistently clear this issue and not have startup problems.
	// If trying to go lower, be sure to run this several times in CI to ensure that there are little issues.
	// If wanting to make this higher, there is no problem except for consideration of how long the test will
	// take to complete.
	expirationInSeconds := 30
	//Configure Server PKI
	serverPKIConfig := &vault.PKIAndAuthRoleConfiguration{
		BaseURL:             "pki",
		PolicyName:          "consul-ca-policy",
		RoleName:            "consul-ca-role",
		KubernetesNamespace: ns,
		DataCenter:          "dc1",
		ServiceAccountName:  fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		AllowedSubdomain:    fmt.Sprintf("%s-consul-%s", consulReleaseName, "server"),
		MaxTTL:              fmt.Sprintf("%ds", expirationInSeconds),
		AuthMethodPath:      "kubernetes",
	}
	vault.ConfigurePKIAndAuthRole(t, vaultClient, serverPKIConfig)

	// -------------------------
	// KV2 secrets
	// -------------------------
	// Gossip key
	gossipKey, err := vault.GenerateGossipSecret()
	require.NoError(t, err)
	gossipSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/gossip",
		Key:        "gossip",
		Value:      gossipKey,
		PolicyName: "gossip",
	}
	vault.SaveSecret(t, vaultClient, gossipSecret)

	// License
	licenseSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/license",
		Key:        "license",
		Value:      cfg.EnterpriseLicense,
		PolicyName: "license",
	}
	if cfg.EnableEnterprise {
		vault.SaveSecret(t, vaultClient, licenseSecret)
	}

	// Bootstrap Token
	bootstrapToken, err := uuid.GenerateUUID()
	require.NoError(t, err)
	bootstrapTokenSecret := &vault.SaveVaultSecretConfiguration{
		Path:       "consul/data/secret/bootstrap",
		Key:        "token",
		Value:      bootstrapToken,
		PolicyName: "bootstrap",
	}
	vault.SaveSecret(t, vaultClient, bootstrapTokenSecret)

	// -------------------------
	// Additional Auth Roles
	// -------------------------
	serverPolicies := fmt.Sprintf("%s,%s,%s,%s", gossipSecret.PolicyName, connectCAPolicy, serverPKIConfig.PolicyName, bootstrapTokenSecret.PolicyName)
	if cfg.EnableEnterprise {
		serverPolicies += fmt.Sprintf(",%s", licenseSecret.PolicyName)
	}

	// server
	consulServerRole := "server"
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  serverPKIConfig.ServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulServerRole,
		PolicyNames:         serverPolicies,
	})

	// client
	consulClientRole := "client"
	consulClientServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "client")
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  consulClientServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            consulClientRole,
		PolicyNames:         gossipSecret.PolicyName,
	})

	// manageSystemACLs
	manageSystemACLsRole := "server-acl-init"
	manageSystemACLsServiceAccountName := fmt.Sprintf("%s-consul-%s", consulReleaseName, "server-acl-init")
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  manageSystemACLsServiceAccountName,
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            manageSystemACLsRole,
		PolicyNames:         bootstrapTokenSecret.PolicyName,
	})

	// allow all components to access server ca
	vault.ConfigureK8SAuthRole(t, vaultClient, &vault.KubernetesAuthRoleConfiguration{
		ServiceAccountName:  "*",
		KubernetesNamespace: ns,
		AuthMethodPath:      "kubernetes",
		RoleName:            serverPKIConfig.RoleName,
		PolicyNames:         serverPKIConfig.PolicyName,
	})

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
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = licenseSecret.Path
		consulHelmValues["global.enterpriseLicense.secretKey"] = licenseSecret.Key
	}

	logger.Log(t, "Installing Consul")
	consulCluster := consul.NewHelmCluster(t, consulHelmValues, ctx, cfg, consulReleaseName)
	consulCluster.Create(t)

	// Validate that the gossip encryption key is set correctly.
	logger.Log(t, "Validating the gossip key has been set correctly.")
	consulCluster.ACLToken = bootstrapToken
	_, httpsAddress := consulCluster.SetupConsulClient(t, true)
	rpcAddress := consulCluster.CreatePortForwardTunnel(t, 8300)

	// here we can verify that the cert expiry changed
	httpsCert, err := getCertificate(t, httpsAddress)
	require.NoError(t, err)
	logger.Logf(t, "HTTPS expiry: %s \n", httpsCert.NotAfter.String())

	rpcCert, err := getCertificate(t, rpcAddress)
	require.NoError(t, err)
	logger.Logf(t, "RPC expiry: %s \n", rpcCert.NotAfter.String())

	// Validate that consul sever is running correctly and the consul members command works
	logger.Log(t, "Confirming that we can run Consul commands when exec'ing into server container")
	membersOutput, err := k8s.RunKubectlAndGetOutputWithLoggerE(t, ctx.KubectlOptions(t), terratestLogger.Discard, "exec", fmt.Sprintf("%s-consul-server-0", consulReleaseName), "-c", "consul", "--", "sh", "-c", fmt.Sprintf("CONSUL_HTTP_TOKEN=%s consul members", bootstrapToken))
	logger.Logf(t, "Members: \n%s", membersOutput)
	require.NoError(t, err)
	require.Contains(t, membersOutput, fmt.Sprintf("%s-consul-server-0", consulReleaseName))

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
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "http://static-server")
	} else {
		k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "http://localhost:1234")
	}

	logger.Logf(t, "Wait %d seconds for certificates to rotate....", expirationInSeconds)
	time.Sleep(time.Duration(expirationInSeconds) * time.Second)

	httpsCert2, err := getCertificate(t, httpsAddress)
	require.NoError(t, err)
	logger.Logf(t, "HTTPS 2 expiry: %s \n", httpsCert2.NotAfter.String())

	rpcCert2, err := getCertificate(t, rpcAddress)
	require.NoError(t, err)
	logger.Logf(t, "RPC 2 expiry: %s \n", rpcCert2.NotAfter.String())

	// verify that a previous cert expired and that a new one has been issued
	// by comparing the NotAfter on the two certs.
	require.NotEqual(t, httpsCert.NotAfter, httpsCert2.NotAfter)
	require.NotEqual(t, rpcCert.NotAfter, rpcCert2.NotAfter)

}

func getCertificate(t *testing.T, address string) (*x509.Certificate, error) {
	logger.Log(t, "Checking TLS....")
	conf := &tls.Config{
		InsecureSkipVerify: true,
	}

	logger.Logf(t, "Dialing %s", address)
	conn, err := tls.Dial("tcp", address, conf)
	if err != nil {
		logger.Log(t, "Error in Dial", err)
		return nil, err
	}
	defer conn.Close()

	connState := conn.ConnectionState()
	logger.Logf(t, "Connection State: %+v", connState)
	cert := connState.PeerCertificates[0]
	return cert, nil
}
