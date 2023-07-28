package sameness

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	primaryDatacenterPartition = "ap1"
	primaryServerDatacenter    = "dc1"
	peer1Datacenter            = "dc2"
	peer2Datacenter            = "dc3"
	staticClientNamespace      = "ns1"
	staticServerNamespace      = "ns2"

	keyPrimaryServer = "server"
	keyPartition     = "partition"
	keyPeer1         = "peer1"
	keyPeer2         = "peer2"

	staticServerDeployment = "deploy/static-server"
	staticClientDeployment = "deploy/static-client"

	primaryServerClusterName = "cluster-01-a"
	partitionClusterName     = "cluster-01-b"
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
				|    |                             |   |    |             |   |     Name: cluster-02-a            |
				|    +-----------------------------+   |    |             |   |                                   |
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
				|   |                               |       |                 |                                   |
				|   +-------------------------------+       |                 |                                   |
				|                                           |                 +-----------------------------------+
				+-------------------------------------------+
			*/

			members := map[string]*member{
				keyPrimaryServer: {context: env.DefaultContext(t), hasServer: true},
				keyPartition:     {context: env.Context(t, 1), hasServer: false},
				keyPeer1:         {context: env.Context(t, 2), hasServer: true},
				keyPeer2:         {context: env.Context(t, 3), hasServer: true},
			}

			// Setup Namespaces.
			for _, v := range members {
				createNamespaces(t, cfg, v.context)
			}

			// Create the Default Cluster.
			commonHelmValues := map[string]string{
				"global.peering.enabled": "true",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": strconv.FormatBool(c.ACLsEnabled),

				"global.enableConsulNamespaces": "true",

				"global.adminPartitions.enabled": "true",

				"global.logLevel": "debug",

				"global.acls.manageSystemACLs": strconv.FormatBool(c.ACLsEnabled),

				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": "true",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"dns.enabled": "true",
			}

			defaultPartitionHelmValues := map[string]string{
				"global.datacenter": primaryServerDatacenter,
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

			releaseName := helpers.RandomName()
			members[keyPrimaryServer].helmCluster = consul.NewHelmCluster(t, defaultPartitionHelmValues, members[keyPrimaryServer].context, cfg, releaseName)
			members[keyPrimaryServer].helmCluster.Create(t)

			// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
			caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)

			logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
			k8s.CopySecret(t, members[keyPrimaryServer].context, members[keyPartition].context, caCertSecretName)

			// Create Secondary Partition Cluster which will apply the primary datacenter.
			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
			if c.ACLsEnabled {
				logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
				k8s.CopySecret(t, members[keyPrimaryServer].context, members[keyPartition].context, partitionToken)
			}

			partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
			partitionSvcAddress := k8s.ServiceHost(t, cfg, members[keyPrimaryServer].context, partitionServiceName)

			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, members[keyPartition].context)

			secondaryPartitionHelmValues := map[string]string{
				"global.enabled":    "false",
				"global.datacenter": primaryServerDatacenter,

				"global.adminPartitions.name": primaryDatacenterPartition,

				"global.tls.caCert.secretName": caCertSecretName,
				"global.tls.caCert.secretKey":  "tls.crt",

				"externalServers.enabled":       "true",
				"externalServers.hosts[0]":      partitionSvcAddress,
				"externalServers.tlsServerName": fmt.Sprintf("server.%s.consul", primaryServerDatacenter),
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

			members[keyPartition].helmCluster = consul.NewHelmCluster(t, secondaryPartitionHelmValues, members[keyPartition].context, cfg, releaseName)
			members[keyPartition].helmCluster.Create(t)

			// Create Peer 1 Cluster.
			PeerOneHelmValues := map[string]string{
				"global.datacenter": peer1Datacenter,
			}

			if cfg.UseKind {
				PeerOneHelmValues["server.exposeGossipAndRPCPorts"] = "true"
				PeerOneHelmValues["meshGateway.service.type"] = "NodePort"
				PeerOneHelmValues["meshGateway.service.nodePort"] = "30100"
			}
			helpers.MergeMaps(PeerOneHelmValues, commonHelmValues)

			members[keyPeer1].helmCluster = consul.NewHelmCluster(t, PeerOneHelmValues, members[keyPeer1].context, cfg, releaseName)
			members[keyPeer1].helmCluster.Create(t)

			// Create Peer 2 Cluster.
			PeerTwoHelmValues := map[string]string{
				"global.datacenter": peer2Datacenter,
			}

			if cfg.UseKind {
				PeerTwoHelmValues["server.exposeGossipAndRPCPorts"] = "true"
				PeerTwoHelmValues["meshGateway.service.type"] = "NodePort"
				PeerTwoHelmValues["meshGateway.service.nodePort"] = "30100"
			}
			helpers.MergeMaps(PeerTwoHelmValues, commonHelmValues)

			members[keyPeer2].helmCluster = consul.NewHelmCluster(t, PeerTwoHelmValues, members[keyPeer2].context, cfg, releaseName)
			members[keyPeer2].helmCluster.Create(t)

			// Create a ProxyDefaults resource to configure services to use the mesh
			// gateways and set server and client opts.
			for k, v := range members {
				logger.Logf(t, "applying resources on %s", v.context.KubectlOptions(t).ContextName)

				// Client will use the client namespace.
				members[k].clientOpts = &terratestk8s.KubectlOptions{
					ContextName: v.context.KubectlOptions(t).ContextName,
					ConfigPath:  v.context.KubectlOptions(t).ConfigPath,
					Namespace:   staticClientNamespace,
				}

				// Server will use the server namespace.
				members[k].serverOpts = &terratestk8s.KubectlOptions{
					ContextName: v.context.KubectlOptions(t).ContextName,
					ConfigPath:  v.context.KubectlOptions(t).ConfigPath,
					Namespace:   staticServerNamespace,
				}

				// Sameness Defaults need to be applied first so that the sameness group exists.
				applyResources(t, cfg, "../fixtures/bases/mesh-gateway", members[k].context.KubectlOptions(t))
				applyResources(t, cfg, "../fixtures/bases/sameness/default-ns", members[k].context.KubectlOptions(t))
				applyResources(t, cfg, "../fixtures/bases/sameness/override-ns", members[k].serverOpts)

				// Only assign a client if the cluster is running a Consul server.
				if v.hasServer {
					members[k].client, _ = members[k].helmCluster.SetupConsulClient(t, c.ACLsEnabled)
				}
			}

			// TODO: Add further setup for peering, right now the rest of this test will only cover Partitions
			// Create static server deployments.
			logger.Log(t, "creating static-server and static-client deployments")
			k8s.DeployKustomize(t, members[keyPrimaryServer].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/default")
			k8s.DeployKustomize(t, members[keyPartition].serverOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-server/partition")

			// Create static client deployments.
			k8s.DeployKustomize(t, members[keyPrimaryServer].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-client/default")
			k8s.DeployKustomize(t, members[keyPartition].clientOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory,
				"../fixtures/cases/sameness/static-client/partition")

			// Verify that both static-server and static-client have been injected and now have 2 containers in server cluster.
			// Also get the server IP
			for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
				podList, err := members[keyPrimaryServer].context.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(),
					metav1.ListOptions{LabelSelector: labelSelector})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
				if labelSelector == "app=static-server" {
					ip := &podList.Items[0].Status.PodIP
					require.NotNil(t, ip)
					logger.Logf(t, "default-static-server-ip: %s", *ip)
					members[keyPrimaryServer].staticServerIP = ip
				}

				podList, err = members[keyPartition].context.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(),
					metav1.ListOptions{LabelSelector: labelSelector})
				require.NoError(t, err)
				require.Len(t, podList.Items, 1)
				require.Len(t, podList.Items[0].Spec.Containers, 2)
				if labelSelector == "app=static-server" {
					ip := &podList.Items[0].Status.PodIP
					require.NotNil(t, ip)
					logger.Logf(t, "partition-static-server-ip: %s", *ip)
					members[keyPartition].staticServerIP = ip
				}
			}

			logger.Log(t, "creating exported services")
			applyResources(t, cfg, "../fixtures/cases/sameness/exported-services/default-partition", members[keyPrimaryServer].context.KubectlOptions(t))
			applyResources(t, cfg, "../fixtures/cases/sameness/exported-services/ap1-partition", members[keyPartition].context.KubectlOptions(t))

			// Setup DNS.
			dnsService, err := members[keyPrimaryServer].context.KubernetesClient(t).CoreV1().Services("default").Get(context.Background(), fmt.Sprintf("%s-%s", releaseName, "consul-dns"), metav1.GetOptions{})
			require.NoError(t, err)
			dnsIP := dnsService.Spec.ClusterIP
			logger.Logf(t, "dnsIP: %s", dnsIP)

			// Setup Prepared Query.
			definition := &api.PreparedQueryDefinition{
				Name: "my-query",
				Service: api.ServiceQuery{
					Service:       "static-server",
					SamenessGroup: "mine",
					Namespace:     staticServerNamespace,
					OnlyPassing:   false,
				},
			}
			resp, _, err := members[keyPrimaryServer].client.PreparedQuery().Create(definition, &api.WriteOptions{})
			require.NoError(t, err)
			logger.Logf(t, "PQ ID: %s", resp)

			logger.Log(t, "all infrastructure up and running")
			logger.Log(t, "verifying failover scenarios")

			const dnsLookup = "static-server.service.ns2.ns.mine.sg.consul"
			const dnsPQLookup = "my-query.query.consul"

			// Verify initial server.
			serviceFailoverCheck(t, primaryServerClusterName, members[keyPrimaryServer])

			// Verify initial dns.
			dnsFailoverCheck(t, releaseName, dnsIP, dnsLookup, members[keyPrimaryServer], members[keyPrimaryServer])

			// Verify initial dns with PQ.
			dnsFailoverCheck(t, releaseName, dnsIP, dnsPQLookup, members[keyPrimaryServer], members[keyPrimaryServer])

			// Scale down static-server on the server, will fail over to partition.
			k8s.KubectlScale(t, members[keyPrimaryServer].serverOpts, staticServerDeployment, 0)

			// Verify failover to partition.
			serviceFailoverCheck(t, partitionClusterName, members[keyPrimaryServer])

			// Verify dns failover to partition.
			dnsFailoverCheck(t, releaseName, dnsIP, dnsLookup, members[keyPrimaryServer], members[keyPartition])

			// Verify prepared query failover.
			dnsFailoverCheck(t, releaseName, dnsIP, dnsPQLookup, members[keyPrimaryServer], members[keyPartition])

			logger.Log(t, "tests complete")
		})
	}
}

