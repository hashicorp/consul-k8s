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
		helpers.EnsurePeeringAcceptorSecret(t, r, staticClientPeerClusterContext.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
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

	logger.Log(t, "check if exported service config entry exists in server peer")
	timer = &retry.Timer{Timeout: 1 * time.Minute, Wait: 5 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		ceServer, _, err := staticServerPeerClient.ConfigEntries().Get(api.ExportedServices, "default", &api.QueryOptions{})
		require.NoError(r, err)
		configEntryServer, ok := ceServer.(*api.ExportedServicesConfigEntry)
		logger.Log(t, "Exported service config entry: ", configEntryServer)
		require.True(r, ok)
		require.Equal(r, configEntryServer.GetName(), "default")
		require.NoError(r, err)
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
	// Apply api-gateway resources with retry logic to handle intermittent failures
	retry.Run(t, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, staticClientOpts, "apply", "-k", "../fixtures/bases/api-gateway")
		require.NoError(r, err, out)
	})
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

		logger.Log(t, "gateway found: \n%s", gateway)
		logger.Log(t, "gateway specs: \n%s", gateway.Spec)
		logger.Log(t, "gateway status(condtion, address, listners): \n%s", gateway.Status)
		logger.Log(t, "gateway address lists: \n%s", gateway.Status.Addresses)
	})

	targetAddress := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))
	logger.Log(t, "target address for gateway requests: %s", targetAddress)

	logger.Log(t, "creating local service resolver")
	k8s.KubectlApplyK(t, staticClientOpts, "../fixtures/cases/api-gateways/peer-resolver")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, staticClientOpts, "../fixtures/cases/api-gateways/peer-resolver")
	})

	// Wait for the httproute to exist before patching, with delete/recreate fallback
	logger.Log(t, "checking for http route")
	helpers.WaitForHTTPRouteWithRetry(t, staticClientOpts, "http-route", "../fixtures/bases/api-gateway")

	logger.Log(t, "patching route to target server")
	k8s.RunKubectl(t, staticClientOpts, "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

	logger.Log(t, "verifying httproute patch")
	retry.RunWith(&retry.Counter{Count: 10, Wait: 1 * time.Second}, t, func(r *retry.R) {
		var route gwv1beta1.HTTPRoute
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "http-route", Namespace: staticClientNamespace}, &route)
		require.NoError(r, err)
		logger.Log(t, "gateway httproute details after patch:\n%s", route)
		require.Len(r, route.Spec.Rules, 1, "expected one rule after patching")
		require.Len(r, route.Spec.Rules[0].BackendRefs, 1, "expected one backendRef after patching")
		require.Equal(r, "mesh-service", string(route.Spec.Rules[0].BackendRefs[0].Name))

		httproute, err := k8s.RunKubectlAndGetOutputE(t, staticClientOpts,
			"get", "httproute", "http-route",
			"-o", "yaml",
		)
		require.NoError(r, err)
		logger.Logf(t, "httproute details after patch:\n%s", httproute)
	})
	logger.Log(t, "httproute patch verified")

	logger.Log(t, "verifying service-resolver config entry exists in client peer")
	retry.RunWith(&retry.Timer{Timeout: 1 * time.Minute, Wait: 2 * time.Second}, t, func(r *retry.R) {
		ce, _, err := staticClientPeerClient.ConfigEntries().Get(api.ServiceResolver, "static-server", &api.QueryOptions{Namespace: staticClientNamespace})
		require.NoError(r, err, "error getting service-resolver config entry")
		require.NotNil(r, ce, "service-resolver config entry should not be nil")

		serviceResolverConfig, ok := ce.(*api.ServiceResolverConfigEntry)
		require.True(r, ok, "config entry is not a service-resolver")
		logger.Logf(t, "service-resolver config entry: %+v", serviceResolverConfig)
		require.Equal(r, "static-server", serviceResolverConfig.Name, "service-resolver name mismatch")
	})
	logger.Log(t, "service-resolver config entry verified")

	// logger.Log(t, "verifying api-gateway pod is ready in client peer")
	// k8s.RunKubectl(t, staticClientOpts, "wait", "--for=condition=Ready", "pod", "-l", "consul.hashicorp.com/gateway-name=gateway", "--timeout=2m")
	// logger.Log(t, "api-gateway pod is ready")

	logger.Log(t, "waiting for api-gateway pod to be created...")
	time.Sleep(10 * time.Second)

	logger.Log(t, "verifying api-gateway pod is ready in client peer")
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 15 * time.Second}, t, func(r *retry.R) {

		// List all pods and their labels in the namespace for debugging.
		podsWithLabels, _ := k8s.RunKubectlAndGetOutputE(r, staticClientOpts, "get", "pods", "--show-labels")
		logger.Logf(r, "Current pods in namespace '%s':\n%s", staticClientOpts.Namespace, podsWithLabels)

		// Try to wait for a short period.
		out, err := k8s.RunKubectlAndGetOutputE(r, staticClientOpts, "wait", "--for=condition=Ready", "pod", "-l", "consul.hashicorp.com/gateway-name=gateway", "--timeout=15s")

		// If the wait fails, get descriptive information for debugging then fail the retry.
		if err != nil {
			logger.Log(r, "api-gateway pod not ready, getting description and events...")
			podName, podErr := k8s.RunKubectlAndGetOutputE(r, staticClientOpts, "get", "pod", "-l", "consul.hashicorp.com/gateway-name=gateway", "-o", "jsonpath={.items[0].metadata.name}")
			if podErr == nil && podName != "" {
				describeOut, _ := k8s.RunKubectlAndGetOutputE(r, staticClientOpts, "describe", "pod", podName)
				logger.Logf(r, "Description of api-gateway pod '%s':\n%s", podName, describeOut)
			}
		}
		// Use require.NoError which will fail the current retry attempt without stopping the whole test.
		require.NoError(r, err, "api-gateway pod not ready yet: %s", out)
		logger.Log(t, "api-gw details", out)
	})
	logger.Log(t, "api-gateway pod is ready")

	logger.Log(t, "verifying mesh-gateway pods are ready in both peers")
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 10 * time.Second}, t, func(r *retry.R) {
		_, err := k8s.RunKubectlAndGetOutputE(r, staticServerPeerClusterContext.KubectlOptions(t), "wait", "--for=condition=Ready", "pod", "-l", "app=consul,component=mesh-gateway", "--timeout=10s")
		require.NoError(r, err, "server peer mesh-gateway not ready")
	})
	retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: 10 * time.Second}, t, func(r *retry.R) {
		_, err := k8s.RunKubectlAndGetOutputE(r, staticClientPeerClusterContext.KubectlOptions(t), "wait", "--for=condition=Ready", "pod", "-l", "app=consul,component=mesh-gateway", "--timeout=10s")
		require.NoError(r, err, "client peer mesh-gateway not ready")
	})
	logger.Log(t, "mesh-gateway pods are ready")

	logger.Log(t, "checking catalog services in client:")
	logger.Log(t, "sleeping 10s before querying for service")
	time.Sleep(10 * time.Second)
	retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 10}, t, func(r *retry.R) {
		services, _, err := staticClientPeerClient.Catalog().Service(
			staticServerName,
			"",
			&api.QueryOptions{
				Namespace:  staticServerNamespace,
				Peer:       staticServerPeer, // ask for service from server peer
				Datacenter: staticClientPeer, // local dc context
			},
		)
		// require.NoError(r, err, "error querying catalog services in client for peer %q service %q", staticServerPeer, staticServerName)
		// require.Greater(r, len(services), 0, "no services found in client catalog")
		if err != nil {
			logger.Logf(t, "error querying catalog services in client for peer %q service %q: %v", staticServerPeer, staticServerName, err)
		} else {
			logger.Logf(t, "found %d service", len(services))
			for i, s := range services {
				logger.Logf(t, "[%d] ServiceName=%s ID=%s Namespace=%s Address=%s Port=%d Meta=%v",
					i, s.ServiceName, s.ServiceID, s.Namespace, s.Address, s.ServicePort, s.ServiceMeta)
			}
		}
	})

	logger.Log(t, "checking servers exported service:")
	if true {
		// Define kubectl options for the 'consul' namespace on the server peer to get the token.
		staticServerConsulNSOpts := &terratestk8s.KubectlOptions{
			ContextName: staticServerPeerClusterContext.KubectlOptions(t).ContextName,
			ConfigPath:  staticServerPeerClusterContext.KubectlOptions(t).ConfigPath,
			Namespace:   "default", // The secret is in the 'consul' namespace.
		}
		serverToken, err := k8s.RunKubectlAndGetOutputE(t, staticServerConsulNSOpts,
			"get", "secret", "consul-bootstrap-acl-token",
			"-o", "go-template={{.data.token|base64decode}}",
		)
		if err != nil {
			logger.Log(t, "error getting server bootstrap token: %v", err)
		} else {
			logger.Log(t, "server bootstrap token: %s", serverToken)
			retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 10}, t, func(r *retry.R) {
				services, _, err := staticServerPeerClient.ExportedServices(
					&api.QueryOptions{
						Namespace: staticServerNamespace,
						Token:     serverToken,
					},
				)
				// require.NoError(r, err, "error querying exported services in server")
				// require.Greater(r, len(services), 0, "no exported static-server service found in server")
				if err != nil {
					logger.Logf(t, "error querying exported services in server: %v", err)
				} else {
					logger.Logf(t, "found %d exported services %q in peer %q", len(services), staticServerName, staticServerPeer)
					for i, s := range services {
						logger.Logf(t, "[%d] ServiceName=%s Namespace=%s Partition=%s Consumers=%s",
							i, s.Service, s.Namespace, s.Partition, s.Consumers)
					}
				}
			})
		}
	}

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
