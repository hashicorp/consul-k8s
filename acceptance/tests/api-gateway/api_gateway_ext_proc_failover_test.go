// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
)

// ext-proc failover fixture directories. common holds the shared backends,
// processors, mesh services, protocols, RouteExtProc CRDs and intentions. single
// and two layer the two gateway topologies on top of common and must be applied
// AFTER it.
const (
	extProcCommonPath = "../fixtures/cases/api-gateways/ext-proc-failover/common"
	extProcSinglePath = "../fixtures/cases/api-gateways/ext-proc-failover/single"
	extProcTwoPath    = "../fixtures/cases/api-gateways/ext-proc-failover/two"
	// extProcAppsPath is the directory that contains each app's Dockerfile and
	// source. It is resolved at runtime relative to this file using
	// runtime.Caller so it is correct regardless of the working directory the
	// test binary is invoked from.
	extProcAppsPath = "../fixtures/cases/api-gateways/ext-proc-failover/apps"
)

// extProcImages is the ordered list of (subdirectory, image tag) pairs that
// buildAndLoadExtProcImages must build and kind-load.
var extProcImages = []struct{ dir, tag string }{
	{"ext-proc-http", "local/ext-proc-http:0.1"},
	{"route-decider", "local/route-decider:0.1"},
	{"service-d", "local/service-d:0.1"},
	{"service-e", "local/service-e:0.1"},
	{"ext-proc-http-connect-proxy", "local/ext-proc-http-connect-proxy:0.1"},
	{"http-decider-connect-proxy", "local/http-decider-connect-proxy:0.1"},
}

// Gateway names for the two topologies under test.
const (
	extProcSingleGateway = "api-gateway-single"
	extProcTwoGateway    = "api-gateway"
)

// extProcServerPeer / extProcClientPeer are the Consul peering names as seen
// from each cluster. The PeeringAcceptor (named "server") is created on the
// client cluster, so the client cluster reaches its peer as "server". The
// PeeringDialer (named "client") is created on the server cluster, so the server
// cluster reaches its peer as "client".
const (
	extProcServerPeer = "server"
	extProcClientPeer = "client"
)

// extProcFailoverServices are the in-mesh services that get a cross-cluster
// ServiceResolver failover target in the peer cluster. This covers the backends,
// BOTH gateway ext-proc processors and the deciders so the entire decision path
// fails over. (The connect-proxy ext-proc for service-e1 runs as a sidecar in
// service-e1's own pod, so failing over service-e1 also fails over its ext-proc.)
var extProcFailoverServices = []string{
	"service-a",
	"service-b",
	"service-c",
	"service-d1",
	"service-e1",
	"service-f",
	"service-g",
	"ext-proc-http",
	"ext-proc-http-path",
	"route-decider",
	"http-decider-connect-proxy",
}

// extProcDeployments are the workloads that must be Ready in each cluster before
// the test drives traffic.
var extProcDeployments = []string{
	"service-a", "service-b", "service-c", "service-f", "service-g",
	"service-d1", "service-e1", "ext-proc-http", "ext-proc-http-path",
	"route-decider", "http-decider-connect-proxy", StaticClientName,
}

