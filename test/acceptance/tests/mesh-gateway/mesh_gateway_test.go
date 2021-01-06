package meshgateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"

// Test that Connect and wan federation over mesh gateways work in a default installation
// i.e. without ACLs because TLS is required for WAN federation over mesh gateways
func TestMeshGatewayDefault(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	primaryContext := env.DefaultContext(t)
	secondaryContext := env.Context(t, environment.SecondaryContextName)

	primaryHelmValues := map[string]string{
		"global.datacenter":                        "dc1",
		"global.tls.enabled":                       "true",
		"global.tls.httpsOnly":                     "false",
		"global.federation.enabled":                "true",
		"global.federation.createFederationSecret": "true",

		"connectInject.enabled": "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",
	}

	if cfg.UseKind {
		primaryHelmValues["meshGateway.service.type"] = "NodePort"
		primaryHelmValues["meshGateway.service.nodePort"] = "30000"
	}

	releaseName := helpers.RandomName()

	// Install the primary consul cluster in the default kubernetes context
	primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
	primaryConsulCluster.Create(t)

	// Get the federation secret from the primary cluster and apply it to secondary cluster
	federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
	logger.Logf(t, "retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
	federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(context.Background(), federationSecretName, metav1.GetOptions{})
	federationSecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(context.Background(), federationSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create secondary cluster
	secondaryHelmValues := map[string]string{
		"global.datacenter": "dc2",

		"global.tls.enabled":           "true",
		"global.tls.httpsOnly":         "false",
		"global.tls.caCert.secretName": federationSecretName,
		"global.tls.caCert.secretKey":  "caCert",
		"global.tls.caKey.secretName":  federationSecretName,
		"global.tls.caKey.secretKey":   "caKey",

		"global.federation.enabled": "true",

		"server.extraVolumes[0].type":          "secret",
		"server.extraVolumes[0].name":          federationSecretName,
		"server.extraVolumes[0].load":          "true",
		"server.extraVolumes[0].items[0].key":  "serverConfigJSON",
		"server.extraVolumes[0].items[0].path": "config.json",

		// Enterprise license job will fail if it runs in the secondary DC,
		// so we're explicitly setting these values to empty to avoid that.
		"server.enterpriseLicense.secretName": "",
		"server.enterpriseLicense.secretKey":  "",

		"connectInject.enabled": "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",
	}

	if cfg.UseKind {
		secondaryHelmValues["meshGateway.service.type"] = "NodePort"
		secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
	}

	// Install the secondary consul cluster in the secondary kubernetes context
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	primaryClient := primaryConsulCluster.SetupConsulClient(t, false)
	secondaryClient := secondaryConsulCluster.SetupConsulClient(t, false)

	// Verify federation between servers
	logger.Log(t, "verifying federation was successful")
	verifyFederation(t, primaryClient, secondaryClient, releaseName, false)

	// Log services in DC2 that DC1 is aware of before exiting this test
	// TODO: remove this code once issue has been debugged
	defer func() {
		svcs, _, err := primaryClient.Catalog().Services(&api.QueryOptions{Datacenter: "dc2"})
		if err != nil {
			logger.Logf(t, "error calling primary on /v1/catalog/services?dc=dc2: %s\n", err.Error())
			return
		}
		logger.Logf(t, "primary on /v1/catalog/services?dc=dc2: %+v\n", svcs)
	}()

	// Check that we can connect services over the mesh gateways
	logger.Log(t, "creating static-server in dc2")
	k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	logger.Log(t, "creating static-client in dc1")
	k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

	logger.Log(t, "checking that connection is successful")
	k8s.CheckStaticServerConnectionSuccessful(t, primaryContext.KubectlOptions(t), staticClientName, "http://localhost:1234")
}

// Test that Connect and wan federation over mesh gateways work in a secure installation,
// with ACLs and TLS with and without auto-encrypt enabled.
func TestMeshGatewaySecure(t *testing.T) {
	cases := []struct {
		name              string
		enableAutoEncrypt string
	}{
		{
			"with ACLs and TLS without auto-encrypt",
			"false",
		},
		{
			"with ACLs and auto-encrypt",
			"true",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()
			cfg := suite.Config()

			primaryContext := env.DefaultContext(t)
			secondaryContext := env.Context(t, environment.SecondaryContextName)

			primaryHelmValues := map[string]string{
				"global.datacenter":            "dc1",
				"global.tls.enabled":           "true",
				"global.tls.enableAutoEncrypt": c.enableAutoEncrypt,

				"global.acls.manageSystemACLs":       "true",
				"global.acls.createReplicationToken": "true",

				"global.federation.enabled":                "true",
				"global.federation.createFederationSecret": "true",

				"connectInject.enabled": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",
			}

			if cfg.UseKind {
				primaryHelmValues["meshGateway.service.type"] = "NodePort"
				primaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			releaseName := helpers.RandomName()

			// Install the primary consul cluster in the default kubernetes context
			primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the federation secret from the primary cluster and apply it to secondary cluster
			federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
			logger.Logf(t, "retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
			federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(context.Background(), federationSecretName, metav1.GetOptions{})
			require.NoError(t, err)
			federationSecret.ResourceVersion = ""
			_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(context.Background(), federationSecret, metav1.CreateOptions{})
			require.NoError(t, err)

			// Create secondary cluster
			secondaryHelmValues := map[string]string{
				"global.datacenter": "dc2",

				"global.tls.enabled":           "true",
				"global.tls.httpsOnly":         "false",
				"global.tls.enableAutoEncrypt": c.enableAutoEncrypt,
				"global.tls.caCert.secretName": federationSecretName,
				"global.tls.caCert.secretKey":  "caCert",
				"global.tls.caKey.secretName":  federationSecretName,
				"global.tls.caKey.secretKey":   "caKey",

				"global.acls.manageSystemACLs":            "true",
				"global.acls.replicationToken.secretName": federationSecretName,
				"global.acls.replicationToken.secretKey":  "replicationToken",

				"global.federation.enabled": "true",

				"server.extraVolumes[0].type":          "secret",
				"server.extraVolumes[0].name":          federationSecretName,
				"server.extraVolumes[0].load":          "true",
				"server.extraVolumes[0].items[0].key":  "serverConfigJSON",
				"server.extraVolumes[0].items[0].path": "config.json",

				// Enterprise license job will fail if it runs in the secondary DC,
				// so we're explicitly setting these values to empty to avoid that.
				"server.enterpriseLicense.secretName": "",
				"server.enterpriseLicense.secretKey":  "",

				"connectInject.enabled": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",
			}

			if cfg.UseKind {
				secondaryHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			// Install the secondary consul cluster in the secondary kubernetes context
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			primaryClient := primaryConsulCluster.SetupConsulClient(t, true)
			secondaryClient := secondaryConsulCluster.SetupConsulClient(t, true)

			// Verify federation between servers
			logger.Log(t, "verifying federation was successful")
			verifyFederation(t, primaryClient, secondaryClient, releaseName, true)

			// Check that we can connect services over the mesh gateways
			logger.Log(t, "creating static-server in dc2")
			k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Log(t, "creating static-client in dc1")
			k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

			logger.Log(t, "creating intention")
			_, _, err = primaryClient.Connect().IntentionCreate(&api.Intention{
				SourceName:      staticClientName,
				DestinationName: "static-server",
				Action:          api.IntentionActionAllow,
			}, nil)
			require.NoError(t, err)

			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, primaryContext.KubectlOptions(t), staticClientName, "http://localhost:1234")
		})
	}
}

// verifyFederation checks that the WAN federation between servers is successful
// by first checking members are alive from the perspective of both servers.
// If secure is true, it will also check that the ACL replication is running on the secondary server.
func verifyFederation(t *testing.T, primaryClient, secondaryClient *api.Client, releaseName string, secure bool) {
	retrier := &retry.Timer{Timeout: 5 * time.Minute, Wait: 1 * time.Second}
	start := time.Now()

	// Check that server in dc1 is healthy from the perspective of the server in dc2, and vice versa.
	// We're calling the Consul health API, as opposed to checking serf membership status,
	// because we need to make sure that the federated servers can make API calls and forward requests
	// from one server to another. From running tests in CI for a while and using serf membership status before,
	// we've noticed that the status could be "alive" as soon as the server in the secondary cluster joins the primary
	// and then switch to "failed". This would require us to check that the status is "alive" is showing consistently for
	// some amount of time, which could be quite flakey. Calling the API in another datacenter allows us to check that
	// each server can forward calls to another, which is what we need for connect.
	retry.RunWith(retrier, t, func(r *retry.R) {
		secondaryServerHealth, _, err := primaryClient.Health().Node(fmt.Sprintf("%s-consul-server-0", releaseName), &api.QueryOptions{Datacenter: "dc2"})
		require.NoError(r, err)
		require.Equal(r, secondaryServerHealth.AggregatedStatus(), api.HealthPassing)

		primaryServerHealth, _, err := secondaryClient.Health().Node(fmt.Sprintf("%s-consul-server-0", releaseName), &api.QueryOptions{Datacenter: "dc1"})
		require.NoError(r, err)
		require.Equal(r, primaryServerHealth.AggregatedStatus(), api.HealthPassing)

		if secure {
			replicationStatus, _, err := secondaryClient.ACL().Replication(nil)
			require.NoError(r, err)
			require.True(r, replicationStatus.Enabled)
			require.True(r, replicationStatus.Running)
		}
	})

	logger.Logf(t, "Took %s to verify federation", time.Since(start))
}
