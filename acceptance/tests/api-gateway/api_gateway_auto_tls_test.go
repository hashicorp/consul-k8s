// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"crypto/x509"
	"encoding/base64"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
)

// TestAPIGateway_AutoTLS exercises the zero-touch TLS termination feature
// introduced by consul-k8s PR #5597 (k8s) + consul PR #23647 (server).
//
// # Feature under test
//
// An API Gateway annotated with consul.hashicorp.com/tls-enabled="true" opts
// into Consul-managed, certificate-free HTTPS termination:
//
//   - The annotation is translated into APIGatewayConfigEntry.TLS.Enabled=true.
//   - The gateway listener has no certificateRefs; Consul uses the auto-issued
//     Connect leaf certificate to terminate downstream HTTPS.
//   - The leaf certificate carries "*.api-gateway.<domain>" wildcard DNS SANs,
//     so static-server.api-gateway.consul is a valid name covered by the cert.
//   - The Consul DNS server resolves static-server.api-gateway.consul to the
//     gateway's address (api-gateway DNS auto-registration).
//   - A client can reach the backend at https://static-server.api-gateway.consul
//     using only the Consul Connect CA — no operator-supplied certificate needed.
//
// # In-mesh backend
//
// static-server is deployed with connect-inject enabled so it is a full mesh
// participant. The gateway fronts it; traffic flows:
//
//	curl → gateway (TLS termination, leaf cert) → consul mTLS → static-server sidecar
//
// # How to run locally against a kind cluster
//
//	kind create cluster
//	# load images, then:
//	cd acceptance
//	go test -v -run TestAPIGateway_AutoTLS \
//	  -use-kind \
//	  -consul-image=consul:local \
//	  -consul-k8s-image=consul-k8s-control-plane:local \
//	  -consul-dataplane-image=hashicorp/consul-dataplane:local \
//	  -no-cleanup-on-failure \
//	  ./tests/api-gateway/

// verifyCESANs asserts the two expected CE SANs are present in the cert.
func verifyCESANs(t require.TestingT, cert *x509.Certificate) {
	var foundWildcard bool
	var foundDCWildcard bool
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, "*.api-gateway.") {
			// *.api-gateway.consul (no extra segment after "api-gateway.")
			// vs *.api-gateway.<dc>.consul (one extra segment).
			// CE emits exactly two: *.api-gateway.consul + *.api-gateway.<dc>.consul.
			rest := strings.TrimPrefix(san, "*.api-gateway.")
			segments := strings.Split(rest, ".")
			switch len(segments) {
			case 1:
				// *.api-gateway.consul  (just "consul")
				foundWildcard = true
			case 2:
				// *.api-gateway.<dc>.consul  (e.g. "dc1.consul")
				foundDCWildcard = true
			}
		}
	}
	require.Truef(t, foundWildcard,
		"CE: expected *.api-gateway.consul SAN; cert DNS SANs: %v", cert.DNSNames)
	require.Truef(t, foundDCWildcard,
		"CE: expected *.api-gateway.<dc>.consul SAN; cert DNS SANs: %v", cert.DNSNames)
}

// verifyEntSANs asserts the two expected Enterprise SANs are present when
// namespace mirroring is enabled and the gateway lives in consulNS.
// Expected patterns:
//
//	*.api-gateway.<ns>.consul
//	*.api-gateway.<ns>.<dc>.consul
func verifyEntSANs(t require.TestingT, cert *x509.Certificate, consulNS string) {
	nsPrefix := fmt.Sprintf("*.api-gateway.%s.", consulNS)
	var foundNS bool
	var foundNSDC bool
	for _, san := range cert.DNSNames {
		if !strings.HasPrefix(san, nsPrefix) {
			continue
		}
		rest := strings.TrimPrefix(san, nsPrefix) // e.g. "consul" or "dc1.consul"
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
		"Ent: expected *.api-gateway.%s.consul SAN; cert DNS SANs: %v", consulNS, cert.DNSNames)
	require.Truef(t, foundNSDC,
		"Ent: expected *.api-gateway.%s.<dc>.consul SAN; cert DNS SANs: %v", consulNS, cert.DNSNames)
}