// TestAPIGateway_ExtProc_MultiClusterFailover exercises Envoy external
// processing (builtin/ext-proc) in HTTP mode across BOTH gateway topologies and
// on a connect-proxy inbound listener, and asserts the whole ext-proc decision
// path fails over across two peered Kubernetes clusters.
//
// Topologies (mirror 103-envoy-ext-proc-kind-failover, HTTP variants only):
//
//   - SINGLE gateway ext-proc (api-gateway-single): one builtin/ext-proc instance
//     with an empty statPrefix, so Consul emits the un-suffixed
//     envoy.filters.http.ext_proc filter and no RouteExtProc filters are needed.
//     /a->service-a, /b->service-b, /c->service-c.
//
//   - TWO gateway ext-proc (api-gateway): two builtin/ext-proc instances keyed by
//     statPrefix base (ext-proc-http) and path (ext-proc-http-path), so Consul
//     emits envoy.filters.http.ext_proc/base and .../path. Routes attach
//     RouteExtProc disable-base/disable-path so bare /a|/b|/c run only base and
//     /ext_proc/a|/b|/c run only path. /aa|/bb|/cc and /ext_proc/aa|/bb|/cc are
//     static bypass routes that disable BOTH instances.
//
//   - Connect-proxy ext-proc (HTTP): /dd1 -> service-d1 -> service-e1, whose
//     inbound connect-proxy runs the HTTP ext-proc sidecar to fan out to
//     service-f/service-g.
//
// Assertions:
//   - Positive routing for the single and two gateways (base + path families) and
//     the /dd1 connect-proxy fan-out.
//   - Negative routing: the two-gateway bypass routes do NOT consult the path
//     processor (proven by response bodies AND by the ext-proc-http-path logs).
//   - Envoy config: the single gateway has ONLY the un-suffixed ext_proc filter;
//     the two gateway has BOTH the /base and /path suffixed filters.
//   - Processor logs confirm the correct processor observed each routed path.
//   - Cross-cluster failover: scaling a backend, a gateway processor, a processor
//   - its decider, and the connect-proxy chain to zero in the server cluster;
//     each route still succeeds because it is served by the client peer.
//
// ext-proc is Consul Enterprise only, and the processor/decider apps are the
// small stdlib HTTP services vendored (with their Dockerfiles) under
// ../fixtures/cases/api-gateways/ext-proc-failover/apps. This test is therefore
// opt-in: it is skipped unless an enterprise license is configured
//
//	cd ../fixtures/cases/api-gateways/ext-proc-failover/apps
//	./build-images.sh <server-cluster-name> <client-cluster-name>
func TestAPIGateway_ExtProc_MultiClusterFailover(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping this test because -enable-enterprise is not set")
	}
	env := suite.Environment()

	if !cfg.UseKind {
		t.Skip("skipping because -use-kind is not set; cross-cluster peering in this test relies on kind NodePort mesh gateways")
	}

	skipUnlessEnterpriseLicenseConfigured(t)

	serverCtx := env.DefaultContext(t)
	clientCtx := env.Context(t, 1)

	commonHelmValues := map[string]string{
		"global.peering.enabled": "true",
		"global.tls.enabled":     "true",

		"connectInject.enabled": "true",

		"meshGateway.enabled":  "true",
		"meshGateway.replicas": "1",

		"dns.enabled":           "true",
		"dns.enableRedirection": strconv.FormatBool(cfg.EnableTransparentProxy),
	}

	releaseName := helpers.RandomName()

	// Install both peers in parallel. On kind there are no load balancers, but the
	// clusters share the docker bridge network, so a NodePort mesh gateway on one
	// cluster is reachable from the other.
	var wg sync.WaitGroup
	var serverCluster, clientCluster *consul.HelmCluster

	wg.Add(1)
	go func() {
		defer wg.Done()
		values := map[string]string{
			"global.datacenter":              "dc1",
			"server.exposeGossipAndRPCPorts": "true",
			"meshGateway.service.type":       "NodePort",
			"meshGateway.service.nodePort":   "30100",
		}
		helpers.MergeMaps(values, commonHelmValues)
		serverCluster = consul.NewHelmCluster(t, values, serverCtx, cfg, releaseName)
		serverCluster.Create(t)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		values := map[string]string{
			"global.datacenter":              "dc2",
			"server.exposeGossipAndRPCPorts": "true",
			"meshGateway.service.type":       "NodePort",
			"meshGateway.service.nodePort":   "30100",
		}
		helpers.MergeMaps(values, commonHelmValues)
		clientCluster = consul.NewHelmCluster(t, values, clientCtx, cfg, releaseName)
		clientCluster.Create(t)
	}()

	logger.Log(t, "waiting for both clusters to start up")
	wg.Wait()

	serverClient, _ := serverCluster.SetupConsulClient(t, false)
	clientClient, _ := clientCluster.SetupConsulClient(t, false)

	serverOpts := extProcKubectlOptions(t, serverCtx)
	clientOpts := extProcKubectlOptions(t, clientCtx)

	// Enable mesh-gateway peering on both clusters.
	logger.Log(t, "creating mesh peering config")
	meshPeeringDir := "../fixtures/bases/mesh-peering"
	k8s.KubectlApplyK(t, serverCtx.KubectlOptions(t), meshPeeringDir)
	k8s.KubectlApplyK(t, clientCtx.KubectlOptions(t), meshPeeringDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, serverCtx.KubectlOptions(t), meshPeeringDir)
		k8s.KubectlDeleteK(t, clientCtx.KubectlOptions(t), meshPeeringDir)
	})

	// Wait for the Mesh config entries to land in Consul on both clusters.
	timer := &retry.Timer{Timeout: 2 * time.Minute, Wait: 1 * time.Second}
	retry.RunWith(timer, t, func(r *retry.R) {
		for _, c := range []*api.Client{serverClient, clientClient} {
			entry, _, err := c.ConfigEntries().Get(api.MeshConfig, "mesh", nil)
			require.NoError(r, err)
			mesh, ok := entry.(*api.MeshConfigEntry)
			require.True(r, ok)
			require.Equal(r, "mesh", mesh.GetName())
		}
	})

	// Establish peering: acceptor on the client cluster, dialer on the server
	// cluster, copying the generated token secret between them.
	logger.Log(t, "establishing cluster peering")
	k8s.KubectlApply(t, clientCtx.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDelete(t, clientCtx.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
	})
	retry.RunWith(timer, t, func(r *retry.R) {
		helpers.EnsurePeeringAcceptorSecret(t, r, clientCtx.KubectlOptions(t), "../fixtures/bases/peering/peering-acceptor.yaml")
	})
	k8s.CopySecret(t, clientCtx, serverCtx, "api-token")
	k8s.KubectlApply(t, serverCtx.KubectlOptions(t), "../fixtures/bases/peering/peering-dialer.yaml")
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, serverCtx.KubectlOptions(t), "delete", "secret", "api-token")
		k8s.KubectlDelete(t, serverCtx.KubectlOptions(t), "../fixtures/bases/peering/peering-dialer.yaml")
	})

	// Wait for the peering to become ACTIVE on both sides.
	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		peering, _, err := serverClient.Peerings().Read(context.Background(), extProcClientPeer, nil)
		require.NoError(r, err)
		require.NotNil(r, peering)
		require.Equal(r, api.PeeringStateActive, peering.State)
	})

	// Route mesh traffic through the mesh gateways on both clusters.
	logger.Log(t, "creating mesh-gateway proxy-defaults")
	meshGatewayDir := "../fixtures/bases/mesh-gateway"
	k8s.KubectlApplyK(t, serverCtx.KubectlOptions(t), meshGatewayDir)
	k8s.KubectlApplyK(t, clientCtx.KubectlOptions(t), meshGatewayDir)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.KubectlDeleteK(t, serverCtx.KubectlOptions(t), meshGatewayDir)
		k8s.KubectlDeleteK(t, clientCtx.KubectlOptions(t), meshGatewayDir)
	})

	// Build the six ext-proc app images from source and load them into both
	// kind clusters so the image cache is warm before any Deployment is created.
	logger.Log(t, "building and loading ext-proc app images into both kind clusters")
	buildAndLoadExtProcImages(t, serverCtx, clientCtx)

	// Deploy the ext-proc stack (common + single + two) + a static-client into
	// BOTH clusters.
	logger.Log(t, "deploying ext-proc stack (common + single + two) into both clusters")
	deployExtProcStack(t, serverOpts)
	deployExtProcStack(t, clientOpts)

	// Wait for all workloads to be Ready in both clusters.
	for _, opts := range []*terratestk8s.KubectlOptions{serverOpts, clientOpts} {
		for _, deployment := range extProcDeployments {
			k8s.RunKubectl(t, opts, "wait", "--for=condition=available", "--timeout=5m", "deploy/"+deployment)
		}
	}

	// Export services and wire cross-cluster failover on both clusters. The peer
	// name differs per cluster (server->client, client->server).
	logger.Log(t, "wiring cross-cluster failover on both clusters")
	applyExtProcFailover(t, serverClient, extProcClientPeer)
	applyExtProcFailover(t, clientClient, extProcServerPeer)

	// Wait for the gateway backends to register in each cluster's catalog.
	for _, c := range []*api.Client{serverClient, clientClient} {
		waitForConsulServiceRegistered(t, c, "service-a")
		waitForConsulServiceRegistered(t, c, "service-d1")
	}

	// Resolve each gateway's address once it is Accepted.
	serverSingleURL := extProcGatewayURL(t, serverCtx, extProcSingleGateway)
	serverTwoURL := extProcGatewayURL(t, serverCtx, extProcTwoGateway)
	clientTwoURL := extProcGatewayURL(t, clientCtx, extProcTwoGateway)

	// ── SINGLE gateway: positive routing ─────────────────────────────────────
	t.Run("single/routing", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireGatewayBodyContains(r, serverOpts, serverSingleURL+"/a", "hello from service-a")
			requireGatewayBodyContains(r, serverOpts, serverSingleURL+"/b", "hello from service-b")
			requireGatewayBodyContains(r, serverOpts, serverSingleURL+"/c", "hello from service-c")
		})
		// The single processor observed each routed path.
		requireProcessorLogContains(t, serverOpts, "ext-proc-http", `path="/b"`)
		requireProcessorLogContains(t, serverOpts, "ext-proc-http", `path="/c"`)
	})

	// ── SINGLE gateway: Envoy config (only the un-suffixed ext_proc filter) ───
	t.Run("single/envoy-config", func(t *testing.T) {
		requireGatewayExtProcFilters(t, serverOpts, extProcSingleGateway, "single")
	})

	// ── TWO gateway: positive base-family routing (bare paths) ────────────────
	t.Run("two/routing-base", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/a", "hello from service-a")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/b", "hello from service-b")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/c", "hello from service-c")
		})
		requireProcessorLogContains(t, serverOpts, "ext-proc-http", `path="/b"`)
	})

	// ── TWO gateway: positive path-family routing (/ext_proc/*) ───────────────
	t.Run("two/routing-path", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/a", "hello from service-a")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/b", "hello from service-b")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/c", "hello from service-c")
		})
		// The path processor observed the /ext_proc/* paths.
		requireProcessorLogContains(t, serverOpts, "ext-proc-http-path", `path="/ext_proc/b"`)
		requireProcessorLogContains(t, serverOpts, "ext-proc-http-path", `path="/ext_proc/c"`)
	})

	// ── TWO gateway: NEGATIVE bypass routing ──────────────────────────────────
	// /ext_proc/aa|bb|cc disable BOTH processors and route statically. For bb/cc
	// the static target (service-b/service-c) differs from what the path processor
	// would have chosen (service-a, since /bb and /cc are not /b or /c), so the
	// body alone proves the bypass. For all three, the path processor must never
	// have observed the path (proven by its logs).
	t.Run("two/routing-negative-bypass", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/aa", "hello from service-a")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/bb", "hello from service-b")
			requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/cc", "hello from service-c")
		})
		// The path processor was bypassed: none of the bypass paths appear in its
		// logs (the quote guards against /ext_proc/a matching /ext_proc/aa).
		requireProcessorLogAbsent(t, serverOpts, "ext-proc-http-path", `path="/ext_proc/aa"`)
		requireProcessorLogAbsent(t, serverOpts, "ext-proc-http-path", `path="/ext_proc/bb"`)
		requireProcessorLogAbsent(t, serverOpts, "ext-proc-http-path", `path="/ext_proc/cc"`)
	})

	// ── TWO gateway: Envoy config (both /base and /path suffixed filters) ─────
	t.Run("two/envoy-config", func(t *testing.T) {
		requireGatewayExtProcFilters(t, serverOpts, extProcTwoGateway, "two")
	})

	// ── TWO gateway: connect-proxy ext-proc fan-out (/dd1) ────────────────────
	t.Run("two/connect-proxy", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireConnectProxyFanOut(r, serverOpts, serverTwoURL+"/dd1")
		})
	})

	// ── Per-cluster sanity: the two gateway also routes on the client cluster ─
	t.Run("two/routing-client-cluster", func(t *testing.T) {
		retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
			requireGatewayBodyContains(r, clientOpts, clientTwoURL+"/a", "hello from service-a")
			requireGatewayBodyContains(r, clientOpts, clientTwoURL+"/ext_proc/b", "hello from service-b")
			requireConnectProxyFanOut(r, clientOpts, clientTwoURL+"/dd1")
		})
	})

	// ── Cross-cluster failover (server cluster) ───────────────────────────────
	// http-echo returns the same body in both clusters, so the proof of failover
	// is "local endpoint scaled to 0, yet the route still succeeds" — the response
	// can only have come from the client peer.
	logger.Log(t, "verifying cross-cluster failover via the server gateways")

	t.Run("failover/backend", func(t *testing.T) {
		withExtProcScaledDown(t, serverOpts, []string{"service-a"}, func() {
			retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
				requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/a", "hello from service-a")
			})
		})
	})

	t.Run("failover/base-processor", func(t *testing.T) {
		withExtProcScaledDown(t, serverOpts, []string{"ext-proc-http"}, func() {
			retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
				requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/b", "hello from service-b")
			})
		})
	})

	t.Run("failover/path-processor", func(t *testing.T) {
		withExtProcScaledDown(t, serverOpts, []string{"ext-proc-http-path"}, func() {
			retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
				requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/ext_proc/b", "hello from service-b")
			})
		})
	})

	t.Run("failover/processor-and-decider", func(t *testing.T) {
		withExtProcScaledDown(t, serverOpts, []string{"ext-proc-http", "route-decider"}, func() {
			retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
				requireGatewayBodyContains(r, serverOpts, serverTwoURL+"/c", "hello from service-c")
			})
		})
	})

	t.Run("failover/connect-proxy-chain", func(t *testing.T) {
		withExtProcScaledDown(t, serverOpts, []string{"service-e1"}, func() {
			retryCheckWithWait(t, 60, 5*time.Second, func(r *retry.R) {
				requireConnectProxyFanOut(r, serverOpts, serverTwoURL+"/dd1")
			})
		})
	})
}

