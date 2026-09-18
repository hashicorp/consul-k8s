// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package camp

// TestMCPGateway_Basic exercises the CAMP Consul AI / MCP mesh end-to-end on a
// single kind cluster:
//
//   - ai-app (Go AI agent + Python HITL approver) routes MCP tool calls through
//     the Consul service mesh to two MCP backends: ameduss (flight/hotel) and
//     weatherly (weather).
//   - L7 ServiceIntentions enforce which tools ai-app may call: hotel.search and
//     weather.current are allowed; hotel.book is denied by the catch-all deny rule.
//   - The consul-mcp-gateway interceptor (injected by connect-inject) aggregates
//     tools from both backends and stamps x-mcp-* headers for intention matching.
//
// The test mirrors the structure of TestAPIGateway_ExtProc_MultiClusterFailover
// and replicates every check from 105-camp-end-to-end-kubernetes/verify.sh.
//
// Prerequisites:
//   - kind (single cluster)
//   - Consul Enterprise license (ai service registration is ENT-only)
//   - -enable-enterprise -enterprise-license <val> -use-kind flags

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
)

// Fixture paths (relative to this source file).
const (
	campAppsPath   = "../fixtures/cases/camp/apps"
	campConfigPath = "../fixtures/cases/camp/config"
)

// campImages is the ordered list of (sub-directory, image tag) pairs that
// buildAndLoadCAMPImages must build and kind-load.
var campImages = []struct{ dir, tag string }{
	{"camp-apps", "camp-apps:latest"},
	{"ameduss", "camp-ameduss:latest"},
	{"weatherly", "camp-weatherly:latest"},
}

// campDeployments are the workloads that must be Available before assertions run.
var campDeployments = []string{"ameduss", "weatherly", "ai-app"}

// campConfigFiles are the Kubernetes manifests applied in dependency order.
// ServiceDefaults must be applied before ServiceIntentions so Consul accepts
// the L7 permissions block (requires protocol=http).
var campConfigFiles = []string{
	"proxy-defaults.yaml",
	"ameduss-defaults.yaml",
	"weatherly-defaults.yaml",
	"ameduss-intentions.yaml",
	"weatherly-intentions.yaml",
	"agent-planner-mcp-configmap.yaml",
	"ameduss.yaml",
	"weatherly.yaml",
	"ai-app.yaml",
}

