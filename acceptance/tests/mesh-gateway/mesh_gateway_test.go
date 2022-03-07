package meshgateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"

// Test that Connect and wan federation over mesh gateways work in a default installation
// i.e. without ACLs because TLS is required for WAN federation over mesh gateways.
func TestMeshGatewayDefault(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	primaryContext := env.DefaultContext(t)
	secondaryContext := env.Context(t, environment.SecondaryContextName)

	primaryHelmValues := map[string]string{
		"global.datacenter":                        "dc1",
		"global.tls.enabled":                       "true",
		"global.tls.httpsOnly":                     "false",
		"global.federation.enabled":                "true",
		"global.federation.createFederationSecret": "true",

		"connectInject.enabled":  "true",
		"connectInject.replicas": "1",
		"controller.enabled":     "true",

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
	federationSecret.ResourceVersion = ""
	require.NoError(t, err)
	_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(context.Background(), federationSecret, metav1.CreateOptions{})
	require.NoError(t, err)

	// Create secondary cluster
	secondaryHelmValues := map[string]string{
		"global.datacenter": "dc2",

		"global.tls.enabled":           "true",
		"global.tls.httpsOnly":         "false",
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
		"controller.enabled":     "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",
	}

	if cfg.UseKind {
		secondaryHelmValues["meshGateway.service.type"] = "NodePort"
		secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
	}

	// Install the secondary consul cluster in the secondary kubernetes context
	secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

	if cfg.UseKind {
		// This is a temporary workaround that seems to fix mesh gateway tests on kind 1.22.x.
		// TODO (ishustava): we need to investigate this further and remove once we've found the issue.
		k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "rollout", "restart", fmt.Sprintf("sts/%s-consul-server", releaseName))
		k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "rollout", "status", fmt.Sprintf("sts/%s-consul-server", releaseName))
	}

	primaryClient := primaryConsulCluster.SetupConsulClient(t, false)
	secondaryClient := secondaryConsulCluster.SetupConsulClient(t, false)

	// Verify federation between servers
	logger.Log(t, "verifying federation was successful")
	helpers.VerifyFederation(t, primaryClient, secondaryClient, releaseName, false)

	// Create a ProxyDefaults resource to configure services to use the mesh
	// gateways.
	logger.Log(t, "creating proxy-defaults config")
	kustomizeDir := "../fixtures/bases/mesh-gateway"
	k8s.KubectlApplyK(t, primaryContext.KubectlOptions(t), kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.KubectlDeleteK(t, primaryContext.KubectlOptions(t), kustomizeDir)
	})

	// Check that we can connect services over the mesh gateways
	logger.Log(t, "creating static-server in dc2")
	k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	logger.Log(t, "creating static-client in dc1")
	k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

	logger.Log(t, "checking that connection is successful")
	k8s.CheckStaticServerConnectionSuccessful(t, primaryContext.KubectlOptions(t), staticClientName, "http://localhost:1234")
}

// Test that Connect and wan federation over mesh gateways work in a secure installation,
// with ACLs and TLS with and without auto-encrypt enabled.
func TestMeshGatewaySecure(t *testing.T) {
	cases := []struct {
		name              string
		enableAutoEncrypt string
	}{
		{
			"with ACLs and TLS without auto-encrypt",
			"false",
		},
		{
			"with ACLs and auto-encrypt",
			"true",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()
			cfg := suite.Config()

			primaryContext := env.DefaultContext(t)
			secondaryContext := env.Context(t, environment.SecondaryContextName)

			primaryHelmValues := map[string]string{
				"global.datacenter":            "dc1",
				"global.tls.enabled":           "true",
				"global.tls.enableAutoEncrypt": c.enableAutoEncrypt,

				"global.acls.manageSystemACLs":       "true",
				"global.acls.createReplicationToken": "true",

				"global.federation.enabled":                "true",
				"global.federation.createFederationSecret": "true",

				"connectInject.enabled":  "true",
				"connectInject.replicas": "1",
				"controller.enabled":     "true",

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
				"global.datacenter": "dc2",

				"global.tls.enabled":           "true",
				"global.tls.httpsOnly":         "false",
				"global.tls.enableAutoEncrypt": c.enableAutoEncrypt,
				"global.tls.caCert.secretName": federationSecretName,
				"global.tls.caCert.secretKey":  "caCert",
				"global.tls.caKey.secretName":  federationSecretName,
				"global.tls.caKey.secretKey":   "caKey",

				"global.acls.manageSystemACLs":            "true",
				"global.acls.replicationToken.secretName": federationSecretName,
				"global.acls.replicationToken.secretKey":  "replicationToken",

				"global.federation.enabled":           "true",
				"global.federation.k8sAuthMethodHost": k8sAuthMethodHost,
				"global.federation.primaryDatacenter": "dc1",

				"server.extraVolumes[0].type":          "secret",
				"server.extraVolumes[0].name":          federationSecretName,
				"server.extraVolumes[0].load":          "true",
				"server.extraVolumes[0].items[0].key":  "serverConfigJSON",
				"server.extraVolumes[0].items[0].path": "config.json",

				"connectInject.enabled":  "true",
				"connectInject.replicas": "1",
				"controller.enabled":     "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",
			}

			if cfg.UseKind {
				secondaryHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryHelmValues["meshGateway.service.nodePort"] = "30000"
			}

			// Install the secondary consul cluster in the secondary kubernetes context
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			if cfg.UseKind {
				// This is a temporary workaround that seems to fix mesh gateway tests on kind 1.22.x.
				// TODO (ishustava): we need to investigate this further and remove once we've found the issue.
				k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "rollout", "restart", fmt.Sprintf("sts/%s-consul-server", releaseName))
				k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "rollout", "status", fmt.Sprintf("sts/%s-consul-server", releaseName))
			}

			primaryClient := primaryConsulCluster.SetupConsulClient(t, true)
			secondaryClient := secondaryConsulCluster.SetupConsulClient(t, true)

			// Verify federation between servers
			logger.Log(t, "verifying federation was successful")
			helpers.VerifyFederation(t, primaryClient, secondaryClient, releaseName, true)

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"
			k8s.KubectlApplyK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, secondaryContext.KubectlOptions(t), kustomizeDir)
			})

			// Check that we can connect services over the mesh gateways
			logger.Log(t, "creating static-server in dc2")
			k8s.DeployKustomize(t, secondaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Log(t, "creating static-client in dc1")
			k8s.DeployKustomize(t, primaryContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-multi-dc")

			logger.Log(t, "creating intention")
			_, _, err = primaryClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
				Kind: api.ServiceIntentions,
				Name: "static-server",
				Sources: []*api.SourceIntention{
					{
						Name:   staticClientName,
						Action: api.IntentionActionAllow,
					},
				},
			}, nil)
			require.NoError(t, err)

			logger.Log(t, "checking that connection is successful")
			k8s.CheckStaticServerConnectionSuccessful(t, primaryContext.KubectlOptions(t), staticClientName, "http://localhost:1234")
		})
	}
}