// buildAndLoadExtProcImages builds all six ext-proc app images from the vendored
// Dockerfiles under extProcAppsPath and loads each one into every kind cluster
// whose name is derived from the given contexts. The kind cluster name is the
// context name with the "kind-" prefix stripped (kind always names contexts
// "kind-<clustername>").
//
// This must be called before deployExtProcStack so that the images are present
// in the cluster nodes' local image cache when the Deployments are created.
func buildAndLoadExtProcImages(t *testing.T, contexts ...environment.TestContext) {
	t.Helper()

	// Resolve the absolute path to the apps directory relative to this source
	// file, so the path is correct regardless of the working directory used
	// when invoking the test binary.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	appsDir := filepath.Join(filepath.Dir(thisFile), extProcAppsPath)

	// Derive the kind cluster name from each context's kubectl context name.
	// kind always names contexts "kind-<clustername>".
	var clusterNames []string
	for _, ctx := range contexts {
		contextName := ctx.KubectlOptions(t).ContextName
		clusterName := strings.TrimPrefix(contextName, "kind-")
		if clusterName == "" {
			t.Fatalf("cannot derive kind cluster name from context %q; expected 'kind-<name>'", contextName)
		}
		clusterNames = append(clusterNames, clusterName)
	}

	for _, img := range extProcImages {
		buildCtx := filepath.Join(appsDir, img.dir)
		logger.Logf(t, "building image %s from %s", img.tag, buildCtx)
		out, err := exec.Command("docker", "build", "-t", img.tag, buildCtx).CombinedOutput()
		require.NoErrorf(t, err, "docker build %s failed:\n%s", img.tag, out)

		for _, cluster := range clusterNames {
			logger.Logf(t, "kind load %s -> cluster %s", img.tag, cluster)
			out, err = exec.Command("kind", "load", "docker-image", img.tag, "--name", cluster).CombinedOutput()
			require.NoErrorf(t, err, "kind load %s into %s failed:\n%s", img.tag, cluster, out)
		}
	}
}