func TestMCPGateway_Basic(t *testing.T) {
	// ── Guard clauses ─────────────────────────────────────────────────────────
	skipUnlessEnterpriseWithLicense(t)

	cfg := suite.Config()
	if !cfg.UseKind {
		t.Skip("skipping: -use-kind is not set; this test builds images locally and requires kind load")
	}

	env := suite.Environment()
	ctx := env.DefaultContext(t)

	// ── Helm install ──────────────────────────────────────────────────────────
	// The framework's NewHelmCluster automatically:
	//   1. Creates the enterprise license secret (name=license, key=key) from cfg.EnterpriseLicense.
	//   2. Sets global.enterpriseLicense.secretName/secretKey in Helm values.
	//   3. Sets global.image to the enterprise image derived from values.yaml.
	// We only override the CAMP-specific custom images on top of those defaults.
	releaseName := helpers.RandomName()
	helmValues := map[string]string{
		"connectInject.enabled":                "true",
		"global.imageConsulDataplane":          "docker.mirror.hashicorp.services/hashicorppreview/consul-dataplane:2.1.0-dev",
		"global.imageConsulAIMCPInterceptor":   "public.ecr.aws/n9h4m6z2/ajay/consul-mcp-gateway:ai-agent-13",
		"global.imageK8S":                      "public.ecr.aws/n9h4m6z2/ajay/consul-k8s-control-plane-dev:ai-agent-test-11",
	}
	logger.Log(t, "installing Consul (enterprise) via Helm")
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
	consulCluster.Create(t)

	// ── Wait for connect-injector rollout ─────────────────────────────────────
	// The Gateway controller leader-election happens after pod readiness; apply
	// nothing until the injector is fully rolled out on the cluster.
	injectorDeploy := releaseName + "-consul-connect-injector"
	logger.Logf(t, "waiting for connect-injector rollout: deploy/%s", injectorDeploy)
	retry.RunWith(&retry.Timer{Timeout: 5 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t),
			"rollout", "status", "deploy/"+injectorDeploy, "--timeout=10s")
		if err != nil {
			r.Errorf("connect-injector rollout not ready: %v\n%s", err, out)
		}
	})
	logger.Log(t, "connect-injector rolled out")

	// Wait for the injector webhook endpoint to have a ready address so sidecar
	// injection is available for all subsequent pod creates.
	logger.Logf(t, "waiting for connect-injector webhook endpoint: %s", injectorDeploy)
	retry.RunWith(&retry.Timer{Timeout: 3 * time.Minute, Wait: 5 * time.Second}, t, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, ctx.KubectlOptions(t),
			"get", "endpoints", injectorDeploy,
			"-o", "jsonpath={.subsets[0].addresses[0].ip}")
		if err != nil || strings.TrimSpace(out) == "" {
			r.Errorf("injector endpoint not ready: ip=%q err=%v", out, err)
		}
	})
	logger.Log(t, "connect-injector webhook endpoint ready")

	opts := campKubectlOptions(t, ctx)

	// ── Build and load CAMP images into the kind cluster ──────────────────────
	logger.Log(t, "building and loading CAMP app images into the kind cluster")
	buildAndLoadCAMPImages(t, ctx)
	logger.Log(t, "CAMP images loaded")

	// ── Apply Consul CRDs and app manifests ───────────────────────────────────
	logger.Log(t, "applying CAMP config manifests")
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	configDir := filepath.Join(filepath.Dir(thisFile), campConfigPath)

	for _, fname := range campConfigFiles {
		fpath := filepath.Join(configDir, fname)
		logger.Logf(t, "kubectl apply -f %s", fname)
		k8s.KubectlApply(t, opts, fpath)
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
			k8s.KubectlDelete(t, opts, fpath)
			logger.Logf(t, "cleaned up %s", fname)
		})
	}
	logger.Log(t, "all manifests applied")

	// ── Wait for CRD sync ─────────────────────────────────────────────────────
	logger.Log(t, "waiting for Consul CRDs to sync")
	for _, cr := range []struct{ kind, name string }{
		{"proxydefaults", "global"},
		{"servicedefaults", "ameduss"},
		{"servicedefaults", "weatherly"},
		{"serviceintentions", "ameduss"},
		{"serviceintentions", "weatherly"},
	} {
		waitForCRDSynced(t, opts, cr.kind, cr.name)
	}
	logger.Log(t, "all CRDs synced")

	// ── Wait for all Deployments to be Available ──────────────────────────────
	logger.Log(t, "waiting for CAMP deployments to be available")
	for _, deploy := range campDeployments {
		logger.Logf(t, "  waiting for deploy/%s", deploy)
		k8s.RunKubectl(t, opts, "wait", "--for=condition=available", "--timeout=5m", "deploy/"+deploy)
	}
	logger.Log(t, "all CAMP deployments available")

	// ── Wait for Envoy sidecars ───────────────────────────────────────────────
	logger.Log(t, "waiting for Envoy sidecars to be ready on all app pods")
	for _, app := range campDeployments {
		waitForEnvoySidecarReady(t, opts, app)
	}
	logger.Log(t, "all Envoy sidecars ready")

	// ────────────────────────────────────────────────────────────────────────────
	// Subtests — mirror every section of verify.sh
	// ────────────────────────────────────────────────────────────────────────────

	// § 1 — Pod readiness
	t.Run("pods/ready", func(t *testing.T) {
		for _, app := range campDeployments {
			app := app
			retryCheckWithWait(t, 30, 5*time.Second, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, opts,
					"get", "pods", "-l", "app="+app,
					"-o", "jsonpath={.items[0].status.containerStatuses[*].ready}")
				require.NoErrorf(r, err, "kubectl get pods app=%s: %v", app, err)
				require.Containsf(r, out, "true", "pod %s not ready (containerStatuses.ready=%q)", app, out)
			})
			logger.Logf(t, "pod %s: ready", app)
		}
	})

	// § 3 — ai-app health endpoint
	t.Run("ai-app/healthz", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "ai-app", 8080)
		retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
			resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
			require.NoError(r, err)
			defer resp.Body.Close()
			require.Equal(r, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			require.Containsf(r, string(body), "ok", "GET /healthz body=%q", string(body))
		})
		logger.Log(t, "ai-app /healthz: OK")

		retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
			resp, err := http.Get(fmt.Sprintf("http://%s/ai-mcp", addr))
			require.NoError(r, err)
			defer resp.Body.Close()
			require.Equal(r, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			require.Containsf(r, string(body), "ai-app OK", "GET /ai-mcp body=%q", string(body))
		})
		logger.Log(t, "ai-app /ai-mcp: OK")
	})

	// § 4 — HITL health (exec into ai-app pod, curl the hitl container)
	t.Run("ai-app/hitl-health", func(t *testing.T) {
		retryCheckWithWait(t, 20, 3*time.Second, func(r *retry.R) {
			out, err := k8s.RunKubectlAndGetOutputE(r, opts,
				"exec", "deploy/ai-app", "-c", "hitl", "--",
				"curl", "-fsS", "http://127.0.0.1:16101/healthz")
			require.NoErrorf(r, err, "hitl healthz exec failed: %v\n%s", err, out)
			require.Containsf(r, out, "ok", "HITL /healthz body=%q", out)
		})
		logger.Log(t, "HITL /healthz: ok")
	})

	// § 5 — MCP server: ameduss
	t.Run("mcp/ameduss", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "ameduss", 8080)
		base := fmt.Sprintf("http://%s/mcp", addr)

		retryCheckWithWait(t, 20, 3*time.Second, func(r *retry.R) {
			sessID, name := mcpInitialize(r, base)
			require.Equalf(r, "ameduss", name, "ameduss: initialize serverInfo.name=%q", name)

			tools := mcpToolsList(r, base, sessID)
			for _, want := range []string{"hotel.search", "hotel.book", "flight.search", "flight.book"} {
				require.Containsf(r, tools, want, "ameduss: tools/list missing %q (got %v)", want, tools)
			}

			ok := mcpToolsCall(r, base, sessID, "hotel.search", map[string]any{
				"location": "San Francisco", "check_in": "2026-08-01", "check_out": "2026-08-04", "guests": 2,
			})
			require.Truef(r, ok, "ameduss: tools/call hotel.search returned an error")
		})
		logger.Log(t, "ameduss MCP: initialize + tools/list + tools/call hotel.search OK")
	})

	// § 5 — MCP server: weatherly
	t.Run("mcp/weatherly", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "weatherly", 8080)
		base := fmt.Sprintf("http://%s/mcp", addr)

		retryCheckWithWait(t, 20, 3*time.Second, func(r *retry.R) {
			sessID, name := mcpInitialize(r, base)
			require.Equalf(r, "weatherly", name, "weatherly: initialize serverInfo.name=%q", name)

			tools := mcpToolsList(r, base, sessID)
			for _, want := range []string{"weather.current", "weather.forecast", "weather.alerts"} {
				require.Containsf(r, tools, want, "weatherly: tools/list missing %q (got %v)", want, tools)
			}

			ok := mcpToolsCall(r, base, sessID, "weather.current", map[string]any{"location": "San Francisco"})
			require.Truef(r, ok, "weatherly: tools/call weather.current returned an error")
		})
		logger.Log(t, "weatherly MCP: initialize + tools/list + tools/call weather.current OK")
	})

	// § 6 — MCP gateway aggregated tools/list via ai-app
	t.Run("mcp/gateway-aggregate", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "ai-app", 8080)
		wantTools := []string{
			"ameduss__hotel.search", "ameduss__hotel.book",
			"ameduss__flight.search", "ameduss__flight.book",
			"weatherly__weather.current", "weatherly__weather.forecast", "weatherly__weather.alerts",
		}
		retryCheckWithWait(t, 30, 3*time.Second, func(r *retry.R) {
			resp, err := http.Get(fmt.Sprintf("http://%s/ai-mcp/tools/list", addr))
			require.NoError(r, err)
			defer resp.Body.Close()
			require.Equal(r, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			bodyStr := string(body)
			for _, want := range wantTools {
				require.Containsf(r, bodyStr, want,
					"gateway tools/list missing %q; body=%s", want, bodyStr)
			}
		})
		logger.Log(t, "gateway aggregated tools/list: all 7 tools present")
	})

	// § 7 — L7 intentions: allowed tool (hotel.search) passes
	t.Run("intentions/hotel-search-allowed", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "ai-app", 8080)
		// Retry up to 12× to absorb random HITL rejections (~25% fail rate by default).
		var lastStatus int
		retryCheckWithWait(t, 12, 3*time.Second, func(r *retry.R) {
			resp, err := http.Get(fmt.Sprintf("http://%s/ai-mcp/ameduss__hotel.search", addr))
			require.NoError(r, err)
			defer resp.Body.Close()
			lastStatus = resp.StatusCode
			if resp.StatusCode == http.StatusForbidden {
				// Random HITL rejection — retry.
				r.Errorf("hotel.search got 403 (random HITL rejection) — retrying")
				return
			}
			require.Equalf(r, http.StatusOK, resp.StatusCode,
				"hotel.search (whitelisted) expected HTTP 200 from L7 intention, got %d", resp.StatusCode)
		})
		logger.Logf(t, "hotel.search allowed by L7 intention (last status %d)", lastStatus)
	})

	// § 7 — L7 intentions: denied tool (hotel.book) is blocked
	t.Run("intentions/hotel-book-denied", func(t *testing.T) {
		addr := portForwardSvc(t, opts, "ai-app", 8080)
		resp, err := http.Get(fmt.Sprintf("http://%s/ai-mcp/ameduss__hotel.book", addr))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.NotEqualf(t, http.StatusOK, resp.StatusCode,
			"hotel.book (non-whitelisted) should have been denied by L7 intention, got HTTP 200")
		logger.Logf(t, "hotel.book denied by L7 intention (status %d)", resp.StatusCode)
	})

	// § 8 — Consul CRD sync status
	t.Run("crds/synced", func(t *testing.T) {
		for _, cr := range []struct{ kind, name string }{
			{"proxydefaults", "global"},
			{"servicedefaults", "ameduss"},
			{"servicedefaults", "weatherly"},
			{"serviceintentions", "ameduss"},
			{"serviceintentions", "weatherly"},
		} {
			cr := cr
			retryCheckWithWait(t, 20, 5*time.Second, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, opts,
					"get", cr.kind+"/"+cr.name,
					"-o", `jsonpath={.status.conditions[?(@.type=="Synced")].status}`)
				require.NoErrorf(r, err, "%s/%s: kubectl get failed: %v", cr.kind, cr.name, err)
				require.Equalf(r, "True", strings.TrimSpace(out),
					"%s/%s synced=%q (expected True)", cr.kind, cr.name, out)
			})
			logger.Logf(t, "%s/%s: Synced=True", cr.kind, cr.name)
		}
	})

	// § 9 — Envoy sidecar readiness (already done pre-subtest, re-assert here)
	t.Run("envoy/sidecar-ready", func(t *testing.T) {
		for _, app := range campDeployments {
			app := app
			retryCheckWithWait(t, 20, 3*time.Second, func(r *retry.R) {
				out, err := k8s.RunKubectlAndGetOutputE(r, opts,
					"exec", "deploy/"+app, "-c", "envoy-sidecar", "--",
					"curl", "-fsS", "http://127.0.0.1:19000/ready")
				require.NoErrorf(r, err, "%s envoy admin /ready: %v", app, err)
				require.Containsf(r, strings.ToUpper(out), "LIVE",
					"%s envoy /ready did not return LIVE: %q", app, out)
			})
			logger.Logf(t, "%s envoy sidecar /ready: LIVE", app)
		}
	})
}

