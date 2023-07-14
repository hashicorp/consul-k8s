// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that Sync Catalog works in a default and ACLsEnabled installations for partitions.
func TestPartitions_Sync(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}
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
			"default destination namespace; ACLs and auto-encrypt enabled",
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
			"single destination namespace; ACLs and auto-encrypt enabled",
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
			"mirror k8s namespaces; ACLs and auto-encrypt enabled",
			staticServerNamespace,
			true,
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			primaryClusterContext := env.DefaultContext(t)
			secondaryClusterContext := env.Context(t, 1)

			commonHelmValues := map[string]string{
				"global.adminPartitions.enabled": "true",
				"global.enableConsulNamespaces":  "true",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.ACLsEnabled),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsEnabled),

				"syncCatalog.enabled": "true",
				// When mirroringK8S is set, this setting is ignored.
				"syncCatalog.consulNamespaces.consulDestinationNamespace": c.destinationNamespace,
				"syncCatalog.consulNamespaces.mirroringK8S":               strconv.FormatBool(c.mirrorK8S),
				"syncCatalog.addK8SNamespaceSuffix":                       "false",

				"dns.enabled":           "true",
				"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),
			}

			serverHelmValues := map[string]string{
				"server.exposeGossipAndRPCPorts": "true",
			}

			// On Kind, there are no load balancers but since all clusters
			// share the same node network (docker bridge), we can use
			// a NodePort service so that we can access node(s) in a different Kind cluster.
			if cfg.UseKind {
				serverHelmValues["server.exposeService.type"] = "NodePort"
				serverHelmValues["server.exposeService.nodePort.https"] = "30000"
			}

			releaseName := helpers.RandomName()

			helpers.MergeMaps(serverHelmValues, commonHelmValues)

			// Install the consul cluster with servers in the default kubernetes context.
			primaryConsulCluster := consul.NewHelmCluster(t, serverHelmValues, primaryClusterContext, cfg, releaseName)
			primaryConsulCluster.Create(t)

			// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
			caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)
			caKeySecretName := fmt.Sprintf("%s-consul-ca-key", releaseName)

			logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
			k8s.CopySecret(t, primaryClusterContext, secondaryClusterContext, caCertSecretName)

			if !c.ACLsEnabled {
				// When auto-encrypt is disabled, we need both
				// the CA cert and CA key to be available in the clients cluster to generate client certificates and keys.
				logger.Logf(t, "retrieving ca key secret %s from the server cluster and applying to the client cluster", caKeySecretName)
				k8s.CopySecret(t, primaryClusterContext, secondaryClusterContext, caKeySecretName)
			}

			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
			if c.ACLsEnabled {
				logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
				k8s.CopySecret(t, primaryClusterContext, secondaryClusterContext, partitionToken)
			}

			partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
			partitionSvcAddress := k8s.ServiceHost(t, cfg, primaryClusterContext, partitionServiceName)

			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryClusterContext)

			// Create client cluster.
			clientHelmValues := map[string]string{
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
				clientHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
				clientHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
				clientHelmValues["externalServers.k8sAuthMethodHost"] = k8sAuthMethodHost
			} else {
				// Provide CA key when auto-encrypt is disabled.
				clientHelmValues["global.tls.caKey.secretName"] = caKeySecretName
				clientHelmValues["global.tls.caKey.secretKey"] = "tls.key"
			}

			if cfg.UseKind {
				clientHelmValues["externalServers.httpsPort"] = "30000"
			}

			helpers.MergeMaps(clientHelmValues, commonHelmValues)

			// Install the consul cluster without servers in the client cluster kubernetes context.
			secondaryConsulCluster := consul.NewHelmCluster(t, clientHelmValues, secondaryClusterContext, cfg, releaseName)
			secondaryConsulCluster.Create(t)

			primaryStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: primaryClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  primaryClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}
			secondaryStaticServerOpts := &terratestk8s.KubectlOptions{
				ContextName: secondaryClusterContext.KubectlOptions(t).ContextName,
				ConfigPath:  secondaryClusterContext.KubectlOptions(t).ConfigPath,
				Namespace:   staticServerNamespace,
			}

			logger.Logf(t, "creating namespaces %s in servers cluster", staticServerNamespace)
			k8s.RunKubectl(t, primaryClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, primaryClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			logger.Logf(t, "creating namespaces %s in clients cluster", staticServerNamespace)
			k8s.RunKubectl(t, secondaryClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, secondaryClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
			})

			consulClient, _ := primaryConsulCluster.SetupConsulClient(t, c.ACLsEnabled)

			defaultPartitionQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: defaultPartition}
			secondaryPartitionQueryOpts := &api.QueryOptions{Namespace: staticServerNamespace, Partition: secondaryPartition}

			if !c.mirrorK8S {
				defaultPartitionQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: defaultPartition}
				secondaryPartitionQueryOpts = &api.QueryOptions{Namespace: c.destinationNamespace, Partition: secondaryPartition}
			}

			// Check that the ACL token is deleted.
			if c.ACLsEnabled {
				// We need to register the cleanup function before we create the deployments
				// because golang will execute them in reverse order i.e. the last registered
				// cleanup function will be executed first.
				t.Cleanup(func() {
					if c.ACLsEnabled {
						retry.Run(t, func(r *retry.R) {
							tokens, _, err := consulClient.ACL().TokenList(defaultPartitionQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}

							tokens, _, err = consulClient.ACL().TokenList(secondaryPartitionQueryOpts)
							require.NoError(r, err)
							for _, token := range tokens {
								require.NotContains(r, token.Description, staticServerName)
							}
						})
					}
				})
			}

			logger.Log(t, "creating a static-server with a service")
			// create service in default partition.
			k8s.DeployKustomize(t, primaryStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
			// create service in secondary partition.
			k8s.DeployKustomize(t, secondaryStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")

			logger.Log(t, "checking that the service has been synced to Consul")
			var services map[string][]string
			counter := &retry.Counter{Count: 30, Wait: 30 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var err error
				// list services in default partition catalog.
				services, _, err = consulClient.Catalog().Services(defaultPartitionQueryOpts)
				require.NoError(r, err)
				require.Contains(r, services, staticServerName)
				if _, ok := services[staticServerName]; !ok {
					r.Errorf("service '%s' is not in Consul's list of services %s in the default partition", staticServerName, services)
				}
				// list services in secondary partition catalog.
				services, _, err = consulClient.Catalog().Services(secondaryPartitionQueryOpts)
				require.NoError(r, err)
				require.Contains(r, services, staticServerName)
				if _, ok := services[staticServerName]; !ok {
					r.Errorf("service '%s' is not in Consul's list of services %s in the secondary partition", staticServerName, services)
				}
			})
			// validate service in the default partition.
			service, _, err := consulClient.Catalog().Service(staticServerName, "", defaultPartitionQueryOpts)
			require.NoError(t, err)
			require.Equal(t, 1, len(service))
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
			// validate service in the secondary partition.
			service, _, err = consulClient.Catalog().Service(staticServerName, "", secondaryPartitionQueryOpts)
			require.NoError(t, err)
			require.Equal(t, 1, len(service))
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
		})
	}
}
