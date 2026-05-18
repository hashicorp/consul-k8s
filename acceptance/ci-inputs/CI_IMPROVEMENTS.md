# Consul-K8s Acceptance CI: Reliability & Performance Improvements

**Authors**: Platform Engineering  
**Status**: Active — ongoing as new ceilings emerge  
**Audience**: Engineering team — serves as both retrospective and living reference

---

## Summary

Acceptance CI for `consul-k8s` suffered from two compounding problems: **unreliable infrastructure** causing non-deterministic failures unrelated to any code change, and **wall-time ceilings** driven by monolithic test functions that couldn't be parallelized by the matrix sharding system.

We addressed both with targeted changes across the test framework, workflow files, and test code itself.

| Metric | Before | After |
|---|---|---|
| Wall time (observed) | ~2 hours | **~60–65 min** |
| Execution ceiling (longest test job) | ~55 min | **~33 min (vault CNI)** |
| Partitions runner time | ~55 min | **~24 min** |
| Flaky failure rate | High (3–5 per run) | **Near zero** |
| Runner-minutes wasted on unnecessary clusters | ~48 min/run | **0** |
| Calico setup wait per CNI runner | 8 min (hardcoded) | **~2 min (condition-based)** |

Wall-clock time is now dominated by runner **queue wait** (up to 27 min) caused by ~620 jobs dispatching simultaneously to a finite private runner pool — not by test execution time. See [What's Next](#whats-next) for options.

---

## Background: The Sharding System

Everything in this document builds on one piece of infrastructure: `generate_test_matrix.py`.

The matrix generator:
1. Reads `kind_acceptance_test_packages.yaml` for the list of packages and per-package metadata
2. Scans each package's `*.go` files for top-level `Test*` functions
3. Looks up each function's historical duration in `test-timings.json`
4. Greedy bin-packs functions into shards targeting `TARGET_SHARD_SECONDS = 1200s` (20 min)
5. Functions with no recorded history receive `DEFAULT_TEST_SECONDS = 600s` (10 min)
6. Emits a GitHub Actions matrix where each shard runs as an independent parallel job

**The fundamental constraint**: the generator shards at the `Test*` function boundary. A `TestFoo` with 6 sequential `t.Run` sub-cases is an atomic unit — all 6 cases land on one runner, and their combined time becomes the ceiling.

This is why test splitting is the highest-leverage optimization: it changes the granularity available to the bin-packer.

---

## Reliability Fixes

These changes address failure modes unrelated to the code under test. Each was responsible for at least one category of false-positive CI failure.

---

### Port-forward reconnect probe

**File**: `acceptance/framework/portforward/port_forward.go`

#### Problem

Tests with `secure: true` use `monitorPortForwardedServer` to reconnect a `kubectl port-forward` tunnel to the Consul gRPC-TLS port (8501) if it drops. The reconnect logic called `ForwardPortE` and, on success, immediately returned.

`ForwardPortE` returning `nil` means the `kubectl port-forward` **process** launched — not that the remote pod is accepting connections. There is a 1–5 second window where every dial gets `ECONNREFUSED`. When this window exceeded the remaining retry budget, tests failed with connection refused errors that looked like test logic failures.

#### Fix

Added a TCP readiness probe loop after a successful `ForwardPortE` call:

```go
deadline := time.Now().Add(reconnectProbeTimeout) // 30s
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
    time.Sleep(reconnectProbeInterval) // 1s
}
```

The `doneChan` check inside the loop prevents blocking cleanup if the test calls `Close()` during the probe window.

#### Impact

Eliminates the "connection refused during reconnect" failure class — the majority of previously unexplained `secure: true` test failures.

---

### `kind` cleanup PATH fix

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### Problem

`helm/kind-action` with `install_only: true` appends `kind` to `$GITHUB_PATH`, but `$GITHUB_PATH` additions are not available in the action's own post-cleanup phase. The cleanup script runs `kind delete cluster` in a subprocess where `kind` is not on PATH — fails with `kind: command not found`. With `ignore_failed_clean: false` (default), this hard-fails the entire post-step and GitHub Actions marks subsequent cluster-creation steps as uncreatable. The entire runner is lost.

#### Fix

```yaml
- uses: helm/kind-action@v1.10.0
  with:
    install_only: true
    ignore_failed_clean: true
```

Kind clusters are destroyed when the ephemeral runner terminates anyway.

#### Impact

Eliminated runner-loss failures that manifested as all cluster-creation steps being skipped.

---

### Docker registry retry with fallback

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### Problem

Two registry failure points:
- **Host-side**: `docker pull` hits `docker-mirror.hashi.app`, which returns HTTP 502 under load from many runners starting simultaneously.
- **Node-side**: `crictl pull` inside kind nodes hits `registry-1.docker.io` directly for images not in the mirror, also returning 502.

#### Fix

Retry wrapper (5 attempts, 10s back-off) for all `docker pull` commands. For `crictl pull`, a fallback from the HashiCorp mirror to `docker.io` if all retries fail.

#### Impact

Eliminated the 502 failure class from both host-side and node-side pulls (~2–3 failures per week).

---

### Explicit job timeout

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

#### Problem

When a runner was OOM-killed or reclaimed (AWS spot), GitHub Actions produced blank step entries and `BlobNotFound` errors in log storage. Without an explicit `timeout-minutes`, the termination was invisible — engineers investigated "missing logs" instead of "job ran out of memory."

#### Fix

```yaml
jobs:
  acceptance:
    timeout-minutes: 120
```

#### Impact

OOM kills and spot reclaims now surface as explicit "Job cancelled: exceeded timeout." Debugging time reduced significantly.

---

### `TestVault_Partitions` flake: async ServiceAccount token population

**File**: `acceptance/framework/vault/vault_cluster.go`

#### Problem

In Kubernetes 1.24+, manually-created `ServiceAccountToken` secrets have their `token` and `ca.crt` fields populated **asynchronously** by the token controller — the fields are empty immediately after `kubectl create`.

`ConfigureAuthMethod` read the secret with a single GET call immediately after creation, then wrote the (possibly empty) `token_reviewer_jwt` into the Vault Kubernetes auth config. Vault configured with an empty reviewer JWT returns HTTP 403 on every subsequent token validation — causing the test to fail on every auth attempt for the rest of the test, exhausting the full 1-hour test timeout. This produced ~1h 15min CI runs whenever it triggered.

#### Fix

Replaced the single GET with a retry loop (up to 60s) that waits for the token to be populated:

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

Eliminates the 403 failure class from `TestVault_Partitions`. In practice the token is ready within 1–3 seconds; the 60s budget is a safety margin.

---

### `TestController` (config-entries) propagation retry too short

**File**: `acceptance/tests/config-entries/config_entries_test.go`

#### Problem

Two `retry.Counter` calls in the config-entries controller test used `Count: 10, Wait: 500ms` (5 seconds total) to check whether CRD patch and delete operations had propagated to Consul. In tproxy environments with more reconciliation overhead, 5 seconds was insufficient. The test failed with "An error is expected but got nil" — the config entry still existed in Consul after the K8s CRD was deleted.

#### Fix

Both counters bumped to `Count: 30, Wait: 2s` (60 seconds total):

```go
counter := &retry.Counter{Count: 30, Wait: 2 * time.Second}
```

#### Impact

Eliminates the tproxy-specific flake in `TestController_Secure_NoVault`.

---

## Performance Optimizations

These changes reduce wall time by eliminating wasted work and enabling more parallelism.

---

### `num-clusters` matrix field: skip unnecessary cluster creation

**Files**: `kind_acceptance_test_packages.yaml`, `generate_test_matrix.py`, both workflow files

#### Problem

Both workflows unconditionally create 4 kind clusters (dc1–dc4) per runner. Several packages only ever use dc1 and dc2. For a CNI runner, creating dc3 + dc4 means two additional `kind create cluster` calls (~3 min each) and two additional Calico installs (~4 min each) — ~14 min of overhead per CNI runner for clusters that are never used.

#### Fix

Three coordinated changes:

**1. YAML declaration** (`kind_acceptance_test_packages.yaml`):
```yaml
- {runner: 0, test-packages: "partitions",    num-clusters: "2"}
- {runner: 1, test-packages: "peering",       num-clusters: "2"}
- {runner: 5, test-packages: "wan-federation", num-clusters: "2"}
- {runner: 10, test-packages: "api-gateway",  num-clusters: "1"}
```

**2. Generator passthrough** (`generate_test_matrix.py`): any field not in the reserved set propagates automatically to every shard — no generator code changes needed per package.

**3. Conditional dc3/dc4 steps** (both workflow files):
```yaml
- name: Create dc3
  if: ${{ !matrix['num-clusters'] || fromJSON(matrix['num-clusters']) > 2 }}
```

Missing field → create all clusters (safe default). `"2"` → skip dc3/dc4. `"1"` → skip dc2–dc4.

#### Impact

| Package | Before | After |
|---|---|---|
| partitions (CNI) | ~25 min setup | ~12 min setup |
| wan-federation (CNI) | ~25 min setup | ~12 min setup |
| peering (CNI) | ~25 min setup | ~12 min setup |
| api-gateway (CNI) | ~25 min setup | ~6 min setup |

---

### Replace hardcoded Calico sleeps with condition-based waits

**Files**: `reusable-kind-cni-acceptance.yml`, `Makefile` (`kind-cni-calico` target)

#### Problem

After installing Calico on each kind cluster, every CNI step executed `sleep 120` unconditionally — 2 min per cluster × 4 clusters = 8 min guaranteed sleep per runner regardless of actual readiness.

The Makefile `kind-cni-calico` target had two additional hardcoded sleeps: `sleep 30` after applying `tigera-operator.yaml` and `sleep 20` after applying the Installation CR.

A naive replacement of `sleep 20` with `kubectl wait --all -n calico-system` fails because `kubectl wait --all` exits immediately with error code 1 when zero pods exist ("no matching resources found") — the calico-node DaemonSet is created asynchronously by the tigera-operator after the Installation CR is processed.

#### Fix

In the workflow, replaced `sleep 120` with:
```bash
kubectl wait --for=condition=Ready pods --all -n calico-system --timeout=180s
kubectl wait --for=condition=Ready pods --all -n tigera-operator --timeout=180s
```

In the Makefile:
```makefile
kubectl wait --for=condition=Available deployment/tigera-operator \
    -n tigera-operator --timeout=120s
# apply custom-resources ...
# Poll until the DaemonSet exists, then wait for rollout
timeout 120 bash -c \
    'until kubectl get daemonset calico-node -n calico-system 2>/dev/null; \
     do sleep 3; done'
kubectl rollout status daemonset/calico-node -n calico-system --timeout=180s
```

The poll loop handles the window between Installation CR application and DaemonSet creation. `rollout status` then blocks until all calico-node pods are Ready.

#### Impact

60–90s saved per cluster × 2–4 clusters per runner = **2–6 min per CNI runner**.

---

### Test splitting: private helper + public wrapper pattern

#### Problem

Multiple test functions ran independent sub-cases sequentially via `t.Run`. To the matrix generator, each monolithic `TestFoo` is one atom — all sub-cases land on one runner and their combined time becomes the CI ceiling.

| Function | Sub-cases | Total duration | Ceiling contribution |
|---|---|---|---|
| `TestPartitions_Connect_MultiportServices` | 4 | ~1802s (30 min) | Highest ceiling |
| `TestPartitions_Sync` | 6 | ~1300s (21 min) | Second highest |
| `TestConnectInject_ProxyLifecycleShutdown` | 6 | ~2235s (37 min) | New ceiling after partitions fix |
| `TestController` (config-entries) | 4 | ~2160s (36 min) | Near-ceiling |
| `TestFailover_Connect` (sameness) | 2 | ~2160s (36 min) | — |

#### Fix

Each was refactored using the same pattern: extract all logic into a private `runXxx(t, params...)` helper, add one public `TestXxx_CaseName` wrapper per case.

```go
// Private helper holds all test logic
func runPartitionsSync(t *testing.T, c partitionsSyncCase) {
    t.Helper()
    // ... test logic unchanged
}

// One public wrapper per case — each discovered as an independent shard
func TestPartitions_Sync_DefaultNamespace(t *testing.T) {
    runPartitionsSync(t, partitionsSyncCase{destinationNamespace: defaultNamespace})
}
func TestPartitions_Sync_MirrorK8S(t *testing.T) {
    runPartitionsSync(t, partitionsSyncCase{mirrorK8S: true})
}
// ... etc.
```

Skip conditions belong in the private helper so all environments are handled consistently.

`test-timings.json` was pre-populated with per-case estimates for each split so the matrix generator bins them correctly from the first post-split run.

#### Impact

| Function | Before | After |
|---|---|---|
| `TestPartitions_Connect_MultiportServices` | 1802s (1 runner) | ~450s × 4 runners |
| `TestPartitions_Sync` | 1300s (1 runner) | ~217s × 6 runners |
| `TestConnectInject_ProxyLifecycleShutdown` | 2235s (1 runner) | ~370s × 6 runners |
| `TestController` (config-entries) | 2160s (1 runner) | ~540s × 4 runners |
| `TestFailover_Connect` (sameness) | 2160s (1 runner) | ~1080s × 2 runners |

---

## Failure Classes Eliminated

| Failure class | Root cause | Fix |
|---|---|---|
| Port-forward `connection refused` on secure tests | Reconnect returned before pod was ready | TCP readiness probe in reconnect path |
| Runner loss after test completion | `kind` not in PATH during cleanup | `ignore_failed_clean: true` |
| Docker 502 on image pull | Mirror instability under concurrent load | 5-attempt retry with fallback |
| OOM/spot-reclaim invisible failures | No job timeout | `timeout-minutes: 120` |
| `TestVault_Partitions` 403 after 1-hour run | Empty `token_reviewer_jwt` from async K8s SA token | Retry loop waiting for token population |
| `TestController_Secure_NoVault` tproxy flake | 5s retry too short for controller reconciliation | Bumped to 60s retry |
| CNI "no matching resources found" | `kubectl wait --all` exits immediately on zero pods | Poll loop + `rollout status` |

---

## Guidelines for Future Test Authors

---

### One top-level function per independent case

**Do this:**
```go
func runMyFeature(t *testing.T, secure bool, ns string) {
    t.Helper()
    cfg := suite.Config()
    if cfg.EnableCNI && secure {
        t.Skip("skipping: CNI + secure not supported")
    }
    // ... test logic
}

func TestMyFeature_Secure_DefaultNamespace(t *testing.T)   { runMyFeature(t, true, "default") }
func TestMyFeature_Insecure_DefaultNamespace(t *testing.T) { runMyFeature(t, false, "default") }
```

**Not this:**
```go
func TestMyFeature(t *testing.T) {
    for _, c := range []struct{ secure bool; ns string }{...} {
        t.Run(c.name, func(t *testing.T) { ... })
    }
}
```

**Exception**: sub-cases that share expensive setup that cannot be duplicated (e.g., a 10-minute cluster bootstrap all cases reuse). Document the trade-off explicitly.

---

### Test function names are stable identifiers

`test-timings.json` maps function names to observed durations. Renaming a function invalidates its stored timing — it falls back to `DEFAULT_TEST_SECONDS = 600s` and may be mis-binned for the first post-rename run.

Naming convention: `TestPackage_Feature_CaseName` in PascalCase:
```
TestPartitions_Connect_DefaultNamespace
TestPartitions_Sync_MirrorK8SACLs
TestConnectInject_ProxyLifecycleShutdown_Secure_DrainListeners_GracePeriod5
```

---

### Declare how many clusters your test actually uses

Before adding a test to a multi-cluster package:
```bash
grep -n "env.Context\|env.DefaultContext" acceptance/tests/<pkg>/*.go
```

If only `DefaultContext(t)` (dc1) or `Context(t, 1)` (dc2) appear, add `num-clusters: "2"` to the package entry in `kind_acceptance_test_packages.yaml`. Not doing this wastes up to 14 min of CNI setup per runner.

---

### Add timing estimates when you add tests

```json
"mypkg": {
  "TestMyFeature_Secure": 300,
  "TestMyFeature_Insecure": 280
}
```

Use a similar test as a reference. After the first successful CI run on the default branch, `update-test-timings` replaces your estimate with observed data.

---

### Never use unconditional sleeps for external readiness

```bash
# Wrong
sleep 120

# Right
kubectl wait --for=condition=Ready pods --all -n my-namespace --timeout=180s
```

```go
// Wrong
time.Sleep(10 * time.Second)

// Right
retry.RunWith(&retry.Counter{Count: 30, Wait: 2 * time.Second}, t, func(r *retry.R) {
    // check condition
})
```

---

### Put skip conditions inside the shared helper

When splitting a table-driven test, skip conditions belong in the private helper — not in individual wrappers. The helper is authoritative for what the test supports across all environments.

---

### Port-forwarded servers are not immediately ready

After establishing a port-forward, do not assume the remote endpoint is accepting connections immediately. Use a readiness probe before running assertions. `monitorPortForwardedServer` already handles this for reconnect paths — replicate the pattern in any custom forwarding logic.

---

## Alternatives Evaluated (Not Implemented)

### Upgrade to `m6a.2xlarge` runners

**Why not**: The `num-clusters` fix eliminated unnecessary cluster creation for the packages actually hitting memory pressure (partitions, wan-fed). Larger runners cost ~2× per minute without reducing wall time. Revisit if peering or sameness sees OOM kills.

---

### Move dual-stack variants to nightly

**Idea**: The 3 dual-stack CI variants account for ~300 of the 620 jobs dispatched per PR. Moving them to nightly would cut total job count roughly in half — reducing queue contention and dropping wall-clock time to ~35–40 min with no further splitting needed.

**Why not yet**: Requires product team sign-off — dual-stack regressions would be detected up to 24 hours after merge rather than at PR time.

**When to revisit**: When the team wants to trade detection latency for CI speed.

---

### Pre-stage images with `kind load docker-image`

**Idea**: Use `kind load docker-image` to copy images directly into the containerd store of each kind node, eliminating node-side `crictl pull`.

**Why not**: `kind load docker-image` is sequential per image per cluster. With ~12 images × 4 clusters, the serial copy loop is comparable to the current parallel `crictl pull`. The retry fix is sufficient for the 502 failure class.

---

## What's Next

The execution ceiling is now **~33 min** (vault CNI runners). Wall-clock time is **~60–65 min**, dominated by runner queue wait (up to 27 min) from dispatching ~620 jobs simultaneously.

### Reduce runner queue contention

**Option A — Consolidate over-split packages**: Some test cases (~100s each) run as dedicated runners when they could be grouped 3–4 per runner without exceeding the 20-min shard target. Reducing total jobs from ~620 to ~500 would lower pool saturation.

**Option B — Additional runner capacity**: CNI dual-stack runners showed the worst queue waits (up to 27 min). More runners on those labels would directly reduce wall time with no code changes.

### Move dual-stack variants to nightly

See Alternatives Evaluated above. This is the highest-leverage option available with no code changes.

### Audit remaining packages for `num-clusters` opportunities

Packages not yet audited: `connect`, `sameness`. Use:
```bash
grep -n "env.Context(t, [2-9])" acceptance/tests/<pkg>/*.go
```
If no results, the package uses at most 2 clusters and can have `num-clusters: "2"` added.

### Automated ceiling alerting

Add a post-test step that reads shard timings and posts a warning if any shard exceeds a threshold (e.g., 25 min). Turns ceiling detection from reactive to proactive.

---

## Appendix: Key Files

| File | Repo | Purpose |
|---|---|---|
| `control-plane/build-support/scripts/generate_test_matrix.py` | consul-k8s | Reads timings + YAML, emits GitHub Actions matrix |
| `acceptance/ci-inputs/kind_acceptance_test_packages.yaml` | consul-k8s | Per-package metadata (runner index, num-clusters, etc.) |
| `acceptance/ci-inputs/test-timings.json` | consul-k8s | Per-function duration history; source of truth for bin-packing |
| `acceptance/framework/portforward/port_forward.go` | consul-k8s | Port-forward reconnect logic |
| `acceptance/framework/vault/vault_cluster.go` | consul-k8s | Vault auth method configuration |
| `.github/workflows/reusable-kind-acceptance.yml` | consul-k8s-workflows | Regular kind acceptance workflow |
| `.github/workflows/reusable-kind-cni-acceptance.yml` | consul-k8s-workflows | CNI kind acceptance workflow |
