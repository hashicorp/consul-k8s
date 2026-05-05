// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"

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

const multiportServiceName = "multiport"
const multiportAdminServiceName = "multiport-admin"

// TestPartitions_Connect_MultiportServices validates cross-partition connectivity to
// three ports exposed by a single Consul service registered from a multi-port workload.
func TestPartitions_Connect_MultiportServices(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	cfg.SkipWhenOpenshiftAndCNI(t)

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	const defaultPartition = "default"
	const secondaryPartition = "secondary"

	meshGatewayModes := []struct {
		name                         string
		defaultPartitionConfigPath   string
		secondaryPartitionConfigPath string
	}{
		{
			name:                         "local",
			defaultPartitionConfigPath:   "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service-config-local",
			secondaryPartitionConfigPath: "../fixtures/cases/crd-partitions/secondary-partition-default-multiport-single-service-config-local",
		},
		{
			name:                         "remote",
			defaultPartitionConfigPath:   "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service-config-remote",
			secondaryPartitionConfigPath: "../fixtures/cases/crd-partitions/secondary-partition-default-multiport-single-service-config-remote",
		},
		{
			name:                         "none",
			defaultPartitionConfigPath:   "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service-config-none",
			secondaryPartitionConfigPath: "../fixtures/cases/crd-partitions/secondary-partition-default-multiport-single-service-config-none",
		},
	}

	for _, meshGatewayMode := range meshGatewayModes {
		t.Run(fmt.Sprintf("/mesh-gateway %s", meshGatewayMode.name), func(t *testing.T) {
			defaultPartitionClusterContext := env.DefaultContext(t)
			secondaryPartitionClusterContext := env.Context(t, 1)

			commonHelmValues := map[string]string{
				"global.adminPartitions.enabled": "true",
				"global.enableConsulNamespaces":  "true",
				"global.logLevel":                "debug",

				"global.tls.enabled":   "true",
				"global.tls.httpsOnly": "true",

				"global.acls.manageSystemACLs": "true",

				"connectInject.enabled":                                     "true",
				"connectInject.transparentProxy.defaultEnabled":             strconv.FormatBool(cfg.EnableTransparentProxy),
				"connectInject.consulNamespaces.consulDestinationNamespace": "default",
				"connectInject.consulNamespaces.mirroringK8S":               "false",

				"meshGateway.enabled":  "true",
				"meshGateway.replicas": "1",

				"dns.enabled":           "true",
				"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),
			}

			defaultPartitionHelmValues := make(map[string]string)
			if cfg.UseKind {
				defaultPartitionHelmValues["meshGateway.service.type"] = "NodePort"
				defaultPartitionHelmValues["meshGateway.service.nodePort"] = "30200"
				defaultPartitionHelmValues["server.exposeService.type"] = "NodePort"
				defaultPartitionHelmValues["server.exposeService.nodePort.https"] = "30000"
				defaultPartitionHelmValues["server.exposeService.nodePort.grpc"] = "30100"
			}

			releaseName := helpers.RandomName()
			helpers.MergeMaps(defaultPartitionHelmValues, commonHelmValues)

			serverConsulCluster := consul.NewHelmCluster(t, defaultPartitionHelmValues, defaultPartitionClusterContext, cfg, releaseName)
			serverConsulCluster.Create(t)

			caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)
			logger.Logf(t, "copying CA cert secret %s to secondary cluster", caCertSecretName)
			k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, caCertSecretName)

			partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)

			logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
			k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, partitionToken)

			partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
			partitionSvcAddress := k8s.ServiceHost(t, cfg, defaultPartitionClusterContext, partitionServiceName)
			k8sAuthMethodHost := k8s.KubernetesAPIServerHost(t, cfg, secondaryPartitionClusterContext)

			secondaryPartitionHelmValues := map[string]string{
				"global.enabled": "false",

				"global.adminPartitions.name": secondaryPartition,

				"global.tls.caCert.secretName": caCertSecretName,
				"global.tls.caCert.secretKey":  "tls.crt",

				"externalServers.enabled":       "true",
				"externalServers.hosts[0]":      partitionSvcAddress,
				"externalServers.tlsServerName": "server.dc1.consul",
			}

			secondaryPartitionHelmValues["global.acls.bootstrapToken.secretName"] = partitionToken
			secondaryPartitionHelmValues["global.acls.bootstrapToken.secretKey"] = "token"
			secondaryPartitionHelmValues["externalServers.k8sAuthMethodHost"] = k8sAuthMethodHost

			if cfg.UseKind {
				secondaryPartitionHelmValues["externalServers.httpsPort"] = "30000"
				secondaryPartitionHelmValues["externalServers.grpcPort"] = "30100"
				secondaryPartitionHelmValues["meshGateway.service.type"] = "NodePort"
				secondaryPartitionHelmValues["meshGateway.service.nodePort"] = "30200"
			}

			helpers.MergeMaps(secondaryPartitionHelmValues, commonHelmValues)

			clientConsulCluster := consul.NewHelmCluster(t, secondaryPartitionHelmValues, secondaryPartitionClusterContext, cfg, releaseName)
			clientConsulCluster.Create(t)

			// For mesh gateway mode "none", sidecars connect directly to each
			// other without going through mesh gateways. This requires flat
			// network routing between the two Kind clusters' pod subnets.
			if meshGatewayMode.name == "none" && cfg.UseKind {
				setupFlatNetworkForKindClusters(t, defaultPartitionClusterContext, secondaryPartitionClusterContext)
			}

			consulClient, _ := serverConsulCluster.SetupConsulClient(t, true)

			// Apply config entries (ProxyDefaults, ServiceDefaults, ServiceResolver) as CRDs
			// in each cluster. The connect-injector syncs CRDs to Consul. Using CRDs
			// (rather than the Consul API) ensures the secondary partition's controller
			// properly manages the config entries.
			logger.Logf(t, "applying config entries with mesh gateway mode %s and protocol http to default partition cluster", meshGatewayMode.name)
			k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), meshGatewayMode.defaultPartitionConfigPath)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), meshGatewayMode.defaultPartitionConfigPath)
			})

			logger.Logf(t, "applying config entries with mesh gateway mode %s, protocol http, and service resolver to secondary partition cluster", meshGatewayMode.name)
			k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), meshGatewayMode.secondaryPartitionConfigPath)
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), meshGatewayMode.secondaryPartitionConfigPath)
			})

			// Apply ExportedServices before deploying workloads so mesh gateways
			// are configured to export multiport before the service registers.
			logger.Log(t, "exporting multi-port services from default partition to secondary partition")
			k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service")
			k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default-multiport-single-service")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service")
				k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default-multiport-single-service")
			})

			// Deploy the multiport server. The base fixture has transparent-proxy
			// explicitly set to "false", so we must use the tproxy overlay when:
			// - cfg.EnableTransparentProxy is true (to honour the test flag), or
			// - mesh gateway mode is "none" (sidecars connect directly to app
			//   ports, which need iptables redirect to the Envoy inbound listener
			//   because Consul xDS returns app ports, not the proxy port 20000).
			logger.Log(t, "deploying multi-port service in default partition cluster")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, defaultPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/multiport-single-service-app-tproxy")
			} else {
				k8s.DeployKustomize(t, defaultPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/multiport-single-service-app")
			}

			// Deploy the client.
			// CNI + tproxy + mode "none" requires explicit upstream annotations.
			// With CNI, iptables are set up by the DaemonSet at pod-creation time,
			// before consul-dataplane starts. The initial xDS snapshot therefore
			// does not include the cross-partition EDS push for direct-connect
			// mode "none" endpoints, leaving the outbound listener with only
			// original-destination. Envoy forwards to the unroutable 240.0.0.x
			// virtual IP and returns 503.
			// Without CNI the init-container runs before consul-dataplane, which
			// causes a later xDS sync that correctly includes the cross-partition
			// cluster, so virtual-DNS tproxy works in that path.
			// Explicit upstream annotations guarantee the cluster is present in
			// xDS regardless of when the dataplane's initial snapshot is taken.
			useTproxyClient := cfg.EnableTransparentProxy && !(cfg.EnableCNI && meshGatewayMode.name == "none")
			logger.Log(t, "deploying client in secondary partition cluster")
			if useTproxyClient {
				k8s.DeployKustomize(t, secondaryPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition-multiport-single-service-tproxy")
			} else {
				k8s.DeployKustomize(t, secondaryPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition-multiport-single-service")
			}

			multiportPods, err := defaultPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: "app=multiport"})
			require.NoError(t, err)
			require.Len(t, multiportPods.Items, 1)
			require.Len(t, multiportPods.Items[0].Spec.Containers, 2)

			staticClientPods, err := secondaryPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-client"})
			require.NoError(t, err)
			require.Len(t, staticClientPods.Items, 1)
			require.Len(t, staticClientPods.Items[0].Spec.Containers, 2)

			consulDefaultQueryOpts := &api.QueryOptions{Partition: defaultPartition, Namespace: "default"}
			retry.Run(t, func(r *retry.R) {
				services, _, err := consulClient.Catalog().Service(multiportServiceName, "", consulDefaultQueryOpts)
				require.NoError(r, err)
				require.Len(r, services, 1)
				require.Equal(r, "api-port:9090,metrics:9091,admin-port:9092", services[0].ServiceMeta["ports"])
				require.Equal(r, "9090", services[0].ServiceMeta["port-api-port"])
				require.Equal(r, "9091", services[0].ServiceMeta["port-metrics"])
				require.Equal(r, "9092", services[0].ServiceMeta["port-admin-port"])

				legacyAdminServices, _, err := consulClient.Catalog().Service(multiportAdminServiceName, "", consulDefaultQueryOpts)
				require.NoError(r, err)
				require.Len(r, legacyAdminServices, 0)
			})

			// Use virtual-DNS URLs only when the tproxy client fixture is deployed.
			var upstreamAPIURL, upstreamMetricsURL, upstreamAdminURL string
			if useTproxyClient {
				upstreamAPIURL = "http://api-port.multiport.virtual.default.ns.default.ap.dc1.dc.consul"
				upstreamMetricsURL = "http://metrics.multiport.virtual.default.ns.default.ap.dc1.dc.consul"
				upstreamAdminURL = "http://admin-port.multiport.virtual.default.ns.default.ap.dc1.dc.consul"
			} else {
				upstreamAPIURL = "http://localhost:1234"
				upstreamMetricsURL = "http://localhost:2234"
				upstreamAdminURL = "http://localhost:3234"
			}

			secondaryClientOpts := secondaryPartitionClusterContext.KubectlOptions(t)

			// Create intention to allow cross-partition traffic.
			logger.Logf(t, "creating intention for destination %s", multiportServiceName)
			_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
				Kind:      api.ServiceIntentions,
				Name:      multiportServiceName,
				Namespace: "default",
				Sources: []*api.SourceIntention{
					{
						Name:      StaticClientName,
						Namespace: "default",
						Partition: secondaryPartition,
						Action:    api.IntentionActionAllow,
					},
				},
			}, &api.WriteOptions{Partition: defaultPartition})
			require.NoError(t, err)

			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				_, err := consulClient.ConfigEntries().Delete(api.ServiceIntentions, multiportServiceName, &api.WriteOptions{Partition: defaultPartition})
				require.NoError(t, err)
			})

			logger.Log(t, "checking cross-partition connectivity for all three ports")
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from api-port 9090: Hello there!", upstreamAPIURL)
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from metrics port 9091: Hello again!", upstreamMetricsURL)
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from admin port 9092: Hello once more!", upstreamAdminURL)

			logger.Log(t, "marking multi-port workload unhealthy")
			k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "exec", "deploy/"+multiportServiceName, "-c", multiportServiceName, "--", "touch", "/tmp/unhealthy-multiport")

			failureMessages := []string{
				"curl: (56) Recv failure: Connection reset by peer",
				"curl: (52) Empty reply from server",
				"curl: (22) The requested URL returned error: 503",
			}
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamAPIURL)
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamMetricsURL)
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamAdminURL)
		})
	}
}

