# Consul-K8s Acceptance CI: Reliability & Performance Overhaul

**Authors**: Platform Engineering  
**Status**: Phase 1 complete, Phase 2 complete, Phase 3 complete, Phase 4 identified  
**Audience**: Engineering team — serves as both retrospective and living reference

---

## Executive Summary

Acceptance CI for `consul-k8s` suffered from two compounding problems: **unreliable infrastructure** that caused non-deterministic failures unrelated to any code change, and **wall-time ceilings** driven by monolithic test functions that couldn't be parallelized by the matrix sharding system.

We addressed both across two phases of work, landing **8 targeted changes** across the test framework, the workflow files, and the test code itself.

| Metric | Before | After Phase 1 | After Phase 2 | After Phase 3 |
|---|---|---|---|---|
| Wall time (observed) | ~2 hours | ~75 min | ~37 min | **~60–65 min*** |
| Execution ceiling (longest test) | ~55 min | ~55 min | ~37 min | **~33 min (vault CNI)** |
| Partitions runner time | ~55 min | ~55 min | ~24 min | ~24 min |
| Flaky failure rate | High (3–5 per run) | Low | Low | **Near zero** |
| Runner-minutes wasted on unnecessary clusters | ~48 min/run | 0 (wan-fed) | 0 (partitions + wan-fed) | **0 (+ peering + api-gateway)** |
| Calico setup wait time per CNI runner | 8 min (hardcoded) | — | ~2 min (condition-based) | ~2 min |
| TestVault_Partitions flake rate | — | — | High | **Fixed** |

\* Wall-clock time is now dominated by runner **queue wait** (up to 27 min) rather than test execution time. With an uncontested runner pool (no concurrent CI runs from other PRs), wall time lands at ~60 min. The execution ceiling itself is ~33 min.

The remaining lever is runner pool capacity — 620 jobs dispatched simultaneously saturate the private runner fleet. See Phase 4 for options.

---

## Context: The Sharding System

Everything in this document builds on one piece of infrastructure: `generate_test_matrix.py`.

The matrix generator:
1. Reads `kind_acceptance_test_packages.yaml` to get the list of packages and any per-package metadata
2. Scans each package's `*.go` files for top-level `Test*` functions using a regex
3. Looks up each function's historical duration in `test-timings.json`
4. Greedy bin-packs functions into shards targeting `TARGET_SHARD_SECONDS = 1200s` (20 min)
5. Functions with no recorded history receive `DEFAULT_TEST_SECONDS = 600s` (10 min)
6. Emits a GitHub Actions matrix where each shard runs as an independent parallel job

**The fundamental constraint**: the generator can only shard at the `Test*` function boundary. A `TestFoo` function with 6 sequential sub-cases (`t.Run(...)`) is an atomic unit to the sharding system — all 6 cases land on a single runner, and their combined time becomes the ceiling.

This is why test splitting is the highest-leverage optimization: it changes the granularity available to the bin-packer.

---

## Phase 1: Reliability Fixes

These changes address failure modes that have nothing to do with the code under test. Each one was responsible for at least one category of false-positive CI failure.

---

### Fix 1 — Port-forward reconnect probe

**File**: `acceptance/framework/portforward/port_forward.go`

#### The Problem

Tests that use `secure: true` establish a `kubectl port-forward` tunnel to the Consul gRPC-TLS port (8501) and use `monitorPortForwardedServer` to reconnect the tunnel if it drops. The reconnect logic called `ForwardPortE` and, on success, immediately returned.

`ForwardPortE` returning `nil` means the `kubectl port-forward` **process** launched. It does not mean the remote Consul pod is accepting connections. There is a window — typically 1–5 seconds — between process launch and the pod binding to the forwarded port. During this window, every `retry.RunWith` call that attempts a dial gets `ECONNREFUSED`.

When this window is longer than the remaining retry budget (which it can be during pod churn), tests fail with `connection refused` that appear to be test logic failures but are purely a race in the reconnect path.

#### The Fix

Added a TCP readiness probe loop after a successful `ForwardPortE` call, before returning to the caller:

```go
const (
    reconnectProbeTimeout  = 30 * time.Second
    reconnectProbeInterval = 1 * time.Second
)

deadline := time.Now().Add(reconnectProbeTimeout)
for time.Now().Before(deadline) {
    select {
    case <-doneChan:
        tunnel.Close()
        return
    default:
    }
    if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
        conn.Close()
        break
    }
    time.Sleep(reconnectProbeInterval)
}
```

The `doneChan` check inside the loop is load-bearing: without it, a test that calls `Close()` on the tunnel during the 30-second probe window would block cleanup.

#### Impact

Eliminates the entire "connection refused during reconnect" failure class. This was responsible for the majority of the previously unexplained `secure: true` test failures.

#### Trade-offs

| | |
|---|---|
| **Pro** | Zero false positives — if a port-forward is genuinely broken after 30s, something is seriously wrong and the retry is correct behavior |
| **Pro** | `doneChan` integration keeps cleanup non-blocking |
| **Con** | Adds up to 30s of latency to tunnel reconnect attempts; in practice, Consul binds within 1–3s so average latency added is negligible |
| **Con** | Doesn't address cases where the remote pod crashes repeatedly in a tight loop — but those should fail |

