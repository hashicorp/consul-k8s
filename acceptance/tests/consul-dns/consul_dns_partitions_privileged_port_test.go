// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestConsulDNSProxy_WithPartitionsAndPrivilegedPort verifies DNS queries for services across partitions
// when DNS proxy is enabled with privileged port 53. It configures CoreDNS to use configured consul domain queries to
// be forwarded to the Consul DNS Proxy.
func TestConsulDNSProxy_WithPartitionsAndPrivilegedPort(t *testing.T) {
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []dnsWithPartitionsTestCase{
		{
			name:   "not secure - ACLs and auto-encrypt not enabled",
			secure: false,
		},
		{
			name:   "secure - ACLs and auto-encrypt enabled",
			secure: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultClusterContext := env.DefaultContext(t)
			secondaryClusterContext := env.Context(t, 1)

			// Setup the clusters and the static service, but with privileged port 53.
			releaseName, consulClient, defaultPartitionOpts, secondaryPartitionQueryOpts, defaultConsulCluster := setupClustersAndStaticServiceWithPrivilegedPort(t, cfg,
				defaultClusterContext, secondaryClusterContext, c, secondaryPartition,
				defaultPartition)

			// Update CoreDNS to use the Consul domain and forward queries to the Consul DNS Service or Proxy.
			updateCoreDNSWithConsulDomainForPrivilegedPort(t, defaultClusterContext, releaseName)
			updateCoreDNSWithConsulDomainForPrivilegedPort(t, secondaryClusterContext, releaseName)

			podLabelSelector := "app=static-server"
			// The index of the dnsUtils pod to use for the DNS queries so that the pod name can be unique.
			dnsUtilsPodIndex := 0

			// When ACLs are enabled, the unexported service should not resolve.
			shouldResolveUnexportedCrossPartitionDNSRecord := true
			if c.secure {
				shouldResolveUnexportedCrossPartitionDNSRecord = false
			}

			// Verify that the service is in the catalog under each partition.
			verifyServiceInCatalog(t, consulClient, defaultPartitionOpts)
			verifyServiceInCatalog(t, consulClient, secondaryPartitionQueryOpts)

			// Verify DNS proxy uses privileged container for privileged port
			verifyDNSProxyUsesPrivilegedCommand(t, defaultClusterContext, releaseName)
			verifyDNSProxyUsesPrivilegedCommand(t, secondaryClusterContext, releaseName)

			logger.Log(t, "verify the service via DNS in the default partition of the Consul catalog with privileged port.")
			for _, v := range getVerificationsForPrivilegedPort(defaultClusterContext, secondaryClusterContext,
				shouldResolveUnexportedCrossPartitionDNSRecord, cfg, releaseName, defaultConsulCluster) {
				t.Run(v.name, func(t *testing.T) {
					if v.preProcessingFunc != nil {
						v.preProcessingFunc(t)
					}
					verifyDNSWithPrivilegedPort(t, releaseName, staticServerNamespace, v.requestingCtx, v.svcContext,
						podLabelSelector, v.svcName, v.shouldResolveDNS, dnsUtilsPodIndex)
					dnsUtilsPodIndex++
				})
			}
		})
	}
}

