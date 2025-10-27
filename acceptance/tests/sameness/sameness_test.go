// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sameness

import (
	ctx "context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	cluster01Partition    = "ap1"
	cluster01Datacenter   = "dc1"
	cluster02Datacenter   = "dc2"
	cluster03Datacenter   = "dc3"
	staticClientNamespace = "ns1"
	staticServerNamespace = "ns2"

	keyCluster01a = "cluster-01-a"
	keyCluster01b = "cluster-01-b"
	keyCluster02a = "cluster-02-a"
	keyCluster03a = "cluster-03-a"

	staticServerName = "static-server"
	staticClientName = "static-client"

	staticServerDeployment = "deploy/static-server"
	staticClientDeployment = "deploy/static-client"

	peerName1a = keyCluster01a
	peerName1b = keyCluster01b
	peerName2a = keyCluster02a
	peerName3a = keyCluster03a

	samenessGroupName = "group-01"

	cluster01Region = "us-east-1"
	cluster02Region = "us-west-1"
	cluster03Region = "us-east-2"

	retryTimeout = 50 * time.Minute
)

func TestFailover_Connect(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []struct {
		name        string
		ACLsEnabled bool
	}{
		{
			"default failover",
			false,
		},
		{
			"secure failover",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			/*
				Architecture Overview:
					Primary Datacenter (DC1)
						Default Partition
							Peer -> DC2 (cluster-02-a)
							Peer -> DC3 (cluster-03-a)
						AP1 Partition
							Peer -> DC2 (cluster-02-a)
							Peer -> DC3 (cluster-03-a)
					Datacenter 2 (DC2)
						Default Partition
							Peer -> DC1 (cluster-01-a)
							Peer -> DC1 (cluster-01-b)
							Peer -> DC3 (cluster-03-a)
					Datacenter 3 (DC3)
						Default Partition
							Peer -> DC1 (cluster-01-a)
							Peer -> DC1 (cluster-01-b)
							Peer -> DC2 (cluster-02-a)


				Architecture Diagram + failover scenarios from perspective of DC1 Default Partition Static-Server
				+-------------------------------------------+
				|                                           |
				|        DC1                                |
				|                                           |
				|    +-----------------------------+        |                 +-----------------------------------+
				|    |                             |        |                 |        DC2                        |
				|    |  +------------------+       |        |    Failover 2   |       +------------------+        |
				|    |  |                  +-------+--------+-----------------+------>|                  |        |
				|    |  |  Static-Server   |       |        |                 |       |  Static-Server   |        |
				|    |  |                  +-------+---+    |                 |       |                  |        |
				|    |  |                  |       |   |    |                 |       |                  |        |
				|    |  |                  |       |   |    |                 |       |                  |        |
				|    |  |                  +-------+---+----+-------------+   |       |                  |        |
				|    |  +------------------+       |   |    |             |   |       +------------------+        |
				|    |  Admin Partitions: Default  |   |    |             |   |                                   |
				|    |  Name: cluster-01-a         |   |    |             |   |     Admin Partitions: Default     |
				|    |  Region: us-east-1          |   |    |             |   |     Name: cluster-02-a            |
				|    +-----------------------------+   |    |             |   |     Region: us-west-1             |
				|                                      |    |             |   +-----------------------------------+
				|                            Failover 1|    |  Failover 3 |
				|   +-------------------------------+  |    |             |   +-----------------------------------+
				|   |                               |  |    |             |   |        DC3                        |
				|   |    +------------------+       |  |    |             |   |       +------------------+        |
				|   |    |                  |       |  |    |             |   |       |  Static-Server   |        |
				|   |    |  Static-Server   |       |  |    |             |   |       |                  |        |
				|   |    |                  |       |  |    |             |   |       |                  |        |
				|   |    |                  |       |  |    |             +---+------>|                  |        |
				|   |    |                  |<------+--+    |                 |       |                  |        |
				|   |    |                  |       |       |                 |       +------------------+        |
				|   |    +------------------+       |       |                 |                                   |
				|   |    Admin Partitions: ap1      |       |                 |     Admin Partitions: Default     |
				|   |    Name: cluster-01-b         |       |                 |     Name: cluster-03-a            |
				|   |    Region: us-east-1          |       |                 |     Region: us-east-2             |
				|   +-------------------------------+       |                 |                                   |
				|                                           |                 +-----------------------------------+
				+-------------------------------------------+
			*/

			testClusters := clusters{
				keyCluster01a: {name: peerName1a, context: env.DefaultContext(t), hasServer: true, acceptors: []string{peerName2a, peerName3a}, locality: localityForRegion(cluster01Region)},
				keyCluster01b: {name: peerName1b, context: env.Context(t, 1), partition: cluster01Partition, hasServer: false, acceptors: []string{peerName2a, peerName3a}, locality: localityForRegion(cluster01Region)},
				keyCluster02a: {name: peerName2a, context: env.Context(t, 2), hasServer: true, acceptors: []string{peerName3a}, locality: localityForRegion(cluster02Region)},
				keyCluster03a: {name: peerName3a, context: env.Context(t, 3), hasServer: true, locality: localityForRegion(cluster03Region)},
			}

			// Set primary clusters per cluster
			// This is helpful for cases like DNS with partitions where many aspects of the primary cluster must be used
			testClusters[keyCluster01a].primaryCluster = testClusters[keyCluster01a]
			testClusters[keyCluster01b].primaryCluster = testClusters[keyCluster01a]
			testClusters[keyCluster02a].primaryCluster = testClusters[keyCluster02a]
			testClusters[keyCluster03a].primaryCluster = testClusters[keyCluster03a]

			// Setup Namespaces.
			for _, v := range testClusters {
				createNamespaces(t, cfg, v.context)
			}

			commonHelmValues := map[string]string{
				"global.peering.enabled": "true",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.ACLsEnabled),

				"global.enableConsulNamespaces": "true",

				"global.adminPartitions.enabled": "true",

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsEnabled),

				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"dns.enabled": "true",
				"connectInject.sidecarProxy.lifecycle.defaultEnabled": "false",
			}

			releaseName := helpers.RandomName()

			var wg sync.WaitGroup

			// Create the cluster-01-a and cluster-01-b
			// create in same routine as 01-b depends on 01-a being created first
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Create the cluster-01-a
				defaultPartitionHelmValues := map[string]string{
					"global.datacenter": cluster01Datacenter,
				}

				// On Kind, there are no load balancers but since all clusters
				// share the same node network (docker bridge), we can use
				// a NodePort service so that we can access node(s) in a different Kind cluster.
				if cfg.UseKind {
					defaultPartitionHelmValues["meshGateway.service.type"] = "NodePort"
					defaultPartitionHelmValues["meshGateway.service.nodePort"] = "30200"
					defaultPartitionHelmValues["server.exposeService.type"] = "NodePort"
					defaultPartitionHelmValues["server.exposeService.nodePort.https"] = "30000"
					defaultPartitionHelmValues["server.exposeService.nodePort.grpc"] = "30100"
				}
				helpers.MergeMaps(defaultPartitionHelmValues, commonHelmValues)

				testClusters[keyCluster01a].helmCluster = consul.NewHelmCluster(t, defaultPartitionHelmValues, testClusters[keyCluster01a].context, cfg, releaseName)
				testClusters[keyCluster01a].helmCluster.Create(t)

				// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
				caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)

				logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
				k8s.CopySecret(t, testClusters[keyCluster01a].context, testClusters[keyCluster01b].context, caCertSecretName)

				// Create Secondary Partition Cluster (cluster-01-b) which will apply the primary (dc1) datacenter.
				partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
				if c.ACLsEnabled {
					logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
					k8s.CopySecret(t, testClusters[keyCluster01a].context, testClusters[keyCluster01b].context, partitionToken)
				}

				partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
				partitionSvcAddress := k8s.ServiceHost(t, cfg, testClusters[keyCluster01a].context, partitionServiceName)

				k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, testClusters[keyCluster01b].context)

				secondaryPartitionHelmValues := map[string]string{
					"global.enabled":    "false",
					"global.datacenter": cluster01Datacenter,

					"global.adminPartitions.name": cluster01Partition,

					"global.tls.caCert.secretName": caCertSecretName,
					"global.tls.caCert.secretKey":  "tls.crt",

					"externalServers.enabled":       "true",
					"externalServers.hosts[0]":      partitionSvcAddress,
					"externalServers.tlsServerName": fmt.Sprintf("server.%s.consul", cluster01Datacenter),
					"global.server.enabled":         "false",
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

				testClusters[keyCluster01b].helmCluster = consul.NewHelmCluster(t, secondaryPartitionHelmValues, testClusters[keyCluster01b].context, cfg, releaseName)
				testClusters[keyCluster01b].helmCluster.Create(t)
			}()

			// Create cluster-02-a Cluster.
			wg.Add(1)
			go func() {
				defer wg.Done()
				PeerOneHelmValues := map[string]string{
					"global.datacenter": cluster02Datacenter,
				}

				if cfg.UseKind {
					PeerOneHelmValues["server.exposeGossipAndRPCPorts"] = "true"
					PeerOneHelmValues["meshGateway.service.type"] = "NodePort"
					PeerOneHelmValues["meshGateway.service.nodePort"] = "30100"
				}
				helpers.MergeMaps(PeerOneHelmValues, commonHelmValues)

				testClusters[keyCluster02a].helmCluster = consul.NewHelmCluster(t, PeerOneHelmValues, testClusters[keyCluster02a].context, cfg, releaseName)
				testClusters[keyCluster02a].helmCluster.Create(t)
			}()

			// Create cluster-03-a Cluster.
			wg.Add(1)
			go func() {
				defer wg.Done()
				PeerTwoHelmValues := map[string]string{
					"global.datacenter": cluster03Datacenter,
				}

				if cfg.UseKind {
					PeerTwoHelmValues["server.exposeGossipAndRPCPorts"] = "true"
					PeerTwoHelmValues["meshGateway.service.type"] = "NodePort"
					PeerTwoHelmValues["meshGateway.service.nodePort"] = "30100"
				}
				helpers.MergeMaps(PeerTwoHelmValues, commonHelmValues)

				testClusters[keyCluster03a].helmCluster = consul.NewHelmCluster(t, PeerTwoHelmValues, testClusters[keyCluster03a].context, cfg, releaseName)
				testClusters[keyCluster03a].helmCluster.Create(t)
			}()

			// Wait for the clusters to start up
			logger.Log(t, "waiting for clusters to start up . . .")
			wg.Wait()

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways and set server and client opts.
			for k, v := range testClusters {
				logger.Logf(t, "applying resources on %s", v.context.KubectlOptions(t).ContextName)

				// Client will use the client namespace.
				testClusters[k].clientOpts = &terratestk8s.KubectlOptions{
					ContextName: v.context.KubectlOptions(t).ContextName,
					ConfigPath:  v.context.KubectlOptions(t).ConfigPath,
					Namespace:   staticClientNamespace,
				}

				// Server will use the server namespace.
				testClusters[k].serverOpts = &terratestk8s.KubectlOptions{
					ContextName: v.context.KubectlOptions(t).ContextName,
					ConfigPath:  v.context.KubectlOptions(t).ConfigPath,
					Namespace:   staticServerNamespace,
				}

				// Sameness Defaults need to be applied first so that the sameness group exists.
				applyResources(t, cfg, "../fixtures/bases/mesh-gateway", v.context.KubectlOptions(t))
				applyResources(t, cfg, "../fixtures/bases/sameness/override-ns", v.serverOpts)

				// Only assign a client if the cluster is running a Consul server.
				if v.hasServer {
					testClusters[k].client, _ = testClusters[k].helmCluster.SetupConsulClient(t, c.ACLsEnabled)
				}
			}

			// Assign the client default partition client to the partition
			testClusters[keyCluster01b].client = testClusters[keyCluster01a].client

			// Apply Mesh resource to default partition and peers
			for _, v := range testClusters {
				if v.hasServer {
					applyResources(t, cfg, "../fixtures/bases/sameness/peering/mesh", v.context.KubectlOptions(t))
				}
			}

			// Apply locality to clusters
			for _, v := range testClusters {
				setK8sNodeLocality(t, v.context, v)
			}

			// Peering/Dialer relationship
			/*
				cluster-01-a         cluster-02-a
				 Dialer -> 2a          1a -> acceptor
				 Dialer -> 3a          1b -> acceptor
				                       Dialer -> 3a

				cluster-01-b         cluster-03-a
				 Dialer -> 2a          1a -> acceptor
				 Dialer -> 3a          1b -> acceptor
				                       2a -> acceptor
			*/
			for _, v := range []*cluster{testClusters[keyCluster02a], testClusters[keyCluster03a]} {
				logger.Logf(t, "creating acceptor on %s", v.name)
				// Create an acceptor token on the cluster
				applyResources(t, cfg, fmt.Sprintf("../fixtures/bases/sameness/peering/%s-acceptor", v.name), v.context.KubectlOptions(t))

				// Copy secrets to the necessary peers to be used for dialing later
				for _, vv := range testClusters {
					if isAcceptor(v.name, vv.acceptors) {
						acceptorSecretName := v.getPeeringAcceptorSecret(t, cfg, vv.name)
						logger.Logf(t, "acceptor %s created on %s", acceptorSecretName, v.name)

						logger.Logf(t, "copying acceptor token %s from %s to %s", acceptorSecretName, v.name, vv.name)
						copySecret(t, cfg, v.context, vv.context, acceptorSecretName)
					}
				}
			}

			// Create the dialers
			for _, v := range []*cluster{testClusters[keyCluster01a], testClusters[keyCluster01b], testClusters[keyCluster02a]} {
				applyResources(t, cfg, fmt.Sprintf("../fixtures/bases/sameness/peering/%s-dialer", v.name), v.context.KubectlOptions(t))
			}

			// If ACLs are enabled, we need to create the intentions
			if c.ACLsEnabled {
				intention := &api.ServiceIntentionsConfigEntry{
					Name:      staticServerName,
					Kind:      api.ServiceIntentions,
					Namespace: staticServerNamespace,
					Sources: []*api.SourceIntention{
						{
							Name:          staticClientName,
							Namespace:     staticClientNamespace,
							SamenessGroup: samenessGroupName,
							Action:        api.IntentionActionAllow,
						},
					},
				}

				for _, v := range testClusters {
					logger.Logf(t, "creating intentions on server %s", v.name)
					_, _, err := v.client.ConfigEntries().Set(intention, &api.WriteOptions{Partition: v.partition})
					require.NoError(t, err)
				}
			}

			logger.Log(t, "creating exported services")
			for _, v := range testClusters {
				if v.hasServer {
					applyResources(t, cfg, "../fixtures/cases/sameness/exported-services/default-partition", v.context.KubectlOptions(t))
				} else {
					applyResources(t, cfg, "../fixtures/cases/sameness/exported-services/ap1-partition", v.context.KubectlOptions(t))
				}
			}

			// Create sameness group after exporting the services, this will reduce flakiness in an automated test
			for _, v := range testClusters {
				applyResources(t, cfg, fmt.Sprintf("../fixtures/bases/sameness/%s-default-ns", v.name), v.context.KubectlOptions(t))
			}

			// Setup DNS.
			for _, v := range testClusters {
				dnsService, err := v.context.KubernetesClient(t).CoreV1().Services("default").Get(ctx.Background(), fmt.Sprintf("%s-%s", releaseName, "consul-dns"), metav1.GetOptions{})
				require.NoError(t, err)
				v.dnsIP = &dnsService.Spec.ClusterIP
				logger.Logf(t, "%s dnsIP: %s", v.name, *v.dnsIP)
			}

			// Setup Prepared Query.

			for k, v := range testClusters {
				definition := &api.PreparedQueryDefinition{
					Name: fmt.Sprintf("my-query-%s", v.fullTextPartition()),
					Service: api.ServiceQuery{
						Service:       staticServerName,
						SamenessGroup: samenessGroupName,
						Namespace:     staticServerNamespace,
						OnlyPassing:   false,
						Partition:     v.fullTextPartition(),
					},
				}

				pqID, _, err := v.client.PreparedQuery().Create(definition, &api.WriteOptions{})
				require.NoError(t, err)
				logger.Logf(t, "%s PQ ID: %s", v.name, pqID)
				testClusters[k].pqID = &pqID
				testClusters[k].pqName = &definition.Name
			}

			// Create static server/client after the rest of the config is setup for a more stable testing experience
			// Create static server deployments.
			logger.Log(t, "creating static-server and static-client deployments")
			deployCustomizeAsync(t, testClusters[keyCluster01a].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/dc1-default", &wg)
			deployCustomizeAsync(t, testClusters[keyCluster01b].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/dc1-partition", &wg)
			deployCustomizeAsync(t, testClusters[keyCluster02a].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/dc2", &wg)
			deployCustomizeAsync(t, testClusters[keyCluster03a].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/dc3", &wg)

			// Create static client deployments.
			staticClientKustomizeDirDefault := "../fixtures/cases/sameness/static-client/default-partition"
			staticClientKustomizeDirAP1 := "../fixtures/cases/sameness/static-client/ap1-partition"

			// If transparent proxy is enabled create clients without explicit upstreams
			if cfg.EnableTransparentProxy {
				staticClientKustomizeDirDefault = fmt.Sprintf("%s-%s", staticClientKustomizeDirDefault, "tproxy")
				staticClientKustomizeDirAP1 = fmt.Sprintf("%s-%s", staticClientKustomizeDirAP1, "tproxy")
			}

			deployCustomizeAsync(t, testClusters[keyCluster01a].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				staticClientKustomizeDirDefault, &wg)
			deployCustomizeAsync(t, testClusters[keyCluster02a].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				staticClientKustomizeDirDefault, &wg)
			deployCustomizeAsync(t, testClusters[keyCluster03a].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				staticClientKustomizeDirDefault, &wg)
			deployCustomizeAsync(t, testClusters[keyCluster01b].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				staticClientKustomizeDirAP1, &wg)
			wg.Wait()

			// Verify that both static-server and static-client have been injected and now have 2 containers in each cluster.
			// Also get the server IP
			testClusters.setServerIP(t)

			// Everything should be up and running now
			testClusters.verifyServerUpState(t, cfg.EnableTransparentProxy)
			logger.Log(t, "all infrastructure up and running")

			// Verify locality is set on services based on node labels previously applied.
			//
			// This is currently the only locality testing we do for k8s and ensures that single-partition
			// locality-aware routing will function in consul-k8s. In the future, this test will be expanded
			// to test multi-cluster locality-based failover with sameness groups.
			for _, v := range testClusters {
				v.checkLocalities(t)
			}

			// Verify all the failover Scenarios
			logger.Log(t, "verifying failover scenarios")

			subCases := []struct {
				name      string
				server    *cluster
				failovers []struct {
					failoverServer *cluster
					expectedPQ     expectedPQ
				}
			}{
				{
					name:   "cluster-01-a perspective", // This matches the diagram at the beginning of the test
					server: testClusters[keyCluster01a],
					failovers: []struct {
						failoverServer *cluster
						expectedPQ     expectedPQ
					}{
						{failoverServer: testClusters[keyCluster01a], expectedPQ: expectedPQ{partition: "default", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01b], expectedPQ: expectedPQ{partition: "ap1", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster02a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster02a].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster03a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster03a].name, namespace: "ns2"}},
					},
				},
				{
					name:   "cluster-01-b partition perspective",
					server: testClusters[keyCluster01b],
					failovers: []struct {
						failoverServer *cluster
						expectedPQ     expectedPQ
					}{
						{failoverServer: testClusters[keyCluster01b], expectedPQ: expectedPQ{partition: "ap1", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01a], expectedPQ: expectedPQ{partition: "default", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster02a], expectedPQ: expectedPQ{partition: "ap1", peerName: testClusters[keyCluster02a].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster03a], expectedPQ: expectedPQ{partition: "ap1", peerName: testClusters[keyCluster03a].name, namespace: "ns2"}},
					},
				},
				{
					name:   "cluster-02-a perspective",
					server: testClusters[keyCluster02a],
					failovers: []struct {
						failoverServer *cluster
						expectedPQ     expectedPQ
					}{
						{failoverServer: testClusters[keyCluster02a], expectedPQ: expectedPQ{partition: "default", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster01a].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01b], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster01b].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster03a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster03a].name, namespace: "ns2"}},
					},
				},
				{
					name:   "cluster-03-a perspective",
					server: testClusters[keyCluster03a],
					failovers: []struct {
						failoverServer *cluster
						expectedPQ     expectedPQ
					}{
						{failoverServer: testClusters[keyCluster03a], expectedPQ: expectedPQ{partition: "default", peerName: "", namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster01a].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster01b], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster01b].name, namespace: "ns2"}},
						{failoverServer: testClusters[keyCluster02a], expectedPQ: expectedPQ{partition: "default", peerName: testClusters[keyCluster02a].name, namespace: "ns2"}},
					},
				},
			}
			for _, sc := range subCases {
				t.Run(sc.name, func(t *testing.T) {
					// Reset the scale of all servers
					testClusters.resetScale(t)
					testClusters.verifyServerUpState(t, cfg.EnableTransparentProxy)
					// We're resetting the scale, so make sure we have all the new IP addresses saved
					testClusters.setServerIP(t)

					for i, v := range sc.failovers {
						// Verify Failover (If this is the first check, then just verifying we're starting with the right server)
						logger.Log(t, "checking service failover", i)

						if cfg.EnableTransparentProxy {
							sc.server.serviceTargetCheck(t, v.failoverServer.name, fmt.Sprintf("http://static-server.virtual.ns2.ns.%s.ap.consul", sc.server.fullTextPartition()))
						} else {
							sc.server.serviceTargetCheck(t, v.failoverServer.name, "localhost:8080")
						}

						// 1. The admin partition does not contain a server, so DNS service will not resolve on the admin partition cluster
						// 2. A workaround to perform the DNS and PQ queries on the primary datacenter cluster by specifying the admin partition
						// e.g kubectl --context kind-dc1 --namespace ns1 exec -i deploy/static-client -c static-client \
						//	-- dig @test-3lmypr-consul-dns.default static-server.service.ns2.ns.mine.sg.ap1.ap.consul
						// Verify DNS.
						logger.Log(t, "verifying dns", i)
						sc.server.dnsFailoverCheck(t, cfg, releaseName, v.failoverServer)

						logger.Log(t, "verifying prepared query", i)
						sc.server.preparedQueryFailoverCheck(t, cfg, releaseName, v.expectedPQ, v.failoverServer)

						// Scale down static-server on the current failover, will fail over to the next.
						logger.Logf(t, "scaling server down on %s", v.failoverServer.name)
						k8s.KubectlScale(t, v.failoverServer.serverOpts, staticServerDeployment, 0)
					}
				})
			}
		})
	}
}