---

### Fix 2 — `kind` cleanup PATH fix

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### The Problem

`helm/kind-action` with `install_only: true` installs `kind` and appends its path to `$GITHUB_PATH`. GitHub Actions processes `$GITHUB_PATH` additions between steps — they are not available in the same step that wrote them, and critically, they are not available in the action's own post-cleanup phase.

The `helm/kind-action` post-cleanup script runs `kind delete cluster --name ...` in its own process. Since `$GITHUB_PATH` is not applied there, the command fails with `kind: command not found`. With the default `ignore_failed_clean: false`, this hard-fails the entire post-step, and GitHub Actions marks subsequent cluster-creation steps as uncreatable. The entire runner is lost.

#### The Fix

```yaml
- uses: helm/kind-action@v1.10.0
  with:
    install_only: true
    ignore_failed_clean: true   # added
```

`ignore_failed_clean: true` suppresses the cleanup error. Kind clusters are short-lived anyway — they are destroyed when the runner terminates.

#### Impact

Eliminated an entire class of runner-loss failures that manifested as all cluster-creation steps being skipped on the same run.

#### Trade-offs

| | |
|---|---|
| **Pro** | One-line fix, zero risk |
| **Con** | Cluster cleanup on runner failure is now best-effort; this is acceptable since runners are ephemeral |

---

### Fix 3 — Docker registry retry with fallback

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### The Problem

Two independent registry failure points:

- **Host-side**: `docker pull` hits `docker-mirror.hashi.app`, which returns HTTP 502 under load spikes from many runners starting simultaneously.
- **Node-side**: `crictl pull` inside kind nodes hits `registry-1.docker.io` directly for images absent from the HashiCorp mirror, also returning 502.

Both are transient — the images exist. The failures were pure infrastructure instability under concurrent load.

#### The Fix

Retry wrapper (5 attempts, 10 s back-off) for all `docker pull` commands. For `crictl pull`, added a fallback from the HashiCorp mirror to `docker.io` if all retries fail.

```bash
retry() {
  local n=1
  local max=5
  local delay=10
  while true; do
    "$@" && break || {
      if [[ $n -lt $max ]]; then
        ((n++))
        echo "Command failed. Attempt $n/$max in ${delay}s..."
        sleep $delay
      else
        return 1
      fi
    }
  done
}
```

#### Impact

Eliminated the 502 failure class from both host-side and node-side pulls. These were responsible for approximately 2–3 failures per week.

#### Trade-offs

| | |
|---|---|
| **Pro** | Handles transient registry instability without code changes to tests |
| **Pro** | Fallback to `docker.io` is transparent to the rest of the workflow |
| **Con** | Adds up to 50s delay when the first attempt fails (4 retries × 10s); this is acceptable because the alternative is a failed run |
| **Con** | Doesn't fix the root cause (mirror instability); this is an infrastructure concern outside our scope |

---

### Fix 4 — Explicit job timeout

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### The Problem

When a runner was OOM-killed or reclaimed (AWS spot instance), GitHub Actions produced blank step entries in the UI and `BlobNotFound` errors in log storage. Without an explicit `timeout-minutes` on the job, the termination was invisible — engineers spent time investigating "missing logs" instead of "job ran out of memory."

#### The Fix

```yaml
jobs:
  acceptance:
    timeout-minutes: 120
```

120 minutes accounts for worst-case 4-cluster CNI setup (~30 min) + go test timeout (60 min) + buffer.

#### Impact

OOM kills and spot reclaims now surface as explicit "Job cancelled: exceeded timeout" in the GitHub Actions UI. Debugging time reduced significantly.

#### Trade-offs

| | |
|---|---|
| **Pro** | Makes the failure mode visible and debuggable |
| **Con** | None — any job exceeding 2 hours is already broken; the timeout doesn't change outcomes, only observability |

---

## Phase 2: Performance Optimizations

These changes reduce wall time by eliminating wasted work and enabling more parallelism. Phase 2 reduced the CI ceiling from ~75 min to ~37 min.

---

### Optimization 1 — `num-clusters` matrix field + matrix generator passthrough

**Files**: `kind_acceptance_test_packages.yaml`, `generate_test_matrix.py`, both workflow files

#### The Problem

Both workflows unconditionally create 4 kind clusters (dc1–dc4) per runner. The `wan-federation` and `partitions` packages only ever use dc1 and dc2:

- `wan-federation`: all test files use only `env.DefaultContext(t)` (dc1) and `env.Context(t, 1)` (dc2)
- `partitions`: `main_test.go` contains `expectedNumberOfClusters := 2`; all test files confirm this

For a CNI runner, creating dc3 + dc4 means:
- Two additional `kind create cluster` calls (~3 min each)
- Two additional Calico installs (another ~4 min each)
- Two additional image-load loops

That's ~14 minutes of pure overhead per CNI runner, for clusters that are never used.

#### The Fix

Three coordinated changes:

**1. YAML declaration** (`kind_acceptance_test_packages.yaml`):
```yaml
- {runner: 0, test-packages: "partitions", num-clusters: "2"}
- {runner: 5, test-packages: "wan-federation", num-clusters: "2"}
```