// extProcKubectlOptions returns kubectl options scoped to the "default"
// namespace for the given cluster context, where the ext-proc stack is deployed.
func extProcKubectlOptions(t *testing.T, ctx environment.TestContext) *terratestk8s.KubectlOptions {
	t.Helper()
	return &terratestk8s.KubectlOptions{
		ContextName: ctx.KubectlOptions(t).ContextName,
		ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
		Namespace:   "default",
	}
}

// deployExtProcStack applies the ext-proc common, single and two stacks plus a
// static-client into the namespace referenced by opts, registering teardown for
// each. Cleanups run last-in-first-out, so the overlays are deleted before the
// common base they depend on.
func deployExtProcStack(t *testing.T, opts *terratestk8s.KubectlOptions) {
	t.Helper()

	c := suite.Config()
	for _, dir := range []string{extProcCommonPath, extProcSinglePath, extProcTwoPath} {
		dir := dir
		out, err := k8s.RunKubectlAndGetOutputE(t, opts, "apply", "-k", dir)
		require.NoError(t, err, out)
		helpers.Cleanup(t, c.NoCleanupOnFailure, c.NoCleanup, func() {
			_, _ = k8s.RunKubectlAndGetOutputE(t, opts, "delete", "-k", dir, "--ignore-not-found=true")
		})
	}

	k8s.DeployKustomize(t, opts, c.NoCleanupOnFailure, c.NoCleanup, c.DebugDirectory, "../fixtures/bases/static-client")
}

