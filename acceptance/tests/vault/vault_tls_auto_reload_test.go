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

	vault.ConfigureGossipVaultSecret(t, vaultClient)

	vault.CreateConnectCAPolicy(t, vaultClient, "dc1")
	if cfg.EnableEnterprise {
		vault.ConfigureEnterpriseLicenseVaultSecret(t, vaultClient, cfg)
	}

	bootstrapToken := vault.ConfigureACLTokenVaultSecret(t, vaultClient, "bootstrap")

	serverPolicies := "gossip,connect-ca-dc1,server-cert-dc1,bootstrap-token"
	if cfg.EnableEnterprise {
		serverPolicies += ",license"
	}
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server", serverPolicies)
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "client", "gossip")
	vault.ConfigureKubernetesAuthRole(t, vaultClient, consulReleaseName, ns, "kubernetes", "server-acl-init", "bootstrap-token")
	vault.ConfigureConsulCAKubernetesAuthRole(t, vaultClient, ns, "kubernetes")

	vault.ConfigurePKICA(t, vaultClient)

	// Initially tried toset the expiration to 5-20s to keep the test as short running as possible,
	// but at those levels, the pods would fail to start becuase the certs had expired and would throw errors.
	// 30s seconds seemed to consistently clear this issue and not have startup problems.
	// If trying to go lower, be sure to run this several times in CI to ensure that there are little issues.
	// If wanting to make this higher, there is no problem except for consideration of how long the test will
	// take to complete.
	expirationInSeconds := 30
	certPath := vault.ConfigurePKICertificates(t, vaultClient, consulReleaseName, ns, "dc1", fmt.Sprintf("%ds", expirationInSeconds))

	vaultCASecret := vault.CASecretName(vaultReleaseName)

	consulHelmValues := map[string]string{
		"client.grpc":                 "true",
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
		"global.gossipEncryption.secretName":    "consul/data/secret/gossip",
		"global.gossipEncryption.secretKey":     "gossip",

		"server.serverCert.secretName": certPath,
		"global.tls.caCert.secretName": "pki/cert/ca",
		"global.tls.enableAutoEncrypt": "true",
	}

	if cfg.EnableEnterprise {
		consulHelmValues["global.enterpriseLicense.secretName"] = "consul/data/secret/license"
		consulHelmValues["global.enterpriseLicense.secretKey"] = "license"
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