**2. Generator passthrough** (`generate_test_matrix.py`):

Before: the generator only forwarded `runner`, `test-packages`, and `run-filter` to each shard.  
After: any field not in the reserved set propagates to every shard automatically.

```python
extra = {k: v for k, v in entry.items()
         if k not in ("runner", "test-packages", "run-filter")}
# every matrix.append() call now includes **extra
matrix.append({"runner": runner_idx, "test-packages": pkg, **extra})
```

This means adding `num-clusters` required no changes to the generator — just adding the YAML field. This same mechanism will work for any future per-package metadata (e.g., `runner-size`, `timeout-minutes-override`).

**3. Conditional dc3/dc4 steps** (both workflow files):
```yaml
- name: Create dc3
  if: ${{ !matrix['num-clusters'] || fromJSON(matrix['num-clusters']) > 2 }}
```

Missing field → create dc3/dc4 (safe default). `"2"` → skip dc3/dc4.

**4. Dynamic kube-contexts**:
```bash
KUBE_CONTEXTS="kind-dc1,kind-dc2,kind-dc3,kind-dc4"
if [[ "${{ matrix['num-clusters'] }}" == "2" ]]; then
  KUBE_CONTEXTS="kind-dc1,kind-dc2"
fi
```

#### Impact

- Partitions CNI setup: ~25 min → ~12 min per runner
- Wan-federation CNI setup: same reduction

#### Trade-offs

| | |
|---|---|
| **Pro** | Purely declarative — adding a new 2-cluster package requires one YAML field, zero code |
| **Pro** | Generator passthrough generalizes to any future per-package metadata |
| **Pro** | Safe default: packages without the field continue to create 4 clusters |
| **Con** | Requires verifying a package actually only uses ≤N clusters before adding the field — a wrong `num-clusters: "2"` will silently cause tests to fail with "cluster not found" |
| **Con** | The `fromJSON()` call in the workflow condition requires `num-clusters` to be a string (`"2"`, not `2`) in YAML due to GitHub Actions type coercion |

---

### Optimization 2 — Replace `sleep 120` with `kubectl wait`

**File**: `reusable-kind-cni-acceptance.yml`

#### The Problem

After installing Calico on each kind cluster, every CNI workflow step executed:

```bash
sleep 120  # give calico time to set itself up
kubectl cluster-info
kubectl get nodes
kubectl get pods -n calico-system
```

The `sleep 120` is unconditional. Whether Calico is ready in 20 seconds or 90 seconds, the runner always waits the full 2 minutes. With 4 clusters per runner (reduced to 2 for partitions/wan-fed), that's 4 × 120s = 8 minutes of guaranteed sleep per runner.

#### The Fix

```bash
kubectl wait --for=condition=Ready pods --all -n calico-system --timeout=180s
kubectl wait --for=condition=Ready pods --all -n tigera-operator --timeout=180s
kubectl cluster-info
kubectl get nodes
kubectl get pods -n calico-system
kubectl get pods -n tigera-operator
```

`kubectl wait` exits as soon as the condition is met. The `--timeout=180s` provides a safety valve if Calico is genuinely broken.

The `tigera-operator` namespace wait was added alongside `calico-system` — the operator controls Calico and must be ready before the system pods.

#### Impact

60–90 seconds saved per cluster × 2 clusters per partitions/wan-fed runner = **2–3 minutes per runner**. On CNI runners that still use 4 clusters, savings are 4–6 minutes.

#### Trade-offs

| | |
|---|---|
| **Pro** | Exits at readiness, not at a hardcoded deadline — inherently correct |
| **Pro** | The 180s timeout is strictly longer than the old 120s sleep, so no new failure modes |
| **Con** | If Calico pods are in a crash loop, `kubectl wait` blocks for the full 180s before failing vs. the old sleep + manual check which would fail faster |

---

### Optimization 3 — Split `TestPartitions_Sync` into 6 top-level functions

**File**: `acceptance/tests/partitions/partitions_sync_test.go`

#### The Problem

`TestPartitions_Sync` ran 6 independent sub-cases sequentially using `t.Run`:

```
default namespace (not secure)
default namespace (secure / ACLs)
single namespace (not secure)
single namespace (secure / ACLs)
mirror k8s (not secure)
mirror k8s (secure / ACLs)
```

Each sub-case installs a full Consul Helm chart and runs an independent sync test. They share no state. Total observed duration: **~1300s (~21 min)** on a single runner — because to the matrix generator, `TestPartitions_Sync` is one function.

#### The Fix

Extracted a private helper function holding all the test logic, and added 6 public wrapper functions matching the matrix generator's regex:

```go
type partitionsSyncCase struct {
    destinationNamespace string
    mirrorK8S            bool
    ACLsEnabled          bool
}

func runPartitionsSync(t *testing.T, c partitionsSyncCase) {
    t.Helper()
    // ... all test logic
}

func TestPartitions_Sync_DefaultNamespace(t *testing.T) {
    runPartitionsSync(t, partitionsSyncCase{
        destinationNamespace: defaultNamespace,
        mirrorK8S:            false,
        ACLsEnabled:          false,
    })
}

func TestPartitions_Sync_DefaultNamespaceACLs(t *testing.T) { ... }
func TestPartitions_Sync_SingleNamespace(t *testing.T)      { ... }
func TestPartitions_Sync_SingleNamespaceACLs(t *testing.T)  { ... }
func TestPartitions_Sync_MirrorK8S(t *testing.T)            { ... }
func TestPartitions_Sync_MirrorK8SACLs(t *testing.T)        { ... }
```

`test-timings.json` was updated with per-case estimates (217s each) so the matrix generator bins them correctly from the first post-split run.

#### Impact

Wall time for sync cases: **1300s on one runner → 217s on 6 runners in parallel**.

#### Trade-offs

| | |
|---|---|
| **Pro** | Each case now independently retryable — a flake in one case doesn't re-run the other 5 |
| **Pro** | Failure attribution is precise — "TestPartitions_Sync_SingleNamespaceACLs failed" instead of "TestPartitions_Sync failed" |
| **Con** | More Go functions to maintain; naming must be stable (renaming breaks `test-timings.json` keys) |
| **Con** | If sub-cases share setup in the future, they'll need to be recombined or the setup duplicated |

---

### Optimization 4 — Split `TestPartitions_Connect_MultiportServices` into 4 top-level functions

**File**: `acceptance/tests/partitions/partitions_multiport_connect_test.go`

#### The Problem

`TestPartitions_Connect_MultiportServices` ran 4 independent combinations sequentially:

```
ACLs disabled × local gateway
ACLs disabled × remote gateway
ACLs enabled  × local gateway
ACLs enabled  × remote gateway
```

Total observed duration: **~1802s (~30 min)** — the highest single-function ceiling in the entire test suite.

#### The Fix

Same pattern as Sync: private helper + 4 public wrappers.

```go
func runPartitionsConnectMultiport(t *testing.T, aclsEnabled bool, fixturePath string) {
    t.Helper()
    cfg := suite.Config()
    cfg.SkipWhenOpenshiftAndCNI(t)

    if !cfg.EnableEnterprise {
        t.Skipf("skipping: requires enterprise")
    }
    if cfg.EnableTransparentProxy && aclsEnabled {
        t.Skip("skipping: tproxy + ACLs")
    }
    if cfg.EnableCNI && aclsEnabled {
        t.Skip("skipping: CNI + ACLs")
    }
    // ... test logic
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
```

Skip conditions were moved from the outer loop into the helper — each wrapper function is valid regardless of which combination it exercises. Callers that would fail skip early rather than failing loudly.

#### Impact

Wall time for multiport cases: **1802s on one runner → 450s on 4 runners in parallel**.

#### Trade-offs

Same as Optimization 3. The skip-condition-in-helper pattern is important: without it, a case would fail with a confusing error rather than cleanly skipping.

---

## Cumulative Impact

### Before → After comparison

```
Before (monolithic functions, 4-cluster CNI setup):

  [partitions (CNI)] ─────────────────────────────────── 55 min ← CEILING
  [wan-federation  ] ──────────── 23 min
  [connect         ] ─────── 14 min
  [api-gateway     ] ──── 8 min
  ...

After Phase 1 + Phase 2 (split functions, 2-cluster setup, kubectl wait):

  [connect-lifecycle (dual-stack)] ──────── 37 min ← NEW CEILING
  [config-entries   (dual-stack)] ─────── 36 min
  [config-entries   (regular)  ] ────── 34 min
  [partitions (CNI)            ] ────── 24 min   (was 55 min)
  [wan-federation              ] ─── 14 min
  [connect                     ] ─── 14 min
  ...
```

### Where the time went

| Root cause | Wasted time (before) | Wasted time (after) |
|---|---|---|
| `TestPartitions_Connect_MultiportServices` ceiling | 30 min on 1 runner | 7.5 min on 4 runners |
| `TestPartitions_Sync` ceiling | 21 min on 1 runner | 3.6 min on 6 runners |
| dc3/dc4 creation (partitions + wan-fed, CNI) | ~28 min total wasted | 0 |
| Calico unconditional sleep (4 clusters × 2 min) | 8 min per CNI runner | ~2 min (condition-based) |
| Port-forward races | 1–3 re-runs per week | 0 |
| Docker 502 retries | 2–3 failures per week | 0 |
| kind cleanup failure → runner loss | 1–2 failures per week | 0 |

---

## Phase 3: Reliability Hardening & Further Splitting

Phase 3 addressed the new ceiling left after Phase 2 (connect-lifecycle and config-entries at ~37 min), fixed the remaining flaky test, and extended the `num-clusters` optimization to two more packages.

---

### Fix 5 — `TestVault_Partitions` flake: async ServiceAccount token population

**File**: `acceptance/framework/vault/vault_cluster.go`

#### The Problem

In Kubernetes 1.24+, manually-created `ServiceAccountToken` secrets are populated asynchronously by the token controller. The `token` and `ca.crt` fields are empty immediately after `kubectl create`, and only filled in within a few seconds.