// setupFlatNetworkForKindClusters establishes flat network routing between two
// Kind clusters so that pods in one cluster can directly reach pods in the other.
// This is required for mesh gateway mode "none" where sidecars connect directly
// to upstream sidecars without routing through a mesh gateway.
//
// It performs the following:
//  1. Discovers the Docker IP of each cluster's control-plane node.
//  2. Discovers the pod CIDR of each cluster.
//  3. Adds routes using symmetric routing through both control-plane nodes:
//     - Worker nodes route via their OWN cluster's control-plane.
//     - Control-plane nodes route via the PEER cluster's control-plane.
//     This ensures traffic traverses both CPs in both directions, avoiding
//     asymmetric routing that causes kube-proxy's KUBE-FORWARD chain to drop
//     SYN-ACK packets as INVALID (conntrack never saw the original SYN).
//  4. Adds iptables rules to prevent masquerading of cross-cluster pod traffic.
func setupFlatNetworkForKindClusters(t *testing.T, defaultCtx, secondaryCtx environment.TestContext) {
	t.Helper()

	// Derive Kind cluster names from the kube context names.
	// Context names are "kind-<cluster-name>" (e.g., "kind-kind", "kind-kind-2").
	defaultContextName := environment.KubernetesContextFromOptions(t, defaultCtx.KubectlOptions(t))
	secondaryContextName := environment.KubernetesContextFromOptions(t, secondaryCtx.KubectlOptions(t))

	defaultClusterName := strings.TrimPrefix(defaultContextName, "kind-")
	secondaryClusterName := strings.TrimPrefix(secondaryContextName, "kind-")

	defaultCPNode := defaultClusterName + "-control-plane"
	secondaryCPNode := secondaryClusterName + "-control-plane"

	logger.Logf(t, "setting up flat network routing between Kind clusters %q and %q", defaultClusterName, secondaryClusterName)

	// Get Docker IPs of control-plane nodes.
	defaultCPIP := dockerInspectIP(t, defaultCPNode)
	secondaryCPIP := dockerInspectIP(t, secondaryCPNode)

	// Get cluster-level pod CIDRs (covering all nodes).
	defaultPodCIDR := getKindClusterPodCIDR(t, defaultCtx)
	secondaryPodCIDR := getKindClusterPodCIDR(t, secondaryCtx)

	logger.Logf(t, "default cluster: node=%s ip=%s podCIDR=%s", defaultCPNode, defaultCPIP, defaultPodCIDR)
	logger.Logf(t, "secondary cluster: node=%s ip=%s podCIDR=%s", secondaryCPNode, secondaryCPIP, secondaryPodCIDR)

	// Add routes on default cluster nodes to reach secondary pod subnet.
	// Use symmetric routing: worker nodes go via own CP, CP goes via peer CP.
	// Full path: default-worker → defaultCP → secondaryCP → secondary-worker
	defaultNodes := kindGetNodes(t, defaultClusterName)
	for _, node := range defaultNodes {
		if node == defaultCPNode {
			// CP routes via peer CP.
			dockerExec(t, node, "ip", "route", "replace", secondaryPodCIDR, "via", secondaryCPIP)
		} else {
			// Workers route via own CP.
			dockerExec(t, node, "ip", "route", "replace", secondaryPodCIDR, "via", defaultCPIP)
		}
		// Prevent masquerading of traffic to the other cluster's pods.
		dockerExecShell(t, node, fmt.Sprintf(
			"iptables -t nat -C KIND-MASQ-AGENT -d %s -j RETURN 2>/dev/null || iptables -t nat -I KIND-MASQ-AGENT 2 -d %s -j RETURN",
			secondaryPodCIDR, secondaryPodCIDR))
	}

	// Add routes on secondary cluster nodes to reach default pod subnet.
	// Full path: secondary-worker → secondaryCP → defaultCP → default-worker
	secondaryNodes := kindGetNodes(t, secondaryClusterName)
	for _, node := range secondaryNodes {
		if node == secondaryCPNode {
			// CP routes via peer CP.
			dockerExec(t, node, "ip", "route", "replace", defaultPodCIDR, "via", defaultCPIP)
		} else {
			// Workers route via own CP.
			dockerExec(t, node, "ip", "route", "replace", defaultPodCIDR, "via", secondaryCPIP)
		}
		// Prevent masquerading of traffic to the other cluster's pods.
		dockerExecShell(t, node, fmt.Sprintf(
			"iptables -t nat -C KIND-MASQ-AGENT -d %s -j RETURN 2>/dev/null || iptables -t nat -I KIND-MASQ-AGENT 2 -d %s -j RETURN",
			defaultPodCIDR, defaultPodCIDR))
	}

	logger.Log(t, "flat network routing configured between Kind clusters")
}