// applyExtProcFailover exports every service to the peer, gives every in-mesh
// ext-proc component a cross-cluster ServiceResolver failover target, and allows
// the peered failover traffic with a wildcard intention. Each config entry is
// registered for teardown so the setup is fully reversible.
func applyExtProcFailover(t *testing.T, consulClient *api.Client, peer string) {
	t.Helper()

	cfg := suite.Config()

	_, _, err := consulClient.ConfigEntries().Set(&api.ExportedServicesConfigEntry{
		Name: "default",
		Services: []api.ExportedService{
			{
				Name:      "*",
				Consumers: []api.ServiceConsumer{{Peer: peer}},
			},
		},
	}, nil)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = consulClient.ConfigEntries().Delete(api.ExportedServices, "default", nil)
	})

	for _, svc := range extProcFailoverServices {
		svc := svc
		_, _, err := consulClient.ConfigEntries().Set(&api.ServiceResolverConfigEntry{
			Kind: api.ServiceResolver,
			Name: svc,
			Failover: map[string]api.ServiceResolverFailover{
				"*": {
					Targets: []api.ServiceResolverFailoverTarget{{Peer: peer}},
				},
			},
		}, nil)
		require.NoErrorf(t, err, "failed to set failover resolver for %s", svc)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			_, _ = consulClient.ConfigEntries().Delete(api.ServiceResolver, svc, nil)
		})
	}

	// Wildcard intention allowing any service imported from the peer to call any
	// local service (peered failover traffic carries the original caller's
	// identity from the peer datacenter).
	_, _, err = consulClient.ConfigEntries().Set(&api.ServiceIntentionsConfigEntry{
		Kind: api.ServiceIntentions,
		Name: "*",
		Sources: []*api.SourceIntention{
			{
				Name:   "*",
				Action: api.IntentionActionAllow,
				Peer:   peer,
			},
		},
	}, nil)
	require.NoError(t, err)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		_, _ = consulClient.ConfigEntries().Delete(api.ServiceIntentions, "*", nil)
	})
}

