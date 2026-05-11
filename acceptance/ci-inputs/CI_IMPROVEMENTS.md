# CI Reliability & Performance Improvements

## Background

Acceptance tests on kind take ~2 hours end-to-end and have historically produced a high rate of
non-deterministic failures — tests that fail on one run and pass on the next with no code change.
This document captures the root-cause analysis, approaches evaluated, what was implemented, and
what remains on the table.

---

## Root Cause Analysis

### 1. Wall time dominated by a handful of large test functions

The test matrix shards across ~36 runners using `generate_test_matrix.py`. The bin-packing works
at the `Test*` function granularity. Several functions contain 6–10 table-driven cases that each
spin up independent clusters — they cannot be further subdivided by the sharding logic and so
land on a single runner, becoming the ceiling for the whole run.

Top offenders from `test-timings.json` at the time of analysis:

| Function | Package | Duration | Cases |
|---|---|---|---|
| `TestPartitions_Connect` | partitions | 55 min | 6 |
| `TestConsulDNSProxy_WithPartitionsAndCatalogSync` | consul-dns | 34 min | ~4 |
| `TestTerminatingGatewayNamespaceMirroring` | terminating-gateway | 30 min | 8 |
| `TestPeering_ConnectNamespaces` | peering | 29 min | 6 |
| `TestWANFederationFailover` | wan-federation | 23 min | 2 |
| `TestConsulDNS` | consul-dns | 23 min | 10 |
| `TestConnectInjectNamespaces` | connect | 14 min | 4 |

### 2. Transient Docker registry failures

Two distinct failure points:

- **Host-side** (`docker pull` in "Pull docker images" step): hitting `docker-mirror.hashi.app`
  which returned 502 under concurrent load from many runners starting simultaneously.
- **Node-side** (`crictl pull` inside kind nodes in "Load docker images" steps): hitting
  `registry-1.docker.io` directly for images not on the HashiCorp mirror, also returning 502.

Neither was an image availability issue — the images exist. Both were registry instability under
load spikes.

### 3. Port-forward reliability under pod restarts

Tests that use `secure: true` establish a port-forward to the Consul gRPC-TLS port (8501).
`monitorPortForwardedServer` detects TCP breaks and reconnects, but `ForwardPortE` returning `nil`
only means the `kubectl port-forward` process launched — not that the remote Consul pod is
accepting connections. During the gap between process start and pod readiness, tests receive
`connection refused`, and their `retry.RunWith` counters can exhaust before the tunnel is actually
usable.

### 4. Unnecessary cluster setup for 2-cluster test packages

The CNI and regular acceptance workflows unconditionally create 4 kind clusters (dc1–dc4) per
runner. Several test packages — most notably `wan-federation` — only ever call
`env.DefaultContext(t)` (dc1) and `env.Context(t, 1)` (dc2). The dc3/dc4 setup (cluster
creation + image loading + Calico install) consumes ~12 minutes and ~8 GB of memory per runner
for no benefit.

### 5. Silent runner termination

Runners killed by the OS OOM killer or by job cancellation produce blank step entries in the
GitHub Actions UI and `BlobNotFound` errors in log storage. Without an explicit `timeout-minutes`
on the job, the termination is invisible — there is no timeout error, just missing output.

### 6. `kind` not on PATH during step cleanup

`helm/kind-action` with `install_only: true` adds `kind` to `$GITHUB_PATH`, which only applies
to subsequent steps. The action's own post-cleanup script runs in the same step and cannot find
`kind`, failing with `cleanup.sh: kind: command not found`. With `ignore_failed_clean: false`
(the default), this hard-fails the step and causes all subsequent cluster-creation steps to be
skipped, resulting in a complete runner loss.

---

## Approaches Evaluated

### Option A — Split ceiling test functions into top-level `Test*` functions

**Idea**: Each table-driven case becomes its own `TestPkg_Case` function. The matrix generator
regex (`^func (Test\w+)\(t \*testing\.T\)`) auto-discovers them; each gets its own runner via
bin-packing.

**Projected ceiling after splitting**:

| Test | Current | Cases | Per-case | New ceiling |
|---|---|---|---|---|
| `TestPartitions_Connect` | 55 min | 6 | ~9 min | 9 min |
| `TestWANFederationFailover` | 23 min | 2 | ~11 min | **11 min ← new ceiling** |
| `TestConsulDNS` | 23 min | 10 | ~2 min | 2 min |
| others | — | — | — | ≤9 min |

Expected total CI time after splitting: **~40 min** (from ~2 hours). A 5× improvement.