type expectedPQ struct {
	partition string
	peerName  string
	namespace string
}

type cluster struct {
	name           string
	partition      string
	locality       api.Locality
	context        environment.TestContext
	helmCluster    *consul.HelmCluster
	client         *api.Client
	hasServer      bool
	serverOpts     *terratestk8s.KubectlOptions
	clientOpts     *terratestk8s.KubectlOptions
	staticServerIP *string
	pqID           *string
	pqName         *string
	dnsIP          *string
	acceptors      []string
	primaryCluster *cluster
}

func (c *cluster) fullTextPartition() string {
	if c.partition == "" {
		return "default"
	} else {
		return c.partition
	}
}

// serviceTargetCheck verifies that curling the `static-server` using the `static-client` responds with the expected
// cluster name. Each static-server responds with a unique name so that we can verify failover occured as expected.
func (c *cluster) serviceTargetCheck(t *testing.T, expectedName string, curlAddress string) {
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 5 * time.Second}
	var resp string
	var err error
	retry.RunWith(timer, t, func(r *retry.R) {
		// Use -s/--silent and -S/--show-error flags w/ curl to reduce noise during retries.
		// This silences extra output like the request progress bar, but preserves errors.
		resp, err = k8s.RunKubectlAndGetOutputE(r, c.clientOpts, "exec", "-i",
			staticClientDeployment, "-c", staticClientName, "--", "curl", "-sS", curlAddress)
		require.NoError(r, err)
		assert.Contains(r, resp, expectedName)
	})
	logger.Log(t, resp)
}

