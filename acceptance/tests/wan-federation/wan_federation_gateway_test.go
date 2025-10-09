// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package wanfederation

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/serf/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestWANFederation_Gateway(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.UseKind {
		// the only way this test can currently run on kind, at least on a Mac, is via leveraging MetalLB, which
		// isn't in CI, so we just skip for now.
		t.Skipf("skipping wan federation tests as they currently fail on Kind even though they work on other clouds.")
	}

	primaryContext := env.DefaultContext(t)
	secondaryContext := env.Context(t, 1)

	primaryHelmValues := map[string]string{
		"global.datacenter": "dc1",

		"global.tls.enabled":   "true",
		"global.tls.httpsOnly": "true",

		"global.federation.enabled":                "true",
		"global.federation.createFederationSecret": "true",

		"global.acls.manageSystemACLs":       "true",
		"global.acls.createReplicationToken": "true",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",

		"meshGateway.enabled":             "true",
		"meshGateway.replicas":            "1",
		"global.dualStack.defaultEnabled": cfg.GetDualStack(),
	}

	releaseName := helpers.RandomName()

	// Install the primary consul cluster in the default kubernetes context
	primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
	primaryConsulCluster.Create(t)

	var k8sAuthMethodHost string
	// When running on kind, the kube API address in kubeconfig will have a localhost address
	// which will not work from inside the container. That's why we need to use the endpoints address instead
	// which will point the node IP.
	if cfg.UseKind {
		// The Kubernetes AuthMethod host is read from the endpoints for the Kubernetes service.
		kubernetesEndpoint, err := secondaryContext.KubernetesClient(t).CoreV1().Endpoints("default").Get(context.Background(), "kubernetes", metav1.GetOptions{})
		require.NoError(t, err)
		k8sAuthMethodHost = fmt.Sprintf("%s:%d", kubernetesEndpoint.Subsets[0].Addresses[0].IP, kubernetesEndpoint.Subsets[0].Ports[0].Port)
	} else {
		k8sAuthMethodHost = k8s.KubernetesAPIServerHostFromOptions(t, secondaryContext.KubectlOptions(t))
	}

	federationSecretName := copyFederationSecret(t, releaseName, primaryContext, secondaryContext)

	// Create secondary cluster
	secondaryHelmValues := map[string]string{
		"global.datacenter": "dc2",

		"global.tls.enabled":           "true",
		"global.tls.httpsOnly":         "false",
		"global.acls.manageSystemACLs": "true",
		"global.tls.caCert.secretName": federationSecretName,
		"global.tls.caCert.secretKey":  "caCert",
		"global.tls.caKey.secretName":  federationSecretName,
		"global.tls.caKey.secretKey":   "caKey",

		"global.federation.enabled": "true",

		"server.extraVolumes[0].type":          "secret",
		"server.extraVolumes[0].name":          federationSecretName,
		"server.extraVolumes[0].load":          "true",
		"server.extraVolumes[0].items[0].key":  "serverConfigJSON",
		"server.extraVolumes[0].items[0].path": "config.json",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",

		"global.acls.replicationToken.secretName": federationSecretName,
		"global.acls.replicationToken.secretKey":  "replicationToken",
		"global.federation.k8sAuthMethodHost":     k8sAuthMethodHost,
		"global.federation.primaryDatacenter":     "dc1",
		"global.dualStack.defaultEnabled":         cfg.GetDualStack(),
	}

	// Install the secondary consul cluster in the secondary kubernetes context
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	primaryClient, _ := primaryConsulCluster.SetupConsulClient(t, true)
	secondaryClient, _ := secondaryConsulCluster.SetupConsulClient(t, true)

	// Verify federation between servers
	logger.Log(t, "verifying federation was successful")
	helpers.VerifyFederation(t, primaryClient, secondaryClient, releaseName, true)

	// Create a ProxyDefaults resource to configure services to use the mesh
	// gateways.
	logger.Log(t, "creating proxy-defaults config in dc1")
	kustomizeDir := "../fixtures/cases/api-gateways/mesh"
	k8s.KubectlApplyK(t, primaryContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, primaryContext.KubectlOptions(t), kustomizeDir)
	})

	// these clients are just there so we can exec in and curl on them.
	logger.Log(t, "creating static-client in dc1")
	k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

	logger.Log(t, "creating static-client in dc2")
	k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

	t.Run("from primary to secondary", func(t *testing.T) {
		logger.Log(t, "creating static-server in dc2")
		k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

		logger.Log(t, "creating api-gateway resources in dc1")
		out, err := k8s.RunKubectlAndGetOutputE(t, primaryContext.KubectlOptions(t), "apply", "-k", "../fixtures/bases/api-gateway")
		require.NoError(t, err, out)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			// Ignore errors here because if the test ran as expected
			// the custom resources will have been deleted.
			k8s.RunKubectlAndGetOutputE(t, primaryContext.KubectlOptions(t), "delete", "-k", "../fixtures/bases/api-gateway")
		})

		// create a service resolver for doing cross-dc redirects.
		k8s.KubectlApplyK(t, secondaryContext.KubectlOptions(t), "../fixtures/cases/api-gateways/dc1-to-dc2-resolver")
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			k8s.KubectlDeleteK(t, secondaryContext.KubectlOptions(t), "../fixtures/cases/api-gateways/dc1-to-dc2-resolver")
		})

		// patching the route to target a MeshService since we don't have the corresponding Kubernetes service in this
		// cluster.
		k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

		checkConnectivity(t, primaryContext, primaryClient)
	})

	t.Run("from secondary to primary", func(t *testing.T) {
		// Check that we can connect services over the mesh gateways
		logger.Log(t, "creating static-server in dc1")
		k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

		logger.Log(t, "creating api-gateway resources in dc2")
		out, err := k8s.RunKubectlAndGetOutputE(t, secondaryContext.KubectlOptions(t), "apply", "-k", "../fixtures/bases/api-gateway")
		require.NoError(t, err, out)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			// Ignore errors here because if the test ran as expected
			// the custom resources will have been deleted.
			k8s.RunKubectlAndGetOutputE(t, secondaryContext.KubectlOptions(t), "delete", "-k", "../fixtures/bases/api-gateway")
		})

		// create a service resolver for doing cross-dc redirects.
		k8s.KubectlApplyK(t, secondaryContext.KubectlOptions(t), "../fixtures/cases/api-gateways/dc2-to-dc1-resolver")
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			k8s.KubectlDeleteK(t, secondaryContext.KubectlOptions(t), "../fixtures/cases/api-gateways/dc2-to-dc1-resolver")
		})

		// patching the route to target a MeshService since we don't have the corresponding Kubernetes service in this
		// cluster.
		k8s.RunKubectl(t, secondaryContext.KubectlOptions(t), "patch", "httproute", "http-route", "-p", `{"spec":{"rules":[{"backendRefs":[{"group":"consul.hashicorp.com","kind":"MeshService","name":"mesh-service","port":80}]}]}}`, "--type=merge")

		checkConnectivity(t, secondaryContext, primaryClient)
	})
}

