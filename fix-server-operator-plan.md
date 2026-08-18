# Consul Server Operator — Gap Closure Plan

Branch: `jm/server-operator` · Baseline: `main`

## 1. Where we are

`main` runs Consul servers from a 755-line Helm-templated StatefulSet. This branch replaces
that with a `ConsulCluster` CRD plus an operator, and deletes six templates:

| Deleted template | Lines | Replaced by |
| --- | ---: | --- |
| `server-statefulset.yaml` | 755 | `controllers/consulcluster/statefulset.go` + `pod.go` |
| `server-config-configmap.yaml` | 228 | `controllers/consulcluster/configmap.go` |
| `server-service.yaml` | 72 | `controllers/consulcluster/services.go` |
| `server-disruptionbudget.yaml` | 26 | `controllers/consulcluster/pdb.go` |
| `server-serviceaccount.yaml` | 23 | `controllers/consulcluster/serviceaccount.go` |
| `server-tmp-extra-config-configmap.yaml` | 21 | *(nothing — see G-30)* |

The workload primitive has since been moved back to a StatefulSet, with the operator
retained for the parts a StatefulSet cannot do: Raft-aware rollout gating and dead-peer
reaping. That change closed the identity, data-reattachment, and quorum-safety gaps.

**This document covers what is still missing.**

## 2. How these gaps were identified

1. Extracted every Helm value referenced by the six deleted templates: **115 distinct values**.
2. Extracted every value referenced by `server-consulcluster.yaml` today: **61 distinct values**.
3. Diffed the two sets. **67 values** are no longer consumed by anything.
4. Read each deleted template in full to catch behaviour not expressed as a value
   (init containers, probes, checksums, hardcoded flags).
5. Grepped every remaining chart template that names or selects a server resource, to find
   collisions between Helm-owned and operator-owned objects.

Reproduce step 1–3:

```sh
D=$(mktemp -d)
for f in server-statefulset server-config-configmap server-service \
         server-disruptionbudget server-serviceaccount server-tmp-extra-config-configmap; do
  git show main:charts/consul/templates/$f.yaml > "$D/$f.yaml"
done
cat "$D"/*.yaml | grep -oE '\.Values\.[A-Za-z0-9_.]+' | sed 's/^\.Values\.//' | sort -u > "$D/then.txt"
grep -oE '\.Values\.[A-Za-z0-9_.]+' charts/consul/templates/server-consulcluster.yaml \
  | sed 's/^\.Values\.//' | sort -u > "$D/now.txt"
comm -23 "$D/then.txt" "$D/now.txt"
```

## 3. Gap inventory

### P0 — Install is broken or silently wrong

| ID | Gap | Evidence |
| --- | --- | --- |
| ~~G-01~~ | ~~`consul-ui` Service has two owners.~~ **Resolved.** Decision D1 (below): Helm remains the delivery mechanism for cross-cutting objects. Deleted `buildClientService`/`ensureClientService`/`clientServiceName` from the operator; `ui-service.yaml` stays the sole owner of `<fullname>-ui`. Verified the chart's selector (`app`/`release`/`component`) still matches operator-created pods, since `serverPodLabels()` merges the CR's own `release` label down onto the pods. | `ui-service.yaml:5` |
| G-02 | **Consul DNS is dead.** `dns-service.yaml` selects `hasDNS: "true"`. `serverPodLabels()` never sets that label, so the `consul-dns` Service has no endpoints. `client-daemonset.yaml` selects on it too. | `dns-service.yaml:38` |
| ~~G-03~~ | ~~Two expose services.~~ **Resolved.** Same D1 answer: deleted `buildExposeService`/`ensureExposeService`/`exposeServiceName` and the `spec.exposeService` CRD field entirely; `expose-servers-service.yaml` (already wired to `global.adminPartitions`) stays the sole owner. | `expose-servers-service.yaml:10` |
| G-04 | **ACL bootstrap is not wired.** `server-acl-init-job.yaml` still runs and writes a bootstrap token Secret, but nothing puts `acl.tokens.initial_management` on the servers. `global.acls.bootstrapToken` reaches the CR only as the operator's *client* credential, never as agent config. `global.acls.replicationToken` is dropped entirely. | `configmap.go`, values diff |
| G-05 | **`peering.enabled` is hardcoded true.** `main` gated it on `global.peering.enabled`; the operator emits it unconditionally. Regression introduced by the port. | `configmap.go`, `main:server-config-configmap.yaml:63` |
| ~~G-06~~ | ~~No in-place upgrade path.~~ **Out of scope per D2** (below): this is a straight replacement, not an upgrade. `spec.dataVolumeName` is kept as a general customization knob but the chart no longer forces it to `data-<namespace>` for compatibility. An install alongside an existing `main`-based StatefulSet is a manual, undocumented operation — data migration is not attempted automatically. | `statefulset.go` |