// extProcGatewayURL waits for the named gateway to be Accepted in the given
// cluster and returns a base URL (http://<addr>:8080) reachable from within that
// cluster. The gateway listens on port 80, mapped to container port 8080 by the
// GatewayClassConfig privileged-port offset.
func extProcGatewayURL(t *testing.T, ctx environment.TestContext, gatewayName string) string {
	t.Helper()

	k8sClient := ctx.ControllerRuntimeClient(t)
	var address string
	retryCheckWithWait(t, 120, 2*time.Second, func(r *retry.R) {
		var gateway gwv1beta1.Gateway
		err := k8sClient.Get(context.Background(), types.NamespacedName{Name: gatewayName, Namespace: "default"}, &gateway)
		require.NoError(r, err)
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
		checkStatusCondition(r, gateway.Status.Conditions, trueCondition("ConsulAccepted", "Accepted"))
		require.Len(r, gateway.Status.Addresses, 1)
		address = gateway.Status.Addresses[0].Value
	})
	return fmt.Sprintf("http://%s:8080", address)
}

// curlGatewayBody execs a curl against the gateway from the static-client pod and
// returns the response body.
func curlGatewayBody(r *retry.R, opts *terratestk8s.KubectlOptions, url string) string {
	output, err := k8s.RunKubectlAndGetOutputE(r, opts, "exec", "deploy/"+StaticClientName, "-c", StaticClientName, "--",
		"curl", "-s", "--connect-timeout", "5", "--max-time", "15", url)
	require.NoError(r, err, output)
	return output
}

// requireGatewayBodyContains asserts the gateway response body for url contains want.
func requireGatewayBodyContains(r *retry.R, opts *terratestk8s.KubectlOptions, url, want string) {
	body := curlGatewayBody(r, opts, url)
	require.Containsf(r, body, want, "expected %q in gateway response for %s, got: %s", want, url, body)
}

// requireConnectProxyFanOut asserts that the connect-proxy ext-proc path fans out
// to one of service-f/service-g.
func requireConnectProxyFanOut(r *retry.R, opts *terratestk8s.KubectlOptions, url string) {
	body := curlGatewayBody(r, opts, url)
	fannedOut := strings.Contains(body, "hello from service-f") || strings.Contains(body, "hello from service-g")
	require.Truef(r, fannedOut, "expected /dd1 to fan out to service-f or service-g, got: %s", body)
}

