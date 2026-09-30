// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"fmt"
	"net"
	"strconv"
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
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

const staticServerName = "static-server"
const staticServerNamespace = "ns1"

type dnsWithPartitionsTestCase struct {
	name   string
	secure bool
	port   string
}

type dnsVerification struct {
	name              string
	requestingCtx     environment.TestContext
	svcContext        environment.TestContext
	svcName           string
	shouldResolveDNS  bool
	preProcessingFunc func(t *testing.T)
}

const defaultPartition = "default"
const secondaryPartition = "secondary"
const defaultNamespace = "default"
const privilegedPort = "53"
const nonPrivilegedPort = "8053"

// TestConsulDNSProxy_WithPartitionsAndCatalogSync verifies DNS queries for services across partitions
// when DNS proxy is enabled. It configures CoreDNS to use configure consul domain queries to
// be forwarded to the Consul DNS Proxy.  The test validates:
// - returning the local partition's service when tenancy is not included in the DNS question.
// - properly not resolving DNS for unexported services when ACLs are enabled.
// - properly resolving DNS for exported services when ACLs are enabled.
func TestConsulDNSProxy_WithPartitionsAndCatalogSync(t *testing.T) {
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
			port:   privilegedPort,
		},
		{
			name:   "secure - ACLs and auto-encrypt enabled",
			secure: true,
			port:   privilegedPort,
		},
		{
			name:   "not secure - ACLs and auto-encrypt not enabled",
			secure: false,
			port:   nonPrivilegedPort,
		},
		{
			name:   "secure - ACLs and auto-encrypt enabled",
			secure: true,
			port:   nonPrivilegedPort,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defaultClusterContext := env.DefaultContext(t)
			secondaryClusterContext := env.Context(t, 1)

			// Setup the clusters and the static service.
			releaseName, consulClient, defaultPartitionOpts, secondaryPartitionQueryOpts, defaultConsulCluster := setupClustersAndStaticService(t, cfg,
				defaultClusterContext, secondaryClusterContext, c, secondaryPartition,
				defaultPartition, c.port)

			// Update CoreDNS to use the Consul domain and forward queries to the Consul DNS Service or Proxy.
			updateCoreDNSWithConsulDomain(t, defaultClusterContext, releaseName, true, c.port)
			updateCoreDNSWithConsulDomain(t, secondaryClusterContext, releaseName, true, c.port)

			if c.port == privilegedPort {
				// Validate DNS proxy privileged port configuration.
				validateDNSProxyPrivilegedPort(t, defaultClusterContext, releaseName)
				validateDNSProxyPrivilegedPort(t, secondaryClusterContext, releaseName)
			}
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

			logger.Log(t, "verify the service via DNS in the default partition of the Consul catalog.")
			for _, v := range getVerifications(defaultClusterContext, secondaryClusterContext,
				shouldResolveUnexportedCrossPartitionDNSRecord, cfg, releaseName, defaultConsulCluster, c.port) {
				t.Run(v.name, func(t *testing.T) {
					if v.preProcessingFunc != nil {
						v.preProcessingFunc(t)
					}
					verifyDNS(t, cfg, releaseName, staticServerNamespace, v.requestingCtx, v.svcContext,
						podLabelSelector, v.svcName, v.shouldResolveDNS, dnsUtilsPodIndex)
					dnsUtilsPodIndex++
				})
			}
		})
	}
}

// privateServerExposureValues returns the Helm values that make the Consul
// servers reachable by the other cluster without making them reachable from
// anywhere else.
//
// There are three ways this deployment can put the servers on the internet, and
// all three have to be closed, because an unauthenticated Consul on 8500 is
// what the insecure test cases serve:
//
//  1. server.exposeService is switched on automatically by admin partitions and
//     defaults to type LoadBalancer, so a managed cloud hands it a public IP.
//  2. A NodePort binds the port on every node's interfaces. On a cloud node
//     pool with public node IPs that is exposed just as directly as a load
//     balancer, so it is not a safe substitute -- it is only safe on Kind,
//     where the "nodes" are containers on a local docker bridge.
//  3. server.exposeGossipAndRPCPorts adds hostPort bindings for 8300, 8301,
//     8302 and 8502 straight onto the node. That is the same node-level
//     exposure as (2) and it bypasses Services entirely.
//
// On a cloud the load balancer already carries every port the other cluster
// needs, so the host ports in (3) buy nothing and are left off. This matches
// what the peering and sameness tests already do -- they set that value only
// under UseKind -- and partitions_connect_test.go runs the same admin-partition
// setup on cloud without it at all.
func privateServerExposureValues(t *testing.T, cfg *config.TestConfig) map[string]string {
	values, err := privateServerExposure(cfg)
	require.NoError(t, err)
	return values
}