### P1 — Supported configurations that no longer work

| ID | Gap | Values dropped |
| --- | --- | --- |
| G-10 | Enterprise license autoload | `global.enterpriseLicense.*` |
| G-11 | Snapshot agent sidecar | `server.snapshotAgent.*` (8 values) |
| G-12a | ~~Vault-backed secret sourcing~~ **Scope resolved, not a separate item.** Falls out of G-04/G-10/G-11's Secret-ref fields once those land — see D1 note below. `global.secretsBackend.vault.agentAnnotations` and `.consulServerRole`/`.ca.*` as used for Agent Injection have **no equivalent** — Vault Agent Injection itself is out of scope by design, not deferred. | `global.secretsBackend.vault.agentAnnotations`, `.consulServerRole`, `.ca.*` |
| G-12b | Vault as the Connect CA provider (`connect.ca_provider = "vault"`). Uses Consul's own native Kubernetes-auth-to-Vault, not the sidecar — config-file work only, no pod-template change. | `global.secretsBackend.vault.connectCA.*`, `.vaultNamespace` |
| G-13 | WAN federation | `global.federation.*`, `meshGateway.enabled` |
| G-14 | Admin partitions and Consul namespaces | `global.adminPartitions.*`, `global.enableConsulNamespaces`, `connectInject.consulNamespaces.*` |
| G-15 | Audit logging | `server.auditLogs.*` |
| G-16 | `auto_encrypt.allow_tls` | `global.tls.enableAutoEncrypt` |
| G-17 | Locality init container (`fetch-server-region` → `locality.json`) | `global.imageK8S` |
| G-18 | Trusted CA bundle + OpenSSL rehash | `global.trustedCAs` |
| G-19 | Datadog: pod labels, `ad.datadoghq.com/*` annotations, dogstatsd UDS volume, telemetry fields | `global.metrics.datadog.*` (6 values) |
| G-20 | Telemetry detail: `disable_hostname`, `enable_host_metrics`, `prefix_filter` | `global.metrics.disableAgentHostName`, `global.metrics.enableHostMetrics` |
| G-21 | UI config: `ui.enabled` is ignored (hardcoded on), no metrics provider/proxy or dashboard templates | `ui.enabled`, `ui.metrics.*`, `ui.dashboardURLTemplates.service` |
| G-22 | `global.experiments` → `-hcl="experiments=[…]"` | `global.experiments` |
| G-23 | Configurable serf LAN port; `retry_join` also omits the port | `server.ports.serflan.port` |
| G-24 | `global.logLevel` fallback and upper-casing | `global.logLevel` |
| G-25 | OpenShift security context handling | `global.openshift.enabled` |
| G-26 | Dual-stack `bind_addr: "::"` / `client_addr: "::"` | `global.dualStack.enabled` |
| G-27 | `global.extraLabels` on the StatefulSet and pods | `global.extraLabels` |
| G-28 | `updatePartition` canary control — the operator owns `partition` now, so this needs a deliberate API (e.g. `spec.rollout.pause`/`maxOrdinal`) rather than a passthrough | `server.updatePartition` |
| G-29 | `server-role.yaml` / `server-clusterrole.yaml` / PSP / SCC still reference the server ServiceAccount but are no longer validated against it | — |
| G-30 | `extraConfig` placeholder substitution: `main` copied the config through an emptyDir and `sed`-replaced `HOST_IP`, `POD_IP`, `HOSTNAME`. The operator merges `extraConfig` at template time, so those tokens now reach Consul literally. | `server.extraConfig` |