`ConfigureAuthMethod` read the secret with a single GET call immediately after creation, then wrote the (possibly empty) `token_reviewer_jwt` into the Vault Kubernetes auth config. Vault configured with an empty reviewer JWT returns HTTP 403 on every subsequent token validation call — causing `TestVault_Partitions` to fail on every auth attempt for the rest of the test, exhausting the full 1-hour test timeout before reporting the failure.

This produced a 1h 15min CI run whenever it triggered — the flake was not deterministic but occurred frequently enough to block the PR.

#### The Fix

Replaced the single GET with a `retry.RunWith` loop that waits up to 60 seconds for the token to be populated:

```go
var tokenSecret *corev1.Secret
retry.RunWith(&retry.Counter{Count: 30, Wait: 2 * time.Second}, t, func(r *retry.R) {
    tokenSecret, err = v.kubernetesClient.CoreV1().Secrets(saNS).Get(
        context.Background(), secretName, metav1.GetOptions{})
    require.NoError(r, err)
    if len(tokenSecret.Data["token"]) == 0 {
        r.Error("service account token not yet populated in secret")
    }
})
```

#### Impact

Eliminates the 403 failure class from `TestVault_Partitions`. In practice the token is ready within 1–3 seconds; the 60-second budget is purely a safety margin.

#### Trade-offs

| | |
|---|---|
| **Pro** | Fixes a class of failure that was invisible — the test appeared to run for the full timeout with unrelated-looking 403 errors |
| **Con** | If the token controller is genuinely broken, this delays failure by up to 60s instead of failing immediately — acceptable given the alternative |

---

### Fix 6 — `kind-cni-calico` Makefile: replace hardcoded sleeps with condition-based waits

**File**: `Makefile` (target: `kind-cni-calico`)

#### The Problem

The `kind-cni-calico` target had two unconditional sleeps:

```makefile
sleep 30   # after applying tigera-operator.yaml — waiting for operator deployment
sleep 20   # after applying custom-resources.yaml — waiting for Calico pods to appear
```

The operator sleep was replaced with `kubectl wait --for=condition=Available deployment/tigera-operator`, which exits as soon as the deployment is ready.

The custom-resources sleep was more subtle: `kubectl wait --all` with zero matching pods exits immediately with exit code 1 ("no matching resources found"). The calico-node DaemonSet is not created until the tigera-operator processes the Installation CR — there is a window of several seconds after `kubectl create -f custom-resources.yaml` where no pods exist in `calico-system`. Removing the sleep without a replacement caused ~188/198 CNI jobs to fail with "no matching resources found".

#### The Fix

```makefile
kubectl wait --for=condition=Available deployment/tigera-operator \
    -n tigera-operator --timeout=120s
# apply custom-resources ...
# Poll until the calico-node DaemonSet exists, then wait for rollout
timeout 120 bash -c \
    'until kubectl get daemonset calico-node -n calico-system 2>/dev/null; \
     do sleep 3; done'
kubectl rollout status daemonset/calico-node \
    -n calico-system --timeout=180s
```

The poll loop (`until kubectl get daemonset ... do sleep 3; done`) handles the gap between CR application and DaemonSet creation. `kubectl rollout status` then blocks until all calico-node pods are Ready.

#### Impact

Eliminates 30s + 20s = 50s of unconditional sleep from every local `make kind` run. In CI this target runs inside the workflow setup steps rather than as a standalone Makefile target, but the same pattern was applied there.

#### Trade-offs

| | |
|---|---|
| **Pro** | Exits at readiness; in fast environments saves the full 50s |
| **Pro** | `rollout status` gives clear progress output — easier to debug if Calico is stuck |
| **Con** | More complex than a sleep; requires the DaemonSet name (`calico-node`) to be stable across Calico versions |

---

### Optimization 5 — `num-clusters: "1"` for api-gateway

**File**: `acceptance/ci-inputs/kind_acceptance_test_packages.yaml`

api-gateway tests only use `DefaultContext(t)` (dc1). Adding `num-clusters: "1"` eliminates dc2–dc4 creation on every api-gateway runner.

```yaml
- {runner: 10, test-packages: "api-gateway", num-clusters: "1"}
```

On CNI runners this saves ~21 min of cluster creation + Calico setup per runner.

---

### Optimization 6 — `num-clusters: "2"` for peering

**File**: `acceptance/ci-inputs/kind_acceptance_test_packages.yaml`

Peering tests use at most `env.Context(t, 1)` (dc2). Adding `num-clusters: "2"` eliminates dc3/dc4 creation.

```yaml
- {runner: 1, test-packages: "peering", num-clusters: "2"}
```

---

### Optimization 7 — Split `TestConnectInject_ProxyLifecycleShutdown` into 6 functions

**File**: `acceptance/tests/connect-lifecycle/connect_proxy_lifecycle_test.go`

#### The Problem

`TestConnectInject_ProxyLifecycleShutdown` ran 6 independent sub-cases sequentially using `t.Run`, each with a full Helm install. Total observed duration: **~2235s (~37 min)** — the ceiling after Phase 2.

The 6 cases cover combinations of:
- `drainListeners`: true / false
- `gracePeriodSeconds`: 0 / 5
- `secure`: true / false

#### The Fix

