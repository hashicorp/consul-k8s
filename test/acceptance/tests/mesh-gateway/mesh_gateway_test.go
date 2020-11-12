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

		"connectInject.enabled": "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",
	}

	// Install the secondary consul cluster in the secondary kubernetes context
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	primaryClient := primaryConsulCluster.SetupConsulClient(t, false)
	secondaryClient := secondaryConsulCluster.SetupConsulClient(t, false)

	// Verify federation between servers
	logger.Log(t, "verifying federation was successful")
	verifyFederation(t, primaryClient, secondaryClient, false)

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

				"connectInject.enabled": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",
			}

			// Install the secondary consul cluster in the secondary kubernetes context
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			primaryClient := primaryConsulCluster.SetupConsulClient(t, true)
			secondaryClient := secondaryConsulCluster.SetupConsulClient(t, true)

			// Verify federation between servers
			logger.Log(t, "verifying federation was successful")
			verifyFederation(t, primaryClient, secondaryClient, true)

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
func verifyFederation(t *testing.T, primaryClient, secondaryClient *api.Client, secure bool) {
	const consulMemberStatusAlive = 1

	retrier := &retry.Timer{Timeout: 1 * time.Minute, Wait: 1 * time.Second}

	retry.RunWith(retrier, t, func(r *retry.R) {
		members, err := primaryClient.Agent().Members(true)
		require.NoError(r, err)
		require.Len(r, members, 2)
		require.Equal(r, members[0].Status, consulMemberStatusAlive)
		require.Equal(r, members[1].Status, consulMemberStatusAlive)

		members, err = secondaryClient.Agent().Members(true)
		require.NoError(r, err)
		require.Len(r, members, 2)
		require.Equal(r, members[0].Status, consulMemberStatusAlive)
		require.Equal(r, members[1].Status, consulMemberStatusAlive)

		if secure {
			replicationStatus, _, err := secondaryClient.ACL().Replication(nil)
			require.NoError(r, err)
			require.True(r, replicationStatus.Enabled)
			require.True(r, replicationStatus.Running)
		}
	})
}
