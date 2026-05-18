// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consuldns

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"net/url"
	"strconv"
	"strings"
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
	t.Skip("skipping test temporarily")
	env := suite.Environment()
	cfg := suite.Config()

	if cfg.EnableCNI {
		t.Skipf("skipping because -enable-cni is set")
	}
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}

	cases := []dnsWithPartitionsTestCase{
		// {
		// 	name:   "not secure - ACLs and auto-encrypt not enabled",
		// 	secure: false,
		// 	port:   privilegedPort,
		// },
		{
			name:   "secure - ACLs and auto-encrypt enabled",
			secure: true,
			port:   privilegedPort,
		},
		// {
		// 	name:   "not secure - ACLs and auto-encrypt not enabled",
		// 	secure: false,
		// 	port:   nonPrivilegedPort,
		// },
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

// fixAuthMethodCACertOnce waits until the named Consul k8s auth methods exist in the given
// partition, then replaces their CACert with the public CA chain fetched from the actual
// API server endpoint.  It must be called as a goroutine started BEFORE
// secondaryConsulCluster.Create(t) so the pods can recover from CrashLoopBackOff.
//
// Why this is needed: server-acl-init reads the service account secret's ca.crt (the cluster-
// internal CA) and stores it in the auth method's CACert field.  On OpenShift ROSA, the
// external API server endpoint (api.*.openshiftapps.com) uses a publicly-trusted certificate
// (e.g. Let's Encrypt), which the internal CA does NOT sign.  Every time a component pod
// calls ACL Login, the Consul server calls /apis/authentication.k8s.io/v1/tokenreviews on
// the external endpoint, fails to verify the cert against the internal CA, and returns
// "x509: certificate signed by unknown authority".
//
// The fix replaces CACert with the intermediate + root CA certificates from the API server's
// TLS chain, allowing the Consul server to validate the endpoint's certificate.
//
// server-acl-init creates the auth method once and exits; it does not re-set CACert on retry.
// Therefore a single fix is sufficient.
func fixAuthMethodCACertOnce(consulClient *api.Client, releaseName, partition string) {
	names := []string{
		fmt.Sprintf("%s-consul-k8s-component-auth-method", releaseName),
		fmt.Sprintf("%s-consul-k8s-auth-method", releaseName),
	}
	queryOpts := &api.QueryOptions{Partition: partition}
	writeOpts := &api.WriteOptions{Partition: partition}

	deadline := time.Now().Add(10 * time.Minute)
	for _, amName := range names {
		for time.Now().Before(deadline) {
			method, _, err := consulClient.ACL().AuthMethodRead(amName, queryOpts)
			if err != nil || method == nil {
				time.Sleep(5 * time.Second)
				continue
			}

			// Get the API server host from the auth method config.
			host, _ := method.Config["Host"].(string)
			if host == "" {
				time.Sleep(5 * time.Second)
				continue
			}

			// Fetch the public CA chain from the API server's TLS endpoint.
			caCert := fetchTLSCACertFromHost(host)
			if caCert == "" {
				time.Sleep(5 * time.Second)
				continue
			}

			method.Config["CACert"] = caCert
			if _, _, err = consulClient.ACL().AuthMethodUpdate(method, writeOpts); err != nil {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
	}
}

// fetchTLSCACertFromHost connects to the given HTTPS host, retrieves the TLS
// certificate chain, and returns PEM-encoded CA certificates (intermediate + root)
// that can validate the server's leaf certificate.
func fetchTLSCACertFromHost(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		return ""
	}
	addr := u.Host
	if !strings.Contains(addr, ":") {
		addr = addr + ":443"
	}

	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return ""
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) < 2 {
		return ""
	}

	// Collect all non-leaf certificates (intermediates + root) as PEM.
	var pemCerts []byte
	for _, cert := range certs[1:] {
		if cert.IsCA || cert.BasicConstraintsValid {
			block := &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}
			pemCerts = append(pemCerts, pem.EncodeToMemory(block)...)
		}
	}

	if len(pemCerts) == 0 {
		return ""
	}
	return string(pemCerts)
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

	// On OpenShift, host ports are forbidden by the restricted-v2 SCC. Cross-partition
	// communication uses the expose-servers LoadBalancer service, so exposeGossipAndRPCPorts
	// (which sets hostPorts) is not needed and must be skipped on OCP.
	if !cfg.EnableOpenshift {
		serverHelmValues["server.exposeGossipAndRPCPorts"] = "true"
	}

	if cfg.UseKind {
		serverHelmValues["server.exposeService.type"] = "NodePort"
		serverHelmValues["server.exposeService.nodePort.https"] = "30000"
	}

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

	// Initialize consulClient now (primary cluster is already ready) so we can pass it to
	// the background goroutine that clears the CACert concurrently with secondary-cluster Create.
	consulClient, _ := defaultConsulCluster.SetupConsulClient(t, c.secure)

	// On OpenShift ROSA, server-acl-init stores the cluster-internal CA in the auth
	// method's CACert field, but the external API server endpoint uses a publicly-
	// trusted certificate.  This goroutine polls until auth methods appear, then
	// replaces CACert with the correct public CA chain fetched from the API server's
	// TLS handshake, allowing pods to recover from CrashLoopBackOff before the
	// readiness timeout expires.
	if c.secure && isOpenShift(t, secondaryClusterContext) {
		go fixAuthMethodCACertOnce(consulClient, releaseName, secondaryPartition)
	}

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
	_, err := k8s.RunKubectlAndGetOutputE(t, defaultClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		require.NoError(t, err)
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, defaultClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
	})

	logger.Logf(t, "creating namespaces %s in clients cluster", staticServerNamespace)
	_, err = k8s.RunKubectlAndGetOutputE(t, secondaryClusterContext.KubectlOptions(t), "create", "ns", staticServerNamespace)
	if err != nil && !strings.Contains(err.Error(), "AlreadyExists") {
		require.NoError(t, err)
	}
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, secondaryClusterContext.KubectlOptions(t), "delete", "ns", staticServerNamespace)
	})

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
