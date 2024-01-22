// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	terminatinggateway "github.com/hashicorp/consul-k8s/acceptance/tests/terminating-gateway"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPeering_Connect validates that service mesh works properly across two peered clusters.
// It deploys a static client in one cluster and a static server in another and checks that requests from the client
// can reach the server.
// It also deploys a static server pod that is not connected to the mesh, but added as a
// destination for a terminating gateway. It checks that requests from the client can reach this server through the
// terminating gateway via the mesh gateways.
func TestPeering_Connect(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	ver, err := version.NewVersion("1.13.0")
	require.NoError(t, err)
	if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
		t.Skipf("skipping this test because peering is not supported in version %v", cfg.ConsulVersion.String())
	}

	const staticServerPeer = "server"
	const staticClientPeer = "client"

	cases := []struct {
		name        string
		ACLsEnabled bool
	}{
		{
			"default installation",
			false,
		},
		{
			"secure installation",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			staticServerPeerClusterContext := env.DefaultContext(t)
			staticClientPeerClusterContext := env.Context(t, 1)

			// Create Clusters starting with our first cluster
			commonHelmValues := map[string]string{
				"global.peering.enabled": "true",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.ACLsEnabled),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsEnabled),

				"connectInject.enabled": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"dns.enabled":           "true",
				"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),
			}

			var wg sync.WaitGroup
			releaseName := helpers.RandomName()

			var staticServerPeerCluster *consul.HelmCluster
			wg.Add(1)
			go func() {
				defer wg.Done()
				staticServerPeerHelmValues := map[string]string{
					"global.datacenter":                        staticServerPeer,
					"terminatingGateways.enabled":              "true",
					"terminatingGateways.gateways[0].name":     "terminating-gateway",
					"terminatingGateways.gateways[0].replicas": "1",
				}

				if !cfg.UseKind {
					staticServerPeerHelmValues["server.replicas"] = "3"
				}

				// On Kind, there are no load balancers but since all clusters
				// share the same node network (docker bridge), we can use
				// a NodePort service so that we can access node(s) in a different Kind cluster.
				if cfg.UseKind {
					staticServerPeerHelmValues["server.exposeGossipAndRPCPorts"] = "true"
					staticServerPeerHelmValues["meshGateway.service.type"] = "NodePort"
					staticServerPeerHelmValues["meshGateway.service.nodePort"] = "30100"
				}

				helpers.MergeMaps(staticServerPeerHelmValues, commonHelmValues)

				// Install the first peer where static-server will be deployed in the static-server kubernetes context.
				staticServerPeerCluster = consul.NewHelmCluster(t, staticServerPeerHelmValues, staticServerPeerClusterContext, cfg, releaseName)
				staticServerPeerCluster.Create(t)
			}()

			var staticClientPeerCluster *consul.HelmCluster
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Create a second cluster to be peered with
				staticClientPeerHelmValues := map[string]string{
					"global.datacenter": staticClientPeer,
				}

				if !cfg.UseKind {
					staticClientPeerHelmValues["server.replicas"] = "3"
				}

				if cfg.UseKind {
					staticClientPeerHelmValues["server.exposeGossipAndRPCPorts"] = "true"
					staticClientPeerHelmValues["meshGateway.service.type"] = "NodePort"
					staticClientPeerHelmValues["meshGateway.service.nodePort"] = "30100"
				}

				helpers.MergeMaps(staticClientPeerHelmValues, commonHelmValues)

				// Install the second peer where static-client will be deployed in the static-client kubernetes context.
				staticClientPeerCluster = consul.NewHelmCluster(t, staticClientPeerHelmValues, staticClientPeerClusterContext, cfg, releaseName)
				staticClientPeerCluster.Create(t)
			}()

			// Wait for the clusters to start up
			logger.Log(t, "waiting for clusters to start up . . .")
			wg.Wait()

			// Create Mesh resource to use mesh gateways.
			logger.Log(t, "creating mesh config")
			kustomizeMeshDir := "../fixtures/bases/mesh-peering"

			k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeMeshDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeMeshDir)
			})

			k8s.KubectlApplyK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeMeshDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeMeshDir)
			})

			staticServerPeerClient, _ := staticServerPeerCluster.SetupConsulClient(t, c.ACLsEnabled)
			staticClientPeerClient, _ := staticClientPeerCluster.SetupConsulClient(t, c.ACLsEnabled)

			// Ensure mesh config entries are created in Consul.
			timer := &retry.Timer{Timeout: 1 * time.Minute, Wait: 1 * time.Second}
			retry.RunWith(timer, t, func(r *retry.R) {
				ceServer, _, err := staticServerPeerClient.ConfigEntries().Get(api.MeshConfig, "mesh", &api.QueryOptions{})
				require.NoError(r, err)
				configEntryServer, ok := ceServer.(*api.MeshConfigEntry)
				require.True(r, ok)
				require.Equal(r, configEntryServer.GetName(), "mesh")
				require.NoError(r, err)

				ceClient, _, err := staticClientPeerClient.ConfigEntries().Get(api.MeshConfig, "mesh", &api.QueryOptions{})
				require.NoError(r, err)
				configEntryClient, ok := ceClient.(*api.MeshConfigEntry)
				require.True(r, ok)
				require.Equal(r, configEntryClient.GetName(), "mesh")
				require.NoError(r, err)
			})

			// Create the peering acceptor on the client peer.
			k8s.KubectlApply(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDelete(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
			})

			// Ensure the secret is created.
			retry.RunWith(timer, t, func(r *retry.R) {
				acceptorSecretName, err := k8s.RunKubectlAndGetOutputE(r, staticClientPeerClusterContext.KubectlOptions(r), "get", "peeringacceptor", "server", "-o", "jsonpath={.status.secret.name}")
				require.NoError(r, err)
				require.NotEmpty(r, acceptorSecretName)
			})

			// Copy secret from client peer to server peer.
			k8s.CopySecret(t, staticClientPeerClusterContext, staticServerPeerClusterContext, "api-token")

			// Create the peering dialer on the server peer.
			k8s.KubectlApply(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-dialer.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "delete", "secret", "api-token")
				k8s.KubectlDelete(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-dialer.yaml")
			})

			staticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: staticServerPeerClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  staticServerPeerClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			staticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: staticClientPeerClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  staticClientPeerClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}

			logger.Logf(t, "creating namespace %s in server peer", staticServerNamespace)
			k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			logger.Logf(t, "creating namespace %s in client peer", staticClientNamespace)
			k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			// Create a ProxyDefaults resource to configure services to use the mesh gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"

			k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
			})

			k8s.KubectlApplyK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
			})

			logger.Log(t, "creating static-server in server peer")
			k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Log(t, "creating static-client deployments in client peer")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-peers/default")
			}
			// Check that both static-server and static-client have been injected and now have 2 containers.
			podList, err := staticServerPeerClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-server",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)

			podList, err = staticClientPeerClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=static-client",
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)

			// Make sure that services are registered in the correct namespace.
			// Server cluster.
			services, _, err := staticServerPeerClient.Catalog().Service(staticServerName, "", &api.QueryOptions{})
			require.NoError(t, err)
			require.Len(t, services, 1)

			// Client cluster.
			services, _, err = staticClientPeerClient.Catalog().Service(staticClientName, "", &api.QueryOptions{})
			require.NoError(t, err)
			require.Len(t, services, 1)

			logger.Log(t, "creating exported services")
			k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/default")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/default")
			})

			if c.ACLsEnabled {
				logger.Log(t, "checking that the connection is not successful because there's no allow intention")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, staticClientOpts, staticClientName, false,
						[]string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", fmt.Sprintf("curl: (6) Could not resolve host: static-server.virtual.%s.consul", staticServerPeer)},
						"", fmt.Sprintf("http://static-server.virtual.%s.consul", staticServerPeer))
				} else {
					k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, staticClientName, "http://localhost:1234")
				}

				intention := &api.ServiceIntentionsConfigEntry{
					Name: staticServerName,
					Kind: api.ServiceIntentions,
					Sources: []*api.SourceIntention{
						{
							Name:   staticClientName,
							Action: api.IntentionActionAllow,
							Peer:   staticClientPeer,
						},
					},
				}

				logger.Log(t, "creating intentions in server peer")
				_, _, err = staticServerPeerClient.ConfigEntries().Set(intention, &api.WriteOptions{})
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, fmt.Sprintf("http://static-server.virtual.%s.consul", staticServerPeer))
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")
			}

			// Check that requests can reach the external static-server from the static client cluster.
			if cfg.EnableTransparentProxy {
				const (
					externalServerK8sNamespace = "external"
					externalServerServiceName  = "static-server"
					externalServerHostnameID   = "static-server-hostname"
					terminatingGatewayRules    = `service_prefix "static-server" {policy = "write"}`
				)

				// Create the namespace for the "external" static server.
				externalServerOpts := &terratestk8s.KubectlOptions{
					ContextName: staticServerOpts.ContextName,
					ConfigPath:  staticServerOpts.ConfigPath,
					Namespace:   externalServerK8sNamespace,
				}
				logger.Logf(t, "creating namespace %s in server peer", externalServerK8sNamespace)
				k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "create", "ns", externalServerK8sNamespace)
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "delete", "ns", externalServerK8sNamespace)
				})

				// Create the external server in the server Kubernetes cluster, outside the mesh in the "external" namespace
				logger.Log(t, "creating static-server deployment in server peer outside of mesh")
				k8s.DeployKustomize(t, externalServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")

				// Prevent dialing the server directly through the sidecar.
				terminatinggateway.CreateMeshConfigEntry(t, staticServerPeerClient, "")
				terminatinggateway.CreateMeshConfigEntry(t, staticClientPeerClient, "")

				// Create the config entry for the terminating gateway
				terminatinggateway.CreateTerminatingGatewayConfigEntry(t, staticServerPeerClient, "", "", externalServerHostnameID)
				if c.ACLsEnabled {
					// Allow the terminating gateway write access to services prefixed with "static-server".
					terminatinggateway.UpdateTerminatingGatewayRole(t, staticServerPeerClient, terminatingGatewayRules)
				}

				// This is the URL that the static-client will use to dial the external static server in the server peer.
				externalServerHostnameURL := fmt.Sprintf("http://%s.virtual.%s.consul", externalServerHostnameID, staticServerPeer)

				// Register the external service.
				terminatinggateway.CreateServiceDefaultDestination(t, staticServerPeerClient, "", externalServerHostnameID, "http", 80, fmt.Sprintf("%s.%s", externalServerServiceName, externalServerK8sNamespace))
				// (t-eckert) this shouldn't be required but currently is with HTTP services. It works around a bug.
				helpers.RegisterExternalService(t, staticServerPeerClient, "", externalServerHostnameID, fmt.Sprintf("%s.%s", externalServerServiceName, externalServerK8sNamespace), 80)

				// Export the external service to the client peer.
				logger.Log(t, "creating exported external services")
				k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/external")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/external")
				})

				// If ACLs are enabled, test that deny intentions prevent connections.
				if c.ACLsEnabled {
					logger.Log(t, "testing intentions prevent connections through the terminating gateway")
					k8s.CheckStaticServerHTTPConnectionFailing(t, staticClientOpts, staticClientName, externalServerHostnameURL)

					logger.Log(t, "adding intentions to allow traffic from client ==> server")
					terminatinggateway.AddIntention(t, staticServerPeerClient, staticClientPeer, "", staticClientName, "", externalServerHostnameID)
				}

				// Test that we can make a call to the terminating gateway.
				logger.Log(t, "trying calls to terminating gateway")
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, externalServerHostnameURL)
			}
		})
	}
}