// ─── Helper functions ─────────────────────────────────────────────────────────

// skipUnlessEnterpriseWithLicense skips the test unless both -enable-enterprise
// and a valid enterprise license are configured. The AI service registration
// (consul.hashicorp.com/ai-role annotation) is a Consul Enterprise-only feature.
// Mirrors skipUnlessEnterpriseLicenseConfigured in api_gateway_scaling_test.go.
func skipUnlessEnterpriseWithLicense(t *testing.T) {
	t.Helper()
	cfg := suite.Config()
	if !cfg.EnableEnterprise {
		t.Skipf("skipping: -enable-enterprise not set (ai-role / MCP gateway is Consul Enterprise only)")
	}
	if cfg.EnterpriseLicense == "" {
		t.Skipf("skipping: no enterprise license configured (pass -enterprise-license or set CONSUL_ENT_LICENSE)")
	}
}

// campKubectlOptions returns kubectl options scoped to the "default" namespace
// for the given cluster context, where the CAMP stack is deployed.
func campKubectlOptions(t *testing.T, ctx environment.TestContext) *terratestk8s.KubectlOptions {
	t.Helper()
	base := ctx.KubectlOptions(t)
	return &terratestk8s.KubectlOptions{
		ContextName: base.ContextName,
		ConfigPath:  base.ConfigPath,
		Namespace:   "default",
	}
}