func TestAPIGateway_AutoTLS(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"connectInject.enabled":        "true",
		"global.acls.manageSystemACLs": "true",
		"global.tls.enabled":           "true",
		"global.logLevel":              "trace",
		// Enable the Consul DNS service so CoreDNS can be pointed at it to
		// resolve <svc>.api-gateway.consul from within the cluster.
		"dns.enabled": "true",
	}

	// When running with -enable-enterprise enable Consul namespaces and
	// namespace mirroring so the gateway's Consul namespace matches its
	// Kubernetes namespace (used to verify Ent-scoped SANs).
	if cfg.EnableEnterprise {
		helmValues["global.enableConsulNamespaces"] = "true"
		helmValues["connectInject.consulNamespaces.mirroringK8S"] = "true"
	}

	releaseName := helpers.RandomName()
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	consulClient, _ := consulCluster.SetupConsulClient(t, true)

	// Set global protocol to HTTP so HTTPRoutes are accepted without per-service
	// ServiceDefaults.
	_, _, err := consulClient.ConfigEntries().Set(&api.ProxyConfigEntry{
		Kind: api.ProxyDefaults,
		Name: api.ProxyConfigGlobal,
		Config: map[string]interface{}{
			"protocol": "http",
		},
	}, nil)
	require.NoError(t, err)

	fixturePath := "../fixtures/cases/api-gateways/auto-tls"

	logger.Log(t, "creating auto-tls api-gateway resources")
	out, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", fixturePath)
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", fixturePath)
	})

	logger.Log(t, "creating static-client pod")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-client")

	// Wait for static-server (in-mesh) to be available and registered.
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "wait", "--for=condition=available", "--timeout=5m", "deploy/static-server")
	waitForConsulServiceRegistered(t, consulClient, "static-server")

	// Allow the gateway to reach the in-mesh static-server.
	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "static-server",
		Sources: []*api.SourceIntention{
			{Name: "auto-tls-gateway", Action: api.IntentionActionAllow},
		},
	}, nil)
	require.NoError(t, err)

	// ── 1. Patch CoreDNS to forward .consul queries to Consul DNS ────────────
	// This lets pods resolve static-server.api-gateway.consul inside the cluster.
	logger.Log(t, "patching CoreDNS to forward .consul to Consul DNS")
	patchCoreDNSForConsul(t, releaseName)

	// ── 2. Wait for gateway Accepted + address ────────────────────────────────
	k8sClient := ctx.ControllerRuntimeClient(t)
	var gatewayAddress string

	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gateway gwv1.Gateway
		err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "auto-tls-gateway", Namespace: ctx.KubectlOptions(t).Namespace},
			&gateway)
		require.NoError(r, err)
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Lenf(r, gateway.Status.Addresses, 1, "expected one gateway address")
		gatewayAddress = gateway.Status.Addresses[0].Value
	})

	// ── 3. Consul config entry carries TLS.Enabled = true ────────────────────
	logger.Log(t, "verifying Consul APIGateway config entry has TLS.Enabled=true")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "auto-tls-gateway", nil)
		require.NoError(r, err)
		gw := entry.(*api.APIGatewayConfigEntry)
		require.Truef(r, gw.TLS.Enabled,
			"expected APIGatewayConfigEntry.TLS.Enabled=true after tls-enabled annotation")
	})

	// ── 4. HTTPRoute is accepted and bound ────────────────────────────────────
	logger.Log(t, "verifying HTTPRoute auto-tls-route is accepted and bound")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		var route gwv1.HTTPRoute
		err := k8sClient.Get(context.Background(),
			types.NamespacedName{Name: "auto-tls-route", Namespace: ctx.KubectlOptions(t).Namespace},
			&route)
		require.NoError(r, err)
		require.Lenf(r, route.Status.Parents, 1, "expected one parent ref")
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, route.Status.Parents[0].Conditions, trueCondition("ConsulAccepted", "Accepted"))
	})

	k8sOptions := ctx.KubectlOptions(t)

	// ── 5. HTTPS via raw IP with -k (baseline sanity) ─────────────────────────
	httpsAddr := fmt.Sprintf("https://%s", net.JoinHostPort(gatewayAddress, "8443"))
	logger.Log(t, "sanity: HTTPS via gateway IP with -k")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, httpsAddr, "-k")

	// ── 6. Fetch the leaf certificate and verify DNS SANs ────────────────────
	logger.Log(t, "fetching gateway leaf certificate via openssl from inside the cluster")
	leafPEM := fetchGatewayCert(t, k8sOptions, gatewayAddress, "8443")

	block, _ := pem.Decode([]byte(leafPEM))
	require.NotNil(t, block, "expected PEM block in openssl output")
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	// ── 7. Verify DNS SANs — CE vs Enterprise ─────────────────────────────────
	// CE:  *.api-gateway.consul  +  *.api-gateway.<dc>.consul
	// Ent: *.api-gateway.<ns>.consul  +  *.api-gateway.<ns>.<dc>.consul
	//      (namespace mirroring maps K8s namespace → Consul namespace)
	logger.Log(t, "verifying leaf cert DNS SANs")
	if cfg.EnableEnterprise {
		consulNS := ctx.KubectlOptions(t).Namespace
		logger.Logf(t, "enterprise mode: verifying namespace-scoped SANs for Consul ns %q", consulNS)
		verifyEntSANs(t, cert, consulNS)
		logger.Log(t, "leaf cert carries enterprise namespace-scoped SANs ✓")
	} else {
		logger.Log(t, "CE mode: verifying *.api-gateway.consul and *.api-gateway.<dc>.consul SANs")
		verifyCESANs(t, cert)
		logger.Log(t, "leaf cert carries CE wildcard SANs ✓")
	}

	// Pick the first *.api-gateway.* SAN to derive a hostname for downstream
	// verification steps (DNS resolution + CA-verified HTTPS).
	var wildcardSAN string
	for _, san := range cert.DNSNames {
		if strings.HasPrefix(san, "*.api-gateway.") {
			wildcardSAN = san
			break
		}
	}
	require.NotEmptyf(t, wildcardSAN,
		"expected leaf cert to carry a *.api-gateway.<domain> SAN; got: %v", cert.DNSNames)
	logger.Logf(t, "wildcard SAN used for hostname derivation: %s", wildcardSAN)

	// Derive the concrete hostname: static-server.<rest of wildcard>
	// e.g. *.api-gateway.consul → static-server.api-gateway.consul
	apiGWHostname := "static-server" + strings.TrimPrefix(wildcardSAN, "*")
	logger.Logf(t, "derived service hostname: %s", apiGWHostname)

	// Confirm x509.VerifyHostname accepts the derived name against the cert.
	err = cert.VerifyHostname(apiGWHostname)
	require.NoErrorf(t, err,
		"expected leaf cert wildcard SAN %q to cover %q: %v", wildcardSAN, apiGWHostname, err)
	logger.Log(t, "leaf cert wildcard SAN covers derived hostname ✓")

	// ── 8. Leaf cert chains to the Consul Connect CA ─────────────────────────
	logger.Log(t, "verifying leaf cert chains to Consul Connect CA")
	caRoots, _, err := consulClient.Connect().CARoots(nil)
	require.NoError(t, err)
	require.NotEmpty(t, caRoots.Roots)

	caPool := x509.NewCertPool()
	for _, root := range caRoots.Roots {
		ok := caPool.AppendCertsFromPEM([]byte(root.RootCertPEM))
		require.Truef(t, ok, "failed to parse CA root %s", root.ID)
	}
	_, verifyErr := cert.Verify(x509.VerifyOptions{
		Roots:     caPool,
		DNSName:   apiGWHostname,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	require.NoErrorf(t, verifyErr,
		"leaf cert must chain to Consul Connect CA for hostname %q", apiGWHostname)
	logger.Log(t, "leaf cert chains to Consul Connect CA ✓")

	// ── 9. Write Connect CA root into static-client for curl verification ─────
	// We write the CA PEM into a temp file inside the static-client container so
	// curl can verify the gateway's leaf cert without -k using the real CA.
	logger.Log(t, "writing Consul Connect CA root into static-client container")
	caPEM := caRoots.Roots[0].RootCertPEM
	writeCAPEMToContainer(t, k8sOptions, StaticClientName, "/tmp/consul-ca.crt", caPEM)

	// ── 10. DNS resolution: static-server.api-gateway.consul → gateway IP ─────
	// Verify that Consul DNS resolves the api-gateway hostname to the gateway's
	// address. This proves the state store gateway-services mapping is live.
	logger.Log(t, "verifying DNS resolution of "+apiGWHostname)
	retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, k8sOptions,
			"exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
			"sh", "-c", fmt.Sprintf("nslookup %s 2>&1 || getent hosts %s 2>&1", apiGWHostname, apiGWHostname),
		)
		require.NoError(r, err, out)
		// Either an A record or the gateway IP must appear in the output.
		require.Truef(r,
			strings.Contains(out, gatewayAddress) || strings.Contains(out, "Address"),
			"expected DNS resolution of %q to include gateway address %q; got: %s",
			apiGWHostname, gatewayAddress, out,
		)
	})
	logger.Log(t, "DNS resolution of "+apiGWHostname+" → gateway ✓")

	// ── 11. CA-verified HTTPS using the hostname (no -k) ─────────────────────
	// This is the definitive end-to-end assertion: the client reaches the
	// in-mesh backend by its Consul API-gateway DNS name, with full TLS
	// verification against the Consul Connect CA — zero operator-supplied cert.
	httpsHostnameURL := fmt.Sprintf("https://%s", net.JoinHostPort(apiGWHostname, "8443"))
	logger.Log(t, "CA-verified HTTPS to "+httpsHostnameURL+" (no -k)")
	retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, k8sOptions,
			"exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
			"curl", "-sf", "--cacert", "/tmp/consul-ca.crt",
			"--resolve", fmt.Sprintf("%s:8443:%s", apiGWHostname, gatewayAddress),
			httpsHostnameURL,
		)
		require.NoErrorf(r, err, "CA-verified HTTPS to %s failed: %s", httpsHostnameURL, out)
		require.Containsf(r, out, "hello world",
			"expected backend response, got: %s", out)
	})
	logger.Log(t, "CA-verified HTTPS to static-server.api-gateway.consul ✓")

	// ── 12. Diff-layer regression: toggling annotation updates Consul ─────────
	// Remove tls-enabled → TLS.Enabled=false in Consul, re-add → TLS.Enabled=true.
	// Before the diff.go fix this would have been a no-op (bug: apiGatewaysEqual
	// did not compare TLS or ExtAuthz).
	logger.Log(t, "regression: removing tls-enabled annotation → expect TLS.Enabled=false")
	k8s.RunKubectl(t, k8sOptions, "annotate", "gateway", "auto-tls-gateway",
		"consul.hashicorp.com/tls-enabled-") // trailing dash removes annotation
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "auto-tls-gateway", nil)
		require.NoError(r, err)
		require.Falsef(r, entry.(*api.APIGatewayConfigEntry).TLS.Enabled,
			"expected TLS.Enabled=false after removing annotation")
	})

	logger.Log(t, "regression: re-adding tls-enabled annotation → expect TLS.Enabled=true")
	k8s.RunKubectl(t, k8sOptions, "annotate", "gateway", "auto-tls-gateway",
		"consul.hashicorp.com/tls-enabled=true")
	retryCheckWithWait(t, 60, 2*time.Second, func(r *retry.R) {
		entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "auto-tls-gateway", nil)
		require.NoError(r, err)
		require.Truef(r, entry.(*api.APIGatewayConfigEntry).TLS.Enabled,
			"expected TLS.Enabled=true after re-adding annotation")
	})

	// Confirm traffic still works after annotation round-trip.
	logger.Log(t, "confirming HTTPS still works after annotation round-trip")
	k8s.CheckStaticServerConnectionSuccessful(t, k8sOptions, StaticClientName, httpsAddr, "-k")
}