// privateServerExposure is the decision itself, split out from the assertion so
// that the refusal is reachable from a test. Returning an error rather than
// falling back is the point: there is no safe default here.
func privateServerExposure(cfg *config.TestConfig) (map[string]string, error) {
	// On Kind there are no load balancers, but every cluster shares the docker
	// bridge network, so a node address is reachable from the other cluster and
	// is not reachable from outside the host running them.
	if cfg.UseKind {
		return map[string]string{
			"server.exposeService.type":           "NodePort",
			"server.exposeService.nodePort.https": "30000",
			"server.exposeGossipAndRPCPorts":      "true",
		}, nil
	}

	// Anywhere else the servers go behind a load balancer pinned to the private
	// network. If the platform is not one we have an annotation for, stop.
	// Letting the chart's own default through would publish the servers, and a
	// NodePort would be both exposed and useless, because ServiceHost resolves
	// this Service through its load balancer ingress on everything except Kind.
	annotation := internalLoadBalancerAnnotation(cfg)
	if annotation == "" {
		return nil, fmt.Errorf("refusing to install: this platform has no known private load balancer " +
			"annotation, so the Consul servers would be published to the internet on a public IP; add the " +
			"platform's internal load balancer annotation to internalLoadBalancerAnnotation first")
	}

	return map[string]string{"server.exposeService.annotations": annotation}, nil
}

// internalLoadBalancerAnnotation returns the provider annotations that keep a
// Service of type LoadBalancer on the cluster's private network, or "" when the
// platform is unknown and there is no safe configuration to install.
//
// Both spellings are emitted where a provider has two, because which one is
// read depends on which controller reconciles the Service, not on the cloud:
// EKS running the AWS Load Balancer Controller reads -scheme and ignores
// -internal, while the in-tree provider reads -internal; GKE moved from
// cloud.google.com to networking.gke.io. An annotation the controller does not
// recognise is silently discarded, so emitting the one that is wrong for the
// cluster would hand the servers a public IP with nothing to say so. Sending
// both costs nothing and removes that guess.
func internalLoadBalancerAnnotation(cfg *config.TestConfig) string {
	switch {
	case cfg.UseAKS:
		return `service.beta.kubernetes.io/azure-load-balancer-internal: "true"`
	case cfg.UseEKS:
		return "service.beta.kubernetes.io/aws-load-balancer-internal: \"true\"\n" +
			"service.beta.kubernetes.io/aws-load-balancer-scheme: internal"
	case cfg.UseGKE, cfg.UseGKEAutopilot:
		return "networking.gke.io/load-balancer-type: \"Internal\"\n" +
			"cloud.google.com/load-balancer-type: \"Internal\""
	default:
		return ""
	}
}

// requirePrivateServerAddress fails the test unless the address the other
// cluster is about to dial is on a private network.
//
// This is the control that actually holds. The annotations above are a request,
// not a guarantee: a provider silently drops an annotation it does not
// recognise, so if the spelling is wrong for this cluster's controller the
// Service still comes up, the test still passes, and the Consul servers sit on
// a public IP -- serving, in the non-secure cases, an unauthenticated HTTP API
// on 8500. Nothing in the Helm values can detect that. Only the address that
// was actually assigned can.
//
// This detects rather than prevents: the load balancer has to exist before its
// address is known. Failing here keeps the window to the length of one failed
// install instead of a full test run, and the cluster is torn down on the way
// out.
func requirePrivateServerAddress(t *testing.T, cfg *config.TestConfig, address string) {
	t.Helper()

	// Kind's node addresses are on a local docker bridge, unreachable from off
	// the machine running the test.
	if cfg.UseKind {
		return
	}

	// AWS hands out a hostname rather than an address, so this has to resolve.
	// It also accepts an address literal unchanged.
	ips, err := net.LookupIP(address)
	require.NoErrorf(t, err, "could not resolve Consul server address %q to verify it is private", address)
	require.NotEmptyf(t, ips, "Consul server address %q resolved to nothing", address)

	for _, ip := range ips {
		require.Truef(t, isPrivateAddress(ip),
			"the Consul servers are reachable at %s (%s), which is a public address: this cluster did not "+
				"honour the internal load balancer annotation, so the servers are exposed to the internet. "+
				"Refusing to continue.", address, ip)
	}
}

// isPrivateAddress reports whether ip is on a network that is not routable from
// the internet.
func isPrivateAddress(ip net.IP) bool {
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	// Carrier-grade NAT, 100.64.0.0/10. Some managed clusters allocate internal
	// load balancers and node addresses from it, and it is not internet
	// routable, but net.IP.IsPrivate does not cover it.
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	return false
}