// preparedQueryFailoverCheck verifies that failover occurs when executing the prepared query. It also assures that
// executing the prepared query via DNS also provides expected results.
func (c *cluster) preparedQueryFailoverCheck(t *testing.T, cfg *config.TestConfig, releaseName string, epq expectedPQ, failover *cluster) {
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 5 * time.Second}
	resp, _, err := c.client.PreparedQuery().Execute(*c.pqID, &api.QueryOptions{Namespace: staticServerNamespace, Partition: c.partition})
	require.NoError(t, err)
	require.Len(t, resp.Nodes, 1)

	assert.Equal(t, epq.partition, resp.Nodes[0].Service.Partition)
	assert.Equal(t, epq.peerName, resp.Nodes[0].Service.PeerName)
	assert.Equal(t, epq.namespace, resp.Nodes[0].Service.Namespace)
	addr := strings.ReplaceAll(resp.Nodes[0].Service.Address, ":0:", "::")
	assert.Equal(t, *failover.staticServerIP, addr)

	// Verify that dns lookup is successful, there is no guarantee that the ip address is unique, so for PQ this is
	// just verifying that we can query using DNS and that the ip address is correct. It does not however prove
	// that failover occurred, that is left to client `Execute`
	dnsPQLookup := []string{fmt.Sprintf("%s.query.consul", *c.pqName)}
	retry.RunWith(timer, t, func(r *retry.R) {
		logs := dnsQuery(r, cfg, releaseName, dnsPQLookup, c.primaryCluster, failover)
		assert.Contains(r, logs, fmt.Sprintf("SERVER: %s", *c.primaryCluster.dnsIP))
		assert.Contains(r, logs, "ANSWER SECTION:")
		assert.Contains(r, logs, *failover.staticServerIP)
	})
}