// processorLogs returns the full logs of the ext-proc processor identified by its
// app label (also its container name).
func processorLogs(t testutil.TestingTB, opts *terratestk8s.KubectlOptions, app string) string {
	out, err := k8s.RunKubectlAndGetOutputE(t, opts, "logs", "-l", "app="+app, "-c", app, "--tail=-1")
	require.NoError(t, err, out)
	return out
}

// requireProcessorLogContains asserts (with retry, since log propagation can lag)
// that the processor's logs contain want, proving it observed that request.
func requireProcessorLogContains(t *testing.T, opts *terratestk8s.KubectlOptions, app, want string) {
	t.Helper()
	retryCheckWithWait(t, 30, 2*time.Second, func(r *retry.R) {
		logs := processorLogs(r, opts, app)
		require.Containsf(r, logs, want, "expected %s logs to contain %q", app, want)
	})
}

// requireProcessorLogAbsent asserts the processor's logs do NOT contain notWant,
// proving a bypass route never reached it.
func requireProcessorLogAbsent(t *testing.T, opts *terratestk8s.KubectlOptions, app, notWant string) {
	t.Helper()
	logs := processorLogs(t, opts, app)
	require.NotContainsf(t, logs, notWant, "expected %s logs to NOT contain %q (route should have bypassed the processor)", app, notWant)
}

// requireGatewayExtProcFilters port-forwards to the named gateway's Envoy admin
// interface (:19000) and asserts the ext_proc filter names in its config_dump:
//   - mode "two":    both envoy.filters.http.ext_proc/base and .../path present.
//   - mode "single": the un-suffixed envoy.filters.http.ext_proc present and NO
//     suffixed variant (statPrefix "" must yield only the un-suffixed filter).
//
// The gateway runs the distroless consul-dataplane image (no shell), so the admin
// port is reached via a short-lived port-forward rather than an exec.
func requireGatewayExtProcFilters(t *testing.T, opts *terratestk8s.KubectlOptions, gatewayName, mode string) {
	t.Helper()

	podName, err := k8s.RunKubectlAndGetOutputE(t, opts, "get", "pods",
		"-l", "gateway.consul.hashicorp.com/name="+gatewayName,
		"-o", "jsonpath={.items[0].metadata.name}")
	require.NoError(t, err, podName)
	require.NotEmptyf(t, podName, "no pod found for gateway %s", gatewayName)

	addr := portforward.CreateTunnelToResourcePort(t, podName, 19000, opts, terratestLogger.Discard)

	var dump string
	retryCheckWithWait(t, 30, 2*time.Second, func(r *retry.R) {
		resp, err := http.Get(fmt.Sprintf("http://%s/config_dump", addr))
		require.NoError(r, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(r, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(r, err)
		dump = string(body)
		require.NotEmpty(r, dump)
	})

	if mode == "two" {
		require.Containsf(t, dump, "envoy.filters.http.ext_proc/base", "%s Envoy is missing the /base ext_proc filter", gatewayName)
		require.Containsf(t, dump, "envoy.filters.http.ext_proc/path", "%s Envoy is missing the /path ext_proc filter", gatewayName)
		return
	}
	require.Containsf(t, dump, `"envoy.filters.http.ext_proc"`, "%s Envoy is missing the un-suffixed ext_proc filter (statPrefix empty)", gatewayName)
	require.NotContainsf(t, dump, "envoy.filters.http.ext_proc/", "%s Envoy unexpectedly has a suffixed ext_proc filter; statPrefix empty must yield only the un-suffixed filter", gatewayName)
}

// withExtProcScaledDown scales the named deployments to 0 in the given cluster,
// runs fn (which asserts the route still succeeds via the peer), then restores
// each deployment to 1 replica.
func withExtProcScaledDown(t *testing.T, opts *terratestk8s.KubectlOptions, deployments []string, fn func()) {
	t.Helper()

	for _, d := range deployments {
		logger.Logf(t, "scaling deploy/%s to 0", d)
		k8s.RunKubectl(t, opts, "scale", "deploy/"+d, "--replicas=0")
	}
	defer func() {
		for _, d := range deployments {
			logger.Logf(t, "restoring deploy/%s to 1", d)
			k8s.RunKubectl(t, opts, "scale", "deploy/"+d, "--replicas=1")
			k8s.RunKubectl(t, opts, "wait", "--for=condition=available", "--timeout=5m", "deploy/"+d)
		}
	}()

	fn()
}