// buildAndLoadCAMPImages builds the three CAMP Docker images from the vendored
// Dockerfiles under campAppsPath and loads each one into the kind cluster.
// Mirrors buildAndLoadExtProcImages in api_gateway_ext_proc_failover_test.go.
func buildAndLoadCAMPImages(t *testing.T, ctx environment.TestContext) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	appsDir := filepath.Join(filepath.Dir(thisFile), campAppsPath)

	if _, statErr := os.Stat(appsDir); statErr != nil {
		t.Fatalf("CAMP apps directory not found at %q: %v", appsDir, statErr)
	}

	contextName := ctx.KubectlOptions(t).ContextName
	clusterName := strings.TrimPrefix(contextName, "kind-")
	if clusterName == "" {
		t.Fatalf("cannot derive kind cluster name from context %q; expected 'kind-<name>'", contextName)
	}
	logger.Logf(t, "will load images into kind cluster %q (context %q)", clusterName, contextName)

	for i, img := range campImages {
		buildCtx := filepath.Join(appsDir, img.dir)
		if _, statErr := os.Stat(filepath.Join(buildCtx, "Dockerfile")); statErr != nil {
			t.Fatalf("[%d/%d] Dockerfile not found for %s at %q: %v",
				i+1, len(campImages), img.tag, buildCtx, statErr)
		}

		logger.Logf(t, "[%d/%d] docker build -t %s %s", i+1, len(campImages), img.tag, buildCtx)
		buildCmd := exec.Command("docker", "build", "-t", img.tag, buildCtx)
		var buildOut bytes.Buffer
		buildCmd.Stdout = &buildOut
		buildCmd.Stderr = &buildOut
		buildErr := buildCmd.Run()
		logger.Logf(t, "docker build output for %s:\n%s", img.tag, buildOut.String())
		if buildErr != nil {
			t.Fatalf("[%d/%d] docker build %s failed (%v):\n%s",
				i+1, len(campImages), img.tag, buildErr, buildOut.String())
		}

		logger.Logf(t, "[%d/%d] kind load docker-image %s --name %s",
			i+1, len(campImages), img.tag, clusterName)
		loadCmd := exec.Command("kind", "load", "docker-image", img.tag, "--name", clusterName)
		var loadOut bytes.Buffer
		loadCmd.Stdout = &loadOut
		loadCmd.Stderr = &loadOut
		loadErr := loadCmd.Run()
		logger.Logf(t, "kind load output for %s -> %s:\n%s", img.tag, clusterName, loadOut.String())
		if loadErr != nil {
			t.Fatalf("[%d/%d] kind load %s into cluster %s failed (%v):\n%s",
				i+1, len(campImages), img.tag, clusterName, loadErr, loadOut.String())
		}
	}
	logger.Logf(t, "all %d CAMP images built and loaded into %s", len(campImages), clusterName)
}