### P2 — Operator engineering quality

| ID | Gap |
| --- | --- |
| G-40 | `consulcluster_deepcopy.go` is hand-written, not `zz_generated.deepcopy.go`. It will drift from the types. |
| G-41 | `consulcluster_types.go` has **zero** `+kubebuilder` markers, and `crds/consulclusters.yaml` is hand-written with 4 validation keywords across ~390 lines. |
| G-42 | The 14 `fail` guards in `main:server-statefulset.yaml` (federation/adminPartitions exclusivity, `bootstrapExpect >= replicas`, paired secretName/secretKey checks) have no replacement — neither CRD validation nor a webhook. |
| ~~G-43~~ | ~~`ConsulClusterStatus.Conditions` is declared but never written.~~ **Type resolved per D3** (below): switched from the legacy custom `Conditions` type to `[]metav1.Condition`, matching the pattern already used by `RouteAuthFilterStatus` and `GatewayPolicyStatus` elsewhere in this API group. Nothing in the operator sets a condition yet — writing `Available`/`Progressing`/`RaftHealthy` in the reconcile loop is still open, tracked below. |
| G-44 | No acceptance-test coverage for operator-specific behaviour (rollout gating, dead-peer reaping, node-failure replacement). |
| G-45 | 5,209 lines of bats coverage were deleted with the templates. The config generation they covered now lives in Go and needs equivalent table tests. |

## 4. Plan

Ordered so that each phase leaves the branch installable and testable.

### Phase 1 — Make a default install correct (P0)

- [x] **G-01** ~~Decide ownership of `<fullname>-ui`.~~ Resolved via D1: deleted the operator's client Service. `ui-service.yaml` owns `<fullname>-ui`.
- [ ] **G-02** Add `hasDNS: "true"` to `serverPodLabels()`, or repoint `dns-service.yaml` and `client-daemonset.yaml` at the operator's label set. Prefer adding the label — it is load-bearing for two other templates and is cheaper than changing their selectors.
- [x] **G-03** ~~Delete the operator's expose service~~ Resolved via D1: deleted the operator's expose Service and the `spec.exposeService` CRD field. `expose-servers-service.yaml` owns `<fullname>-expose-servers`.
- [ ] **G-04** Add `spec.acls.replicationToken` and `spec.acls.initialManagementToken` to the CRD; emit `acl.tokens.*` into `server.json` from Secret refs, mirroring `main`'s `-hcl` flags. Verify `server-acl-init-job` completes end to end.
- [ ] **G-05** Gate `peering.enabled` on a new `spec.peering.enabled`, wired from `global.peering.enabled`.
- [x] **G-06** ~~Write and test the migration.~~ Out of scope per D2 — this is a straight replacement, no in-place upgrade path. Dropped the chart's forced `dataVolumeName: data-<namespace>` compatibility wiring; the field remains as a general escape hatch, defaulting to `"data"`.
- [ ] Document (README/upgrade notes) that installing this branch over a `main`-based release replaces the server StatefulSet — no automated data migration. This is the one documentation task D2 still leaves open.

### Phase 2 — Restore config-file parity (P1, low risk)

These are all `configmap.go` plus a CRD field and a chart passthrough. No pod-template changes.

- [ ] **G-16** `auto_encrypt.allow_tls`
- [ ] **G-15** `audit-logging.json`
- [ ] **G-13** `federation-config.json` (+ re-add the federation/adminPartitions/TLS/meshGateway exclusivity checks from G-42)
- [ ] **G-12b** Vault Connect CA provider (`connect.ca_provider = "vault"`) — `connect-ca-config.json` via Consul's native Kubernetes-auth-to-Vault. Needs `spec.connect.caProvider.vault` (address, roles/PKI paths, `vaultNamespace`, and a `ConsulSecretRef` for the CA cert used to trust Vault's own TLS listener). No pod-template change, no agent-inject annotations.
- [ ] **G-21** `ui_config` — honour `ui.enabled`, metrics provider/proxy, dashboard URL templates
- [ ] **G-20** telemetry `disable_hostname`, `enable_host_metrics`, `prefix_filter`
- [ ] **G-24** `global.logLevel` fallback and upper-casing
- [ ] **G-26** dual-stack bind/client addresses
- [ ] **G-23** configurable serf LAN port, including in `retry_join`
- [ ] **G-45** Table tests for every branch above in `configmap_test.go`, replacing what `server-config-configmap.bats` covered