**Pros**:
- Largest single lever available. Eliminates the ceiling effect entirely.
- No infrastructure cost — same runners, same images.
- `test-timings.json` self-corrects after the first post-split run.

**Cons**:
- Significant code change: 6 files, ~200 lines of refactoring across high-traffic test packages.
- Increases runner count from ~36 to ~50+, which raises total runner-minute cost (though total
  wall time drops, peak concurrent runners rise).
- Naming convention must be established and enforced (`TestPkg_OriginalName_CaseName`).

**Status**: Evaluated, not yet implemented. Highest-impact remaining item.

---

### Option B — Upgrade runners to `m6a.2xlarge` for multi-cluster packages

**Idea**: Tests like `partitions` and `peering` spin up 2–4 kind clusters simultaneously,
competing for 16 GB RAM on `m6a.xlarge`. Upgrading to `m6a.2xlarge` (8 vCPU, 32 GB) reduces
memory pressure and speeds up concurrent cluster scheduling.

**Pros**:
- Reduces OOM-kill risk for 4-cluster packages (partitions, peering, sameness).
- Speeds up `kind create cluster` and Calico convergence under higher parallelism.
- Can be applied selectively to only the heavy packages via the matrix.

**Cons**:
- ~2× cost per runner-minute for affected packages.
- Doesn't address wall-time ceiling (a slower test on a bigger machine is still slow).
- Requires workflow changes to pass per-package runner labels.

**Status**: Evaluated, not implemented. Worth revisiting if OOM kills persist after Option A.

---

### Option C — Move dual-stack variants to nightly

**Idea**: Currently 6 kind variants × 2 k8s versions = 12 suites per PR. The 3 dual-stack
variants test additive networking behavior that rarely breaks independently of core
connect/inject changes. Running them nightly instead of per-PR would halve runner consumption.

**Pros**:
- Halves PR CI cost (runner-minutes) with no test code changes.
- Dual-stack failures are still caught within 24 hours.

**Cons**:
- Requires product sign-off: defect detection is delayed from PR time to nightly.
- A dual-stack regression can slip into `main` before the nightly run catches it.

**Status**: Evaluated, not implemented. Requires team alignment.

---

### Option D — Pre-stage images using `kind load docker-image`

**Idea**: After pulling images to the host with `docker pull`, use `kind load docker-image` to
copy them into the kind node's containerd store. This eliminates all `crictl pull` network
traffic from within the nodes.

**Pros**:
- Eliminates node-side Docker 502 failures entirely (no network request from kind node).
- Faster than `crictl pull` (local copy vs. network fetch).

**Cons**:
- `kind load docker-image` is sequential per image per cluster. With 12 images × 4 clusters,
  the load loop takes comparable time to the current parallel `crictl pull` approach.
- The `consul-image` (Consul server) is intentionally excluded from pre-staging in the current
  setup and is still pulled by kind nodes at Helm deploy time — so node-side pulls are not
  fully eliminated without also pre-staging `consul-image`.
- Blast radius: if the host pull succeeds but the load fails, the error is harder to diagnose
  than a crictl failure (no retry mechanism in `kind load`).

**Status**: Partially implemented (retry + fallback logic for `crictl pull`). Full `kind load`
migration deferred.

---

## Changes Implemented

### Fix 1 — `kind` cleanup PATH issue (`ignore_failed_clean: true`)

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

Added `ignore_failed_clean: true` to the `helm/kind-action` step that installs kind with
`install_only: true`. The action's post-cleanup runs in the same step and cannot find `kind`
because `$GITHUB_PATH` additions only apply to subsequent steps. Ignoring the cleanup failure
prevents the entire runner from being lost.

---

### Fix 2 — Docker registry retry with mirror fallback

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

Added a `retry()` shell function (5 attempts, 10 s delay) wrapping every `docker pull` call in
the "Pull docker images" step. For `crictl pull` inside kind nodes, added the same retry loop
plus a fallback: if all retries against `docker.mirror.hashicorp.services` fail, retries against
`docker.io` directly with the same credentials.

---

### Fix 3 — Port-forward reconnect readiness probe

**File**: `acceptance/framework/portforward/port_forward.go`

`monitorPortForwardedServer` previously called `ForwardPortE` once and moved on. `ForwardPortE`
returning `nil` means only that the `kubectl port-forward` process launched — not that the remote
endpoint is accepting TCP connections. Added a probe loop (up to 30 s, 1 s interval) after a
successful `ForwardPortE`, checking `doneChan` each iteration so test cleanup is not blocked. If
the port is not accepting within the timeout, the tunnel is closed and the next ticker tick
retries the full reconnect.

