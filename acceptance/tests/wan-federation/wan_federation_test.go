// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package wanfederation

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	terratestK8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	staticClientDeployment = "deploy/static-client"
	staticServerDeployment = "deploy/static-server"

	retryTimeout = 5 * time.Minute

	primaryDatacenter   = "dc1"
	secondaryDatacenter = "dc2"

	localServerPort = "1234"

	primaryNamespace   = "ns1"
	secondaryNamespace = "ns2"
)

// Test that Connect and wan federation over mesh gateways work in a default installation
// i.e. without ACLs because TLS is required for WAN federation over mesh gateways.
func TestWANFederation(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
	}{
		{
			name:   "secure",
			secure: true,
		},
		{
			name:   "default",
			secure: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			env := suite.Environment()
			cfg := suite.Config()

			primaryContext := env.DefaultContext(t)
			secondaryContext := env.Context(t, 1)

			primaryHelmValues := map[string]string{
				"global.datacenter": primaryDatacenter,

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.secure),

				"global.federation.enabled":                "true",
				"global.federation.createFederationSecret": "true",

				"global.acls.manageSystemACLs":       strconv.FormatBool(c.secure),
				"global.acls.createReplicationToken": strconv.FormatBool(c.secure),

				"connectInject.enabled":  "true",
				"connectInject.replicas": "1",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",
			}

			if cfg.UseKind {
				primaryHelmValues["meshGateway.service.type"] = "NodePort"
				primaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			releaseName := helpers.RandomName()

			// Install the primary consul cluster in the default kubernetes context
			primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the federation secret from the primary cluster and apply it to secondary cluster
			federationSecretName := copyFederationSecret(t, releaseName, primaryContext, secondaryContext)

			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryContext)

			// Create secondary cluster
			secondaryHelmValues := map[string]string{
				"global.datacenter": secondaryDatacenter,

				"global.tls.enabled":           "true",
				"global.tls.httpsOnly":         "false",
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
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
			}

			if c.secure {
				secondaryHelmValues["global.acls.replicationToken.secretName"] = federationSecretName
				secondaryHelmValues["global.acls.replicationToken.secretKey"] = "replicationToken"
				secondaryHelmValues["global.federation.k8sAuthMethodHost"] = k8sAuthMethodHost
				secondaryHelmValues["global.federation.primaryDatacenter"] = primaryDatacenter
			}

			if cfg.UseKind {
				secondaryHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			// Install the secondary consul cluster in the secondary kubernetes context
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			primaryClient, _ := primaryConsulCluster.SetupConsulClient(t, c.secure)
			secondaryClient, _ := secondaryConsulCluster.SetupConsulClient(t, c.secure)

			// Verify federation between servers
			logger.Log(t, "verifying federation was successful")
			helpers.VerifyFederation(t, primaryClient, secondaryClient, releaseName, c.secure)

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"
			k8s.KubectlApplyK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			})

			primaryHelper := connhelper.ConnectHelper{
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             primaryContext,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				ConsulClient:    primaryClient,
			}
			secondaryHelper := connhelper.ConnectHelper{
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             secondaryContext,
				UseAppNamespace: cfg.EnableRestrictedPSAEnforcement,
				Cfg:             cfg,
				ConsulClient:    secondaryClient,
			}

			// When restricted PSA enforcement is enabled on the Consul
			// namespace, deploy the test apps to a different unrestricted
			// namespace because they can't run in a restricted namespace.
			// This creates the app namespace only if necessary.
			primaryHelper.SetupAppNamespace(t)
			secondaryHelper.SetupAppNamespace(t)

			// Check that we can connect services over the mesh gateways
			logger.Log(t, "creating static-server in dc2")
			k8s.DeployKustomize(t, secondaryHelper.KubectlOptsForApp(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Log(t, "creating static-client in dc1")
			k8s.DeployKustomize(t, primaryHelper.KubectlOptsForApp(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

			if c.secure {
				primaryHelper.CreateIntention(t, connhelper.IntentionOpts{})
			}

			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, primaryHelper.KubectlOptsForApp(t), connhelper.StaticClientName, "http://localhost:1234")
		})
	}
}

