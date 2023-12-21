// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package segments

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
)

// TestSegments_MeshWithAgentfulClients is a simple test that verifies that
// the Consul service mesh can be configured to use segments with:
// - one cluster with an alpha segment configured on the servers.
// - clients enabled and joining the alpha segment.
// - static client can communicate with static server.
func TestSegments_MeshWithAgentfulClients(t *testing.T) {
	cases := map[string]struct {
		secure bool
	}{
		"not-secure": {secure: false},
		"secure":     {secure: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			if !cfg.EnableEnterprise {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}
			ctx := suite.Environment().DefaultContext(t)

			releaseName := helpers.RandomName()

			helmValues := map[string]string{
				"connectInject.enabled": "true",

				"server.replicas":    "3",
				"server.extraConfig": `"{\"segments\": [{\"name\":\"alpha1\"\,\"bind\":\"0.0.0.0\"\,\"port\":8303}]}"`,

				"client.enabled": "true",
				// need to configure clients to connect to port 8303 that the alpha segment was configured on rather than
				// the standard serf LAN port.
				"client.join[0]":     "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[1]":     "${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[2]":     "${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.extraConfig": `"{\"segment\": \"alpha1\"}"`,
			}

			connHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             ctx,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				HelmValues:      helmValues,
			}

			connHelper.Setup(t)

			connHelper.Install(t)
			connHelper.DeployClientAndServer(t)
			if c.secure {
				connHelper.TestConnectionFailureWithoutIntention(t)
				connHelper.CreateIntention(t)
			}

			connHelper.TestConnectionSuccess(t)
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}
