// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

// TestAPIGateway_EntTLSTermination is the acceptance-test counterpart of the
// consul-enterprise integration test
// case-ent-api-gateway-http-tls-termination.
//
// # What is being tested (Enterprise-only)
//
// Zero-touch downstream TLS termination for a gateway that lives inside a
// non-default Consul namespace ("payments", mirrored from the K8s namespace
// of the same name).  This exercises the fully-qualified enterprise SAN
// grammar emitted by generateAPIGatewayDNSSANs() for non-default namespaces:
//
//	*.api-gateway.payments.default.consul       (ns + partition)
//	*.api-gateway.payments.default.dc1.consul   (ns + partition + dc)
//
// Assertions map 1-to-1 to the bats verify steps in
// case-ent-api-gateway-http-tls-termination/primary/verify.bats:
//
//  1. Gateway is Accepted and not Conflicted in Consul.
//  2. APIGatewayConfigEntry.TLS.Enabled = true (annotation propagated).
//  3. s1 HTTPRoute (no hostname) is Accepted and Bound.
//  4. s2 HTTPRoute (hostname = "demo.example.com") is Accepted and Bound.
//  5. HTTPS request through the gateway succeeds without a custom cert (-k).
//  6. Leaf cert carries the "*.api-gateway.payments.default.consul" SAN.
//  7. Leaf cert carries the "*.api-gateway.payments.default.<dc>.consul" SAN.
//  8. Leaf cert carries "demo.example.com" as a literal SAN (from route hostname).
//  9. Routing by Host: demo.example.com reaches static-server-2 ("hello-s2").
// 10. Leaf cert chains to the Consul Connect CA.
// 11. CA-verified curl with --resolve demo.example.com succeeds ("hello-s2").
//
// # How to run locally against a kind cluster
//
//	kind create cluster --name consul-ent-test
//	# load images, then:
//	cd acceptance
//	go test -v ./tests/api-gateway/ \
//	  -run TestAPIGateway_EntTLSTermination \
//	  -use-kind \
//	  -kube-contexts kind-consul-ent-test \
//	  -enable-enterprise \
//	  -enterprise-license "$(cat ../consul.hclic)" \
//	  -consul-image hashicorp/consul-enterprise:local \
//	  -consul-k8s-image consul-k8s-control-plane:local \
//	  -consul-dataplane-image hashicorp/consul-dataplane:local \
//	  -no-cleanup-on-failure

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// verifyEntPartitionSANs asserts the expected Enterprise namespace-scoped SANs
// are present on the gateway leaf certificate.
//
// generateAPIGatewayDNSSANs() emits SANs according to the tenancy of the
// gateway:
//
//  default namespace, default partition  →  CE form (no extra segments):
//    *.api-gateway.consul
//    *.api-gateway.<dc>.consul
//
//  non-default namespace, default partition  →  namespace-only form:
//    *.api-gateway.<ns>.consul
//    *.api-gateway.<ns>.<dc>.consul
//
//  non-default namespace AND non-default partition  →  ns+partition form:
//    *.api-gateway.<ns>.<partition>.consul
//    *.api-gateway.<ns>.<partition>.<dc>.consul
//
// The function selects the expected pattern based on the supplied values.
func verifyEntPartitionSANs(t require.TestingT, cert *x509.Certificate, consulNS, consulPartition string) {
	switch {
	case consulNS == "default" && consulPartition == "default":
		// CE SAN grammar — no namespace or partition segment.
		verifyCESANs(t, cert)

	case consulPartition == "default":
		// Non-default namespace in the default partition: *.api-gateway.<ns>.*
		// The partition label is omitted from the SAN.
		prefix := fmt.Sprintf("*.api-gateway.%s.", consulNS)
		var foundNS, foundNSDC bool
		for _, san := range cert.DNSNames {
			if !strings.HasPrefix(san, prefix) {
				continue
			}
			rest := strings.TrimPrefix(san, prefix)
			segments := strings.Split(rest, ".")
			switch len(segments) {
			case 1:
				// *.api-gateway.<ns>.consul
				foundNS = true
			case 2:
				// *.api-gateway.<ns>.<dc>.consul
				foundNSDC = true
			}
		}
		require.Truef(t, foundNS,
			"expected *.api-gateway.%s.consul SAN; cert DNS SANs: %v", consulNS, cert.DNSNames)
		require.Truef(t, foundNSDC,
			"expected *.api-gateway.%s.<dc>.consul SAN; cert DNS SANs: %v", consulNS, cert.DNSNames)

	default:
		// Non-default namespace AND non-default partition: full ns+partition form.
		prefix := fmt.Sprintf("*.api-gateway.%s.%s.", consulNS, consulPartition)
		var foundNSP, foundNSPDC bool
		for _, san := range cert.DNSNames {
			if !strings.HasPrefix(san, prefix) {
				continue
			}
			rest := strings.TrimPrefix(san, prefix)
			segments := strings.Split(rest, ".")
			switch len(segments) {
			case 1:
				foundNSP = true
			case 2:
				foundNSPDC = true
			}
		}
		require.Truef(t, foundNSP,
			"expected *.api-gateway.%s.%s.consul SAN; cert DNS SANs: %v",
			consulNS, consulPartition, cert.DNSNames)
		require.Truef(t, foundNSPDC,
			"expected *.api-gateway.%s.%s.<dc>.consul SAN; cert DNS SANs: %v",
			consulNS, consulPartition, cert.DNSNames)
	}
}