### Phase 3 — Restore pod-template parity (P1, higher risk)

- [ ] **G-10** Enterprise license: Secret volume + `CONSUL_LICENSE_PATH`
- [ ] **G-18** `global.trustedCAs`: emptyDir, cert write, OpenSSL rehash, `SSL_CERT_DIR`
- [ ] **G-17** Locality init container — needs `spec.image` for `global.imageK8S` and RBAC for node reads
- [ ] **G-11** Snapshot agent as a second container, with its own config Secret, CA, interval, resources, and extra volumes
- [ ] **G-19** Datadog: pod labels, `ad.datadoghq.com/*` annotations, dogstatsd UDS hostPath
- [ ] **G-22** `global.experiments`
- [ ] **G-25** OpenShift security context
- [ ] **G-27** `global.extraLabels`
- [ ] **G-30** Decide `extraConfig` semantics: either restore the emptyDir + `sed` substitution, or document that `HOST_IP`/`POD_IP`/`HOSTNAME` are no longer substituted and provide `spec.pod.extraEnvVars` as the replacement. Do not leave it silently changed.
- [ ] **G-45** Pod-template tests replacing `server-statefulset.bats` coverage

### Phase 4 — Admin partitions and namespaces (P1)

Resolved by D1 (see below): this phase is no longer "Vault, partitions, and namespaces."
Vault-backed secret sourcing isn't a distinct capability — it's whatever fields G-04, G-10,
and G-11 already give a `SecretName`/`SecretKey` ref, populated by an external Vault sync
mechanism instead of a plain `kubectl create secret`. Nothing operator-side to build here
beyond those fields landing in Phases 1–3. **G-12b** (Vault Connect CA provider) is config-only
and moved to Phase 2.

- [ ] **G-14** Admin partitions and Consul namespaces
- [ ] Re-add the `vaultNamespace`/PKI-path validation guards from G-42 that are still relevant under G-12b

### Phase 5 — Operator engineering (P2)

- [ ] **G-40** Delete `consulcluster_deepcopy.go`; generate `zz_generated.deepcopy.go` with `controller-gen`
- [ ] **G-41** Add `+kubebuilder` markers to `consulcluster_types.go` and generate `crds/consulclusters.yaml`; wire both into `make ctrl-manifests` and add a CI check that the tree is clean after regeneration
- [ ] **G-42** Express the recoverable guards as CRD validation (`enum` for log level and request-limit mode, `minimum` for size, `required` on paired secret refs) and the cross-field ones (`bootstrapExpect >= size`, federation/adminPartitions exclusivity) as CEL rules or a validating webhook
- [x] **G-43a** ~~Pick a `Conditions` type~~ Resolved via D3: `ConsulClusterStatus.Conditions` is now `[]metav1.Condition` (was the legacy custom `Conditions` type), matching `RouteAuthFilterStatus`/`GatewayPolicyStatus`. CRD schema and deepcopy updated to match.
- [ ] **G-43b** Write `Available`, `Progressing`, `RaftHealthy` conditions in the reconcile loop and surface the rollout hold reason there instead of only in logs — the type is ready, nothing sets a condition yet
- [ ] **G-28** Design the canary API to replace `server.updatePartition`
- [ ] **G-29** Re-validate `server-role`, `server-clusterrole`, PSP, and SCC against the operator-created ServiceAccount
- [ ] **G-44** Acceptance tests: rolling upgrade holds when `FailureTolerance` is 0; a deleted pod reattaches its PVC and rejoins; a cordoned node's peer is reaped; scale up and down preserve quorum

## 5. Decisions

### D1 — Is Helm still the delivery mechanism, or is the CR the public API?