// waitForEnvoySidecarReady polls the Envoy admin /ready endpoint inside the
// named app's pod until it returns LIVE.
func waitForEnvoySidecarReady(t *testing.T, opts *terratestk8s.KubectlOptions, app string) {
	t.Helper()
	retryCheckWithWait(t, 30, 5*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, opts,
			"exec", "deploy/"+app, "-c", "envoy-sidecar", "--",
			"curl", "-fsS", "http://127.0.0.1:19000/ready")
		if err != nil || !strings.Contains(strings.ToUpper(out), "LIVE") {
			r.Errorf("%s envoy sidecar not LIVE yet (out=%q err=%v)", app, out, err)
		}
	})
	logger.Logf(t, "%s envoy sidecar ready", app)
}

// waitForCRDSynced polls until the named CR has Synced=True.
func waitForCRDSynced(t *testing.T, opts *terratestk8s.KubectlOptions, kind, name string) {
	t.Helper()
	retryCheckWithWait(t, 30, 5*time.Second, func(r *retry.R) {
		out, err := k8s.RunKubectlAndGetOutputE(r, opts,
			"get", kind+"/"+name,
			"-o", `jsonpath={.status.conditions[?(@.type=="Synced")].status}`)
		if err != nil || strings.TrimSpace(out) != "True" {
			r.Errorf("%s/%s not synced yet (status=%q err=%v)", kind, name, out, err)
		}
	})
	logger.Logf(t, "%s/%s: Synced=True", kind, name)
}