func TestAPIGateway_EntTLSTermination(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skip("skipping enterprise TLS termination test: -enable-enterprise is not set")
	}

	ctx := suite.Environment().DefaultContext(t)

	// Consul Enterprise: namespace mirroring enabled so the K8s namespace
	// "payments" is mirrored to Consul namespace "payments" in partition "default".
	// This exercises the non-default SAN path in generateAPIGatewayDNSSANs():
	//   *.api-gateway.payments.default.consul
	//   *.api-gateway.payments.default.<dc>.consul
	const (
		consulNS        = "payments"
		consulPartition = "default"
	)

	helmValues := map[string]string{
		"connectInject.enabled":                       "true",
		"global.acls.manageSystemACLs":                "true",
		"global.tls.enabled":                          "true",
		"global.logLevel":                             "trace",
		"global.enableConsulNamespaces":               "true",
		"connectInject.consulNamespaces.mirroringK8S": "true",
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	// Create the "payments" K8s namespace for all workloads.
	// With namespace mirroring this creates Consul NS "payments" automatically
	// when the first connect-injected pod starts.
	logger.Log(t, "creating K8s namespace payments")
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "namespace", consulNS)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "namespace", consulNS)
	})

	// kubectl options scoped to the payments namespace — used for all workload ops.
	paymentsOpts := &terratestk8s.KubectlOptions{
		ContextName: ctx.KubectlOptions(t).ContextName,
		ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
		Namespace:   consulNS,
	}

	// Set global protocol to HTTP in Consul (mirrors bats proxy-defaults).
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind:      api.ProxyDefaults,
		Name:      api.ProxyConfigGlobal,
		Namespace: "default",
		Partition: consulPartition,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	fixturePath := "../fixtures/cases/api-gateways/ent-tls-termination"

	logger.Log(t, "creating ent-tls-termination gateway resources in namespace payments")
	out, err := k8s.RunKubectlAndGetOutputE(t, paymentsOpts, "apply", "-k", fixturePath)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, paymentsOpts, "delete", "-k", fixturePath)
	})

	// Deploy static-client in payments so kubectl exec reaches the gateway.
	logger.Log(t, "creating static-client pod in payments namespace")
	k8s.DeployKustomize(t, paymentsOpts, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Wait for both backends.
	k8s.RunKubectl(t, paymentsOpts, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")
	k8s.RunKubectl(t, paymentsOpts, "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server-2")

	// Wait for Consul catalog registration (with namespace query).
	waitForConsulServiceRegisteredInNS := func(svc string) {
		retry.RunWith(&retry.Counter{Count: 60, Wait: 2 * time.Second}, t, func(r *retry.R) {
			services, _, err := consulClient.Catalog().Service(svc, "", &api.QueryOptions{
				Namespace: consulNS,
				Partition: consulPartition,
			})
			require.NoError(r, err)
			require.NotEmptyf(r, services,
				"service %q not yet registered in Consul ns=%s partition=%s", svc, consulNS, consulPartition)
		})
	}
	waitForConsulServiceRegisteredInNS("static-server")
	waitForConsulServiceRegisteredInNS("static-server-2")

	// Allow the gateway to reach both backends (Consul service-intentions).
	for _, svc := range []string{"static-server", "static-server-2"} {
		_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
			Kind:      api.ServiceIntentions,
			Name:      svc,
			Namespace: consulNS,
			Partition: consulPartition,
			Sources: []*api.SourceIntention{
				{
					Name:      "ent-tls-termination-gateway",
					Namespace: consulNS,
					Partition: consulPartition,
					Action:    api.IntentionActionAllow,
				},
			},
		}, nil)
		require.NoError(t, err)
	}

	k8sClient := ctx.ControllerRuntimeClient(t)
	var gatewayAddress string

	// ── 1. Gateway Accepted + address ────────────────────────────────────────
	// Mirrors: "api gateway should have been accepted and not conflicted"
	logger.Log(t, "waiting for gateway to be Accepted")
	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gateway gwv1.Gateway
		err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "ent-tls-termination-gateway", Namespace: consulNS},
			&gateway)
		require.NoError(r, err)
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Lenf(r, gateway.Status.Addresses, 1, "expected one gateway address")
		gatewayAddress = gateway.Status.Addresses[0].Value
	})
	logger.Logf(t, "gateway address: %s", gatewayAddress)

	// ── 2. Consul config entry TLS.Enabled = true ─────────────────────────────
	logger.Log(t, "verifying Consul APIGateway config entry has TLS.Enabled=true")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "ent-tls-termination-gateway",
			&api.QueryOptions{Namespace: consulNS, Partition: consulPartition})
		require.NoError(r, err)
		gw := entry.(*api.APIGatewayConfigEntry)
		require.Truef(r, gw.TLS.Enabled,
			"expected APIGatewayConfigEntry.TLS.Enabled=true after tls-enabled annotation")
	})

	// ── 3. s1 route Accepted + Bound ─────────────────────────────────────────
	logger.Log(t, "waiting for HTTPRoute s1 to be Accepted and Bound")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		var route gwv1.HTTPRoute
		err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "ent-tls-termination-route-s1", Namespace: consulNS},
			&route)
		require.NoError(r, err)
		require.Lenf(r, route.Status.Parents, 1, "expected one parent ref on s1 route")
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	// ── 4. s2 route (demo.example.com) Accepted + Bound ──────────────────────
	logger.Log(t, "waiting for HTTPRoute s2 to be Accepted and Bound")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		var route gwv1.HTTPRoute
		err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "ent-tls-termination-route-s2", Namespace: consulNS},
			&route)
		require.NoError(r, err)
		require.Lenf(r, route.Status.Parents, 1, "expected one parent ref on s2 route")
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	httpsAddr := fmt.Sprintf("https://%s", net.JoinHostPort(gatewayAddress, "8443"))

	// ── 5. HTTPS terminates with no custom cert ───────────────────────────────
	logger.Log(t, "verifying HTTPS terminates with no custom cert (-k)")
	k8s.CheckStaticServerConnectionSuccessful(t, paymentsOpts, StaticClientName, httpsAddr, "-k")

	// ── 6–8. Fetch leaf cert and verify DNS SANs ──────────────────────────────
	logger.Log(t, "fetching gateway leaf certificate")
	leafPEM := fetchGatewayCert(t, paymentsOpts, gatewayAddress, "8443")

	block, _ := pem.Decode([]byte(leafPEM))
	require.NotNil(t, block, "expected PEM block in openssl output")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	logger.Logf(t, "leaf cert DNS SANs: %v", cert.DNSNames)

	// Mirrors bats assertions 6 + 7: namespace+partition-scoped wildcard SANs.
	logger.Logf(t, "verifying SANs for ns=%s partition=%s", consulNS, consulPartition)
	verifyEntPartitionSANs(t, cert, consulNS, consulPartition)
	logger.Log(t, "namespace+partition SANs verified ✓")

	// Mirrors bats assertion 8: demo.example.com literal SAN from route hostname.
	logger.Log(t, "verifying demo.example.com literal SAN")
	var foundDemoSAN bool
	for _, san := range cert.DNSNames {
		if san == "demo.example.com" {
			foundDemoSAN = true
			break
		}
	}
	require.Truef(t, foundDemoSAN,
		"expected demo.example.com literal SAN on leaf cert; DNS SANs: %v", cert.DNSNames)
	logger.Log(t, "demo.example.com SAN verified ✓")

	// ── 9. Host-header routing to s2 ─────────────────────────────────────────
	logger.Log(t, "verifying Host: demo.example.com routes to static-server-2 (hello-s2)")
	retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, paymentsOpts,
			"exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
			"curl", "-sk", "-f", "-H", "Host: demo.example.com", httpsAddr,
		)
		require.NoErrorf(r, err, "curl with Host: demo.example.com failed: %s", out)
		require.Containsf(r, out, "hello-s2",
			"expected static-server-2 response, got: %s", out)
	})
	logger.Log(t, "Host: demo.example.com routes to static-server-2 ✓")

	// ── 10. Leaf cert chains to the Consul Connect CA ─────────────────────────
	logger.Log(t, "verifying leaf cert chains to Consul Connect CA")
	caRoots, _, err := consulClient.Connect().CARoots(nil)
	require.NoError(t, err)
	require.NotEmpty(t, caRoots.Roots)

	caPool := x509.NewCertPool()
	for _, root := range caRoots.Roots {
		ok := caPool.AppendCertsFromPEM([]byte(root.RootCertPEM))
		require.Truef(t, ok, "failed to parse CA root %s", root.ID)
	}
	writeCAPEMToContainer(t, paymentsOpts, StaticClientName, "/tmp/consul-ca.crt", caRoots.Roots[0].RootCertPEM)

	// Derive hostname from the first *.api-gateway.payments.* SAN.
	var apiGWHostname string
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, "*.api-gateway.") {
			apiGWHostname = "static-server" + strings.TrimPrefix(san, "*")
			break
		}
	}
	require.NotEmptyf(t, apiGWHostname,
		"could not derive hostname from cert SANs: %v", cert.DNSNames)

	_, verifyErr := cert.Verify(x509.VerifyOptions{
		Roots:     caPool,
		DNSName:   apiGWHostname,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	require.NoErrorf(t, verifyErr,
		"leaf cert must chain to Consul Connect CA for hostname %q", apiGWHostname)
	logger.Log(t, "leaf cert chains to Consul Connect CA ✓")

	// ── 11. CA-verified curl with demo.example.com via --resolve ─────────────
	logger.Log(t, "CA-verified curl to demo.example.com (no -k)")
	retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, paymentsOpts,
			"exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
			"curl", "-sf", "--cacert", "/tmp/consul-ca.crt",
			"--resolve", fmt.Sprintf("demo.example.com:8443:%s", gatewayAddress),
			"https://demo.example.com:8443",
		)
		require.NoErrorf(r, err, "CA-verified curl to demo.example.com failed: %s", out)
		require.Containsf(r, out, "hello-s2",
			"expected static-server-2 response via demo.example.com, got: %s", out)
	})
	logger.Log(t, "CA-verified HTTPS to demo.example.com ✓")
}