// DNS failover check verifies that failover occurred when querying the DNS.
func (c *cluster) dnsFailoverCheck(t *testing.T, cfg *config.TestConfig, releaseName string, failover *cluster) {
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 5 * time.Second}
	dnsLookup := []string{fmt.Sprintf("static-server.service.ns2.ns.%s.sg.%s.ap.consul", samenessGroupName, c.fullTextPartition()), "+tcp", "SRV"}
	retry.RunWith(timer, t, func(r *retry.R) {
		// Use the primary cluster when performing a DNS lookup, this mostly affects cases
		// where we are verifying DNS for a partition
		logs := dnsQuery(r, cfg, releaseName, dnsLookup, c.primaryCluster, failover)

		assert.Contains(r, logs, fmt.Sprintf("SERVER: %s", *c.primaryCluster.dnsIP))
		assert.Contains(r, logs, "ANSWER SECTION:")
		assert.Contains(r, logs, *failover.staticServerIP)

		// Additional checks
		// When accessing the SRV record for DNS we can get more information. In the case of Kind,
		// the context can be used to determine that failover occured to the expected kubernetes cluster
		// hosting Consul
		assert.Contains(r, logs, "ADDITIONAL SECTION:")
		expectedName := failover.context.KubectlOptions(r).ContextName
		if cfg.UseKind {
			expectedName = strings.Replace(expectedName, "kind-", "", -1)
		}
		assert.Contains(r, logs, expectedName)
	})
}

