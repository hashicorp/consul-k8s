// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
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

// runPartitionsConnectMultiport validates cross-partition connectivity to three ports exposed by
// a single Consul service registered from a multi-port workload. aclsEnabled controls ACL/TLS
// mode; fixturePath selects the mesh-gateway mode (local vs remote).
func runPartitionsConnectMultiport(t *testing.T, aclsEnabled bool, fixturePath string) {
	t.Helper()
	env := suite.Environment()
	cfg := suite.Config()

	cfg.SkipWhenOpenshiftAndCNI(t)

	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	if cfg.EnableTransparentProxy && !aclsEnabled {
		t.Skipf("skipping this test because transparent proxy requires ACLs to be enabled")
	}

	if cfg.EnableCNI && !aclsEnabled {
		t.Skipf("skipping because -enable-cni is set and ACLs are disabled")
	}

	defaultPartitionClusterContext := env.DefaultContext(t)
	secondaryPartitionClusterContext := env.Context(t, 1)

	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled": "true",
		"global.enableConsulNamespaces":  "true",
		"global.logLevel":                "debug",

		"global.tls.enabled":   "true",
		"global.tls.httpsOnly": strconv.FormatBool(aclsEnabled),

		"global.acls.manageSystemACLs": strconv.FormatBool(aclsEnabled),

		"connectInject.enabled": "true",
		"connectInject.consulNamespaces.consulDestinationNamespace": "default",
		"connectInject.consulNamespaces.mirroringK8S":               "false",
		"connectInject.transparentProxy.defaultEnabled":             strconv.FormatBool(cfg.EnableTransparentProxy),

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
	if aclsEnabled {
		logger.Logf(t, "copying partition ACL token secret %s to secondary cluster", partitionToken)
		k8s.CopySecret(t, defaultPartitionClusterContext, secondaryPartitionClusterContext, partitionToken)
	}

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

	if aclsEnabled {
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

	clientConsulCluster := consul.NewHelmCluster(t, secondaryPartitionHelmValues, secondaryPartitionClusterContext, cfg, releaseName)
	clientConsulCluster.Create(t)

	// Ensure mesh gateways are available in both partition clusters before proceeding with config entries and workloads.
	k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s-consul-mesh-gateway", releaseName))
	k8s.RunKubectl(t, secondaryPartitionClusterContext.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", fmt.Sprintf("deploy/%s-consul-mesh-gateway", releaseName))

	consulClient, _ := serverConsulCluster.SetupConsulClient(t, aclsEnabled)

	logger.Logf(t, "creating proxy defaults with mesh gateway fixture %s", fixturePath)
	k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), fixturePath)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), fixturePath)
	})
	k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), fixturePath)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), fixturePath)
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
	// explicitly set to "false", so we must use the tproxy overlay when
	// cfg.EnableTransparentProxy is true (to honour the test flag).
	logger.Log(t, "deploying multi-port service in default partition cluster")
	if cfg.EnableTransparentProxy {
		k8s.DeployKustomize(t, defaultPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/multiport-single-service-app-tproxy")
	} else {
		k8s.DeployKustomize(t, defaultPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/multiport-single-service-app")
	}

	multiportPods, err := defaultPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: "app=multiport"})
	require.NoError(t, err)
	require.Len(t, multiportPods.Items, 1)
	require.Len(t, multiportPods.Items[0].Spec.Containers, 2)

	// Verify multiport service is registered with all ports and is healthy
	// BEFORE deploying the client. This ensures that when the client's
	// proxy starts, its initial xDS snapshot includes the complete
	// cross-partition per-port configuration. Without this ordering, the
	// proxy may receive an incomplete initial snapshot (missing some port
	// VIPs/clusters) that never self-corrects — particularly in
	// ACLs-disabled (default-allow) mode or with CNI where iptables are
	// configured before consul-dataplane starts.
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

	// Verify the service is healthy (not just registered). The proxy
	// needs healthy endpoints in EDS to route traffic.
	retry.Run(t, func(r *retry.R) {
		healthServices, _, err := consulClient.Health().Service(multiportServiceName, "", true, consulDefaultQueryOpts)
		require.NoError(r, err)
		require.Len(r, healthServices, 1)
	})

	// Create intention BEFORE deploying the client so the proxy's initial
	// xDS snapshot includes multiport as an allowed upstream with all
	// per-port VIPs and clusters configured. In ACLs-disabled mode
	// (default-allow), creating the intention early still helps because it
	// provides an explicit signal to Consul's xDS machinery to push the
	// complete per-port configuration for the cross-partition service.
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

	// Deploy the client.
	// CNI + tproxy requires explicit upstream annotations because with
	// CNI, iptables are set up by the DaemonSet at pod-creation time,
	// before consul-dataplane starts. The initial xDS snapshot therefore
	// may not include the cross-partition per-port VIP filter chains,
	// leaving the outbound listener unable to route traffic to 240.0.0.x
	// virtual IPs (Envoy returns 503).
	// Explicit upstream annotations guarantee the cluster is present in
	// xDS regardless of when the dataplane's initial snapshot is taken.
	useTproxyClient := cfg.EnableTransparentProxy && !cfg.EnableCNI
	logger.Log(t, "deploying client in secondary partition cluster")
	if useTproxyClient {
		k8s.DeployKustomize(t, secondaryPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition-multiport-single-service-tproxy")
	} else {
		k8s.DeployKustomize(t, secondaryPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-partitions/default-ns-default-partition-multiport-single-service")
	}

	staticClientPods, err := secondaryPartitionClusterContext.KubernetesClient(t).CoreV1().Pods(metav1.NamespaceAll).List(context.Background(), metav1.ListOptions{LabelSelector: "app=static-client"})
	require.NoError(t, err)
	require.Len(t, staticClientPods.Items, 1)
	require.Len(t, staticClientPods.Items[0].Spec.Containers, 2)

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

	logger.Log(t, "checking cross-partition connectivity for all three ports")
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from api-port 9090: Hello there!", upstreamAPIURL)
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from metrics port 9091: Hello again!", upstreamMetricsURL)
	k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from admin port 9092: Hello once more!", upstreamAdminURL)
}

func TestPartitions_Connect_MultiportServices_ACLsDisabled_LocalGateway(t *testing.T) {
	runPartitionsConnectMultiport(t, false, "../fixtures/bases/mesh-gateway")
}

func TestPartitions_Connect_MultiportServices_ACLsDisabled_RemoteGateway(t *testing.T) {
	runPartitionsConnectMultiport(t, false, "../fixtures/bases/mesh-gateway-remote")
}

func TestPartitions_Connect_MultiportServices_ACLsEnabled_LocalGateway(t *testing.T) {
	runPartitionsConnectMultiport(t, true, "../fixtures/bases/mesh-gateway")
}

func TestPartitions_Connect_MultiportServices_ACLsEnabled_RemoteGateway(t *testing.T) {
	runPartitionsConnectMultiport(t, true, "../fixtures/bases/mesh-gateway-remote")
}