func getVerificationsForPrivilegedPort(defaultClusterContext environment.TestContext, secondaryClusterContext environment.TestContext,
	shouldResolveUnexportedCrossPartitionDNSRecord bool, cfg *config.TestConfig, releaseName string, defaultConsulCluster *consul.HelmCluster) []dnsVerification {
	serviceRequestWithNoPartition := fmt.Sprintf("%s.service.consul", staticServerName)
	serviceRequestInDefaultPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, defaultPartition)
	serviceRequestInSecondaryPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, secondaryPartition)
	verifications := []dnsVerification{
		{
			name:             "verify static-server.service.consul from default partition resolves the default partition ip address with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul resolves the default partition ip address with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify the unexported static-server.service.secondary.ap.consul from the default partition with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the secondary partition with privileged port.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.consul from the secondary partition should return the ip in the secondary with privileged port.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul from the secondary partition with privileged port.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the default partition once the service is exported with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "verify static-server.service.default.ap.consul from the secondary partition once the service is exported with privileged port.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				k8s.KubectlApplyK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				})
			},
		},
		{
			name:             "after rollout restart of dns-proxy in default partition - verify static-server.service.secondary.ap.consul from the default partition with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				restartDNSProxy(t, releaseName, defaultClusterContext)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "after rollout restart of dns-proxy in secondary partition - verify static-server.service.default.ap.consul from the secondary partition with privileged port.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				restartDNSProxy(t, releaseName, secondaryClusterContext)
				k8s.KubectlApplyK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, defaultClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/default-partition-default")
				})
			},
		},
		{
			name:             "flip default cluster to use DNS service instead - verify static-server.service.secondary.ap.consul from the default partition with privileged port.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				// Disable DNS proxy, should fall back to DNS service
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "false"})

				// Now we need to update CoreDNS to use the DNS service (not proxy)
				dnsIP, err := getDNSServiceOrProxyIP(t, defaultClusterContext, releaseName, false)
				require.NoError(t, err)

				// When using a privileged port (53), we don't need to specify the port in the CoreDNS config
				input, err := os.ReadFile("coredns-template.yaml")
				require.NoError(t, err)

				// Replace the template placeholder with the DNS service IP
				newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsIP, -1)
				err = os.WriteFile("coredns-custom.yaml", []byte(newContents), 0644)
				require.NoError(t, err)

				// Update CoreDNS with the new configuration
				updateCoreDNS(t, defaultClusterContext, "coredns-custom.yaml")

				// Apply the export configuration
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "flip default cluster back to using DNS Proxy with privileged port - verify static-server.service.secondary.ap.consul from the default partition.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				// Re-enable DNS proxy with privileged port
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "true", "dns.proxy.port": "53"})

				// Now we need to update CoreDNS to use the DNS proxy
				dnsIP, err := getDNSServiceOrProxyIP(t, defaultClusterContext, releaseName, true)
				require.NoError(t, err)

				// When using a privileged port (53), we don't need to specify the port in the CoreDNS config
				input, err := os.ReadFile("coredns-template.yaml")
				require.NoError(t, err)

				// Replace the template placeholder with the DNS proxy IP
				newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsIP, -1)
				err = os.WriteFile("coredns-custom.yaml", []byte(newContents), 0644)
				require.NoError(t, err)

				// Update CoreDNS with the new configuration
				updateCoreDNS(t, defaultClusterContext, "coredns-custom.yaml")

				// Apply the export configuration
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
	}

	return verifications
}

