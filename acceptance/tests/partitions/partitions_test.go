package partitions

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const staticClientName = "static-client"
const staticServerName = "static-server"
const staticServerNamespace = "ns1"
const staticClientNamespace = "ns2"

// Test that Connect works in a default installation for X-Partition and in-partition networking.
func TestPartitions(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	if !cfg.UseKind {
		t.Skipf("skipping this test because Admin Partition tests are only supported in Kind for now")
	}

	const defaultPartition = "default"
	const secondaryPartition = "secondary"
	const defaultNamespace = "default"
	cases := []struct {
		name                 string
		destinationNamespace string
		mirrorK8S            bool
		secure               bool
	}{
		{
			"default namespace",
			defaultNamespace,
			false,
			false,
		},
		{
			"default namespace; secure",
			defaultNamespace,
			false,
			true,
		},
		{
			"single destination namespace",
			staticServerNamespace,
			false,
			false,
		},
		{
			"single destination namespace; secure",
			staticServerNamespace,
			false,
			true,
		},
		{
			"mirror k8s namespaces",
			staticServerNamespace,
			true,
			false,
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
			serverClusterContext := env.DefaultContext(t)
			clientClusterContext := env.Context(t, environment.SecondaryContextName)

			ctx := context.Background()

			serverHelmValues := map[string]string{
				"global.datacenter": "dc1",

				"global.adminPartitions.enabled": "true",
				"global.enableConsulNamespaces":  "true",
				"global.tls.enabled":             "true",
				"global.tls.httpsOnly":           strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt":   strconv.FormatBool(c.secure),

				"server.exposeGossipAndRPCPorts": "true",

				"connectInject.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"controller.enabled": "true",

				"dns.enabled": "true",
			}

			if cfg.UseKind {
				serverHelmValues["global.adminPartitions.service.type"] = "NodePort"
				serverHelmValues["global.adminPartitions.service.nodePort.https"] = "30000"
				serverHelmValues["meshGateway.service.type"] = "NodePort"
				serverHelmValues["meshGateway.service.nodePort"] = "30100"
			}

			if cfg.EnableTransparentProxy {
				serverHelmValues["dns.enableRedirection"] = "true"
			}

			releaseName := helpers.RandomName()

			// Install the consul cluster with servers in the default kubernetes context.
			serverConsulCluster := consul.NewHelmCluster(t, serverHelmValues, serverClusterContext, cfg, releaseName)
			serverConsulCluster.Create(t)

			// Get the TLS CA certificate and key secret from the server cluster and apply it to client cluster.
			tlsCert := fmt.Sprintf("%s-consul-ca-cert", releaseName)
			tlsKey := fmt.Sprintf("%s-consul-ca-key", releaseName)

			logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", tlsCert)
			caCertSecret, err := serverClusterContext.KubernetesClient(t).CoreV1().Secrets(serverClusterContext.KubectlOptions(t).Namespace).Get(ctx, tlsCert, metav1.GetOptions{})
			caCertSecret.ResourceVersion = ""
			require.NoError(t, err)
			_, err = clientClusterContext.KubernetesClient(t).CoreV1().Secrets(clientClusterContext.KubectlOptions(t).Namespace).Create(ctx, caCertSecret, metav1.CreateOptions{})
			require.NoError(t, err)

			if !c.secure {
				// When running in the insecure mode, auto-encrypt is disabled which requires both
				// the CA cert and CA key to be available in the clients cluster.
				logger.Logf(t, "retrieving ca key secret %s from the server cluster and applying to the client cluster", tlsKey)
				caKeySecret, err := serverClusterContext.KubernetesClient(t).CoreV1().Secrets(serverClusterContext.KubectlOptions(t).Namespace).Get(ctx, tlsKey, metav1.GetOptions{})
				caKeySecret.ResourceVersion = ""
				require.NoError(t, err)
				_, err = clientClusterContext.KubernetesClient(t).CoreV1().Secrets(clientClusterContext.KubectlOptions(t).Namespace).Create(ctx, caKeySecret, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
			if c.secure {
				logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", tlsKey)
				token, err := serverClusterContext.KubernetesClient(t).CoreV1().Secrets(serverClusterContext.KubectlOptions(t).Namespace).Get(ctx, partitionToken, metav1.GetOptions{})
				token.ResourceVersion = ""
				require.NoError(t, err)
				_, err = clientClusterContext.KubernetesClient(t).CoreV1().Secrets(clientClusterContext.KubectlOptions(t).Namespace).Create(ctx, token, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			var partitionSvcIP string
			if !cfg.UseKind {
				// Get the IP of the partition service to configure the external server address in the values file for the workload cluster.
				partitionSecretName := fmt.Sprintf("%s-partition-secret", releaseName)
				logger.Logf(t, "retrieving partition service to determine external IP for servers")
				partitionsSvc, err := serverClusterContext.KubernetesClient(t).CoreV1().Services(serverClusterContext.KubectlOptions(t).Namespace).Get(ctx, partitionSecretName, metav1.GetOptions{})
				require.NoError(t, err)
				partitionSvcIP = partitionsSvc.Status.LoadBalancer.Ingress[0].IP
			} else {
				nodeList, err := serverClusterContext.KubernetesClient(t).CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				require.NoError(t, err)
				// Get the address of the (only) node from the Kind cluster.
				partitionSvcIP = nodeList.Items[0].Status.Addresses[0].Address
			}

			var k8sAuthMethodHost string
			if cfg.UseKind {
				// The Kubernetes AuthMethod IP for Kind is read from the endpoint for the Kubernetes service. On other clouds,
				// this can be identified by reading the cluster config.
				kubernetesEndpoint, err := clientClusterContext.KubernetesClient(t).CoreV1().Endpoints(defaultNamespace).Get(ctx, "kubernetes", metav1.GetOptions{})
				require.NoError(t, err)
				k8sAuthMethodHost = fmt.Sprintf("%s:%d", kubernetesEndpoint.Subsets[0].Addresses[0].IP, kubernetesEndpoint.Subsets[0].Ports[0].Port)
			}

			// Create client cluster.
			clientHelmValues := map[string]string{
				"global.datacenter": "dc1",
				"global.enabled":    "false",

				"global.tls.enabled":           "true",
				"global.tls.httpsOnly":         strconv.FormatBool(c.secure),
				"global.tls.enableAutoEncrypt": strconv.FormatBool(c.secure),

				"server.exposeGossipAndRPCPorts": "true",

				"connectInject.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"connectInject.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"connectInject.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),

				"global.adminPartitions.enabled": "true",
				"global.adminPartitions.name":    secondaryPartition,
				"global.enableConsulNamespaces":  "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"controller.enabled": "true",

				"global.tls.caCert.secretName": tlsCert,
				"global.tls.caCert.secretKey":  "tls.crt",

				"externalServers.enabled":       "true",
				"externalServers.hosts[0]":      partitionSvcIP,
				"externalServers.tlsServerName": "server.dc1.consul",

				"client.enabled":           "true",
				"client.exposeGossipPorts": "true",
				"client.join[0]":           partitionSvcIP,

				"dns.enabled": "true",
			}

			if c.secure {
				// setup partition token if ACLs enabled.
				clientHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
				clientHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
				clientHelmValues["externalServers.k8sAuthMethodHost"] = k8sAuthMethodHost
			} else {
				// provide CA key when auto-encrypt is disabled.
				clientHelmValues["global.tls.caKey.secretName"] = tlsKey
				clientHelmValues["global.tls.caKey.secretKey"] = "tls.key"
			}

			if cfg.UseKind {
				clientHelmValues["externalServers.httpsPort"] = "30000"
				clientHelmValues["meshGateway.service.type"] = "NodePort"
				clientHelmValues["meshGateway.service.nodePort"] = "30100"
			}

			if cfg.EnableTransparentProxy {
				clientHelmValues["dns.enableRedirection"] = "true"
			}

			// Install the consul cluster without servers in the client cluster kubernetes context.
			clientConsulCluster := consul.NewHelmCluster(t, clientHelmValues, clientClusterContext, cfg, releaseName)
			clientConsulCluster.Create(t)

			// Ensure consul client are created.
			agentPodList, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(clientClusterContext.KubectlOptions(t).Namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=consul,component=client"})
			require.NoError(t, err)
			require.Len(t, agentPodList.Items, 1)

			output, err := k8s.RunKubectlAndGetOutputE(t, clientClusterContext.KubectlOptions(t), "logs", agentPodList.Items[0].Name, "-n", clientClusterContext.KubectlOptions(t).Namespace)
			require.NoError(t, err)
			require.Contains(t, output, "Partition: 'secondary'")

			serverClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: serverClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  serverClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			serverClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: serverClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  serverClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}
			clientClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: clientClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  clientClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			clientClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: clientClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  clientClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticClientNamespace,
			}

			logger.Logf(t, "creating namespaces %s and %s in servers cluster", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, serverClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			k8s.RunKubectl(t, serverClusterContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, serverClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, staticClientNamespace)
			})

			logger.Logf(t, "creating namespaces %s and %s in clients cluster", staticServerNamespace, staticClientNamespace)
			k8s.RunKubectl(t, clientClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			k8s.RunKubectl(t, clientClusterContext.KubectlOptions(t), "create", "ns", staticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.RunKubectl(t, clientClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, staticClientNamespace)
			})

			consulClient := serverConsulCluster.SetupConsulClient(t, c.secure)

			serverQueryServerOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: defaultPartition}
			clientQueryServerOpts := &api.QueryOptions{Namespace: staticClientNamespace, Partition: defaultPartition}

			serverQueryClientOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: secondaryPartition}
			clientQueryClientOpts := &api.QueryOptions{Namespace: staticClientNamespace, Partition: secondaryPartition}

			if !c.mirrorK8S {
				serverQueryServerOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: defaultPartition}
				clientQueryServerOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: defaultPartition}
				serverQueryClientOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: secondaryPartition}
				clientQueryClientOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: secondaryPartition}
			}

			// Check that the ACL token is deleted.
			if c.secure {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					if c.secure {
						retry.Run(t, func(r *retry.R) {
							tokens, _, err := consulClient.ACL().TokenList(serverQueryServerOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(clientQueryServerOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticClientName)
							}
							tokens, _, err = consulClient.ACL().TokenList(serverQueryClientOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(clientQueryClientOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticClientName)
							}
						})
					}
				})
			}

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"

			k8s.KubectlApplyK(t, serverClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, serverClusterContext.KubectlOptions(t), kustomizeDir)
			})

			k8s.KubectlApplyK(t, clientClusterContext.KubectlOptions(t), kustomizeDir)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
				k8s.KubectlDeleteK(t, clientClusterContext.KubectlOptions(t), kustomizeDir)
			})
			// This section of the tests run the in-partition networking tests.
			t.Run("in-partition", func(t *testing.T) {
				logger.Log(t, "test in-partition networking")
				logger.Log(t, "creating static-server and static-client deployments in server cluster")
				k8s.DeployKustomize(t, serverClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
					} else {
						k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
					}
				}
				logger.Log(t, "creating static-server and static-client deployments in client cluster")
				k8s.DeployKustomize(t, clientClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
					} else {
						k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
					}
				}
				// Check that both static-server and static-client have been injected and now have 2 containers in server cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := serverClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					require.NoError(t, err)
					require.Len(t, podList.Items, 1)
					require.Len(t, podList.Items[0].Spec.Containers, 2)
				}

				// Check that both static-server and static-client have been injected and now have 2 containers in client cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
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
				services, _, err := consulClient.Catalog().Service(staticServerName, "", serverQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				// Client cluster.
				services, _, err = consulClient.Catalog().Service(staticServerName, "", serverQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryClientOpts)
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

					intention := &api.ServiceIntentionsConfigEntry{
						Kind:      api.ServiceIntentions,
						Name:      staticServerName,
						Namespace: staticServerNamespace,
						Sources: []*api.SourceIntention{
							{
								Name:      staticClientName,
								Namespace: staticClientNamespace,
								Action:    api.IntentionActionAllow,
							},
						},
					}

					// Set the destination namespace to be the same
					// unless mirrorK8S is true.
					if !c.mirrorK8S {
						intention.Namespace = c.destinationNamespace
						intention.Sources[0].Namespace = c.destinationNamespace
					}

					logger.Log(t, "creating intention")
					_, _, err := consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: defaultPartition})
					require.NoError(t, err)
					_, _, err = consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: secondaryPartition})
					require.NoError(t, err)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						_, err := consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: defaultPartition})
						require.NoError(t, err)
						_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: secondaryPartition})
						require.NoError(t, err)
					})
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
				k8s.RunKubectl(t, clientClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

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
			// This section of the tests run the cross-partition networking tests.
			t.Run("cross-partition", func(t *testing.T) {
				logger.Log(t, "test cross-partition networking")
				logger.Log(t, "creating static-server and static-client deployments in server cluster")
				k8s.DeployKustomize(t, serverClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-partition")
					} else {
						k8s.DeployKustomize(t, serverClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/ns-partition")
					}
				}
				logger.Log(t, "creating static-server and static-client deployments in client cluster")
				k8s.DeployKustomize(t, clientClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition")
					} else {
						k8s.DeployKustomize(t, clientClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/ns-default-partition")
					}
				}
				// Check that both static-server and static-client have been injected and now have 2 containers in server cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := serverClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					require.NoError(t, err)
					require.Len(t, podList.Items, 1)
					require.Len(t, podList.Items[0].Spec.Containers, 2)
				}

				// Check that both static-server and static-client have been injected and now have 2 containers in client cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := clientClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
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
				// We are going to test that static-clients deployed in each partition can
				// access the static-servers running in another partition.
				// ie default -> secondary and secondary -> default.
				services, _, err := consulClient.Catalog().Service(staticServerName, "", serverQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				// Client cluster.
				services, _, err = consulClient.Catalog().Service(staticServerName, "", serverQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(staticClientName, "", clientQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				logger.Log(t, "creating exported services")
				if c.destinationNamespace == defaultNamespace {
					k8s.KubectlApplyK(t, serverClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
					k8s.KubectlApplyK(t, clientClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						k8s.KubectlDeleteK(t, serverClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
						k8s.KubectlDeleteK(t, clientClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
					})
				} else {
					k8s.KubectlApplyK(t, serverClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
					k8s.KubectlApplyK(t, clientClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-ns1")
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						k8s.KubectlDeleteK(t, serverClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
						k8s.KubectlDeleteK(t, clientClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-ns1")
					})
				}

				if c.secure {
					logger.Log(t, "checking that the connection is not successful because there's no intention")
					if cfg.EnableTransparentProxy {
						if !c.mirrorK8S {
							k8s.CheckStaticServerConnectionFailing(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
							k8s.CheckStaticServerConnectionFailing(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
						} else {
							k8s.CheckStaticServerConnectionFailing(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
							k8s.CheckStaticServerConnectionFailing(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
						}
					} else {
						k8s.CheckStaticServerConnectionFailing(t, serverClusterStaticClientOpts, "http://localhost:1234")
						k8s.CheckStaticServerConnectionFailing(t, clientClusterStaticClientOpts, "http://localhost:1234")
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
							},
						},
					}

					// Set the destination namespace to be the same
					// unless mirrorK8S is true.
					if !c.mirrorK8S {
						intention.Namespace = c.destinationNamespace
						intention.Sources[0].Namespace = c.destinationNamespace
					}

					logger.Log(t, "creating intention")
					intention.Sources[0].Partition = secondaryPartition
					_, _, err := consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: defaultPartition})
					require.NoError(t, err)
					intention.Sources[0].Partition = defaultPartition
					_, _, err = consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: secondaryPartition})
					require.NoError(t, err)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
						_, err := consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: defaultPartition})
						require.NoError(t, err)
						_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: secondaryPartition})
						require.NoError(t, err)
					})
				}

				logger.Log(t, "checking that connection is successful")
				if cfg.EnableTransparentProxy {
					if !c.mirrorK8S {
						k8s.CheckStaticServerConnectionSuccessful(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionSuccessful(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
					} else {
						k8s.CheckStaticServerConnectionSuccessful(t, serverClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionSuccessful(t, clientClusterStaticClientOpts, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
					}
				} else {
					k8s.CheckStaticServerConnectionSuccessful(t, serverClusterStaticClientOpts, "http://localhost:1234")
					k8s.CheckStaticServerConnectionSuccessful(t, clientClusterStaticClientOpts, "http://localhost:1234")
				}

				// Test that kubernetes readiness status is synced to Consul.
				// Create the file so that the readiness probe of the static-server pod fails.
				logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
				k8s.RunKubectl(t, serverClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")
				k8s.RunKubectl(t, clientClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "--", "touch", "/tmp/unhealthy")

				// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
				// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
				// We are expecting a "connection reset by peer" error because in a case of health checks,
				// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
				// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
				logger.Log(t, "checking that connection is unsuccessful")
				if cfg.EnableTransparentProxy {
					if !c.mirrorK8S {
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, serverClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, clientClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
					} else {
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, serverClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, clientClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
					}
				} else {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, serverClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, clientClusterStaticClientOpts, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "http://localhost:1234")
				}
			})
		})
	}
}
