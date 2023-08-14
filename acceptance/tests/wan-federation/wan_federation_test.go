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
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	StaticClientName = "static-client"

	staticClientDeployment = "deploy/static-client"
	staticServerDeployment = "deploy/static-server"

	retryTimeout = 5 * time.Minute

	primaryDatacenter   = "dc1"
	secondaryDatacenter = "dc2"

	localServerPort = "1234"
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
			federationSecretName := fmt.Sprintf("%s-consul-federation", releaseName)
			logger.Logf(t, "retrieving federation secret %s from the primary cluster and applying to the secondary", federationSecretName)
			federationSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(context.Background(), federationSecretName, metav1.GetOptions{})
			require.NoError(t, err)
			federationSecret.ResourceVersion = ""
			federationSecret.Namespace = secondaryContext.KubectlOptions(t).Namespace
			_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(context.Background(), federationSecret, metav1.CreateOptions{})
			require.NoError(t, err)

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
				primaryHelper.CreateIntention(t)
			}

			// Test a basic scenario where a static-client on dc1 reaches out to a wan federated static-server on dc1
			t.Run("simple connection", func(t *testing.T) {
				logger.Log(t, "checking that connection is successful")
				k8s.CheckStaticServerConnectionSuccessful(t, primaryHelper.KubectlOptsForApp(t), StaticClientName, "http://localhost:1234")
			})

			// Test failover scenarios with a static-server in dc1 and a static-server
			// in dc2. Use the static-client on dc1 to reach static-server on dc1 in the
			// nominal scenario, then cause a failure in dc1 static-server to see the static-client failover to
			// the static-server in dc2
			/*
				dc1-static-client -- nominal -- > dc1-static-server
				dc1-static-client -- failover --> dc2-static-server
			*/
			logger.Log(t, "setting up infrastructure for failover")
			t.Run("service failover", func(t *testing.T) {
				// Override static-server in dc2 to respond with its own name for checking failover.
				// Don't clean up overrides because they will already be cleaned up from previous deployments
				logger.Log(t, "overriding static-server in dc2 for failover")
				k8s.DeployKustomize(t, secondaryHelper.KubectlOptsForApp(t), true, true, cfg.DebugDirectory, "../fixtures/cases/wan-federation/dc2-static-server")

				// Spin up a server on dc1 which will be the primary upstream for our client
				logger.Log(t, "creating static-server in dc1 for failover")
				k8s.DeployKustomize(t, primaryHelper.KubectlOptsForApp(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/dc1-static-server")
				logger.Log(t, "overriding static-client in dc2 for failover")
				k8s.DeployKustomize(t, primaryHelper.KubectlOptsForApp(t), true, true, cfg.DebugDirectory, "../fixtures/cases/wan-federation/static-client")

				// Create a service resolver for failover
				logger.Log(t, "creating service resolver")
				k8s.DeployKustomize(t, primaryHelper.KubectlOptsForApp(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/wan-federation/service-resolver")

				// Verify that we respond with the server in the primary datacenter
				logger.Log(t, "verifying static-server in dc1 responds")
				serviceFailoverCheck(t, primaryHelper.KubectlOptsForApp(t), localServerPort, primaryDatacenter)

				// scale down the primary datacenter server and see the failover
				logger.Log(t, "scale down dc1 static-server")
				k8s.KubectlScale(t, primaryHelper.KubectlOptsForApp(t), staticServerDeployment, 0)

				// Verify that we respond with the server in the secondary datacenter
				logger.Log(t, "verifying static-server in dc2 responds")
				serviceFailoverCheck(t, primaryHelper.KubectlOptsForApp(t), localServerPort, secondaryDatacenter)
			})
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
	retry.RunWith(timer, t, func(r *retry.R) {
		resp, err = k8s.RunKubectlAndGetOutputE(t, options, "exec", "-i",
			staticClientDeployment, "-c", StaticClientName, "--", "curl", fmt.Sprintf("localhost:%s", port))
		require.NoError(r, err)
		assert.Contains(r, resp, expectedName)
	})
	logger.Log(t, resp)
}