// getPeeringAcceptorSecret assures that the secret is created and retrieves the secret from the provided acceptor.
func (c *cluster) getPeeringAcceptorSecret(t *testing.T, cfg *config.TestConfig, acceptorName string) string {
	// Ensure the secrets are created.
	var acceptorSecretName string
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 1 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		var err error
		acceptorSecretName, err = k8s.RunKubectlAndGetOutputE(r, c.context.KubectlOptions(r), "get", "peeringacceptor", acceptorName, "-o", "jsonpath={.status.secret.name}")
		require.NoError(r, err)
		require.NotEmpty(r, acceptorSecretName)
	})

	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, c.context.KubectlOptions(t), "delete", "secret", acceptorSecretName)
	})

	return acceptorSecretName
}

// checkLocalities checks the given cluster for `static-client` and `static-server` instances matching the locality
// expected for the cluster.
func (c *cluster) checkLocalities(t *testing.T) {
	for ns, svcs := range map[string][]string{
		staticClientNamespace: {
			staticClientName,
			staticClientName + "-sidecar-proxy",
		},
		staticServerNamespace: {
			staticServerName,
			staticServerName + "-sidecar-proxy",
		},
	} {
		for _, svc := range svcs {
			cs := c.getCatalogService(t, svc, ns, c.partition)
			assert.NotNil(t, cs.ServiceLocality, "service %s in %s did not have locality set", svc, c.name)
			assert.Equal(t, c.locality, *cs.ServiceLocality, "locality for service %s in %s did not match expected", svc, c.name)
		}
	}
}