type member struct {
	context        environment.TestContext
	helmCluster    *consul.HelmCluster
	client         *api.Client
	hasServer      bool
	serverOpts     *terratestk8s.KubectlOptions
	clientOpts     *terratestk8s.KubectlOptions
	staticServerIP *string
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

// serviceFailoverCheck verifies that the server failed over as expected by checking that curling the `static-server`
// using the `static-client` responds with the expected cluster name. Each static-server responds with a uniquue
// name so that we can verify failover occured as expected.
func serviceFailoverCheck(t *testing.T, expectedClusterName string, server *member) {
	retry.Run(t, func(r *retry.R) {
		resp, err := k8s.RunKubectlAndGetOutputE(t, server.clientOpts, "exec", "-i",
			staticClientDeployment, "-c", "static-client", "--", "curl", "localhost:8080")
		require.NoError(r, err)
		assert.Contains(r, resp, expectedClusterName)
		logger.Log(t, resp)
	})
}

func dnsFailoverCheck(t *testing.T, releaseName string, dnsIP string, dnsQuery string, server, failover *member) {
	retry.Run(t, func(r *retry.R) {
		logs, err := k8s.RunKubectlAndGetOutputE(t, server.clientOpts, "exec", "-i",
			staticClientDeployment, "-c", "static-client", "--", "dig", fmt.Sprintf("@%s-consul-dns.default", releaseName), dnsQuery)
		require.NoError(r, err)

		// When the `dig` request is successful, a section of its response looks like the following:
		//
		// ;; ANSWER SECTION:
		// static-server.service.mine.sg.ns2.ns.consul.	0	IN	A	<consul-server-pod-ip>
		//
		// ;; Query time: 2 msec
		// ;; SERVER: <dns-ip>#<dns-port>(<dns-ip>)
		// ;; WHEN: Mon Aug 10 15:02:40 UTC 2020
		// ;; MSG SIZE  rcvd: 98
		//
		// We assert on the existence of the ANSWER SECTION, The consul-server IPs being present in
		// the ANSWER SECTION and the DNS IP mentioned in the SERVER: field

		assert.Contains(r, logs, fmt.Sprintf("SERVER: %s", dnsIP))
		assert.Contains(r, logs, "ANSWER SECTION:")
		assert.Contains(r, logs, *failover.staticServerIP)
	})
}