func checkConnectivity(t *testing.T, ctx environment.TestContext, client *api.Client) {
	k8sClient := ctx.ControllerRuntimeClient(t)

	// On startup, the controller can take upwards of 1m to perform
	// leader election so we may need to wait a long time for
	// the reconcile loop to run (hence the 1m timeout here).
	var gatewayAddress string
	counter := &retry.Counter{Count: 600, Wait: 2 * time.Second}
	retry.RunWith(counter, t, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: "default"}, &gateway)
		require.NoError(r, err)

		// check that we have an address to use
		require.Len(r, gateway.Status.Addresses, 1)
		// now we know we have an address, set it so we can use it
		gatewayAddress = gateway.Status.Addresses[0].Value
	})

	targetAddress := fmt.Sprintf("http://%s/", net.JoinHostPort(gatewayAddress, "8080"))

	logger.Log(t, "checking that the connection is not successful because there's no intention")
	k8s.CheckStaticServerHTTPConnectionFailing(t, ctx.KubectlOptions(t), connhelper.StaticClientName, targetAddress)

	logger.Log(t, "creating intention")
	_, _, err := client.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server",
		Sources: []*api.SourceIntention{
			{
				Name:   "gateway",
				Action: api.IntentionActionAllow,
			},
		},
	}, nil)
	require.NoError(t, err)
	defer func() {
		_, err := client.ConfigEntries().Delete(api.ServiceIntentions, "static-server", &api.WriteOptions{})
		require.NoError(t, err)
	}()

	logger.Log(t, "checking that connection is successful")
	k8s.CheckStaticServerConnectionSuccessful(t, ctx.KubectlOptions(t), connhelper.StaticClientName, targetAddress)
}