func setupClustersAndStaticServiceWithPrivilegedPort(t *testing.T, cfg *config.TestConfig, defaultClusterContext environment.TestContext,
	secondaryClusterContext environment.TestContext, c dnsWithPartitionsTestCase, secondaryPartition string,
	defaultPartition string) (string, *api.Client, *api.QueryOptions, *api.QueryOptions, *consul.HelmCluster) {
	commonHelmValues := map[string]string{
		"global.adminPartitions.enabled": "true",
		"global.enableConsulNamespaces":  "true",

		"global.tls.enabled":   "true",
		"global.tls.httpsOnly": strconv.FormatBool(c.secure),

		"global.acls.manageSystemACLs": strconv.FormatBool(c.secure),

		"syncCatalog.enabled": "true",
		// When mirroringK8S is set, this setting is ignored.
		"syncCatalog.consulNamespaces.consulDestinationNamespace": defaultNamespace,
		"syncCatalog.consulNamespaces.mirroringK8S":               "false",
		"syncCatalog.addK8SNamespaceSuffix":                       "false",

		"dns.enabled":           "true",
		"dns.proxy.enabled":     "true",
		"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),

		// Configure DNS proxy to use privileged port 53
		"dns.proxy.port": "53",
	}

	serverHelmValues := map[string]string{
		"server.exposeGossipAndRPCPorts": "true",
		"server.extraConfig":             `"{\"log_level\": \"TRACE\"}"`,
	}

	if cfg.UseKind {
		serverHelmValues["server.exposeService.type"] = "NodePort"
		serverHelmValues["server.exposeService.nodePort.https"] = "30000"
	}

	releaseName := helpers.RandomName()

	helpers.MergeMaps(serverHelmValues, commonHelmValues)

	// Set up the default partition cluster.
	defaultConsulCluster := consul.NewHelmCluster(t, serverHelmValues, defaultClusterContext, cfg, releaseName)
	defaultConsulCluster.Create(t)

	// Get the TLS CA certificate and key secret from the server cluster and apply it to the client cluster.
	caCertSecretName := fmt.Sprintf("%s-consul-ca-cert", releaseName)
	caKeySecretName := fmt.Sprintf("%s-consul-ca-key", releaseName)

	logger.Logf(t, "retrieving ca cert secret %s from the server cluster and applying to the client cluster", caCertSecretName)
	k8s.CopySecret(t, defaultClusterContext, secondaryClusterContext, caCertSecretName)

	if !c.secure {
		// When auto-encrypt is disabled, we need both
		// the CA cert and CA key to be available in the clients cluster to generate client certificates and keys.
		logger.Logf(t, "retrieving ca key secret %s from the server cluster and applying to the client cluster", caKeySecretName)
		k8s.CopySecret(t, defaultClusterContext, secondaryClusterContext, caKeySecretName)
	}

	partitionToken := fmt.Sprintf("%s-consul-partitions-acl-token", releaseName)
	if c.secure {
		logger.Logf(t, "retrieving partition token secret %s from the server cluster and applying to the client cluster", partitionToken)
		k8s.CopySecret(t, defaultClusterContext, secondaryClusterContext, partitionToken)
	}

	partitionServiceName := fmt.Sprintf("%s-consul-expose-servers", releaseName)
	partitionSvcAddress := k8s.ServiceHost(t, cfg, defaultClusterContext, partitionServiceName)

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

	if c.secure {
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

	// Set up the secondary partition cluster to join with the default partition cluster.
	secondaryConsulCluster := consul.NewHelmCluster(t, clientHelmValues, secondaryClusterContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)
	logger.Logf(t, "secondary partition cluster joined the default partition's cluster")

	// Create the namespaces for our static-server apps
	logger.Logf(t, "creating namespaces %s in servers cluster", staticServerNamespace)
	k8s.RunKubectl(t, defaultClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, defaultClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
	})

	logger.Logf(t, "creating namespaces %s in clients cluster", staticServerNamespace)
	k8s.RunKubectl(t, secondaryClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, secondaryClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
	})

	defaultStaticServerOpts := &terratestk8s.KubectlOptions{
		ContextName: defaultClusterContext.KubectlOptions(t).ContextName,
		ConfigPath:  defaultClusterContext.KubectlOptions(t).ConfigPath,
		Namespace:   staticServerNamespace,
	}

	secondaryStaticServerOpts := &terratestk8s.KubectlOptions{
		ContextName: secondaryClusterContext.KubectlOptions(t).ContextName,
		ConfigPath:  secondaryClusterContext.KubectlOptions(t).ConfigPath,
		Namespace:   staticServerNamespace,
	}

	consulClient, _ := defaultConsulCluster.SetupConsulClient(t, c.secure)

	defaultPartitionQueryOpts := &api.QueryOptions{Namespace: defaultNamespace, Partition: defaultPartition}
	secondaryPartitionQueryOpts := &api.QueryOptions{Namespace: defaultNamespace, Partition: secondaryPartition}

	// Check that the ACL token is deleted.
	if c.secure {
		// We need to register the cleanup function before we create the deployments
		// because golang will execute them in reverse order i.e. the last registered
		// cleanup function will be executed first.
		t.Cleanup(func() {
			if c.secure {
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
	k8s.DeployKustomize(t, defaultStaticServerOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
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

	logger.Log(t, "verify the service in the default partition of the Consul catalog.")
	service, _, err := consulClient.Catalog().Service(staticServerName, "", defaultPartitionQueryOpts)
	require.NoError(t, err)
	require.Equal(t, 1, len(service))
	require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
	return releaseName, consulClient, defaultPartitionQueryOpts, secondaryPartitionQueryOpts, defaultConsulCluster
}

func updateCoreDNSWithConsulDomainForPrivilegedPort(t *testing.T, ctx environment.TestContext, releaseName string) {
	// Get the DNS service/proxy IP and update CoreDNS to forward to it
	dnsIP, err := getDNSServiceOrProxyIP(t, ctx, releaseName, true)
	require.NoError(t, err)

	// When using a privileged port (53), we don't need to specify the port in the CoreDNS config
	input, err := os.ReadFile("coredns-template.yaml")
	require.NoError(t, err)

	// Replace the template placeholder with the DNS IP (no port needed for standard DNS port 53)
	newContents := strings.Replace(string(input), "{{CONSUL_DNS_IP}}", dnsIP, -1)
	err = os.WriteFile("coredns-custom.yaml", []byte(newContents), 0644)
	require.NoError(t, err)

	updateCoreDNS(t, ctx, "coredns-custom.yaml")

	t.Cleanup(func() {
		updateCoreDNS(t, ctx, "coredns-original.yaml")
	})
}

// getDNSServiceOrProxyIP returns the DNS service or proxy service ClusterIP depending on whether DNS proxy is enabled or not.
func getDNSServiceOrProxyIP(t *testing.T, ctx environment.TestContext, releaseName string, enableDNSProxy bool) (string, error) {
	t.Helper()

	logger.Logf(t, "getting the in cluster %s", getDNSServiceName(releaseName, enableDNSProxy))

	dnsService, err := ctx.KubernetesClient(t).CoreV1().Services(ctx.KubectlOptions(t).Namespace).Get(
		context.Background(),
		getDNSServiceName(releaseName, enableDNSProxy),
		metav1.GetOptions{},
	)

	require.NoError(t, err)
	return dnsService.Spec.ClusterIP, nil
}

// getDNSServiceName returns the correct service name for either DNS proxy or DNS service
func getDNSServiceName(releaseName string, enableDNSProxy bool) string {
	if enableDNSProxy {
		return fmt.Sprintf("%s-consul-dns-proxy", releaseName)
	}
	return fmt.Sprintf("%s-consul-dns", releaseName)
}
