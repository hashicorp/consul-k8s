package peering

import (
	"context"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test that Connect works in installations for X-Peers networking.
func TestPeering_Connect(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.EnableTransparentProxy {
		t.Skipf("skipping this test because Transparent Proxy is enabled")
	}

	const staticServerPeer = "server"
	const staticClientPeer = "client"
	cases := []struct {
		name                      string
		ACLsAndAutoEncryptEnabled bool
	}{
		{
			"default installation",
			false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			staticServerPeerClusterContext := env.DefaultContext(t)
			staticClientPeerClusterContext := env.Context(t, environment.SecondaryContextName)

			commonHelmValues := map[string]string{
				"global.peering.enabled": "true",

				"global.image": "hashicorp/consul:1.13.0-alpha2",

				"global.tls.enabled":           "false",
				"global.tls.httpsOnly":         strconv.FormatBool(c.ACLsAndAutoEncryptEnabled),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.ACLsAndAutoEncryptEnabled),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsAndAutoEncryptEnabled),

				"connectInject.enabled":                         "true",
				"connectInject.transparentProxy.defaultEnabled": "false",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"controller.enabled": "true",
			}

			staticServerPeerHelmValues := map[string]string{
				"global.datacenter": staticServerPeer,
			}

			// On Kind, there are no load balancers but since all clusters
			// share the same node network (docker bridge), we can use
			// a NodePort service so that we can access node(s) in a different Kind cluster.
			if cfg.UseKind {
				staticServerPeerHelmValues["server.exposeGossipAndRPCPorts"] = "true"
				staticServerPeerHelmValues["meshGateway.service.type"] = "NodePort"
				staticServerPeerHelmValues["meshGateway.service.nodePort"] = "30100"
			}

			releaseName := helpers.RandomName()

			helpers.MergeMaps(staticServerPeerHelmValues, commonHelmValues)

			// Install the first peer where static-server will be deployed in the static-server kubernetes context.
			staticServerPeerCluster := consul.NewHelmCluster(t, staticServerPeerHelmValues, staticServerPeerClusterContext, cfg, releaseName)
			staticServerPeerCluster.Create(t)

			staticClientPeerHelmValues := map[string]string{
				"global.datacenter": staticClientPeer,
			}

			if cfg.UseKind {
				staticClientPeerHelmValues["server.exposeGossipAndRPCPorts"] = "true"
				staticClientPeerHelmValues["meshGateway.service.type"] = "NodePort"
				staticClientPeerHelmValues["meshGateway.service.nodePort"] = "30100"
			}

			helpers.MergeMaps(staticClientPeerHelmValues, commonHelmValues)

			// Install the second peer where static-client will be deployed in the static-client kubernetes context.
			staticClientPeerCluster := consul.NewHelmCluster(t, staticClientPeerHelmValues, staticClientPeerClusterContext, cfg, releaseName)
			staticClientPeerCluster.Create(t)

			// Create the peering acceptor on the client peer.
			k8s.KubectlApply(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDelete(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
			})

			// Copy secret from client peer to server peer.
			k8s.CopySecret(t, staticClientPeerClusterContext, staticServerPeerClusterContext, "api-token")

			// Create the peering dialer on the server peer.
			k8s.KubectlApply(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-dialer.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
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
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, staticServerPeerClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			logger.Logf(t, "creating namespaces %s in client peer", staticClientNamespace)
			k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, staticClientPeerClusterContext.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			staticServerPeerClient, _ := staticServerPeerCluster.SetupConsulClient(t, c.ACLsAndAutoEncryptEnabled)
			staticClientPeerClient, _ := staticClientPeerCluster.SetupConsulClient(t, c.ACLsAndAutoEncryptEnabled)

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"

			k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
			})

			k8s.KubectlApplyK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
			})

			logger.Log(t, "creating static-server in server peer")
			k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Log(t, "creating static-client deployments in client peer")
			k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-peers/default")
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
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/default")
			})
			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")

			denyAllIntention := &api.ServiceIntentionsConfigEntry{
				Name: "*",
				Kind: api.ServiceIntentions,
				Sources: []*api.SourceIntention{
					{
						Name:   "*",
						Action: api.IntentionActionDeny,
						Peer:   staticClientPeer,
					},
				},
			}
			_, _, err = staticServerPeerClient.ConfigEntries().Set(denyAllIntention, &api.WriteOptions{})
			require.NoError(t, err)

			logger.Log(t, "checking that the connection is not successful because there's no allow intention")
			k8s.CheckStaticServerConnectionFailing(t, staticClientOpts, staticClientName, "http://localhost:1234")

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

			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, "http://localhost:1234")
		})
	}
}
