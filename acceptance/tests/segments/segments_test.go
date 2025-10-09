// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package segments

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
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
				"client.join[0]":                  "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[1]":                  "${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[2]":                  "${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.extraConfig":              `"{\"segment\": \"alpha1\"}"`,
				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
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

// TestSegments_MeshWithAgentfulClientsMultiCluster is a simple test that verifies that
// the Consul service mesh can be configured to use segments with:
// - one cluster with an alpha segment configured on the servers.
// - clients enabled on another cluster and joining the alpha segment.
// - static client can communicate with static server.
func TestSegments_MeshWithAgentfulClientsMultiCluster(t *testing.T) {
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
			releaseName := helpers.RandomName()

			// deploy server cluster
			serverClusterContext := suite.Environment().DefaultContext(t)
			serverClusterHelmValues := map[string]string{
				"connectInject.enabled": "true",

				"server.replicas":    "3",
				"server.extraConfig": `"{\"segments\": [{\"name\":\"alpha1\"\,\"bind\":\"0.0.0.0\"\,\"port\":8303}]}"`,

				"client.enabled":                  "true",
				"client.join[0]":                  "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[1]":                  "${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[2]":                  "${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.extraConfig":              `"{\"segment\": \"alpha1\"}"`,
				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
			}

			serverConnHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             serverClusterContext,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				HelmValues:      serverClusterHelmValues,
			}

			serverConnHelper.Setup(t)
			serverConnHelper.Install(t)
			serverConnHelper.DeployServer(t)

			// deploy client cluster
			clientClusterContext := suite.Environment().Context(t, 1)
			clientClusterHelmValues := map[string]string{
				"connectInject.enabled": "true",

				"server.enabled": "false",

				"client.enabled":                  "true",
				"client.join[0]":                  "${CONSUL_FULLNAME}-server-0.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[1]":                  "${CONSUL_FULLNAME}-server-1.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.join[2]":                  "${CONSUL_FULLNAME}-server-2.${CONSUL_FULLNAME}-server.${NAMESPACE}.svc:8303",
				"client.extraConfig":              `"{\"segment\": \"alpha1\"}"`,
				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
			}

			clientClusterConnHelper := connhelper.ConnectHelper{
				ClusterKind:     consul.Helm,
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             clientClusterContext,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				HelmValues:      clientClusterHelmValues,
			}

			clientClusterConnHelper.Setup(t)
			clientClusterConnHelper.Install(t)
			logger.Log(t, "creating static-client deployments in client cluster")
			opts := clientClusterConnHelper.KubectlOptsForApp(t)

			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, opts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, opts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
			}

			// Check that the static-client has been injected and now have 2 containers in client cluster.
			for _, labelSelector := range []string{"app=static-client"} {
				podList, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			//if c.secure {
			//	connHelper.TestConnectionFailureWithoutIntention(t, connhelper.ConnHelperOpts{})
			//	connHelper.CreateIntention(t, connhelper.IntentionOpts{})
			//}
			//
			//connHelper.TestConnectionSuccess(t, connhelper.ConnHelperOpts{})
			//connHelper.TestConnectionFailureWhenUnhealthy(t)
		})
	}
}