// portForwardSvc creates a port-forward tunnel to the named ClusterIP service
// on remotePort and returns the local "host:port" string.
func portForwardSvc(t *testing.T, opts *terratestk8s.KubectlOptions, svc string, remotePort int) string {
	t.Helper()
	// Resolve the pod name for the service label selector.
	podName, err := k8s.RunKubectlAndGetOutputE(t, opts,
		"get", "pods", "-l", "app="+svc,
		"-o", "jsonpath={.items[0].metadata.name}")
	require.NoErrorf(t, err, "get pod for svc %s: %v", svc, err)
	require.NotEmptyf(t, podName, "no pod found for app=%s", svc)
	return portforward.CreateTunnelToResourcePort(t, podName, remotePort, opts, terratestLogger.Discard)
}

// mcpInitialize sends an MCP initialize request and returns the session ID and
// serverInfo.name. Mirrors the initialize step in verify.sh §5.
func mcpInitialize(t require.TestingT, baseURL string) (sessID, serverName string) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "camp-test", "version": "1.0"},
		},
	})
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	sessID = resp.Header.Get("Mcp-Session-Id")

	raw, _ := io.ReadAll(resp.Body)
	body := unwrapMCPSSE(string(raw))

	var result struct {
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &result)
	return sessID, result.Result.ServerInfo.Name
}

// mcpToolsList sends an MCP tools/list request and returns the list of tool names.
func mcpToolsList(t require.TestingT, baseURL, sessID string) []string {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessID != "" {
		req.Header.Set("Mcp-Session-Id", sessID)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	raw, _ := io.ReadAll(resp.Body)
	body := unwrapMCPSSE(string(raw))

	var result struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &result)
	names := make([]string, 0, len(result.Result.Tools))
	for _, tool := range result.Result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// mcpToolsCall sends an MCP tools/call request and returns true if the result
// is a non-error tool result.
func mcpToolsCall(t require.TestingT, baseURL, sessID, toolName string, args map[string]any) bool {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params":  map[string]any{"name": toolName, "arguments": args},
	})
	req, err := http.NewRequest(http.MethodPost, baseURL, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessID != "" {
		req.Header.Set("Mcp-Session-Id", sessID)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}

	raw, _ := io.ReadAll(resp.Body)
	body := unwrapMCPSSE(string(raw))

	var result struct {
		Error    *struct{} `json:"error"`
		Result   *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(body), &result)
	if result.Error != nil {
		return false
	}
	if result.Result != nil && result.Result.IsError {
		return false
	}
	return result.Result != nil
}

// unwrapMCPSSE strips Streamable-HTTP SSE framing ("data: {json}") when present.
func unwrapMCPSSE(s string) string {
	if strings.Contains(s, "data:") {
		var b strings.Builder
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "data:") {
				b.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return strings.TrimSpace(s)
}

// retryCheckWithWait runs fn up to count times, waiting wait between attempts.
// Mirrors the same helper in api_gateway_tenancy_test.go (package apigateway).
func retryCheckWithWait(t *testing.T, count int, wait time.Duration, fn func(r *retry.R)) {
	t.Helper()
	counter := &retry.Counter{Count: count, Wait: wait}
	retry.RunWith(counter, t, fn)
}