func getVerifications(defaultClusterContext environment.TestContext, secondaryClusterContext environment.TestContext,
	shouldResolveUnexportedCrossPartitionDNSRecord bool, cfg *config.TestConfig, releaseName string, defaultConsulCluster *consul.HelmCluster, port string) []dnsVerification {
	serviceRequestWithNoPartition := fmt.Sprintf("%s.service.consul", staticServerName)
	serviceRequestInDefaultPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, defaultPartition)
	serviceRequestInSecondaryPartition := fmt.Sprintf("%s.service.%s.ap.consul", staticServerName, secondaryPartition)
	verifications := []dnsVerification{
		{
			name:             "verify static-server.service.consul from default partition resolves the default partition ip address.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul resolves the default partition ip address.",
			requestingCtx:    defaultClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify the unexported static-server.service.secondary.ap.consul from the default partition. With ACLs turned on, this should not resolve. Otherwise, it will resolve.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the secondary partition.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.consul from the secondary partition should return the ip in the secondary.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestWithNoPartition,
			shouldResolveDNS: true,
		},
		{
			name:             "verify static-server.service.default.ap.consul from the secondary partition. With ACLs turned on, this should not resolve. Otherwise, it will resolve.",
			requestingCtx:    secondaryClusterContext,
			svcContext:       defaultClusterContext,
			svcName:          serviceRequestInDefaultPartition,
			shouldResolveDNS: shouldResolveUnexportedCrossPartitionDNSRecord,
		},
		{
			name:             "verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
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
			name:             "verify static-server.service.default.ap.consul from the secondary partition once the service is exported.",
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
			name:             "after rollout restart of dns-proxy in default partition - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
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
			name:             "after rollout restart of dns-proxy in secondary partition - verify static-server.service.default.ap.consul from the secondary partition once the service is exported.",
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
			name:             "flip default cluster to use DNS service instead - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "false"})
				updateCoreDNSWithConsulDomain(t, defaultClusterContext, releaseName, false, port)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
		{
			name:             "flip default cluster back to using DNS Proxy - verify static-server.service.secondary.ap.consul from the default partition once the service is exported.",
			requestingCtx:    defaultClusterContext,
			svcContext:       secondaryClusterContext,
			svcName:          serviceRequestInSecondaryPartition,
			shouldResolveDNS: true,
			preProcessingFunc: func(t *testing.T) {
				defaultConsulCluster.Upgrade(t, map[string]string{"dns.proxy.enabled": "true"})
				updateCoreDNSWithConsulDomain(t, defaultClusterContext, releaseName, true, port)
				k8s.KubectlApplyK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
					k8s.KubectlDeleteK(t, secondaryClusterContext.KubectlOptions(t), "../fixtures/cases/crd-partitions/secondary-partition-default")
				})
			},
		},
	}

	return verifications
}

func restartDNSProxy(t *testing.T, releaseName string, ctx environment.TestContext) {
	dnsDeploymentName := fmt.Sprintf("deployment/%s-consul-dns-proxy", releaseName)
	restartDNSProxyCommand := []string{"rollout", "restart", dnsDeploymentName}
	k8sOptions := ctx.KubectlOptions(t)
	logger.Log(t, fmt.Sprintf("restarting the dns-proxy deployment in %s k8s context", k8sOptions.ContextName))
	_, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, restartDNSProxyCommand...)
	require.NoError(t, err)

	// Wait for restart to finish.
	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "rollout", "status", "--timeout", "1m", "--watch", dnsDeploymentName)
	require.NoError(t, err, out, "rollout status command errored, this likely means the rollout didn't complete in time")
	logger.Log(t, fmt.Sprintf("dns-proxy deployment in %s k8s context has finished restarting", k8sOptions.ContextName))
}
func verifyServiceInCatalog(t *testing.T, consulClient *api.Client, queryOpts *api.QueryOptions) {
	logger.Log(t, "verify the service in the secondary partition of the Consul catalog.")
	svc, _, err := consulClient.Catalog().Service(staticServerName, "", queryOpts)
	require.NoError(t, err)
	require.Equal(t, 1, len(svc))
	require.Equal(t, []string{"k8s"}, svc[0].ServiceTags)
}

func setupClustersAndStaticService(t *testing.T, cfg *config.TestConfig, defaultClusterContext environment.TestContext,
	secondaryClusterContext environment.TestContext, c dnsWithPartitionsTestCase, secondaryPartition string,
	defaultPartition string, port string) (string, *api.Client, *api.QueryOptions, *api.QueryOptions, *consul.HelmCluster) {
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

		"dns.proxy.port": port,
	}

	serverHelmValues := map[string]string{
		"server.extraConfig": `"{\"log_level\": \"TRACE\"}"`,
	}

	helpers.MergeMaps(serverHelmValues, privateServerExposureValues(t, cfg))

	releaseName := helpers.RandomName()

	helpers.MergeMaps(serverHelmValues, commonHelmValues)

	// Install the consul cluster with servers in the default kubernetes context.
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
	requirePrivateServerAddress(t, cfg, partitionSvcAddress)

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

	// Install the consul cluster without servers in the client cluster kubernetes context.
	secondaryConsulCluster := consul.NewHelmCluster(t, clientHelmValues, secondaryClusterContext, cfg, releaseName)
	secondaryConsulCluster.Create(t)

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