// patchCoreDNSForConsul patches the cluster's CoreDNS ConfigMap to forward all
// .consul domain queries to the Consul DNS service, then rolls CoreDNS out so
// pods immediately see the new configuration. The original Corefile is restored
// on test cleanup.
func patchCoreDNSForConsul(t *testing.T, releaseName string) {
	t.Helper()

	ctx := suite.Environment().DefaultContext(t)
	k8sOptions := ctx.KubectlOptions(t)
	k8sClient := ctx.KubernetesClient(t)

	// 1. Find the Consul DNS service ClusterIP.
	dnsSvcName := fmt.Sprintf("%s-consul-dns", releaseName)
	dnsSvc, err := k8sClient.CoreV1().Services(k8sOptions.Namespace).
		Get(context.Background(), dnsSvcName, metav1.GetOptions{})
	require.NoErrorf(t, err, "could not find Consul DNS service %s", dnsSvcName)
	consulDNSIP := dnsSvc.Spec.ClusterIP
	logger.Logf(t, "Consul DNS service ClusterIP: %s", consulDNSIP)

	// 2. Detect whether the cluster uses coredns or kube-dns.
	cmClient := k8sClient.CoreV1().ConfigMaps("kube-system")
	cmName := "coredns"
	for _, candidate := range []string{"coredns", "kube-dns"} {
		if _, err := cmClient.Get(context.Background(), candidate, metav1.GetOptions{}); err == nil {
			cmName = candidate
			break
		}
	}
	logger.Logf(t, "DNS ConfigMap name: %s", cmName)

	// 3. Backup original Corefile.
	origCM, err := cmClient.Get(context.Background(), cmName, metav1.GetOptions{})
	require.NoError(t, err)
	origCorefile := origCM.Data["Corefile"]

	// 4. Build new Corefile with consul stub zone.
	newCorefile := origCorefile + fmt.Sprintf(`
consul:53 {
    errors
    cache 30
    forward . %s
}
`, consulDNSIP)

	// 5. Apply the patch.
	patch := fmt.Sprintf(`{"data":{"Corefile":%q}}`, newCorefile)
	_, err = k8sClient.CoreV1().ConfigMaps("kube-system").Patch(
		context.Background(), cmName,
		types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	require.NoError(t, err, "failed to patch CoreDNS ConfigMap")

	// 6. Rollout restart so CoreDNS picks up the new config immediately.
	_, err = k8s.RunKubectlAndGetOutputE(t, k8sOptions,
		"rollout", "restart", "deployment/coredns", "-n", "kube-system")
	if err != nil {
		// kube-dns clusters use a different deployment name; ignore if not found.
		logger.Logf(t, "coredns rollout restart: %v (may be kube-dns cluster, continuing)", err)
	}
	_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions,
		"rollout", "status", "--timeout=2m", "--watch", "deployment/coredns", "-n", "kube-system")

	// 7. Restore original Corefile on test cleanup.
	restorePatch := fmt.Sprintf(`{"data":{"Corefile":%q}}`, origCorefile)
	t.Cleanup(func() {
		logger.Log(t, "restoring original CoreDNS Corefile")
		_, _ = k8sClient.CoreV1().ConfigMaps("kube-system").Patch(
			context.Background(), cmName,
			types.MergePatchType, []byte(restorePatch), metav1.PatchOptions{},
		)
		_, _ = k8s.RunKubectlAndGetOutputE(t, k8sOptions,
			"rollout", "restart", "deployment/coredns", "-n", "kube-system")
	})
}