Private helper `runProxyLifecycleShutdown(t, LifecycleShutdownConfig{...})` + 6 public wrappers:

```go
func TestConnectInject_ProxyLifecycleShutdown_NotSecure_DrainListeners_GracePeriod5(t *testing.T) {
    runProxyLifecycleShutdown(t, LifecycleShutdownConfig{secure: false, helmValues: map[string]string{
        helmDrainListenersKey:     "true",
        helmGracePeriodSecondsKey: "5",
    }})
}
// ... 5 more wrappers
```

`test-timings.json` pre-populated with 370s per case.

#### Impact

Wall time: **2235s on one runner → ~370s on 6 runners in parallel**.

---

### Optimization 8 — Split `TestController` (config-entries) into 4 functions

**File**: `acceptance/tests/config-entries/config_entries_test.go`

#### The Problem

`TestController_Namespaces` (now `TestController`) ran 4 sequential sub-cases: secure × vault combinations. Each installs a full Consul Helm chart. Total observed duration: **~2160s (~36 min)**.

Additionally, both `retry.Counter` calls within the test used `Count: 10, Wait: 500ms` (5 seconds total) — far too short for the controller to propagate CRD patch/delete operations to Consul in tproxy environments, causing `TestController_Secure_NoVault` to flake with "An error is expected but got nil" (config entry still present in Consul after the K8s CRD was deleted).

#### The Fix

**Split**: Private helper `runController(t, secure, useVault bool)` + 4 public wrappers:

```go
func TestController_NotSecure_NoVault(t *testing.T) { runController(t, false, false) }
func TestController_Secure_NoVault(t *testing.T)    { runController(t, true, false) }
func TestController_NotSecure_Vault(t *testing.T)   { runController(t, false, true) }
func TestController_Secure_Vault(t *testing.T)      { runController(t, true, true) }
```

**Retry fix**: Both `retry.Counter` instances bumped from `Count: 10, Wait: 500ms` to `Count: 30, Wait: 2s` (60 seconds total), giving the controller adequate time to reconcile deletions and patches in all environments.

`test-timings.json` pre-populated with 540s per case.

#### Impact

Wall time: **2160s on one runner → ~540s on 4 runners in parallel**. Retry fix eliminates the tproxy flake.

---

### Optimization 9 — Split `TestFailover_Connect` (sameness) into 2 functions

**File**: `acceptance/tests/sameness/sameness_test.go`

`TestFailover_Connect` had 2 sub-cases (`ACLsEnabled: false/true`). Split into:

```go
func TestFailover_Connect_DefaultFailover(t *testing.T) { runFailoverConnect(t, false) }
func TestFailover_Connect_SecureFailover(t *testing.T)  { runFailoverConnect(t, true) }
```

`test-timings.json` pre-populated with 1080s per case.

---

## Cumulative Impact (Phase 1 + 2 + 3)

```
Before (monolithic functions, 4-cluster CNI setup, flaky tests):

  [partitions (CNI)     ] ─────────────────────────────────── 55 min ← CEILING
  [connect-lifecycle    ] ───────────────────────────── 37 min
  [config-entries       ] ──────────────────────────── 36 min
  [wan-federation       ] ──────────── 23 min
  ...

After Phase 1 + 2 + 3:

  [vault (CNI)          ] ──────── 33 min ← NEW CEILING (test execution)
  [partitions (CNI)     ] ────── 24 min   (was 55 min)
  [connect-lifecycle    ] ─── ~6 min      (was 37 min)
  [config-entries       ] ─── ~9 min      (was 36 min)
  [sameness             ] ─── ~18 min     (was 36 min)
  ...

  Wall clock: ~60–65 min (runner queue wait up to 27 min adds overhead)
```

### Failure classes eliminated

| Failure class | Root cause | Phase fixed |
|---|---|---|
| Port-forward `connection refused` on secure tests | Reconnect returned before pod was ready | Phase 1 |
| Runner loss after test completion | `kind` not in PATH during cleanup | Phase 1 |
| Docker 502 on image pull | Mirror instability under concurrent load | Phase 1 |
| OOM/spot-reclaim invisible failures | No job timeout | Phase 1 |
| `TestVault_Partitions` 403 after 1-hour run | Empty `token_reviewer_jwt` from async K8s token | Phase 3 |
| `TestController_Secure_NoVault` tproxy flake | 5s retry too short for controller reconciliation | Phase 3 |
| CNI "no matching resources found" regression | `kubectl wait --all` exits on zero pods | Phase 3 (self-corrected) |

---

## Alternatives Evaluated (Not Implemented)

### Option A — Upgrade to `m6a.2xlarge` runners

**Idea**: Move 4-cluster packages to 32 GB runners to reduce OOM-kill risk.

**Why we didn't**: The `num-clusters` fix eliminated the unnecessary 4-cluster setup for the packages that were actually hitting memory pressure (partitions, wan-fed). OOM kills for the remaining 4-cluster packages (peering, sameness) can be addressed when observed. Larger runners cost ~2× per minute without reducing wall time.

**When to revisit**: If peering or sameness sees OOM kills after the `num-clusters` analysis.

---

### Option B — Move dual-stack variants to nightly

