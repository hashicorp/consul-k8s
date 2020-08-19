package meshgateway

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that Connect and wan federation over mesh gateways work in a default installation
// i.e. without ACLs because TLS is required for WAN federation over mesh gateways
func TestMeshGatewayDefault(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	primaryContext := env.DefaultContext(t)
	secondaryContext := env.Context(t, framework.SecondaryContextName)

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
	primaryConsulCluster := framework.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
	primaryConsulCluster.Create(t)

	// Get the federation secret from the primary cluster and apply it to secondary cluster
	federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
	t.Logf("retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
	federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions().Namespace).Get(federationSecretName, metav1.GetOptions{})
	federationSecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions().Namespace).Create(federationSecret)
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
	secondaryConsulCluster := framework.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	// Verify federation between servers
	t.Log("verifying federation was successful")
	consulClient := primaryConsulCluster.SetupConsulClient(t, false)
	members, err := consulClient.Agent().Members(true)
	require.NoError(t, err)
	// Expect two consul servers
	require.Len(t, members, 2)

	// Check that we can connect services over the mesh gateways
	t.Log("creating static-server in dc2")
	helpers.Deploy(t, secondaryContext.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-server.yaml")

	t.Log("creating static-client in dc1")
	helpers.Deploy(t, primaryContext.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-client.yaml")

	t.Log("checking that connection is successful")
	helpers.CheckConnection(t,
		primaryContext.KubectlOptions(),
		"static-client",
		true,
		"http://localhost:1234")
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
			secondaryContext := env.Context(t, framework.SecondaryContextName)

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
			primaryConsulCluster := framework.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the federation secret from the primary cluster and apply it to secondary cluster
			federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
			t.Logf("retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
			federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions().Namespace).Get(federationSecretName, metav1.GetOptions{})
			require.NoError(t, err)
			federationSecret.ResourceVersion = ""
			_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions().Namespace).Create(federationSecret)
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
			secondaryConsulCluster := framework.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			// Verify federation between servers
			t.Log("verifying federation was successful")
			consulClient := primaryConsulCluster.SetupConsulClient(t, true)
			members, err := consulClient.Agent().Members(true)
			require.NoError(t, err)
			// Expect two consul servers
			require.Len(t, members, 2)

			// Check that we can connect services over the mesh gateways
			t.Log("creating static-server in dc2")
			helpers.Deploy(t, secondaryContext.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-server.yaml")

			t.Log("creating static-client in dc1")
			helpers.Deploy(t, primaryContext.KubectlOptions(), cfg.NoCleanupOnFailure, "fixtures/static-client.yaml")

			t.Log("creating intention")
			_, _, err = consulClient.Connect().IntentionCreate(&api.Intention{
				SourceName:      "static-client",
				DestinationName: "static-server",
				Action:          api.IntentionActionAllow,
			}, nil)
			require.NoError(t, err)

			t.Log("checking that connection is successful")
			helpers.CheckConnection(t,
				primaryContext.KubectlOptions(),
				"static-client",
				true,
				"http://localhost:1234")
		})
	}
}