func (c *cluster) getCatalogService(t *testing.T, svc, ns, partition string) *api.CatalogService {
	resp, _, err := c.client.Catalog().Service(svc, "", &api.QueryOptions{Namespace: ns, Partition: partition})
	require.NoError(t, err)
	assert.NotEmpty(t, resp, "did not find service %s in cluster %s (partition=%s ns=%s)", svc, c.name, partition, ns)
	return resp[0]
}

type clusters map[string]*cluster

func (c clusters) resetScale(t *testing.T) {
	for _, v := range c {
		k8s.KubectlScale(t, v.serverOpts, staticServerDeployment, 1)
	}
}

// setServerIP makes sure everything is up and running and then saves the
// static-server IP to the appropriate cluster. IP addresses can change when
// services are scaled up and down.
func (c clusters) setServerIP(t *testing.T) {
	for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
		for k, v := range c {
			podList, err := v.context.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(ctx.Background(),
				metav1.ListOptions{LabelSelector: labelSelector})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)
			if labelSelector == "app=static-server" {
				ip := net.ParseIP(podList.Items[0].Status.PodIP)
				require.NotNil(t, ip)
				ipStr := strings.ReplaceAll(ip.String(), ":0:", "::")
				logger.Logf(t, "%s-static-server-ip: %s", v.name, ip.String())
				c[k].staticServerIP = &ipStr
			}
		}
	}
}

