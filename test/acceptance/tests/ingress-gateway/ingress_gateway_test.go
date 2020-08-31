package ingressgateway

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

// Test that ingress gateways work in a default installation and a secure installation.
func TestIngressGateway(t *testing.T) {
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
			false,
		},
		{
			true,
			true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"connectInject.enabled":                "true",
				"ingressGateways.enabled":              "true",
				"ingressGateways.gateways[0].name":     "ingress-gateway",
				"ingressGateways.gateways[0].replicas": "1",
			}
			if c.secure {
				helmValues["global.acls.manageSystemACLs"] = "true"
				helmValues["global.tls.enabled"] = "true"
				helmValues["global.tls.autoEncrypt"] = strconv.FormatBool(c.autoEncrypt)
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			t.Log("creating server")
			helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "fixtures/static-server.yaml")

			// We use a "bounce" pod so that we can make calls to the ingress gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			t.Log("creating bounce pod")
			helpers.Deploy(t, ctx.KubectlOptions(), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "fixtures/bounce.yaml")

			// With the cluster up, we can create our ingress-gateway config entry.
			t.Log("creating config entry")
			consulClient := consulCluster.SetupConsulClient(t, c.secure)

			// Create config entry
			created, _, err := consulClient.ConfigEntries().Set(&api.IngressGatewayConfigEntry{
				Kind: api.IngressGateway,
				Name: "ingress-gateway",
				Listeners: []api.IngressListener{
					{
						Port:     8080,
						Protocol: "tcp",
						Services: []api.IngressService{
							{
								Name: "static-server",
							},
						},
					},
				},
			}, nil)
			require.NoError(t, err)
			require.Equal(t, true, created, "config entry failed")

			k8sOptions := ctx.KubectlOptions()

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the ingress gateway up, we test that we can make a call to it
				// via the bounce pod. It should fail to connect with the
				// static-server pod because of intentions.
				t.Log("testing intentions prevent ingress")
				helpers.CheckStaticServerConnection(t, k8sOptions, false, "bounce", "-H", "Host: static-server.ingress.consul", fmt.Sprintf("http://%s-consul-ingress-gateway:8080/", releaseName))

				// Now we create the allow intention.
				t.Log("creating ingress-gateway => static-server intention")
				_, _, err = consulClient.Connect().IntentionCreate(&api.Intention{
					SourceName:      "ingress-gateway",
					DestinationName: "static-server",
					Action:          api.IntentionActionAllow,
				}, nil)
				require.NoError(t, err)
			}

			// Test that we can make a call to the ingress gateway
			// via the bounce pod. It should route to the static-server pod.
			t.Log("trying calls to ingress gateway")
			helpers.CheckStaticServerConnection(t, k8sOptions, true, "bounce", "-H", "Host: static-server.ingress.consul", fmt.Sprintf("http://%s-consul-ingress-gateway:8080/", releaseName))
		})
	}
}