**Idea**: The 3 dual-stack CI variants (CNI dual-stack, tproxy dual-stack, acceptance dual-stack) run on every PR. Moving them to nightly would cut PR runner consumption by ~40% and eliminate the dual-stack contribution to the ceiling.

**Why we didn't**: Requires product team sign-off — dual-stack regressions would be detected up to 24 hours after merge rather than at PR time.

**Current cost**: `connect-lifecycle` and `config-entries` are the current ceiling *specifically* in the dual-stack variants. Without dual-stack on PRs, the ceiling would drop to ~24 min immediately, with no code changes.

**When to revisit**: When the team wants to trade detection latency for CI speed — worth doing before or alongside Phase 3 test splitting.

---

### Option C — Pre-stage images with `kind load docker-image`

**Idea**: After `docker pull` on the host, use `kind load docker-image` to copy images directly into the containerd store of each kind node. Eliminates node-side `crictl pull` entirely.

**Why we didn't**: `kind load docker-image` is sequential per image per cluster. With ~12 images × 4 clusters, the serial copy loop is comparable to the current parallel `crictl pull` approach. The `consul-image` (Consul server) is intentionally not pre-staged in the current setup anyway — pre-staging it requires additional Helm value changes. The retry fix is sufficient for the 502 failure class.

**When to revisit**: If transient 502 failures resurface after the retry logic is in place.

---

### Option D — Gate `update-test-timings` to the default branch only

**Idea**: The `update-test-timings` job tries to commit updated timing data and push to the triggering branch. On PR branches, this fails due to branch protection. The failure appears as a red job in the PR UI.

**Why we didn't implement yet**: The job currently doesn't run on PRs (only on push to main). It was surfacing on PRs for unrelated reasons. Confirm the current behavior before adding an explicit condition.

---

## Guidelines for Future Test Authors

These are prescriptive rules derived from the failures and optimizations described above. They are listed in order of impact.

---

### Rule 1 — One top-level function per independent case

**Do this:**
```go
func runMyFeature(t *testing.T, secure bool, ns string) {
    t.Helper()
    // ... test logic
}

func TestMyFeature_Secure_DefaultNamespace(t *testing.T)    { runMyFeature(t, true, "default") }
func TestMyFeature_Insecure_DefaultNamespace(t *testing.T)  { runMyFeature(t, false, "default") }
func TestMyFeature_Secure_CustomNamespace(t *testing.T)     { runMyFeature(t, true, "custom") }
```

**Not this:**
```go
func TestMyFeature(t *testing.T) {
    for _, c := range []struct{ secure bool; ns string }{...} {
        t.Run(c.name, func(t *testing.T) {
            // ... all N cases run sequentially
        })
    }
}
```

**Why**: The matrix generator bins at `Test*` function boundaries. A monolithic function with 6 sub-cases takes 6× as long as any one case and cannot be parallelized. A monolithic function with 30-minute total runtime single-handedly determines the CI ceiling for the entire project.

**Exception**: If sub-cases genuinely share expensive setup that cannot be duplicated (e.g., a 10-minute cluster bootstrap that all cases reuse), sharing one top-level function is acceptable. Document the trade-off explicitly.

---

### Rule 2 — Test function names are stable identifiers

`test-timings.json` maps function names to observed durations. Renaming `TestMyFeature_SecureMode` to `TestMyFeature_Secure` invalidates the stored timing and the test falls back to `DEFAULT_TEST_SECONDS = 600s`, potentially mis-binning it for the first post-rename run.

**Naming convention for split tests**: `TestPackage_Feature_CaseName` using PascalCase:
```
TestPartitions_Connect_DefaultNamespace
TestPartitions_Connect_DefaultNamespaceACLs
TestPartitions_Sync_MirrorK8S
```

Avoid including dynamic parameters (e.g., Kubernetes version, runner index) in function names.

---

### Rule 3 — Declare how many clusters your test actually uses

Before adding a test to a multi-cluster package, verify which cluster contexts it actually calls:

```bash
grep -n "env.Context\|env.DefaultContext" acceptance/tests/mypkg/*.go
```

If your test only uses `DefaultContext(t)` (dc1) or `Context(t, 1)` (dc2), add `num-clusters: "2"` to your package entry in `kind_acceptance_test_packages.yaml`. Not doing this wastes ~14 min of CNI setup time per runner.

If `main_test.go` for your package asserts `expectedNumberOfClusters`, keep that number consistent with the YAML field.

---

### Rule 4 — Add timing estimates when you add tests

When you add a new `Test*` function, add a timing estimate to `test-timings.json` under the appropriate package key:

```json
"mypkg": {
  "TestMyFeature_Secure": 300,
  "TestMyFeature_Insecure": 280
}
```

If you don't know the actual duration, use a similar test as a reference. `DEFAULT_TEST_SECONDS = 600s` is intentionally conservative — an unknown test is assumed to be 10 minutes. Under-estimating here causes over-packing; over-estimating causes the test to land alone on a dedicated runner.

After the first successful CI run on the default branch, `update-test-timings` will replace your estimate with observed data.

---

### Rule 5 — Never use unconditional sleeps for external readiness