**Answered: A — Helm remains the delivery mechanism.** Helm continues to own every
cross-cutting object it owned on `main` (`ui-service.yaml`, `expose-servers-service.yaml`).
The operator owns only what a StatefulSet-based install cannot get from Helm: the headless
Service the StatefulSet requires, the generated config, and the reconciliation logic
itself. Vault secret sourcing follows the same principle at one remove — see the Vault
resolution below.

**Applied:** G-01 and G-03 resolved — deleted `buildClientService`, `ensureClientService`,
`clientServiceName`, `buildExposeService`, `ensureExposeService`, `exposeServiceName`, and
the `spec.exposeService` CRD field. Verified `ui-service.yaml`'s and
`expose-servers-service.yaml`'s selectors (`app`/`release`/`component`) still match
operator-created pods, since `serverPodLabels()` merges the `ConsulCluster` object's own
`release` label onto the pods it creates.

**Resolved for Vault (2026-08-18):** the CR takes Vault-sourced values the same way it
already takes ACL and gossip-encryption Secret refs (`SecretName`/`SecretKey` pointing at
a plain Kubernetes Secret), with Helm only responsible for populating those CR fields from
`global.secretsBackend.vault.*`. The consequence, worked out from that shape: it means the
operator does not do Vault Agent Injection at all — no sidecar, no
`vault.hashicorp.com/agent-inject-*` annotations on the pod template it builds. Getting
the secret material from Vault into the Secret those refs point at becomes an external
concern (Vault Secrets Operator, External Secrets Operator, or a manual sync), not
something this operator or chart does.

The one Vault-specific capability that survives is Consul acting as its own client to
Vault for the **Connect CA provider** (`connect.ca_provider = "vault"`) — that already used
Consul's *native* Kubernetes-auth-to-Vault in `main` (the server's own ServiceAccount token,
not the Agent sidecar), so it fits the "just a Secret ref plus some config" shape without
contradicting the no-injection decision. See **G-12a** (resolved — no separate work; it
falls out of G-04/G-10/G-11) and **G-12b** (Connect CA provider, moved to Phase 2 as
config-only work) in the gap table above.

### D2 — Is in-place upgrade from a `main` install a requirement for GA?

**Answered: No — this is a straight replacement.** No automated migration path, no PVC
adoption tooling, no dual-install guard. Installing this branch over an existing
`main`-based release replaces the server StatefulSet; existing data is not carried
forward automatically.

**Applied:** G-06 dropped from Phase 1. Removed the chart's forced
`dataVolumeName: data-<namespace>` compatibility wiring (it existed only to make PVC names
match an old install); `spec.dataVolumeName` remains as a general escape hatch, defaulting
to `"data"`. One documentation task remains: call out the replace-not-upgrade behavior in
release notes.

**Consequence for the rest of the plan:** later phases and capabilities (Vault, admin
partitions, Enterprise features, canary rollout control) are added incrementally as
follow-up work, not gated on this document. Phase boundaries below are for planning
convenience, not a required sequence.

### D3 — What type should `ConsulClusterStatus.Conditions` use?

**Answered: `[]metav1.Condition`.** Matches the pattern already used by
`RouteAuthFilterStatus` and `GatewayPolicyStatus` elsewhere in `api/v1alpha1` (the legacy
custom `Conditions` type is what most older CRDs in this package still use, but it is not
the pattern for new ones).

**Applied:** `consulcluster_types.go`, `consulcluster_deepcopy.go`, and
`crds/consulclusters.yaml` all updated. This was a type change only — the reconciler does
not yet write any condition. That remains G-43b.

### D4 — Does `server.updatePartition` need an equivalent, or is operator-driven health gating sufficient?

**Open.** Not addressed in this pass. Tracked as G-28 in Phase 5.

## 6. Suggested sequencing

Phase 1 is a prerequisite for any usable build and should land first. With D2 resolved,
nothing else in this document blocks on a decision — Phase 2 is low-risk and
parallelisable, and Phases 3/4/5 can be picked up in any order as capacity allows. Phase 5
items G-40 and G-41 are still worth pulling forward: every later phase adds CRD fields, and
doing that by hand against a hand-written deepcopy and schema compounds the drift — as it
already has, twice, in the D1/D3 edits above.
