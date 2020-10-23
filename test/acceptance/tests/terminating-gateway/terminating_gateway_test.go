package terminatinggateway

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const staticClientName = "static-client"
const staticServerName = "static-server"

// Test that terminating gateways work in a default and secure installations.
func TestTerminatingGateway(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{
			false,
			false,
		},
		{
			true,
			true,
		},
		{
			true,
			true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"connectInject.enabled":                    "true",
				"terminatingGateways.enabled":              "true",
				"terminatingGateways.gateways[0].name":     "terminating-gateway",
				"terminatingGateways.gateways[0].replicas": "1",

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
				"global.tls.autoEncrypt":       strconv.FormatBool(c.autoEncrypt),
			}

			t.Log("creating consul cluster")
			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)

			// Deploy a static-server that will play the role of an external service.
			t.Log("creating static-server deployment")
			helpers.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/bases/static-server")

			// Once the cluster is up, register the external service, then create the config entry.
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Register the external service
			registerExternalService(t, consulClient, "")

			// If ACLs are enabled we need to update the token of the terminating gateway
			// with service:write permissions to the static-server service
			// so that it can can request Connect certificates for it.
			if c.secure {
				updateTerminatingGatewayToken(t, consulClient, staticServerPolicyRules)
			}

			// Create the config entry for the terminating gateway.
			createTerminatingGatewayConfigEntry(t, consulClient, "", "")

			// Deploy the static client
			t.Log("deploying static client")
			helpers.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the terminating gateway up, we test that we can make a call to it
				// via the static-server. It should fail to connect with the
				// static-server pod because of intentions.
				assertNoConnectionAndAddIntention(t, consulClient, ctx.KubectlOptions(t), "", "")
			}

			// Test that we can make a call to the terminating gateway.
			t.Log("trying calls to terminating gateway")
			helpers.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), staticClientName, "http://localhost:1234")
		})
	}
}

const staticServerPolicyRules = `service "static-server" {
  policy = "write"
}`

func registerExternalService(t *testing.T, consulClient *api.Client, namespace string) {
	address := staticServerName
	service := &api.AgentService{
		ID:      staticServerName,
		Service: staticServerName,
		Port:    80,
	}

	if namespace != "" {
		address = fmt.Sprintf("%s.%s", staticServerName, namespace)
		service.Namespace = namespace

		t.Logf("creating the %s namespace in Consul", namespace)
		_, _, err := consulClient.Namespaces().Create(&api.Namespace{
			Name: namespace,
		}, nil)
		require.NoError(t, err)
	}

	t.Log("registering the external service")
	_, err := consulClient.Catalog().Register(&api.CatalogRegistration{
		Node:     "legacy_node",
		Address:  address,
		NodeMeta: map[string]string{"external-node": "true", "external-probe": "true"},
		Service:  service,
	}, nil)
	require.NoError(t, err)
}

func updateTerminatingGatewayToken(t *testing.T, consulClient *api.Client, rules string) {
	// Create a write policy for the static-server.
	_, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:  "static-server-write-policy",
		Rules: rules,
	}, nil)
	require.NoError(t, err)

	// Get the terminating gateway token.
	tokens, _, err := consulClient.ACL().TokenList(nil)
	require.NoError(t, err)
	var termGwTokenID string
	for _, token := range tokens {
		if strings.Contains(token.Description, "terminating-gateway-terminating-gateway-token") {
			termGwTokenID = token.AccessorID
			break
		}
	}
	termGwToken, _, err := consulClient.ACL().TokenRead(termGwTokenID, nil)
	require.NoError(t, err)

	// Add policy to the token and update it
	termGwToken.Policies = append(termGwToken.Policies, &api.ACLTokenPolicyLink{Name: "static-server-write-policy"})
	_, _, err = consulClient.ACL().TokenUpdate(termGwToken, nil)
	require.NoError(t, err)
}

func createTerminatingGatewayConfigEntry(t *testing.T, consulClient *api.Client, gwNamespace, serviceNamespace string) {
	t.Log("creating config entry")

	if serviceNamespace != "" {
		t.Logf("creating the %s namespace in Consul", serviceNamespace)
		_, _, err := consulClient.Namespaces().Create(&api.Namespace{
			Name: serviceNamespace,
		}, nil)
		require.NoError(t, err)
	}

	configEntry := &api.TerminatingGatewayConfigEntry{
		Kind:      api.TerminatingGateway,
		Name:      "terminating-gateway",
		Namespace: gwNamespace,
		Services:  []api.LinkedService{{Name: staticServerName, Namespace: serviceNamespace}},
	}

	created, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
	require.NoError(t, err)
	require.True(t, created, "failed to create config entry")
}

func assertNoConnectionAndAddIntention(t *testing.T, consulClient *api.Client, k8sOptions *k8s.KubectlOptions, sourceNS, destinationNS string) {
	t.Log("testing intentions prevent connections through the terminating gateway")
	helpers.CheckStaticServerConnectionFailing(t, k8sOptions, staticClientName, "http://localhost:1234")

	t.Log("creating static-client => static-server intention")
	_, _, err := consulClient.Connect().IntentionCreate(&api.Intention{
		SourceName:      staticClientName,
		SourceNS:        sourceNS,
		DestinationName: staticServerName,
		DestinationNS:   destinationNS,
		Action:          api.IntentionActionAllow,
	}, nil)
	require.NoError(t, err)
}