// dockerInspectIP returns the Docker container IP address for a given container name.
func dockerInspectIP(t *testing.T, containerName string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerName).CombinedOutput()
	require.NoError(t, err, "docker inspect %s failed: %s", containerName, string(out))
	ip := strings.TrimSpace(string(out))
	require.NotEmpty(t, ip, "no IP found for container %s", containerName)
	return ip
}

// getKindClusterPodCIDR retrieves the cluster-level pod CIDR by computing the
// minimal CIDR that covers all node pod CIDRs. In multi-node Kind clusters,
// each node gets a /24 slice of the broader cluster CIDR (e.g. 10.244.0.0/16).
// Using a single node's /24 would miss pods on other nodes.
func getKindClusterPodCIDR(t *testing.T, ctx environment.TestContext) string {
	t.Helper()
	opts := ctx.KubectlOptions(t)

	// Get all node pod CIDRs.
	output, err := k8s.RunKubectlAndGetOutputE(t, opts, "get", "nodes", "-o",
		"jsonpath={.items[*].spec.podCIDR}")
	require.NoError(t, err, "failed to get podCIDRs")

	cidrs := strings.Fields(strings.TrimSpace(output))
	require.NotEmpty(t, cidrs, "no podCIDRs found")

	if len(cidrs) == 1 {
		return cidrs[0]
	}

	// Parse all CIDRs to find the common prefix and compute the covering CIDR.
	// For Kind clusters, all node CIDRs are slices of the same cluster CIDR,
	// so we widen the mask to cover all of them.
	firstIP, firstNet, err := net.ParseCIDR(cidrs[0])
	require.NoError(t, err, "failed to parse CIDR %s", cidrs[0])

	// Start with the first CIDR's network IP as a 32-bit value.
	firstIPv4 := firstIP.Mask(firstNet.Mask).To4()
	require.NotNil(t, firstIPv4, "expected IPv4 CIDR")

	minIP := ipToUint32(firstIPv4)
	maxIP := minIP
	for _, cidr := range cidrs[1:] {
		ip, ipNet, err := net.ParseCIDR(cidr)
		require.NoError(t, err, "failed to parse CIDR %s", cidr)
		ipv4 := ip.Mask(ipNet.Mask).To4()
		require.NotNil(t, ipv4, "expected IPv4 CIDR")

		val := ipToUint32(ipv4)
		if val < minIP {
			minIP = val
		}
		// Compute the broadcast (last IP) of this node's CIDR.
		ones, _ := ipNet.Mask.Size()
		broadcast := val | (0xFFFFFFFF >> uint(ones))
		if broadcast > maxIP {
			maxIP = broadcast
		}
	}

	// Find the prefix length that covers minIP..maxIP.
	diff := minIP ^ maxIP
	prefixLen := 32
	for diff > 0 {
		diff >>= 1
		prefixLen--
	}

	// Mask minIP to the covering prefix.
	mask := uint32(0xFFFFFFFF) << uint(32-prefixLen)
	baseIP := minIP & mask

	return fmt.Sprintf("%s/%d", uint32ToIP(baseIP).String(), prefixLen)
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(val uint32) net.IP {
	return net.IPv4(byte(val>>24), byte(val>>16&0xFF), byte(val>>8&0xFF), byte(val&0xFF))
}

// kindGetNodes returns the list of Docker container names for nodes in a Kind cluster.
func kindGetNodes(t *testing.T, clusterName string) []string {
	t.Helper()
	out, err := exec.Command("kind", "get", "nodes", "--name", clusterName).CombinedOutput()
	require.NoError(t, err, "kind get nodes --name %s failed: %s", clusterName, string(out))
	var nodes []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			nodes = append(nodes, line)
		}
	}
	require.NotEmpty(t, nodes, "no nodes found for Kind cluster %s", clusterName)
	return nodes
}

// dockerExec runs a command inside a Docker container.
func dockerExec(t *testing.T, container string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"exec", container}, args...)
	out, err := exec.Command("docker", cmdArgs...).CombinedOutput()
	// Ignore errors from "ip route replace" if route already exists.
	if err != nil {
		logger.Logf(t, "docker exec %s %v: %s (err: %v)", container, args, string(out), err)
	}
}

// dockerExecShell runs a shell command inside a Docker container.
func dockerExecShell(t *testing.T, container, shellCmd string) {
	t.Helper()
	out, err := exec.Command("docker", "exec", container, "sh", "-c", shellCmd).CombinedOutput()
	if err != nil {
		logger.Logf(t, "docker exec %s sh -c %q: %s (err: %v)", container, shellCmd, string(out), err)
	}
}
