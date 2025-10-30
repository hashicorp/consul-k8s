// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"context"
	"fmt"
	"net"
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
	"k8s.io/apimachinery/pkg/types"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestPeering_Gateway(t *testing.T) {
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

	staticServerPeerClusterContext := env.DefaultContext(t)
	staticClientPeerClusterContext := env.Context(t, 1)

	commonHelmValues := map[string]string{
		"global.peering.enabled":        "true",
		"global.enableConsulNamespaces": "true",

		"global.tls.enabled":   "true",
		"global.tls.httpsOnly": "true",

		"global.acls.manageSystemACLs": "true",

		"connectInject.enabled": "true",

		// When mirroringK8S is set, this setting is ignored.
		"connectInject.consulNamespaces.mirroringK8S": "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",

		"dns.enabled": "true",
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

	staticServerPeerClient, _ := staticServerPeerCluster.SetupConsulClient(t, true)
	staticClientPeerClient, _ := staticClientPeerCluster.SetupConsulClient(t, true)

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

		// If the secret name is empty, retry recreating the peering acceptor up to 5 times
		if acceptorSecretName == "" {
			const maxRetries = 5
			for attempt := 1; attempt <= maxRetries; attempt++ {
				logger.Log(t, fmt.Sprintf("peering acceptor secret name is empty, recreating peering acceptor (attempt %d/%d)", attempt, maxRetries))
				k8s.KubectlDelete(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")

				time.Sleep(5 * time.Second)

				k8s.KubectlApply(t, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")

				time.Sleep(10 * time.Second)

				acceptorSecretName, err = k8s.RunKubectlAndGetOutputE(r, staticClientPeerClusterContext.KubectlOptions(r), "get", "peeringacceptor", "server", "-o", "jsonpath={.status.secret.name}")
				require.NoError(r, err)

				if acceptorSecretName != "" {
					logger.Log(t, fmt.Sprintf("peering acceptor secret name successfully created after %d attempts", attempt))
					break
				}

				if attempt == maxRetries {
					logger.Log(t, fmt.Sprintf("peering acceptor secret name still empty after %d attempts", maxRetries))
				}
			}
		}

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

	// Create a ProxyDefaults resource to configure services to use the mesh
	// gateways.
	logger.Log(t, "creating proxy-defaults config")
	kustomizeDir := "../fixtures/cases/api-gateways/mesh"

	k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), kustomizeDir)
	})

	k8s.KubectlApplyK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, staticClientPeerClusterContext.KubectlOptions(t), kustomizeDir)
	})

	// We use the static-client pod so that we can make calls to the api gateway
	// via kubectl exec without needing a route into the cluster from the test machine.
	// Since we're deploying the gateway in the secondary cluster, we create the static client
	// in the secondary as well.
	logger.Log(t, "creating static-client pod in client peer")
	k8s.DeployKustomize(t, staticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-peers/non-default-namespace")

	logger.Log(t, "creating static-server in server peer")
	k8s.DeployKustomize(t, staticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	logger.Log(t, "creating exported services")
	k8s.KubectlApplyK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/non-default-namespace")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, staticServerPeerClusterContext.KubectlOptions(t), "../fixtures/cases/crd-peers/non-default-namespace")
	})

	// Create certificate secret, we do this separately since
	// applying the secret will make an invalid certificate that breaks other tests
	logger.Log(t, "creating certificate secret")
	out, err := k8s.RunKubectlAndGetOutputE(t, staticClientOpts, "apply", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, staticClientOpts, "delete", "-f", "../fixtures/bases/api-gateway/certificate.yaml")
	})

	logger.Log(t, "creating api-gateway resources in client peer")
	out, err = k8s.RunKubectlAndGetOutputE(t, staticClientOpts, "apply", "-k", "../fixtures/bases/api-gateway")
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		// Ignore errors here because if the test ran as expected
		// the custom resources will have been deleted.
		k8s.RunKubectlAndGetOutputE(t, staticClientOpts, "delete", "-k", "../fixtures/bases/api-gateway")
	})

	// Grab a kubernetes client so that we can verify binding
	// behavior prior to issuing requests through the gateway.
	k8sClient := staticClientPeerClusterContext.ControllerRuntimeClient(t)

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 1m timeout here).
	var gatewayAddress string
	counter := &retry.Counter{Count: 10, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: staticClientNamespace}, &gateway)
		require.NoError(r, err)

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value
	})

	targetAddress := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))

	logger.Log(t, "creating local service resolver")
	k8s.KubectlApplyK(t, staticClientOpts, "../fixtures/cases/api-gateways/peer-resolver")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, staticClientOpts, "../fixtures/cases/api-gateways/peer-resolver")
	})

	// Wait for the httproute to be created before patching
	logger.Log(t, "waiting for httproute to be created")
	k8s.RunKubectl(t, staticClientOpts, "wait", "--for=jsonpath='{.metadata.name}'=http-route", "httproute", "http-route", "--timeout=60s")

	logger.Log(t, "patching route to target server")
	k8s.RunKubectl(t, staticClientOpts, "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

	logger.Log(t, "checking that the connection is not successful because there's no intention")
	k8s.CheckStaticServerHTTPConnectionFailing(t, staticClientOpts, staticClientName, targetAddress)

	intention := &api.ServiceIntentionsConfigEntry{
		Kind:      api.ServiceIntentions,
		Name:      staticServerName,
		Namespace: staticServerNamespace,
		Sources: []*api.SourceIntention{
			{
				Name:      "gateway",
				Namespace: staticClientNamespace,
				Action:    api.IntentionActionAllow,
				Peer:      staticClientPeer,
			},
		},
	}

	logger.Log(t, "creating intention")
	_, _, err = staticServerPeerClient.ConfigEntries().Set(intention, &api.WriteOptions{})
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, err = staticServerPeerClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{})
		require.NoError(t, err)
	})

	logger.Log(t, "checking that connection is successful")
	k8s.CheckStaticServerConnectionSuccessful(t, staticClientOpts, staticClientName, targetAddress)
}
