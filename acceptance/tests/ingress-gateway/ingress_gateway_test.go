// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ingressgateway

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

const StaticClientName = "static-client"

// Test that ingress gateways work in a default installation and a secure installation.
func TestIngressGateway(t *testing.T) {
	cases := []struct {
		secure bool
	}{
		{
			secure: false,
		},
		{
			secure: true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t", c.secure)
		t.Run(name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()
			igName := "ingress-gateway"
			helmValues := map[string]string{
				"connectInject.enabled":                "true",
				"ingressGateways.enabled":              "true",
				"ingressGateways.gateways[0].name":     igName,
				"ingressGateways.gateways[0].replicas": "1",

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
				"global.tls.enabled":           strconv.FormatBool(c.secure),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			logger.Log(t, "creating server")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			// We use the static-client pod so that we can make calls to the ingress gateway
			// via kubectl exec without needing a route into the cluster from the test machine.
			logger.Log(t, "creating static-client pod")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

			// With the cluster up, we can create our ingress-gateway config entry.
			logger.Log(t, "creating config entry")
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			// Create config entry
			created, _, err := consulClient.ConfigEntries().Set(&api.IngressGatewayConfigEntry{
				Kind: api.IngressGateway,
				Name: igName,
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

			k8sOptions := ctx.KubectlOptions(t)

			// If ACLs are enabled, test that intentions prevent connections.
			if c.secure {
				// With the ingress gateway up, we test that we can make a call to it
				// via the bounce pod. It should fail to connect with the
				// static-server pod because of intentions.
				logger.Log(t, "testing intentions prevent ingress")
				k8s.CheckStaticServerConnectionFailing(t, k8sOptions,
					StaticClientName, "-H", "Host: static-server.ingress.consul",
					fmt.Sprintf("http://%s-consul-%s:8080/", releaseName, igName))

				// Now we create the allow intention.
				logger.Log(t, "creating ingress-gateway => static-server intention")
				_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: "static-server",
					Sources: []*api.SourceIntention{
						{
							Name:   igName,
							Action: api.IntentionActionAllow,
						},
					},
				}, nil)
				require.NoError(t, err)
			}

			// Test that we can make a call to the ingress gateway
			// via the static-client pod. It should route to the static-server pod.
			logger.Log(t, "trying calls to ingress gateway")
			k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions,
				StaticClientName, "-H", "Host: static-server.ingress.consul",
				fmt.Sprintf("http://%s-consul-%s:8080/", releaseName, igName))
		})
	}
}
