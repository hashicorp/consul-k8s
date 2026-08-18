# Plan: Closing the gaps in the Consul server operator migration

Branch: `jm/server-operator` (commits `861d9a26e`..`25696b3d1`)

This plan covers the work needed to take the `ConsulCluster` operator from
"basic install works" to a real replacement for `server-statefulset.yaml`.
Findings are grouped into phases ordered by dependency and risk. Phase 1 and 2
are blocking; nothing else is worth doing until pods reliably start and data
survives a pod restart.

---

## Open decisions

These change the shape of the work and should be settled before Phase 2 starts.

### D1. Pod identity and PVC binding — **blocking**

`generatePodName` (`control-plane/controllers/consulcluster/pod.go:379`) mints
`<cluster>-server-<random-hex>` and derives the PVC name from it. Any pod loss
produces a new name, a new empty PVC, and an orphaned old PVC. This is the root
cause of most of Phase 2.

| Option | Pros | Cons |
|---|---|---|
| **A. Stable ordinals** (`<cluster>-server-0..N-1`), PVC `data-<cluster>-server-N`, operator reconciles the set | Keeps the operator's per-pod control (rolling upgrade gating, raft-aware scale-down); PVCs reattach; predictable Consul node names | Operator must reimplement identity/slot bookkeeping a StatefulSet gives free |
| **B. Operator owns a StatefulSet** | Identity, PVC reattachment, ordered rollout all free | Loses the per-pod raft-health gating that motivated the operator; largely reverts the migration |
| **C. Keep random names, track PVC reuse in status** | Smallest diff | Fragile; status becomes the source of truth for storage binding; still loses stable Consul node names |

**Recommendation: A.** It preserves the reason the operator exists while
restoring the guarantees the StatefulSet provided. The rest of this plan assumes A.

### D2. Feature-parity scope for the first shippable version

Phase 5 is large (ACLs, Vault, federation, admin partitions, snapshot agent,
enterprise license, audit logs, Datadog, OpenShift...). Options:

- **Gate the operator behind an opt-in flag** (e.g. `server.experimentalOperator.enabled`),
  keep `server-statefulset.yaml` as the default path, and close parity
  incrementally. Lets Phases 1–4 ship without regressing existing users.
- **Block the migration on full parity.** Safer for users, much longer runway.

**Recommendation: opt-in flag.** Restoring `server-statefulset.yaml` from
`a23566ca0` and gating both paths on one value makes Phases 1–4 shippable and
turns Phase 5 into independent, reviewable increments. This also un-breaks the
332 orphaned bats tests immediately (Phase 6).

### D3. Conditions type

`ConsulClusterStatus.Conditions` uses the repo's hand-rolled
`v1alpha1.Conditions` (`control-plane/api/v1alpha1/status.go:12`), which is
built for the config-entry `Synced` condition and has no upsert helper. Using
`[]metav1.Condition` instead unlocks `meta.SetStatusCondition` and
`kubectl wait --for=condition=Ready`. **Recommendation: switch to
`[]metav1.Condition`** — the CRD is `v1alpha1` and unreleased, so this is free now
and expensive later.

---

## Phase 1 — Pods must actually start and become ready

Every item here reproduces with `helm template consul . --set global.tls.enabled=true`.

- [ ] **1.1 Wire default TLS secret names.** `charts/consul/templates/server-consulcluster.yaml:63-70`
  only emits `caSecretName`/`serverCertSecretName` when the user supplies them,
  but the default path is the chart's own `tls-init` job creating
  `<fullname>-ca-cert` / `<fullname>-server-cert`. The operator then builds
  Secret volumes with an empty `secretName` (`pod.go:262-282`).
  *Fix:* default to the chart-generated names; also honour
  `global.tls.caCert.secretKey` (currently the operator hardcodes `tls.crt`).
  *Accept:* `helm template --set global.tls.enabled=true` yields both secret
  names; a TLS install reaches Ready.

