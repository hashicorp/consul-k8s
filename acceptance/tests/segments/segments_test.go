// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package segments

import (
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
)

// Test that Connect works in a default and ACLsEnabled installations for X-Partition and in-partition networking.
func TestSegments_Mesh(t *testing.T) {
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

				"controller.enabled": "true",

				"server.replicas":    "1",
				"server.extraConfig": `"{\"segments\": [{\"name\":\"alpha1\"\,\"bind\":\"0.0.0.0\"\,\"port\":8303}]}"`,

				"client.enabled": "true",
				"client.join[0]": "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",

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
				connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
				connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}
