// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const StaticClientName = "static-client"
const staticServerName = "static-server"
const staticServerNamespace = "ns1"
const StaticClientNamespace = "ns2"

// Test that Connect works in a default and ACLsEnabled installations for X-Partition and in-partition networking.
func TestPartitions_Connect(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	// TODO: We are monitoring that NET-5819 is fixed, if this test is still flaking in CNI, re-enable this skip
	//if cfg.EnableCNI {
	//	t.Skipf("TODO(flaky): NET-5819")
	//}

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	const defaultPartition = "default"
	const secondaryPartition = "secondary"
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
			"default destination namespace; ACLs enabled",
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
			"single destination namespace; ACLs enabled",
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
			"mirror k8s namespaces; ACLs enabled",
			staticServerNamespace,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultPartitionClusterContext := env.DefaultContext(t)
			secondaryPartitionClusterContext := env.Context(t, 1)

			commonHelmValues := map[string]string{
				"global.adminPartitions.enabled": "true",
				"global.enableConsulNamespaces":  "true",
				"global.logLevel":                "debug",

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
			}

			// Setup the default partition
			defaultPartitionHelmValues := make(map[string]string)

			// On Kind, there are no load balancers but since all clusters
			// share the same node network (docker bridge), we can use
			// a NodePort service so that we can access node(s) in a different Kind cluster.
			if cfg.UseKind {
				defaultPartitionHelmValues["meshGateway.service.type"] = "NodePort"
				defaultPartitionHelmValues["meshGateway.service.nodePort"] = "30200" // todo: do we need to set this port?
				defaultPartitionHelmValues["server.exposeService.type"] = "NodePort"
				defaultPartitionHelmValues["server.exposeService.nodePort.https"] = "30000"
				defaultPartitionHelmValues["server.exposeService.nodePort.grpc"] = "30100"
			}

			releaseName := helpers.RandomName()

			helpers.MergeMaps(defaultPartitionHelmValues, commonHelmValues)

			// Install the consul cluster with servers in the default kubernetes context.
			serverConsulCluster := consul.NewHelmCluster(t, defaultPartitionHelmValues, defaultPartitionClusterContext, cfg, releaseName)
			serverConsulCluster.Create(t)

			// Copy secrets from the default partition to the secondary partition
			// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
			caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)

			logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
			k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, caCertSecretName)

			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
			if c.ACLsEnabled {
				logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
				k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, partitionToken)
			}

			partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
			partitionSvcAddress := k8s.ServiceHost(t, cfg, defaultPartitionClusterContext, partitionServiceName)

			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryPartitionClusterContext)

			// Create secondary partition cluster.
			secondaryPartitionHelmValues := map[string]string{
				"global.enabled": "false",

				"global.adminPartitions.name": secondaryPartition,

				"global.tls.caCert.secretName": caCertSecretName,
				"global.tls.caCert.secretKey":  "tls.crt",

				"externalServers.enabled":       "true",
				"externalServers.hosts[0]":      partitionSvcAddress,
				"externalServers.tlsServerName": "server.dc1.consul",
			}

			if c.ACLsEnabled {
				// Setup partition token and auth method host if ACLs enabled.
				secondaryPartitionHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
				secondaryPartitionHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
				secondaryPartitionHelmValues["externalServers.k8sAuthMethodHost"] = k8sAuthMethodHost
			}

			if cfg.UseKind {
				secondaryPartitionHelmValues["externalServers.httpsPort"] = "30000"
				secondaryPartitionHelmValues["externalServers.grpcPort"] = "30100"
				secondaryPartitionHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryPartitionHelmValues["meshGateway.service.nodePort"] = "30200"
			}

			helpers.MergeMaps(secondaryPartitionHelmValues, commonHelmValues)

			// Install the consul cluster without servers in the client cluster kubernetes context.
			clientConsulCluster := consul.NewHelmCluster(t, secondaryPartitionHelmValues, secondaryPartitionClusterContext, cfg, releaseName)
			clientConsulCluster.Create(t)

			defaultPartitionClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: defaultPartitionClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  defaultPartitionClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			defaultPartitionClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: defaultPartitionClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  defaultPartitionClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   StaticClientNamespace,
			}
			secondaryPartitionClusterStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: secondaryPartitionClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  secondaryPartitionClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			secondaryPartitionClusterStaticClientOpts := &terratestk8s.KubectlOptions{
				ContextName: secondaryPartitionClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  secondaryPartitionClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   StaticClientNamespace,
			}

			logger.Logf(t, "creating namespaces %s and %s in servers cluster", staticServerNamespace, StaticClientNamespace)
			k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "create", "ns", StaticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, StaticClientNamespace)
			})

			logger.Logf(t, "creating namespaces %s and %s in clients cluster", staticServerNamespace, StaticClientNamespace)
			k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "create", "ns", StaticClientNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace, StaticClientNamespace)
			})

			consulClient, _ := serverConsulCluster.SetupConsulClient(t, c.ACLsEnabled)

			serverQueryServerOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: defaultPartition}
			clientQueryServerOpts := &api.QueryOptions{Namespace: StaticClientNamespace, Partition: defaultPartition}

			serverQueryClientOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: secondaryPartition}
			clientQueryClientOpts := &api.QueryOptions{Namespace: StaticClientNamespace, Partition: secondaryPartition}

			if !c.mirrorK8S {
				serverQueryServerOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: defaultPartition}
				clientQueryServerOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: defaultPartition}
				serverQueryClientOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: secondaryPartition}
				clientQueryClientOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: secondaryPartition}
			}

			// Check that the ACL token is deleted.
			if c.ACLsEnabled {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					if c.ACLsEnabled {
						retry.Run(t, func(r *retry.R) {
							tokens, _, err := consulClient.ACL().TokenList(serverQueryServerOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(clientQueryServerOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, StaticClientName)
							}
							tokens, _, err = consulClient.ACL().TokenList(serverQueryClientOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(clientQueryClientOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, StaticClientName)
							}
						})
					}
				})
			}

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways.
			logger.Log(t, "creating proxy-defaults config")
			kustomizeDir := "../fixtures/bases/mesh-gateway"
			logger.Log(t, "==================================> 1111111")

			k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), kustomizeDir)
			logger.Log(t, "==================================> 2222222")

			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				logger.Log(t, "==================================> 33333333")

				k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), kustomizeDir)
			})
			logger.Log(t, "==================================> 44444444444")

			k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), kustomizeDir)
			logger.Log(t, "==================================> 55555555555")

			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				logger.Log(t, "==================================> 6666666666")
				k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), kustomizeDir)
			})
			// This section of the tests runs the in-partition networking tests.
			t.Run("in-partition", func(t *testing.T) {
				logger.Log(t, "test in-partition networking")
				logger.Log(t, "creating static-server and static-client deployments in server cluster")
				k8s.DeployKustomize(t, defaultPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
					} else {
						k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
					}
				}
				logger.Log(t, "creating static-server and static-client deployments in client cluster")
				k8s.DeployKustomize(t, secondaryPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
					} else {
						k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-namespaces")
					}
				}
				// Check that both static-server and static-client have been injected and now have 2 containers in server cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := defaultPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					require.NoError(t, err)
					require.Len(t, podList.Items, 1)
					require.Len(t, podList.Items[0].Spec.Containers, 2)
				}

				// Check that both static-server and static-client have been injected and now have 2 containers in client cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := secondaryPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
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

				services, _, err = consulClient.Catalog().Service(StaticClientName, "", clientQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				// Client cluster.
				services, _, err = consulClient.Catalog().Service(staticServerName, "", serverQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(StaticClientName, "", clientQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				if c.ACLsEnabled {
					logger.Log(t, "checking that the connection is not successful because there's no intention")
					if cfg.EnableTransparentProxy {
						k8s.CheckStaticServerConnectionFailing(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
						k8s.CheckStaticServerConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
					} else {
						k8s.CheckStaticServerConnectionFailing(t, defaultPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
						k8s.CheckStaticServerConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
					}

					intention := &api.ServiceIntentionsConfigEntry{
						Kind:      api.ServiceIntentions,
						Name:      staticServerName,
						Namespace: staticServerNamespace,
						Sources: []*api.SourceIntention{
							{
								Name:      StaticClientName,
								Namespace: StaticClientNamespace,
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
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
						_, err := consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: defaultPartition})
						require.NoError(t, err)
						_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: secondaryPartition})
						require.NoError(t, err)
					})
				}

				logger.Log(t, "checking that connection is successful")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionSuccessful(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
					k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				} else {
					k8s.CheckStaticServerConnectionSuccessful(t, defaultPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
					k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
				}

				// Test that kubernetes readiness status is synced to Consul.
				// Create the file so that the readiness probe of the static-server pod fails.
				logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
				k8s.RunKubectl(t, defaultPartitionClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")
				k8s.RunKubectl(t, secondaryPartitionClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")

				// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
				// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
				// We are expecting a "connection reset by peer" error because in a case of health checks,
				// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
				// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
				logger.Log(t, "checking that connection is unsuccessful")
				if cfg.EnableTransparentProxy {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, defaultPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.%s", staticServerNamespace))
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.%s", staticServerNamespace))
				} else {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, defaultPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				}
			})
			// This section of the tests runs the cross-partition networking tests.
			t.Run("cross-partition", func(t *testing.T) {
				logger.Log(t, "test cross-partition networking")
				logger.Log(t, "creating static-server and static-client deployments in server cluster")
				k8s.DeployKustomize(t, defaultPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-partition")
					} else {
						k8s.DeployKustomize(t, defaultPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/ns-partition")
					}
				}
				logger.Log(t, "creating static-server and static-client deployments in client cluster")
				k8s.DeployKustomize(t, secondaryPartitionClusterStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
				if cfg.EnableTransparentProxy {
					k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
				} else {
					if c.destinationNamespace == defaultNamespace {
						k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition")
					} else {
						k8s.DeployKustomize(t, secondaryPartitionClusterStaticClientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/ns-default-partition")
					}
				}
				// Check that both static-server and static-client have been injected and now have 2 containers in server cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := defaultPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
						LabelSelector: labelSelector,
					})
					require.NoError(t, err)
					require.Len(t, podList.Items, 1)
					require.Len(t, podList.Items[0].Spec.Containers, 2)
				}

				// Check that both static-server and static-client have been injected and now have 2 containers in client cluster.
				for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
					podList, err := secondaryPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{
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

				services, _, err = consulClient.Catalog().Service(StaticClientName, "", clientQueryServerOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				// Client cluster.
				services, _, err = consulClient.Catalog().Service(staticServerName, "", serverQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				services, _, err = consulClient.Catalog().Service(StaticClientName, "", clientQueryClientOpts)
				require.NoError(t, err)
				require.Len(t, services, 1)

				logger.Log(t, "creating exported services")
				if c.destinationNamespace == defaultNamespace {
					k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
					k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
						k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
						k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
					})
				} else {
					k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
					k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-ns1")
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
						k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-ns1")
						k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-ns1")
					})
				}

				if c.ACLsEnabled {
					logger.Log(t, "checking that the connection is not successful because there's no intention")
					if cfg.EnableTransparentProxy {
						if !c.mirrorK8S {
							k8s.CheckStaticServerConnectionFailing(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
							k8s.CheckStaticServerConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
						} else {
							k8s.CheckStaticServerConnectionFailing(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
							k8s.CheckStaticServerConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
						}
					} else {
						k8s.CheckStaticServerConnectionFailing(t, defaultPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
						k8s.CheckStaticServerConnectionFailing(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
					}

					intention := &api.ServiceIntentionsConfigEntry{
						Name:      staticServerName,
						Kind:      api.ServiceIntentions,
						Namespace: staticServerNamespace,
						Sources: []*api.SourceIntention{
							{
								Name:      StaticClientName,
								Namespace: StaticClientNamespace,
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

					logger.Log(t, "creating intentions in each partition")
					intention.Sources[0].Partition = secondaryPartition
					_, _, err := consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: defaultPartition})
					require.NoError(t, err)
					intention.Sources[0].Partition = defaultPartition
					_, _, err = consulClient.ConfigEntries().Set(intention, &api.WriteOptions{Partition: secondaryPartition})
					require.NoError(t, err)
					helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
						_, err := consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: defaultPartition})
						require.NoError(t, err)
						_, err = consulClient.ConfigEntries().Delete(api.ServiceIntentions, staticServerName, &api.WriteOptions{Partition: secondaryPartition})
						require.NoError(t, err)
					})
				}

				logger.Log(t, "checking that connection is successful")
				if cfg.EnableTransparentProxy {
					if !c.mirrorK8S {
						k8s.CheckStaticServerConnectionSuccessful(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
					} else {
						k8s.CheckStaticServerConnectionSuccessful(t, defaultPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
					}
				} else {
					k8s.CheckStaticServerConnectionSuccessful(t, defaultPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
					k8s.CheckStaticServerConnectionSuccessful(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, "http://localhost:1234")
				}

				// Test that kubernetes readiness status is synced to Consul.
				// Create the file so that the readiness probe of the static-server pod fails.
				logger.Log(t, "testing k8s -> consul health checks sync by making the static-server unhealthy")
				k8s.RunKubectl(t, defaultPartitionClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")
				k8s.RunKubectl(t, secondaryPartitionClusterStaticServerOpts, "exec", "deploy/"+staticServerName, "-c", "static-server", "--", "touch", "/tmp/unhealthy")

				// The readiness probe should take a moment to be reflected in Consul, CheckStaticServerConnection will retry
				// until Consul marks the service instance unavailable for mesh traffic, causing the connection to fail.
				// We are expecting a "connection reset by peer" error because in a case of health checks,
				// there will be no healthy proxy host to connect to. That's why we can't assert that we receive an empty reply
				// from server, which is the case when a connection is unsuccessful due to intentions in other tests.
				logger.Log(t, "checking that connection is unsuccessful")
				if cfg.EnableTransparentProxy {
					if !c.mirrorK8S {
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, defaultPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", c.destinationNamespace, defaultPartition))
					} else {
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, defaultPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, secondaryPartition))
						k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server", "curl: (7) Failed to connect to static-server.ns1 port 80: Connection refused"}, "", fmt.Sprintf("http://static-server.virtual.%s.ns.%s.ap.dc1.dc.consul", staticServerNamespace, defaultPartition))
					}
				} else {
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, defaultPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
					k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryPartitionClusterStaticClientOpts, StaticClientName, false, []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}, "", "http://localhost:1234")
				}
			})
		})
	}
}
