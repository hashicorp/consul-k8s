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

	cases := []struct {
		name        string
		aclsEnabled bool
	}{
		{name: "acls disabled", aclsEnabled: false},
		{name: "acls enabled", aclsEnabled: true},
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
				"global.tls.httpsOnly": strconv.FormatBool(c.aclsEnabled),

				"global.acls.manageSystemACLs": strconv.FormatBool(c.aclsEnabled),

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
			if c.aclsEnabled {
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

			if c.aclsEnabled {
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

			consulClient, _ := serverConsulCluster.SetupConsulClient(t, c.aclsEnabled)

			logger.Log(t, "creating proxy defaults with mesh gateway mode")
			k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/bases/mesh-gateway")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/bases/mesh-gateway")
			})
			k8s.KubectlApplyK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/bases/mesh-gateway")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, secondaryPartitionClusterContext.KubectlOptions(t), "../fixtures/bases/mesh-gateway")
			})

			logger.Log(t, "deploying multi-port service in default partition cluster")
			k8s.DeployKustomize(t, defaultPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/multiport-single-service-app")

			logger.Log(t, "deploying client in secondary partition cluster")
			if cfg.EnableTransparentProxy {
				k8s.DeployKustomize(t, secondaryPartitionClusterContext.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-client-tproxy")
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

			logger.Log(t, "exporting multi-port services from default partition to secondary partition")
			k8s.KubectlApplyK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, defaultPartitionClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default-multiport-single-service")
			})

			upstreamAPIURL := "http://localhost:1234"
			upstreamMetricsURL := "http://localhost:2234"
			upstreamAdminURL := "http://localhost:3234"
			if cfg.EnableTransparentProxy {
				upstreamAPIURL = fmt.Sprintf("http://%s.virtual.default.ns.%s.ap.dc1.dc.consul:9090", multiportServiceName, defaultPartition)
				upstreamMetricsURL = fmt.Sprintf("http://%s.virtual.default.ns.%s.ap.dc1.dc.consul:9091", multiportServiceName, defaultPartition)
				upstreamAdminURL = fmt.Sprintf("http://%s.virtual.default.ns.%s.ap.dc1.dc.consul:9092", multiportServiceName, defaultPartition)
			}

			secondaryClientOpts := secondaryPartitionClusterContext.KubectlOptions(t)

			if c.aclsEnabled {
				logger.Log(t, "checking that cross-partition connections fail before intentions are configured")
				k8s.CheckStaticServerConnectionFailing(t, secondaryClientOpts, StaticClientName, upstreamAPIURL)
				k8s.CheckStaticServerConnectionFailing(t, secondaryClientOpts, StaticClientName, upstreamMetricsURL)
				k8s.CheckStaticServerConnectionFailing(t, secondaryClientOpts, StaticClientName, upstreamAdminURL)

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
			}

			logger.Log(t, "checking cross-partition connectivity for all three ports")
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from api-port 9090: Hello there!", upstreamAPIURL)
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from metrics port 9091: Hello again!", upstreamMetricsURL)
			k8s.CheckStaticServerConnectionSuccessfulWithMessage(t, secondaryClientOpts, StaticClientName, "Response from admin port 9092: Hello once more!", upstreamAdminURL)

			logger.Log(t, "marking multi-port workload unhealthy")
			k8s.RunKubectl(t, defaultPartitionClusterContext.KubectlOptions(t), "exec", "deploy/"+multiportServiceName, "-c", multiportServiceName, "--", "touch", "/tmp/unhealthy-multiport")

			failureMessages := []string{"curl: (56) Recv failure: Connection reset by peer", "curl: (52) Empty reply from server"}
			if cfg.EnableTransparentProxy {
				failureMessages = append(failureMessages, "curl: (7) Failed to connect")
			}
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamAPIURL)
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamMetricsURL)
			k8s.CheckStaticServerConnectionMultipleFailureMessages(t, secondaryClientOpts, StaticClientName, false, failureMessages, "", upstreamAdminURL)
		})
	}
}