- [ ] **1.2 Remove `-config-dir=/consul/tls-config`.** `pod.go:165` points at a
  directory that is never mounted; `consul agent` exits on a missing config dir.
  TLS settings are already emitted into `server.json` by `configmap.go`.
  *Accept:* no `tls-config` reference remains; TLS pod starts.

- [ ] **1.3 Fix the inverted readiness probe.** `pod.go:206` selects HTTPS only
  when `httpsOnly` is *false*. With `httpsOnly: true` the ConfigMap sets
  `ports.http: -1` while the probe still hits `http:8500`, so pods never go
  Ready — and `httpsOnly` is the chart default when TLS is on.
  *Fix:* HTTPS whenever `TLS.Enabled`; keep HTTP only when TLS is off. Port the
  StatefulSet's timings (`initialDelaySeconds: 5`, `periodSeconds: 3`,
  `failureThreshold: 2`) rather than the current 30s/10s/3.
  *Accept:* unit test covering all four `{Enabled, HTTPSOnly}` combinations.

- [ ] **1.4 Stop hardcoding `-domain=cluster.local`.** `pod.go:158`. Consul's
  `-domain` is the Consul DNS domain (`.Values.global.domain`, default `consul`),
  not the Kubernetes cluster domain — this breaks `.consul` resolution.
  *Fix:* add `spec.domain` to the CRD, default `consul`, wire from `global.domain`.

- [ ] **1.5 Wire `gossipEncryption.autoGenerate`.** `server-consulcluster.yaml:55`
  only emits the block when both `secretName` and `secretKey` are set, so the
  `gossip-encryption-autogenerate-job` still runs but secure installs silently
  run **unencrypted**. `acceptance/tests/basic/basic_test.go` sets exactly this flag.
  *Fix:* when `autoGenerate`, emit `secretName: <fullname>-gossip-encryption-key`,
  `secretKey: key`. Also default `SecretKey` in the operator
  (`ConsulGossipSpec.SecretKey` is optional but `pod.go` uses it unguarded).
  *Accept:* `consul keyring -list` shows a key on a secure install.

- [ ] **1.6 Fix `exposeGossipAndRPCPorts`.** `pod.go:196-200` sets `hostPort` on
  *every* container port including HTTP and DNS; the StatefulSet limited it to
  serf/RPC/gRPC. `ADVERTISE_IP` also still uses `status.podIP` (`pod.go:129`)
  where the StatefulSet used `status.hostIP` for this case, so external client
  agents cannot reach the servers.
  *Accept:* only 8300/8301/8302/8502 get `hostPort`; advertise address is the host IP.

- [ ] **1.7 Restore the `/tmp` emptyDir.** The container runs with
  `readOnlyRootFilesystem: true` (`pod.go:~290`) but has no writable `/tmp`; the
  StatefulSet mounted one.

