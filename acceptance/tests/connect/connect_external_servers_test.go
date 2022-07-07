package connect

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject_ExternalServers tests that connect works when using external servers.
// It sets up an external Consul server in the same cluster but a different Helm installation
// and then treats this server as external.
func TestConnectInject_ExternalServers(t *testing.T) {
	cases := []bool{false, true}

	for _, secure := range cases {
		caseName := fmt.Sprintf("secure: %t", secure)
		t.Run(caseName, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			serverHelmValues := map[string]string{
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),
				"global.tls.enabled":           strconv.FormatBool(secure),
			}
			serverReleaseName := helpers.RandomName()
			consulServerCluster := consul.NewHelmCluster(t, serverHelmValues, ctx, cfg, serverReleaseName)

			consulServerCluster.Create(t)

			helmValues := map[string]string{
				"server.enabled":               "false",
				"global.acls.manageSystemACLs": strconv.FormatBool(secure),

				"global.tls.enabled": strconv.FormatBool(secure),

				"connectInject.enabled": "true",

				"externalServers.enabled":   "true",
				"externalServers.hosts[0]":  fmt.Sprintf("%s-consul-server", serverReleaseName),
				"externalServers.httpsPort": "8500",
			}

			if secure {
				helmValues["global.tls.caCert.secretName"] = fmt.Sprintf("%s-consul-ca-cert", serverReleaseName)
				helmValues["global.tls.caCert.secretKey"] = "tls.crt"
				helmValues["global.acls.bootstrapToken.secretName"] = fmt.Sprintf("%s-consul-bootstrap-acl-token", serverReleaseName)
				helmValues["global.acls.bootstrapToken.secretKey"] = "token"
				helmValues["externalServers.httpsPort"] = "8501"
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.SkipCheckForPreviousInstallations = true

			consulCluster.Create(t)

			logger.Log(t, "creating static-server and static-client deployments")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
			}

			// Check that both static-server and static-client have been injected and now have 2 containers.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := ctx.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			if secure {
				consulClient, _ := consulServerCluster.SetupConsulClient(t, true)

				logger.Log(t, "checking that the connection is not successful because there's no intention")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), StaticClientName, "http://static-server")
				} else {
					k8s.CheckStaticServerConnectionFailing(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
				}

				intention := &api.ServiceIntentionsConfigEntry{
					Kind: api.ServiceIntentions,
					Name: staticServerName,
					Sources: []*api.SourceIntention{
						{
							Name:   StaticClientName,
							Action: api.IntentionActionAllow,
						},
					},
				}

				logger.Log(t, "creating intention")
				_, _, err := consulClient.ConfigEntries().Set(intention, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), StaticClientName, "http://static-server")
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), StaticClientName, "http://localhost:1234")
			}

			// Test that kubernetes readiness status is synced to Consul.
			// Create the file so that the readiness probe of the static-server pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			logger.Log(t, "checking that connection is unsuccessful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server port 80: Connection refused"}, "", "http://static-server.%s")
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, ctx.KubectlOptions(t), StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
			}
		})
	}
}
