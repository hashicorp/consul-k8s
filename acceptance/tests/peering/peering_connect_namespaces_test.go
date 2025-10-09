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
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-version"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"
const staticServerName = "static-server"
const staticServerNamespace = "ns1"
const staticClientNamespace = "ns2"

// Test that Connect works in installations for X-Peers networking.
func TestPeering_ConnectNamespaces(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	ver, err := version.NewVersion("1.13.0")
	require.NoError(t, err)
	if cfg.ConsulVersion != nil && cfg.ConsulVersion.LessThan(ver) {
		t.Skipf("skipping this test because peering is not supported in version %v", cfg.ConsulVersion.String())
	}

	const staticServerPeer = "server"
	const staticClientPeer = "client"
	const defaultNamespace = "default"
	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		ACLsEnabled          bool
	}{
		{
			"default destination namespace",
			defaultNamespace,
			false,
			false,
		},
		{
			"single destination namespace",
			staticServerNamespace,
			false,
			false,
		},
		{
			"mirror k8s namespaces",
			staticServerNamespace,
			true,
			false,
		},
		{
			"default destination namespace; secure",
			defaultNamespace,
			false,
			true,
		},
		{
			"single destination namespace; secure",
			staticServerNamespace,
			false,
			true,
		},
		{
			"mirror k8s namespaces; secure",
			staticServerNamespace,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			staticServerPeerClusterContext := env.DefaultContext(t)
			staticClientPeerClusterContext := env.Context(t, 1)

			commonHelmValues := map[string]string{
				"global.peering.enabled":        "true",
				"global.enableConsulNamespaces": "true",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.ACLsEnabled),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsEnabled),

				"connectInject.enabled": "true",

				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"dns.enabled":           "true",
				"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),

				"global.dualStack.defaultEnabled": cfg.GetDualStack(),
			}

			var wg sync.WaitGroup
			releaseName := helpers.RandomName()

			var staticServerPeerCluster *consul.HelmCluster
			wg.Add(1)
			go func() {
				defer wg.Done()
				staticServerPeerHelmValues := map[string]string{
					"global.datacenter": staticServerPeer,
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
			timer := &retry.Timer{Timeout: 2 * time.Minute, Wait: 1 * time.Second}
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

			logger.Logf(t, "creating namespaces %s in server peer", staticServerNamespace)
			k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			logger.Logf(t, "creating namespaces %s in client peer", staticClientNamespace)
			k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			serverQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace}
			clientQueryOpts := &api.QueryOptions{Namespace: staticClientNamespace}

			if !c.mirrorK8S {
				serverQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
				clientQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
			}

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
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
				if c.destinationNamespace == defaultNamespace {
					k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-peers/default-namespace")
				} else {
					k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-peers/non-default-namespace")
				}
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
			// If mirroring is enabled, we expect services to be registered in the
			// Consul namespace with the same name as their source
			// Kubernetes namespace.
			// If a single destination namespace is set, we expect all services
			// to be registered in that destination Consul namespace.

			// Server cluster.
			services, _, err := staticServerPeerClient.Catalog().Service(staticServerName, "", serverQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			// Client cluster.
			services, _, err = staticClientPeerClient.Catalog().Service(staticClientName, "", clientQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			logger.Log(t, "creating exported services")
			if c.destinationNamespace == defaultNamespace {
				k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/default-namespace")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/default-namespace")
				})
			} else {
				k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/non-default-namespace")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/non-default-namespace")
				})
			}

			if c.ACLsEnabled {
				logger.Log(t, "checking that the connection is not successful because there's no allow intention")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, staticClientOpts, staticClientName, false,
						[]string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", fmt.Sprintf("curl: (6) Could not resolve host: static-server.virtual.%s.%s.consul", c.destinationNamespace, staticServerPeer)},
						"", fmt.Sprintf("http://static-server.virtual.%s.%s.consul", c.destinationNamespace, staticServerPeer))
				} else {
					k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, staticClientName, "http://localhost:1234")
				}

				intention := &api.ServiceIntentionsConfigEntry{
					Name:      staticServerName,
					Kind:      api.ServiceIntentions,
					Namespace: staticServerNamespace,
					Sources: []*api.SourceIntention{
						{
							Name:      staticClientName,
							Namespace: staticClientNamespace,
							Action:    api.IntentionActionAllow,
							Peer:      staticClientPeer,
						},
					},
				}

				// Set the destination namespace to be the same
				// unless mirrorK8S is true.
				if !c.mirrorK8S {
					intention.Namespace = c.destinationNamespace
					intention.Sources[0].Namespace = c.destinationNamespace
				}

				logger.Log(t, "creating intentions in server peer")
				_, _, err = staticServerPeerClient.ConfigEntries().Set(intention, &api.WriteOptions{})
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, fmt.Sprintf("http://static-server.virtual.%s.%s.consul", c.destinationNamespace, staticServerPeer))
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")
			}
		})
	}
}
