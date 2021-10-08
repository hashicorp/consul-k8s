package partitions

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/charts/consul/test/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"
const staticServerName = "static-server"
const staticServerNamespace = "ns1"
const staticClientNamespace = "ns2"

// Test that Connect works in a default installation.
// i.e. without ACLs because TLS is required for setting up Admin Partitions.
func TestPartitions(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	if !cfg.UseKind {
		t.Skipf("skipping this test because Admin Partition tests are only supported in Kind for now")
	}

	consulDestNS := "consul-dest"
	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		secure               bool
	}{
		{
			"default namespace",
			"default",
			false,
			false,
		},
		{
			"default namespace; secure",
			"default",
			false,
			true,
		},
		{
			"single destination namespace",
			consulDestNS,
			false,
			false,
		},
		{
			"single destination namespace; secure",
			consulDestNS,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			consulDestNS,
			true,
			false,
		},
		{
			"mirror k8s namespaces; secure",
			consulDestNS,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := suite.Config()
			primaryContext := env.DefaultContext(t)
			secondaryContext := env.Context(t, environment.SecondaryContextName)

			ctx := context.Background()

			primaryHelmValues := map[string]string{
				"global.datacenter": "dc1",
				"global.image":      "ashwinvenkatesh/consul@sha256:2b19b62963306a312acaa223d19afa493fe02ec033a15ad5e6d31f1879408e49",
				"global.imageK8S":   "ashwinvenkatesh/consul-k8s@sha256:8a10fcf7ef80dd540389bc9b10c03a4629a7d08b5e9317b9cc3499c6df71a03b",

				"global.adminPartitions.enabled": "true",
				"global.enableConsulNamespaces":  "true",
				"global.tls.enabled":             "true",
				"global.tls.enableAutoEncrypt":   strconv.FormatBool(c.secure),

				"server.exposeGossipAndRPCPorts": "true",

				"connectInject.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),
			}

			if cfg.UseKind {
				primaryHelmValues["global.adminPartitions.service.type"] = "NodePort"
				primaryHelmValues["global.adminPartitions.service.nodePort.https"] = "30000"
			}

			releaseName := helpers.RandomName()

			// Install the consul cluster with servers in the default kubernetes context.
			primaryConsulCluster := consul.NewHelmCluster(t, primaryHelmValues, primaryContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the TLS CA certificate and key secret from the primary cluster and apply it to secondary cluster
			tlsCert := fmt.Sprintf("%s-consul-ca-cert", releaseName)
			tlsKey := fmt.Sprintf("%s-consul-ca-key", releaseName)

			logger.Logf(t, "retrieving ca cert secret %s from the primary cluster and applying to the secondary", tlsCert)
			caCertSecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(ctx, tlsCert, metav1.GetOptions{})
			caCertSecret.ResourceVersion = ""
			require.NoError(t, err)
			_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(ctx, caCertSecret, metav1.CreateOptions{})
			require.NoError(t, err)

			if !c.secure {
				logger.Logf(t, "retrieving ca key secret %s from the primary cluster and applying to the secondary", tlsKey)
				caKeySecret, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(ctx, tlsKey, metav1.GetOptions{})
				caKeySecret.ResourceVersion = ""
				require.NoError(t, err)
				_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(ctx, caKeySecret, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
			if c.secure {
				logger.Logf(t, "retrieving partition token secret %s from the primary cluster and applying to the secondary", tlsKey)
				token, err := primaryContext.KubernetesClient(t).CoreV1().Secrets(primaryContext.KubectlOptions(t).Namespace).Get(ctx, partitionToken, metav1.GetOptions{})
				token.ResourceVersion = ""
				require.NoError(t, err)
				_, err = secondaryContext.KubernetesClient(t).CoreV1().Secrets(secondaryContext.KubectlOptions(t).Namespace).Create(ctx, token, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			var partitionSvcIP string
			if !cfg.UseKind {
				// Get the IP of the partition service to configure the external server address in the values file for the workload cluster.
				partitionServiceName := fmt.Sprintf("%s-partition-secret", releaseName)
				logger.Logf(t, "retrieving partition service to determine external IP for servers")
				partitionsSvc, err := primaryContext.KubernetesClient(t).CoreV1().Services(primaryContext.KubectlOptions(t).Namespace).Get(ctx, partitionServiceName, metav1.GetOptions{})
				require.NoError(t, err)
				partitionSvcIP = partitionsSvc.Status.LoadBalancer.Ingress[0].IP
			} else {
				nodeList, err := primaryContext.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				require.NoError(t, err)
				// Get the address of the (only) node from the Kind cluster.
				partitionSvcIP = nodeList.Items[0].Status.Addresses[0].Address
			}

			// Create secondary cluster
			secondaryHelmValues := map[string]string{
				"global.datacenter": "dc1",
				"global.image":      "ashwinvenkatesh/consul@sha256:2b19b62963306a312acaa223d19afa493fe02ec033a15ad5e6d31f1879408e49",
				"global.imageK8S":   "ashwinvenkatesh/consul-k8s@sha256:8a10fcf7ef80dd540389bc9b10c03a4629a7d08b5e9317b9cc3499c6df71a03b",
				"global.enabled":    "false",

				"global.tls.enabled":           "true",
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.secure),

				"server.exposeGossipAndRPCPorts": "true",

				"connectInject.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),

				"global.adminPartitions.enabled": "true",
				"global.adminPartitions.name":    "secondary",
				"global.enableConsulNamespaces":  "true",

				"global.tls.caCert.secretName": tlsCert,
				"global.tls.caCert.secretKey":  "tls.crt",

				"externalServers.enabled":       "true",
				"externalServers.hosts[0]":      partitionSvcIP,
				"externalServers.tlsServerName": "server.dc1.consul",

				"client.enabled":           "true",
				"client.exposeGossipPorts": "true",
				"client.join[0]":           partitionSvcIP,
			}

			if c.secure {
				// setup partition token if ACLs enabled.
				secondaryHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
				secondaryHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
			} else {
				// provide CA key when auto-encrypt is disabled.
				secondaryHelmValues["global.tls.caKey.secretName"] = tlsKey
				secondaryHelmValues["global.tls.caKey.secretKey"] = "tls.key"
			}

			if cfg.UseKind {
				secondaryHelmValues["externalServers.httpsPort"] = "30000"
			}

			// Install the consul cluster without servers in the secondary kubernetes context.
			secondaryConsulCluster := consul.NewHelmCluster(t, secondaryHelmValues, secondaryContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			agentPodList, err := secondaryContext.KubernetesClient(t).CoreV1().Pods(secondaryContext.KubectlOptions(t).Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=consul,component=client"})
			require.NoError(t, err)
			require.Len(t, agentPodList.Items, 1)

			output, err := k8s.RunKubectlAndGetOutputE(t, secondaryContext.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "-n", secondaryContext.KubectlOptions(t).Namespace)
			require.NoError(t, err)
			require.Contains(t, output, "Partition: 'secondary'")

			serverClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: primaryContext.KubectlOptions(t).ContextName,
				ConfigPath:  primaryContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			serverClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: primaryContext.KubectlOptions(t).ContextName,
				ConfigPath:  primaryContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}
			clientClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: secondaryContext.KubectlOptions(t).ContextName,
				ConfigPath:  secondaryContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			clientClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: secondaryContext.KubectlOptions(t).ContextName,
				ConfigPath:  secondaryContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}

			logger.Logf(t, "creating namespaces %s and %s in servers cluster", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				// Note: this deletion will take longer in cases when the static-client deployment
				// hasn't yet fully terminated.
				k8s.RunKubectl(t, primaryContext.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			logger.Logf(t, "creating namespaces %s and %s in clients cluster", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, secondaryContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, secondaryContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			k8s.RunKubectl(t, secondaryContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				// Note: this deletion will take longer in cases when the static-client deployment
				// hasn't yet fully terminated.
				k8s.RunKubectl(t, secondaryContext.KubectlOptions(t), "delete", "ns", staticClientNamespace)
			})

			serverClusterConsulClient := primaryConsulCluster.SetupConsulClient(t, c.secure)
			clientClusterConsulClient := secondaryConsulCluster.SetupConsulClient(t, c.secure)

			serverQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace}
			clientQueryOpts := &api.QueryOptions{Namespace: staticClientNamespace}

			if !c.mirrorK8S {
				serverQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
				clientQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace}
			}

			// Check that the ACL token is deleted.
			if c.secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					if c.secure {
						retry.Run(t, func(r *retry.R) {
							tokens, _, err := serverClusterConsulClient.ACL().TokenList(serverQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = serverClusterConsulClient.ACL().TokenList(clientQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticClientName)
							}
							tokens, _, err = clientClusterConsulClient.ACL().TokenList(serverQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = clientClusterConsulClient.ACL().TokenList(clientQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticClientName)
							}
						})
					}
				})
			}

			logger.Log(t, "creating static-server and static-client deployments in server cluster")
			k8s.DeployKustomize(t, serverClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
			}

			logger.Log(t, "creating static-server and static-client deployments in client cluster")
			k8s.DeployKustomize(t, clientClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
			} else {
				k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
			}

			// Check that both static-server and static-client have been injected and now have 2 containers in server cluster.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := primaryContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			// Check that both static-server and static-client have been injected and now have 2 containers in client cluster.
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := secondaryContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
					LabelSelector: labelSelector,
				})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
			}

			// Make sure that services are registered in the correct namespace.
			// If mirroring is enabled, we expect services to be registered in the
			// Consul namespace with the same name as their source
			// Kubernetes namespace.
			// If a single destination namespace is set, we expect all services
			// to be registered in that destination Consul namespace.
			// Server cluster.
			services, _, err := serverClusterConsulClient.Catalog().Service(staticServerName, "", serverQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			services, _, err = serverClusterConsulClient.Catalog().Service(staticClientName, "", clientQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			// Client cluster.
			services, _, err = clientClusterConsulClient.Catalog().Service(staticServerName, "", serverQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			services, _, err = clientClusterConsulClient.Catalog().Service(staticClientName, "", clientQueryOpts)
			require.NoError(t, err)
			require.Len(t, services, 1)

			if c.secure {
				logger.Log(t, "checking that the connection is not successful because there's no intention")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionFailing(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
					k8s.CheckStaticServerConnectionFailing(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				} else {
					k8s.CheckStaticServerConnectionFailing(t, serverClusterStaticClientOpts, "http://localhost:1234")
					k8s.CheckStaticServerConnectionFailing(t, clientClusterStaticClientOpts, "http://localhost:1234")
				}

				intention := &api.Intention{
					SourceName:      staticClientName,
					SourceNS:        staticClientNamespace,
					DestinationName: staticServerName,
					DestinationNS:   staticServerNamespace,
					Action:          api.IntentionActionAllow,
				}

				// Set the destination namespace to be the same
				// unless mirrorK8S is true.
				if !c.mirrorK8S {
					intention.SourceNS = c.destinationNamespace
					intention.DestinationNS = c.destinationNamespace
				}

				logger.Log(t, "creating intention")
				_, err := serverClusterConsulClient.Connect().IntentionUpsert(intention, nil)
				require.NoError(t, err)
				_, err = clientClusterConsulClient.Connect().IntentionUpsert(intention, nil)
				require.NoError(t, err)
			}

			logger.Log(t, "checking that connection is successful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionSuccessful(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				k8s.CheckStaticServerConnectionSuccessful(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
			} else {
				k8s.CheckStaticServerConnectionSuccessful(t, serverClusterStaticClientOpts, "http://localhost:1234")
				k8s.CheckStaticServerConnectionSuccessful(t, clientClusterStaticClientOpts, "http://localhost:1234")
			}

			// Test that kubernetes readiness status is synced to Consul.
			// Create the file so that the readiness probe of the static-server pod fails.
			logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
			k8s.RunKubectl(t, serverClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")
			k8s.RunKubectl(t, clientClusterStaticClientOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

			// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
			// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
			// We are expecting a "connection reset by peer" error because in a case of health checks,
			// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
			// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
			logger.Log(t, "checking that connection is unsuccessful")
			if cfg.EnableTransparentProxy {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, serverClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, clientClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
			} else {
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, serverClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
				k8s.CheckStaticServerConnectionMultipleFailureMessages(t, clientClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
			}
		})
	}
}