// verifyServerUpState will verify that the static-servers are all up and running as
// expected by curling them from their local datacenters.
func (c clusters) verifyServerUpState(t *testing.T, isTproxyEnabled bool) {
	logger.Logf(t, "verifying that static-servers are up")
	for _, v := range c {
		// Query using a client and expect its own name, no failover should occur
		if isTproxyEnabled {
			v.serviceTargetCheck(t, v.name, fmt.Sprintf("http://static-server.virtual.ns2.ns.%s.ap.consul", v.fullTextPartition()))
		} else {
			v.serviceTargetCheck(t, v.name, "localhost:8080")
		}
	}
}

func copySecret(t *testing.T, cfg *config.TestConfig, sourceContext, destContext environment.TestContext, secretName string) {
	k8s.CopySecret(t, sourceContext, destContext, secretName)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, destContext.KubectlOptions(t), "delete", "secret", secretName)
	})
}

func createNamespaces(t *testing.T, cfg *config.TestConfig, context environment.TestContext) {
	logger.Logf(t, "creating namespaces in %s", context.KubectlOptions(t).ContextName)
	k8s.RunKubectl(t, context.KubectlOptions(t), "create", "ns", staticServerNamespace)
	k8s.RunKubectl(t, context.KubectlOptions(t), "create", "ns", staticClientNamespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, context.KubectlOptions(t), "delete", "ns", staticClientNamespace, staticServerNamespace)
	})
}