// writeCAPEMToContainer base64-encodes a PEM and decodes it inside the
// container, avoiding shell-escaping issues with the raw PEM content.
func writeCAPEMToContainer(t *testing.T, opts *terratestk8s.KubectlOptions, deployName, remotePath, pemContent string) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(pemContent))
	cmd := fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", encoded, remotePath)
	out, err := k8s.RunKubectlAndGetOutputE(t, opts,
		"exec", "deploy/"+deployName, "-c", deployName, "--", "sh", "-c", cmd)
	require.NoErrorf(t, err, "failed to write CA PEM to container: %s", out)
}

// fetchGatewayCert execs into the static-client pod and uses openssl s_client
// to retrieve the leaf certificate presented by the gateway at host:port.
// Returns the first PEM block in the openssl output.
func fetchGatewayCert(t *testing.T, opts *terratestk8s.KubectlOptions, host, port string) string {
	t.Helper()
	var pemOut string
	retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
		cmd := fmt.Sprintf(
			"echo | openssl s_client -connect %s:%s -showcerts 2>/dev/null | "+
				"openssl x509 -outform PEM 2>/dev/null",
			host, port,
		)
		out, err := k8s.RunKubectlAndGetOutputE(r, opts,
			"exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
			"sh", "-c", cmd,
		)
		require.NoError(r, err, "openssl s_client failed")
		require.Contains(r, out, "-----BEGIN CERTIFICATE-----",
			"expected PEM cert block; got: %s", out)
		pemOut = out
	})
	return pemOut
}