```bash
# Wrong: always burns the full wait time
sleep 120

# Right: exits as soon as the condition is met
kubectl wait --for=condition=Ready pods --all -n my-namespace --timeout=180s
```

The same principle applies in Go test code:

```go
// Wrong
time.Sleep(10 * time.Second)

// Right
retry.RunWith(&retry.Timer{Timeout: 30 * time.Second, Wait: 1 * time.Second}, t, func(r *retry.R) {
    // check condition
})
```

---

### Rule 6 — Put skip conditions inside the shared helper, not in individual wrappers

When splitting a table-driven test, skip conditions belong in the private helper:

```go
func runMyFeature(t *testing.T, aclsEnabled bool) {
    t.Helper()
    cfg := suite.Config()
    cfg.SkipWhenOpenshiftAndCNI(t)
    if cfg.EnableCNI && aclsEnabled {
        t.Skip("skipping: CNI + ACLs not supported")
    }
    // ...
}
```

**Why**: If skip conditions live only in some wrappers, you get inconsistent behavior across CI variants. The helper is authoritative for what the test supports.

---

### Rule 7 — Port-forwarded servers are not immediately ready

After establishing a port-forward (whether via `monitorPortForwardedServer` or directly), do not assume the remote endpoint is accepting connections immediately. Use a readiness probe before running assertions. The framework's `monitorPortForwardedServer` already handles this for reconnect paths, but if you write your own forwarding logic, replicate the pattern.

---

### Rule 8 — Verify cluster teardown doesn't mask bugs

The acceptance framework tears down Consul via Helm at the end of each test. If your test leaves resources in a broken state (stuck finalizers, orphaned services), the next test on the same runner may inherit them. Use `helpers.Cleanup(t, ...)` to register explicit cleanup for every resource you create, especially CRDs with finalizers.

---

## What's Next (Phase 4)

The execution ceiling is now **~33 min** (vault CNI runners). However, observed wall-clock time is **~60–65 min**, dominated by runner queue wait (up to 27 min) caused by dispatching ~620 parallel jobs simultaneously to a finite private runner pool.

New targets, in priority order:

### 1. Reduce runner queue contention (highest impact, no code changes)

The queue wait is the single biggest remaining lever. Two options:

**Option A — Consolidate over-split packages**: Some test cases (~100s each) run as dedicated runners when they could be grouped 3–4 per runner without exceeding the 20-min shard target. Reducing total jobs from ~620 to ~500 would reduce runner pool saturation and shorten queue waits.

**Option B — HashiCorp provisions more private runners**: The CNI dual-stack runners showed the worst queue waits (up to 27 min). Adding capacity to those runner labels would directly reduce wall time with no code changes.

### 2. Evaluate moving dual-stack variants to nightly

The 3 dual-stack variants (CNI dual-stack, tproxy dual-stack, acceptance dual-stack) account for ~300 of the 620 jobs dispatched per PR. Moving them to nightly would:
- Cut total job count roughly in half → substantially reduce queue contention
- Drop wall-clock time to ~35–40 min with no further splitting needed

Requires product team sign-off — dual-stack regressions would be detected up to 24 hours after merge.

### 3. Audit remaining packages for `num-clusters` opportunities

Packages still creating unnecessary clusters:
- `connect`: verify which tests actually use dc3/dc4
- `sameness`: verify cluster usage in all test files

Use the verification command in the appendix.

### 4. Automated ceiling alerting

There is currently no mechanism to detect when a new test becomes the CI ceiling. A lightweight fix: add a post-test step that reads shard timings from `$GITHUB_STEP_SUMMARY` and fails with a warning if any shard exceeds a threshold (e.g., 25 min). This turns ceiling detection from reactive (observe → investigate → fix) to proactive (fail → fix before merge).

---

## Appendix: Key Files

| File | Repo | Purpose |
|---|---|---|
| `control-plane/build-support/scripts/generate_test_matrix.py` | consul-k8s | Reads timings + YAML, emits GitHub Actions matrix |
| `acceptance/ci-inputs/kind_acceptance_test_packages.yaml` | consul-k8s | Per-package metadata (runner index, num-clusters, etc.) |
| `acceptance/ci-inputs/test-timings.json` | consul-k8s | Per-function duration history; source of truth for bin-packing |
| `acceptance/framework/portforward/port_forward.go` | consul-k8s | Port-forward reconnect logic |
| `.github/workflows/reusable-kind-acceptance.yml` | consul-k8s-workflows | Regular kind acceptance workflow |
| `.github/workflows/reusable-kind-cni-acceptance.yml` | consul-k8s-workflows | CNI kind acceptance workflow |

---

## Appendix: How to Verify a Package Only Uses N Clusters

```bash
# Check for cluster context usage beyond dc2
grep -n "env.Context(t, [2-9])\|env.Context(t, [0-9][0-9])" \
    acceptance/tests/<package>/*.go

# If this returns nothing, the package uses at most 2 clusters (dc1 + dc2).
# Confirm with main_test.go:
grep "expectedNumberOfClusters" acceptance/tests/<package>/main_test.go
```

If both checks confirm ≤2 clusters, you can safely add `num-clusters: "2"` to the package YAML entry.