func applyResources(t *testing.T, cfg *config.TestConfig, kustomizeDir string, opts *terratestk8s.KubectlOptions) {
	k8s.KubectlApplyK(t, opts, kustomizeDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, opts, kustomizeDir)
	})
}

// setK8sNodeLocality labels the k8s node corresponding to the given cluster with standard labels indicating the
// locality of that node. These are propagated by connect-inject to registered Consul services.
func setK8sNodeLocality(t *testing.T, context environment.TestContext, c *cluster) {
	nodeList, err := context.KubernetesClient(t).CoreV1().Nodes().List(ctx.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	// Get the name of the (only) node from the Kind cluster.
	node := nodeList.Items[0].Name
	k8s.KubectlLabel(t, context.KubectlOptions(t), "node", node, corev1.LabelTopologyRegion, c.locality.Region)
	k8s.KubectlLabel(t, context.KubectlOptions(t), "node", node, corev1.LabelTopologyZone, c.locality.Zone)
}

// dnsQuery performs a dns query with the provided query string.
func dnsQuery(t testutil.TestingTB, cfg *config.TestConfig, releaseName string, dnsQuery []string, dnsServer, failover *cluster) string {
	timer := &retry.Timer{Timeout: retryTimeout, Wait: 1 * time.Second}
	var logs string

	retry.RunWith(timer, t, func(r *retry.R) {
		args := []string{"exec", "-i",
			staticClientDeployment, "-c", staticClientName, "--", "dig", fmt.Sprintf("@%s-consul-dns.default",
				releaseName), "ANY"}
		args = append(args, dnsQuery...)
		var err error
		logs, err = k8s.RunKubectlAndGetOutputE(r, dnsServer.clientOpts, args...)
		require.NoError(r, err)
	})
	logger.Logf(t, "%s: %s", failover.name, logs)
	return logs
}

// isAcceptor iterates through the provided acceptor list of cluster names and determines if
// any match the provided name. Returns true if a match is found, false otherwise.
func isAcceptor(name string, acceptorList []string) bool {
	for _, v := range acceptorList {
		if name == v {
			return true
		}
	}
	return false
}

// localityForRegion returns the full api.Locality to use in tests for a given region string.
func localityForRegion(r string) api.Locality {
	return api.Locality{
		Region: r,
		Zone:   r + "a",
	}
}

func deployCustomizeAsync(t *testing.T, opts *terratestk8s.KubectlOptions, noCleanupOnFailure bool, noCleanup bool, debugDirectory string, kustomizeDir string, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		k8s.DeployKustomize(t, opts, noCleanupOnFailure, noCleanup, debugDirectory, kustomizeDir)
	}()
}