Named constants (`reconnectProbeTimeout`, `reconnectProbeInterval`) replace inline literals.

---

### Fix 4 — `num-clusters` matrix field for 2-cluster packages

**Files**: `kind_acceptance_test_packages.yaml`, `generate_test_matrix.py`,
`reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

**Problem**: `wan-federation` only uses dc1 and dc2 (`DefaultContext` and `Context(t, 1)`), but
the workflow unconditionally creates and configures dc3 and dc4, wasting ~12 min and ~8 GB per
runner.

**Implementation**:

1. Added `num-clusters: "2"` to the `wan-federation` entry in `kind_acceptance_test_packages.yaml`.
2. Modified `generate_test_matrix.py` to extract and forward any extra YAML fields (beyond
   `runner`, `test-packages`, `run-filter`) to every shard matrix entry for that package. This
   makes `num-clusters` available to the workflow via `matrix['num-clusters']` without hardcoding
   package names in the workflow.
3. All dc3/dc4 steps in both workflows are gated with:
   ```yaml
   if: ${{ !matrix['num-clusters'] || fromJSON(matrix['num-clusters']) > 2 }}
   ```
   Missing field → create dc3/dc4 (default). `"2"` → skip dc3/dc4.
4. The `-kube-contexts` flag is now computed dynamically in the test-run shell:
   ```bash
   KUBE_CONTEXTS="kind-dc1,kind-dc2,kind-dc3,kind-dc4"
   if [[ "${{ matrix['num-clusters'] }}" == "2" ]]; then
     KUBE_CONTEXTS="kind-dc1,kind-dc2"
   fi
   ```

The `num-clusters` field propagates correctly to all shards of a package. Adding a new 2-cluster
package in the future requires only a one-field YAML change.

---

### Fix 5 — Explicit job timeout

**Files**: `reusable-kind-acceptance.yml`, `reusable-kind-cni-acceptance.yml`

Added `timeout-minutes: 120` to the `acceptance` job in both workflows. Without this, runner
terminations (OOM kill, AWS spot reclaim) produce silent `BlobNotFound` errors with no
indication of what failed or when. With the explicit timeout, GitHub Actions surfaces a clear
cancellation after 2 hours.

The 120-minute budget accounts for worst-case setup (~30 min for 4-cluster CNI with Calico) plus
the go test timeout (60 min) plus headroom.

---

## What Can Still Be Improved

### 1. Split ceiling test functions (Option A — highest impact)

This is the single largest remaining lever. The current ceiling is `TestWANFederationFailover`
at ~23 min (post `num-clusters` fix) or `TestPartitions_Connect` at ~55 min on non-split runs.
Splitting the 6 identified functions into per-case `Test*` functions drops the ceiling to ~11
min and total CI to ~40 min.

Recommended naming: `TestPkg_OriginalSuffix_CaseName` (e.g.,
`TestPartitions_Connect_DefaultNamespace`). Update `test-timings.json` at the same time to
pre-populate per-case estimates and avoid the `DEFAULT_TEST_SECONDS` bucket on first run.

### 2. Restrict `update-test-timings` to the default branch

The `update-test-timings` job in `test.yml` tries to commit and push to whatever branch the
workflow runs on. On PR branches this fails due to branch-protection rules (expected), but the
failure appears as a red job in the PR UI and causes confusion. Adding
`github.ref == 'refs/heads/main'` to the job condition restricts it to post-merge runs where the
push is safe.

### 3. Pre-load `consul-image` into kind nodes

The `consul-image` (Consul server) is currently absent from the "Load docker images" steps and
is pulled by kind nodes at Helm deploy time during tests. This is the one remaining network pull
from inside kind nodes. Pre-loading it (either via `kind load docker-image` after the host pull,
or by adding it to the `crictl pull` loop) eliminates the last class of node-side Docker
registry failures.

### 4. Larger runners for 4-cluster packages (Option B)

If OOM kills persist after test splitting reduces per-runner workload, selectively upgrading
`partitions` and `peering` runners to `m6a.2xlarge` (32 GB) is the next mitigation. The
`num-clusters` matrix field pattern established in Fix 4 can be extended to a `runner-size`
field, allowing per-package runner selection without workflow duplication.

### 5. Move dual-stack variants to nightly (Option C)

With team agreement, running the 3 dual-stack kind variants (CNI dual-stack, tproxy dual-stack,
acceptance dual-stack) only in nightly CI rather than per-PR would halve runner consumption and
cost. The tradeoff is a 24-hour defect detection delay for dual-stack-specific regressions.