- [ ] **1.8 Make `/consul/extra-config` reachable.** The emptyDir is mounted
  (`pod.go:229`) but never passed as a `-config-dir`, so it is inert. Either pass
  it (needed by Phase 5's locality-init work) or drop the volume.

- [ ] **1.9 Also honour `server.extraVolumes[].load`.** `server-consulcluster.yaml`
  mounts extra volumes at `/consul/userconfig/<name>` but never adds a
  corresponding `-config-dir`, so `load: true` is silently ignored.
  *Fix:* add a `loadConfig` flag to `ConsulPodPolicy.ExtraVolumeMounts` (or a
  dedicated `extraConfigDirs` field) and append `-config-dir` for each.

- [ ] **1.10 Fix the Consul API client for TLS and ACLs.** `consul.go:31`
  hardcodes plain HTTP on port 8500, so `removeDeadRaftPeers` and
  `waitForGossipLeave` break under `httpsOnly` and under ACLs.
  *Fix:* build the client from the cluster spec (scheme, CA cert, ACL token from
  the bootstrap-token Secret once Phase 5 lands ACLs).

---

## Phase 2 — Data integrity and lifecycle

Assumes **D1 option A**.

- [ ] **2.1 Stable pod identity and PVC reattachment.** Replace
  `generatePodName` (`pod.go:379`) with ordinal slot allocation: compute the set
  of occupied ordinals from existing pods/PVCs, create into the lowest free slot,
  and bind `data-<cluster>-server-<ordinal>`. A replaced pod must re-bind its
  existing PVC.
  *Accept:* deleting a server pod produces a same-named replacement bound to the
  original PVC; no orphaned PVCs; Consul node names stable across restarts.

- [ ] **2.2 Classify pod phases correctly.** `controller.go:125-131` buckets
  everything that is not `Pending`/terminating into `running`, so `Failed`,
  `Succeeded`, and `Unknown` pods keep the cluster at desired size and are never
  replaced. Nothing else recreates them — these are bare Pods.
  *Fix:* treat `Failed`/`Succeeded` as replaceable and delete+recreate them;
  handle `Unknown` (node lost) with a grace period before replacement.
  *Accept:* unit test where a pod is `Failed` and the operator replaces it.

- [ ] **2.3 Make `persistentVolumeClaimRetentionPolicy: Retain` actually retain.**
  PVCs get an owner reference to the ConsulCluster (`pod.go:65-68`, plus a
  redundant `ctrl.SetControllerReference`), so the Kubernetes GC deletes them on
  CR deletion regardless of `WhenDeleted`. The `Retain` branch at
  `controller.go:220` only skips the explicit delete.
  *Fix:* set the owner reference only when the effective policy is `Delete`, or
  strip it during `reconcileDelete` before removing the finalizer.
  *Accept:* envtest confirming PVCs survive CR deletion under `Retain`.

- [ ] **2.4 Raft-aware scale-down.** `controller.go:235` picks
  `running[len(running)-1]` — with random suffixes that is an arbitrary pod,
  possibly the raft leader. It also deletes the pod *before* waiting for gossip
  leave and never removes the raft peer for the pod it just removed.
  *Fix:* with ordinals, always remove the highest ordinal; never remove the
  leader without a step-down; sequence as *drain → `consul leave` → wait for
  gossip departure → remove raft peer → delete pod → handle PVC*.

- [ ] **2.5 Restart or reload servers on config change.** `ensureConfigMap`
  updates the ConfigMap, but nothing restarts the pods and Consul does not reload
  on its own — the StatefulSet used a `consul.hashicorp.com/config-checksum` pod
  annotation to force a rollout.
  *Fix:* stamp a config checksum annotation on pods and roll them one at a time
  (reusing the Phase 2.6 gate) when it changes.

- [ ] **2.6 Gate rolling upgrades on raft health.** `controller.go:298`
  (`upgradeOnePod`) patches the container image in place with no autopilot/raft
  health check between pods, so a bad image can take out quorum.
  *Fix:* before moving to the next pod, require the previous one Ready, present
  in `Operator().AutopilotServerHealth()`, and the cluster to have a leader.
  *Accept:* unit/envtest asserting the operator stops after one pod when the
  cluster is unhealthy.

---

## Phase 3 — Reconciliation completeness

- [ ] **3.1 Reconcile drift on owned objects.** `ensureHeadlessService`,
  `ensureClientService`, `ensureExposeService` (`services.go`),
  `ensureServiceAccount` (`serviceaccount.go`), and `ensurePodDisruptionBudget`
  (`pdb.go`) all `Get` and return `nil` when the object exists. Only the
  ConfigMap is updated. Editing annotations, PDB `maxUnavailable`, or the
  expose-service type has no effect after first creation.
  *Fix:* use `controllerutil.CreateOrUpdate` (or an explicit build-desired /
  diff / patch) for each.
  *Accept:* a test that mutates each spec field and asserts the live object converges.

- [ ] **3.2 Reconcile pod spec drift.** Only `image` and the version label are
  patched today. Resources, tolerations, node selector, affinity, env, and
  volumes never change on an existing pod.
  *Fix:* hash the pod spec into an annotation and roll pods whose hash drifts,
  through the same one-at-a-time gate as 2.6.

- [ ] **3.3 Populate `Status.Conditions`.** Declared in the CRD, never written.
  Reconcile errors surface only in operator logs (`controller.go:99`), so
  `kubectl get consulcluster` and GitOps tooling see nothing.
  *Fix:* per **D3**, switch to `[]metav1.Condition` and set `Ready`,
  `QuorumHealthy`, and `ConfigSynced` via `meta.SetStatusCondition`, with
  `Reason`/`Message` carrying the wrapped reconcile error.
  *Accept:* `kubectl wait --for=condition=Ready consulcluster/consul` works.

- [ ] **3.4 Code hygiene.**
  - `gofmt -w control-plane/controllers/consulcluster/pod.go control-plane/subcommand/consul-cluster-operator/command.go` (both currently fail `gofmt -l`).
  - Delete unused `removeDeadRaftPeer` (`consul.go:39`) — the controller has its
    own inline copy at `controller.go:263`.
  - Delete `setOwner` (`services.go:74`), which ignores its context and just
    wraps `ownerRef`.
  - Drop the unused `*runtime.Scheme` parameter from `ownerRef` (`controller.go:420`),
    or use it via `controllerutil.SetControllerReference`.
  - Remove the duplicate ownership call in `ensurePVC` (manual `OwnerReferences`
    *and* `ctrl.SetControllerReference`).
  - Reconsider the fixed `requeueAfterSafetyNet = 30 * time.Second` poll now that
    `Owns()` watches are wired.

---

## Phase 4 — Chart wiring left dangling

- [ ] **4.1 Restore the `hasDNS: "true"` pod label.** `templates/dns-service.yaml:37`
  selects on it; server pods no longer carry it, so the Consul DNS service has
  zero endpoints whenever `client.enabled=false`.
  *Fix:* add it in `serverPodLabels` (`controller.go:~330`) or via the CR's
  `pod.labels`. Note it must be in the label set *before* the headless Service
  selector is built.

- [ ] **4.2 Resolve the `<fullname>-ui` name collision.** The operator's "client
  service" is `<cluster>-ui` (`controller.go:411`) — the exact name of the
  chart's `templates/ui-service.yaml`. Whichever exists first wins and
  `ensureClientService` silently no-ops.
  *Fix:* rename the operator's service (e.g. `<cluster>-server-client`) or drop
  it and let the chart own the UI service. Decide which component owns UI exposure.

- [ ] **4.3 Collapse the duplicate expose services.** `expose-servers-service.yaml`
  renders `<fullname>-expose-servers` while the operator creates
  `<fullname>-expose` (`services.go:~185`). Only the chart's name is what
  peering and admin-partition docs and other templates reference.
  *Fix:* have the operator adopt the `-expose-servers` name and delete the chart
  template, or drop `ExposeService` from the CRD and keep it chart-owned.

- [ ] **4.4 Re-attach the orphaned validations.** `consul.validateMetricsConfig`,
  `consul.validateDatadogConfiguration`, and `consul.validateExtraConfig` were
  only invoked from `server-statefulset.yaml` and are now called from nowhere.
  The same applies to the ~17 inline `fail` guards at the top of that file:
  federation ⊕ adminPartitions, `bootstrapExpect >= replicas`,
  gossip/license/bootstrap-token `secretName`+`secretKey` pairing,
  `consulServerRole` required with Vault, and the `disableFsGroupSecurityContext`
  removal error.
  *Fix:* move them into `server-consulcluster.yaml` (or a shared
  `_server-validations.tpl` included by both paths).
  *Accept:* bats tests asserting each `fail` still triggers.

- [ ] **4.5 Move the CRD into `templates/`.** `charts/consul/crds/consulclusters.yaml`
  is the only consul-k8s CRD outside `templates/`. Helm never upgrades or deletes
  anything in `crds/`, so schema changes will not roll out on `helm upgrade`.
  *Fix:* relocate to `templates/crd-consulclusters.yaml` following the existing
  `crd-controlplanerequestlimits.yaml` pattern, and drop the
  `kubectl apply -f <chart>/crds` shim in the acceptance framework (4.8).

- [ ] **4.6 Fix `cli status` and `cli debug`.** `cli/cmd/status/status.go:285`
  lists StatefulSets with `app=consul,chart=consul-helm,component=server`, so
  `consul-k8s status` now reports no servers. `cli/cmd/debug/debugPodLogs.go`
  makes the same assumption.
  *Fix:* query the ConsulCluster CR (and its pods by label) instead.

- [ ] **4.7 Clean up `values.yaml`.** `charts/consul/values.yaml:3819-3821` ends
  with an orphaned comment block describing `server.clusterOperator` with no keys
  under it — the real block is at `charts/consul/values.yaml:1558`.
  Also confirm `values.schema.json` (if the chart has one) covers the new keys.

- [ ] **4.8 Fix the acceptance framework.**
  - `acceptance/framework/consul/helm_cluster.go:262-267` and `:381-385` still
    delete and assert-absent StatefulSets on teardown; they need to clean up
    ConsulCluster CRs, server pods, and PVCs.
  - `helm_cluster.go:160-165` applies CRDs with `kubectl apply -f <chart>/crds`
    and **swallows the error**, so a CRD failure surfaces later as a confusing
    helm error. Remove once 4.5 lands; until then, fail loudly.

---

## Phase 5 — Feature parity with `server-statefulset.yaml`

Everything below existed in `server-statefulset.yaml` / `server-config-configmap.yaml`
at `a23566ca0` and has no path through the CRD. Assumes **D2**: each item is an
independent increment behind the opt-in flag.

Ordered by how many users it blocks:

- [ ] **5.1 ACLs** (`global.acls.manageSystemACLs`) — `acl-config.json`,
  `initial_management` from the bootstrap-token Secret, agent/replication token
  wiring. Blocks every secure install; `server-acl-init` already assumes servers exist.
- [ ] **5.2 Enterprise license** (`global.enterpriseLicense`) — `CONSUL_LICENSE_PATH`
  and the license Secret volume.
- [ ] **5.3 Vault secrets backend** (`global.secretsBackend.vault`) — the full
  `vault.hashicorp.com/*` annotation set, agent injection, and the
  `/vault/secrets/*` file paths for gossip key, server cert, CA, and tokens.
- [ ] **5.4 Snapshot agent sidecar** (`server.snapshotAgent`) —
  `server-snapshot-agent-configmap.yaml` still renders with no consumer.
- [ ] **5.5 WAN federation** (`global.federation`) — `federation-config.json`,
  primary datacenter/gateways, `enable_mesh_gateway_wan_federation`.
- [ ] **5.6 Admin partitions** (`global.adminPartitions`).
- [ ] **5.7 Server locality** — the `locality-init` init container running
  `consul-k8s-control-plane fetch-server-region`; without it, zone-aware failover
  is gone. Depends on 1.8.
- [ ] **5.8 `global.recursors`** and DNS config.
- [ ] **5.9 Audit logs** (`server.auditLogs`) — including the "ACLs must be
  enabled" validation from the old ConfigMap template.
- [ ] **5.10 Datadog integration** (`global.metrics.datadog`) — pod annotations,
  DogStatsD UDS hostPath volume, `tags.datadoghq.com/*` labels.
- [ ] **5.11 `global.trustedCAs`** — the CA rehash preamble and `SSL_CERT_DIR`.
- [ ] **5.12 OpenShift** (`global.openshift.enabled`) — securityContext handling
  and the SCC role reference (`server-securitycontextconstraints.yaml` still renders).
- [ ] **5.13 `global.dualStack`** — IPv6 probe and address handling.
- [ ] **5.14 `global.experiments`** — the `-hcl="experiments=[...]"` flag.
- [ ] **5.15 `server.updatePartition`** — still referenced by
  `server-acl-init-job.yaml:20` and `server-acl-init-cleanup-job.yaml:5`, which
  now gate on a value nothing implements.
- [ ] **5.16 `server.ports.serflan.port`** — currently hardcoded to 8301 in
  `pod.go`/`services.go`; `server-podsecuritypolicy.yaml:34` still reads the value.
- [ ] **5.17 PodSecurityPolicy / Role wiring** — `server-role.yaml`,
  `server-rolebinding.yaml`, `server-podsecuritypolicy.yaml` still bind to the
  `<fullname>-server` ServiceAccount, which the operator now creates. Confirm
  ordering (the SA must exist before the binding is used) and that
  `global.enablePodSecurityPolicies` still works.
- [ ] **5.18 Restore `consul.hashicorp.com/mesh-inject: "false"`** on server pods
  (`pod.go:~100` sets only `connect-inject`).
- [ ] **5.19 `CONSUL_DISABLE_PERM_MGMT`** and the `docker-entrypoint.sh` entrypoint
  — verify data-dir permissions still work without them, especially with the
  operator's default pod securityContext, which omits the chart's `fsGroup: 1000`.

---

## Phase 6 — Tests

- [ ] **6.1 Un-break the 332 orphaned bats tests.** These still target deleted
  templates and fail outright:

  | file | tests |
  |---|---|
  | `charts/consul/test/unit/server-statefulset.bats` | 193 |
  | `charts/consul/test/unit/server-config-configmap.bats` | 100 |
  | `charts/consul/test/unit/server-disruptionbudget.bats` | 15 |
  | `charts/consul/test/unit/server-service.bats` | 11 |
  | `charts/consul/test/unit/server-serviceaccount.bats` | 8 |
  | `charts/consul/test/unit/server-tmp-extra-config-configmap.bats` | 5 |

  Under **D2** they are restored alongside `server-statefulset.yaml` and run
  against the legacy path. If D2 goes the other way, each must be ported or
  deliberately deleted with the coverage moved to `server-consulcluster.bats`.

- [ ] **6.2 Add bats coverage for the new templates** — there is currently none
  for `server-consulcluster.yaml`, `consul-cluster-operator-deployment.yaml`,
  `-clusterrole.yaml`, `-clusterrolebinding.yaml`, `-serviceaccount.yaml`, or
  `-leader-election-role.yaml`. Mirror the value-coverage style of
  `server-statefulset.bats`: every `.Values.server.*` key that reaches the CR
  gets a test.

- [ ] **6.3 Deepen Go unit tests.** `controller_test.go` has 16 fake-client
  happy-path tests. Missing: TLS config generation, gossip wiring, readiness
  probe matrix (1.3), rolling upgrade, pod replacement after failure (2.2),
  PVC reattachment (2.1), `Retain` semantics (2.3), scale-down ordering (2.4),
  and drift reconciliation (3.1/3.2).

- [ ] **6.4 Add envtest coverage** for the finalizer/GC interactions in 2.3 —
  the fake client does not model garbage collection, so the `Retain` bug is
  invisible to the current suite.

- [ ] **6.5 Acceptance tests.** Add a `ConsulCluster` case to
  `acceptance/tests/server/`: scale up, scale down, rolling upgrade, pod deletion
  with PVC reattachment, and CR deletion under both retention policies.

- [ ] **6.6 Regenerate the CRD from the types** rather than hand-editing
  `consulclusters.yaml`, and wire it into whatever `make ctrl-manifests` /
  controller-gen target the repo already uses, so the schema cannot drift from
  `consulcluster_types.go`.

---

## Suggested sequencing

1. **D1, D2, D3** settled.
2. **Phase 1** — one PR; makes secure installs work at all.
3. **Phase 2.1 + 2.2 + 2.3** — one PR; the data-loss fixes. Highest review scrutiny.
4. **Phase 2.4 – 2.6** — one PR; lifecycle safety.
5. **Phase 3** — one PR; drift + conditions + hygiene.
6. **Phase 4** — one PR; chart wiring. Ships alongside 6.1/6.2.
7. **Phase 5** — one PR per item, ordered 5.1 → 5.19.

Phases 1–4 plus 6.1–6.3 constitute a defensible "experimental operator, opt-in"
release. Phase 5 is what makes it the default.