// Test failover scenarios with a static-server in dc1 and a static-server
// in dc2. Use the static-client on dc1 to reach static-server on dc1 in the
// nominal scenario, then cause a failure in dc1 static-server to see the static-client failover to
// the static-server in dc2
/*
	dc1-static-client -- nominal -- > dc1-static-server in namespace ns1
	dc1-static-client -- failover --> dc2-static-server in namespace ns1
	dc1-static-client -- failover --> dc1-static-server in namespace ns2
*/
func TestWANFederationFailover(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
	}{
		{
			name:   "secure",
			secure: true,
		},
		{
			name:   "default",
			secure: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()
			cfg := suite.Config()

			if cfg.EnableRestrictedPSAEnforcement {
				t.Skip("This test case is not run with enable restricted PSA enforcement enabled")
			}

			primaryContext := env.DefaultContext(t)
			secondaryContext := env.Context(t, 1)

			primaryHelmValues := map[string]string{
				"global.datacenter": primaryDatacenter,

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.secure),

				"global.federation.enabled":                "true",
				"global.federation.createFederationSecret": "true",

				"global.acls.manageSystemACLs":       strconv.FormatBool(c.secure),
				"global.acls.createReplicationToken": strconv.FormatBool(c.secure),

				"connectInject.enabled":  "true",
				"connectInject.replicas": "1",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"global.enableConsulNamespaces":               "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",
			}

			if cfg.UseKind {
				primaryHelmValues["meshGateway.service.type"] = "NodePort"
				primaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			releaseName := helpers.RandomName()

			// Install the primary consul cluster in the default kubernetes context
			primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the federation secret from the primary cluster and apply it to secondary cluster
			federationSecretName := copyFederationSecret(t, releaseName, primaryContext, secondaryContext)

			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryContext)

			// Create secondary cluster
			secondaryHelmValues := map[string]string{
				"global.datacenter": secondaryDatacenter,

				"global.tls.enabled":           "true",
				"global.tls.httpsOnly":         "false",
				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
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

				"global.enableConsulNamespaces":               "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",
			}

			if c.secure {
				secondaryHelmValues["global.acls.replicationToken.secretName"] = federationSecretName
				secondaryHelmValues["global.acls.replicationToken.secretKey"] = "replicationToken"
				secondaryHelmValues["global.federation.k8sAuthMethodHost"] = k8sAuthMethodHost
				secondaryHelmValues["global.federation.primaryDatacenter"] = primaryDatacenter
			}

			if cfg.UseKind {
				secondaryHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			// Install the secondary consul cluster in the secondary kubernetes context
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			primaryClient, _ := primaryConsulCluster.SetupConsulClient(t, c.secure)
			secondaryClient, _ := secondaryConsulCluster.SetupConsulClient(t, c.secure)

			// Verify federation between servers
			logger.Log(t, "Verifying federation was successful")
			helpers.VerifyFederation(t, primaryClient, secondaryClient, releaseName, c.secure)

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "Creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"
			k8s.KubectlApplyK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			})

			primaryHelper := connhelper.ConnectHelper{
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             primaryContext,
				UseAppNamespace: false,
				Cfg:             cfg,
				ConsulClient:    primaryClient,
			}
			secondaryHelper := connhelper.ConnectHelper{
				Secure:          c.secure,
				ReleaseName:     releaseName,
				Ctx:             secondaryContext,
				UseAppNamespace: false,
				Cfg:             cfg,
				ConsulClient:    secondaryClient,
			}

			// Create Namespaces
			// We create a namespace (ns1) in both the primary and secondary datacenters (dc1, dc2)
			// We then create a secondary namespace (ns2) in the primary datacenter (dc1)
			primaryNamespaceOpts := primaryHelper.Ctx.KubectlOptionsForNamespace(primaryNamespace)
			primaryHelper.CreateNamespace(t, primaryNamespaceOpts.Namespace)
			primarySecondaryNamepsaceOpts := primaryHelper.Ctx.KubectlOptionsForNamespace(secondaryNamespace)
			primaryHelper.CreateNamespace(t, primarySecondaryNamepsaceOpts.Namespace)
			secondaryNamespaceOpts := secondaryHelper.Ctx.KubectlOptionsForNamespace(primaryNamespace)
			secondaryHelper.CreateNamespace(t, secondaryNamespaceOpts.Namespace)

			// Create a static-server in dc2 to respond with its own name for checking failover.
			logger.Log(t, "Creating static-server in dc2")
			k8s.DeployKustomize(t, secondaryNamespaceOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/dc2-static-server")

			// Spin up a server on dc1 which will be the primary upstream for our client
			logger.Log(t, "Creating static-server in dc1")
			k8s.DeployKustomize(t, primaryNamespaceOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/dc1-static-server")
			logger.Log(t, "Creating static-client in dc1")
			k8s.DeployKustomize(t, primaryNamespaceOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/static-client")

			// Spin up a second server on dc1 in a separate namespace
			logger.Logf(t, "Creating server on dc1 in namespace %s", primarySecondaryNamepsaceOpts.Namespace)
			k8s.DeployKustomize(t, primarySecondaryNamepsaceOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/dc1-ns2-static-server")

			// There is currently an issue that requires the intentions and resolvers to be created after
			// the static-server/clients when using namespaces. When created before, Consul gives a "namespace does not exist"
			// error
			if c.secure {
				// Only need to create intentions in the primary datacenter as they will be replicated to the secondary
				// ns1 static-client (source) -> ns1 static-server (destination)
				primaryHelper.CreateIntention(t, connhelper.IntentionOpts{DestinationNamespace: primaryNamespaceOpts.Namespace, SourceNamespace: primaryNamespaceOpts.Namespace})

				// ns1 static-client (source) -> ns2 static-server (destination)
				primaryHelper.CreateIntention(t, connhelper.IntentionOpts{DestinationNamespace: primarySecondaryNamepsaceOpts.Namespace, SourceNamespace: primaryNamespaceOpts.Namespace})
			}

			// Create a service resolver for failover
			logger.Log(t, "Creating service resolver")
			k8s.DeployKustomize(t, primaryNamespaceOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/service-resolver")

			// Verify that we respond with the static-server in the primary datacenter
			logger.Log(t, "Verifying static-server in dc1 responds")
			serviceFailoverCheck(t, primaryNamespaceOpts, localServerPort, primaryDatacenter)

			// Scale down the primary datacenter static-server and see the failover
			logger.Log(t, "Scale down dc1 static-server")
			k8s.KubectlScale(t, primaryNamespaceOpts, staticServerDeployment, 0)

			// Verify that we respond with the static-server in the secondary datacenter
			logger.Log(t, "Verifying static-server in dc2 responds")
			serviceFailoverCheck(t, primaryNamespaceOpts, localServerPort, secondaryDatacenter)

			// scale down the primary datacenter static-server and see the failover
			logger.Log(t, "Scale down dc2 static-server")
			k8s.KubectlScale(t, secondaryNamespaceOpts, staticServerDeployment, 0)

			// Verify that we respond with the static-server in the secondary datacenter
			logger.Log(t, "Verifying static-server in secondary namespace (ns2) responds")
			serviceFailoverCheck(t, primaryNamespaceOpts, localServerPort, secondaryNamespace)
		})
	}
}

// serviceFailoverCheck verifies that the server failed over as expected by checking that curling the `static-server`
// using the `static-client` responds with the expected cluster name. Each static-server responds with a unique
// name so that we can verify failover occurred as expected.
func serviceFailoverCheck(t *testing.T, options *terratestK8s.KubectlOptions, port string, expectedName string) {
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 5 * time.Second}
	var resp string
	var err error

	// Retry until we get the response we expect, sometimes you get back the previous server until things stabalize
	logger.Log(t, "Initial failover check")
	retry.RunWith(timer, t, func(r *retry.R) {
		resp, err = k8s.RunKubectlAndGetOutputE(r, options, "exec", "-i",
			staticClientDeployment, "-c", connhelper.StaticClientName, "--", "curl", fmt.Sprintf("localhost:%s", port))
		assert.NoError(r, err)
		assert.Contains(r, resp, expectedName)
	})

	// Try again to rule out load-balancing. Errors can still happen so retry
	logger.Log(t, "Check failover again to rule out load balancing")
	for i := 0; i < 10; i++ {
		time.Sleep(500 * time.Millisecond)
		resp = ""
		retry.RunWith(timer, t, func(r *retry.R) {
			resp, err = k8s.RunKubectlAndGetOutputE(r, options, "exec", "-i",
				staticClientDeployment, "-c", connhelper.StaticClientName, "--", "curl", fmt.Sprintf("localhost:%s", port))
			assert.NoError(r, err)
		})
		require.Contains(t, resp, expectedName)
	}

	logger.Log(t, resp)
}

func copyFederationSecret(t *testing.T, releaseName string, primaryContext, secondaryContext environment.TestContext) string {
	// Get the federation secret from the primary cluster and apply it to secondary cluster
	federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
	logger.Logf(t, "Retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
	federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(context.Background(), federationSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	federationSecret.ResourceVersion = ""
	federationSecret.Namespace = secondaryContext.KubectlOptions(t).Namespace
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(context.Background(), federationSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	return federationSecretName
}
