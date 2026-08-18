# ConsulCluster Operator Plan

## Overview

A Kubernetes operator to provision and manage Consul server clusters via a `ConsulCluster` CRD, modeled after the original etcd-operator pattern. The existing StatefulSet in the Helm chart is left intact; users opt in to operator-managed servers by setting `server.enabled=false` in Helm values.

**Key insight**: Consul is simpler than etcd for this use case. Because Consul uses DNS-based `retry_join` with Autopilot (no pre-registration API call required before starting a new server), we do not need the etcd-operator's two-phase "MemberAdd → then start pod" protocol. The reconcile loop is straightforward: ensure the right number of Pods and PVCs exist, wait for pods to settle before acting, and requeue periodically as a safety net.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Pod management | Individual `Pod` + `PVC` objects (not StatefulSet) | Mirrors etcd-operator; gives full control over pod lifecycle |
| Pod naming | Random 5-char suffix (e.g., `consul-a3f9b`) | Simpler operator state; identity tracked by labels, not ordinal |
| PVC retention on scale-down | Configurable via `spec.persistentVolumeClaimRetentionPolicy` | Defaults to `Delete`; `Retain` allows faster re-join on scale-up |
| Operator framework | `controller-runtime` directly — no kubebuilder CLI | Consistent with user preference; kubebuilder markers still used for codegen |
| Binary | Extend existing `control-plane` binary | No new deployment needed; registered with existing controller-runtime manager |
| Helm integration | `server.enabled=false` suppresses StatefulSet | No chart changes required; operator names services to match existing expectations |

## Consul Server Join Mechanics

Unlike etcd, Consul requires **no pre-registration step** before a new server pod starts. The joining process is entirely config-driven:

- `retry_join` points to the headless Service DNS (`<cluster-name>-server.<namespace>.svc`)
- The headless Service has `publishNotReadyAddresses: true` so pods can find peers before passing readiness checks
- `bootstrap_expect` tells servers how many peers to wait for before electing a leader; once a cluster is bootstrapped (raft state on disk), restarting pods simply re-join via gossip
- Consul's Autopilot automatically promotes new servers to full voters with no external API call

This means the operator only needs to: create the Pod, create the PVC, ensure the ConfigMap and Services exist, and let Consul's built-in clustering handle the rest.

## New CRD: `ConsulCluster`

**Group/Version/Kind**: `consul.hashicorp.com/v1alpha1/ConsulCluster`

```go
type ConsulClusterSpec struct {
    // Number of server pods. Default: 3
    Size int `json:"size"`

    // Consul version, e.g. "1.18.0"
    Version string `json:"version"`

    // Container image override. Defaults to the standard consul image for the version.
    Image string `json:"image,omitempty"`

    // Suspend reconciliation without deleting the cluster.
    Paused bool `json:"paused,omitempty"`

    // Consul datacenter name. Default: "dc1"
    DatacenterName string `json:"datacenterName,omitempty"`

    // How many servers must connect before electing a leader.
    // Defaults to Size.
    BootstrapExpect *int `json:"bootstrapExpect,omitempty"`

    // Pod-level configuration: resources, nodeSelector, tolerations, affinity, extra env vars.
    Pod *ConsulPodPolicy `json:"pod,omitempty"`

    // PVC size per server pod. Default: "10Gi"
    Storage resource.Quantity `json:"storage,omitempty"`

    // StorageClass for PVCs. Uses cluster default if unset.
    StorageClassName *string `json:"storageClassName,omitempty"`

    // Controls PVC lifecycle on scale-down and cluster deletion.
    // Mirrors StatefulSet's persistentVolumeClaimRetentionPolicy.
    // Default: {WhenScaled: Delete, WhenDeleted: Delete}
    PersistentVolumeClaimRetentionPolicy *ConsulClusterPVCRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

    // TLS configuration (CA secret ref, server cert secret ref).
    TLS *ConsulTLSSpec `json:"tls,omitempty"`

    // Gossip encryption key secret ref.
    GossipEncryption *ConsulGossipSpec `json:"gossipEncryption,omitempty"`

    // Expose gossip and RPC ports as hostPorts. Default: false
    ExposeGossipAndRPCPorts bool `json:"exposeGossipAndRPCPorts,omitempty"`
}

type ConsulClusterPVCRetentionPolicy struct {
    // What to do with PVCs when the cluster is scaled down. Delete | Retain
    WhenScaled string `json:"whenScaled,omitempty"`
    // What to do with PVCs when the ConsulCluster CR is deleted. Delete | Retain
    WhenDeleted string `json:"whenDeleted,omitempty"`
}

type ConsulClusterStatus struct {
    // Phase: Creating | Running | Upgrading | Failed
    Phase ConsulClusterPhase `json:"phase"`
    // Number of ready server pods
    ReadyCount int `json:"readyCount"`
    // Names of member pods
    Members []string `json:"members,omitempty"`
    // Image version currently running across the cluster
    CurrentVersion string `json:"currentVersion,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

## Files to Create

| File | Purpose |
|---|---|
| `control-plane/api/v1alpha1/consulcluster_types.go` | CRD Go types (`ConsulCluster`, `ConsulClusterSpec`, `ConsulClusterStatus`, sub-types) |
| `control-plane/controllers/consulcluster/controller.go` | Main reconciler — owns the full reconcile loop |
| `control-plane/controllers/consulcluster/pod.go` | Pod + PVC construction logic (mirrors server StatefulSet container spec) |
| `control-plane/controllers/consulcluster/services.go` | Headless Service and client Service construction |
| `control-plane/controllers/consulcluster/configmap.go` | Server config JSON generation (`retry_join`, `bootstrap_expect`, `datacenter`, TLS, autopilot) |
| `control-plane/controllers/consulcluster/controller_test.go` | Unit tests (testify, envtest fake client) |

## Files to Modify

| File | Change |
|---|---|
| `control-plane/api/v1alpha1/register.go` (or scheme setup) | Register `ConsulCluster` + `ConsulClusterList` with the scheme |
| `control-plane/api/v1alpha1/zz_generated.deepcopy.go` | Regenerated via `controller-gen` after types are added |
| Manager setup subcommand (inject-connect or dedicated) | Add `ConsulClusterReconciler.SetupWithManager(mgr)` |

## Controller Structure

Uses `controller-runtime` directly. No kubebuilder-generated scaffolding files. kubebuilder comment markers (`// +kubebuilder:rbac:...`) are written manually on the reconciler struct for RBAC generation.

```go
type ConsulClusterReconciler struct {
    client.Client
    Log    logr.Logger
    Scheme *runtime.Scheme
}

func (r *ConsulClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.ConsulCluster{}).
        Owns(&corev1.Pod{}).
        Owns(&corev1.PersistentVolumeClaim{}).
        Owns(&corev1.Service{}).
        Owns(&corev1.ConfigMap{}).
        Complete(r)
}
```

## Reconcile Loop

```
 1. Fetch ConsulCluster CR — return if not found (deleted before reconcile ran)
 2. Deletion path: if DeletionTimestamp set and finalizer present
      → delete owned Pods and PVCs per PVCRetentionPolicy.WhenDeleted
      → remove finalizer, return
 3. Add finalizer if absent
 4. Paused check → update status, requeue after 30s, return
 5. Ensure headless Service exists (publishNotReadyAddresses: true)
 6. Ensure client Service exists (HTTP/HTTPS/gRPC ports)
 7. Ensure ConfigMap exists and is up-to-date (triggers pod restart annotation if config changes)
 8. List owned Pods via label selector (consul.hashicorp.com/cluster=<name>)
 9. Categorize pods: Running, Pending, Terminating
10. Any Pending pods? → update status, requeue after 5s (wait for pods to settle before acting)
11. Scale up: if len(running + pending) < spec.Size → createOnePod() + createPVC(), requeue
12. Scale down: if len(running) > spec.Size → deleteOnePod() gracefully
      → if PVCRetentionPolicy.WhenScaled == Delete → deletePVC()
      → requeue
13. Upgrade: if any pod's image label != desired image → patch one pod image per reconcile, requeue
14. Update status (phase, readyCount, members list, currentVersion)
15. Requeue after 30s (periodic safety net — catches missed watch events)
```

One pod is created or deleted per reconcile pass. This prevents thundering-herd issues and matches Consul's Autopilot, which also promotes new voters one at a time.

## Owned Resources

### Pods

- **Name**: `<cluster-name>-<random-5-char-suffix>` (e.g., `consul-a3f9b`)
- **Labels**: `app=consul`, `component=server`, `consul.hashicorp.com/cluster=<name>`, `consul.hashicorp.com/version=<version>`
- **OwnerReference** → `ConsulCluster` CR (enables automatic GC)
- Container spec mirrors the existing server StatefulSet:
  - Same ports (8500, 8501, 8502, 8301, 8302, 8300, 8600)
  - Same readiness probe (`/v1/status/leader`)
  - Same security context (non-root, read-only root FS, drop ALL caps)
  - `ADVERTISE_IP` from `status.podIP`
  - `-config-dir=/consul/config` (from ConfigMap volume mount)
  - `-retry-join=<cluster-name>-server.<namespace>.svc`

### PersistentVolumeClaims

- **Name**: `data-<pod-name>` (mirrors StatefulSet PVC naming for familiarity)
- **OwnerReference** → Pod (so PVC follows pod lifecycle per retention policy)
- Size from `spec.storage`, StorageClass from `spec.storageClassName`

### Services

- **Headless**: `<cluster-name>-server`, `clusterIP: None`, `publishNotReadyAddresses: true`
  - Selector: `consul.hashicorp.com/cluster=<name>, component=server`
  - Used for server-to-server gossip and `retry_join`
- **Client**: `<cluster-name>-ui`, exposes HTTP (8500), HTTPS (8501), gRPC (8502), DNS (8600)

### ConfigMap

- **Name**: `<cluster-name>-server-config`
- Contains server config JSON equivalent to what `server-config-configmap.yaml` produces:
  - `datacenter`, `data_dir`, `server: true`, `leave_on_terminate: true`
  - `bootstrap_expect`, `retry_join`
  - `ports`, `tls`, `autopilot` settings

## Helm Integration

No changes to the Helm chart are required. Users opt in by setting `server.enabled=false`:

```yaml
# values.yaml — operator-managed servers
server:
  enabled: false  # suppresses StatefulSet, server Services, and server ConfigMap
```

Then apply a `ConsulCluster` CR:

```yaml
apiVersion: consul.hashicorp.com/v1alpha1
kind: ConsulCluster
metadata:
  name: consul          # should match helm release name for service naming compatibility
  namespace: consul
spec:
  size: 3
  version: "1.18.0"
  storage: "10Gi"
  datacenterName: "dc1"
  persistentVolumeClaimRetentionPolicy:
    whenScaled: Retain    # keep PVCs when scaling down; allows faster re-join on scale-up
    whenDeleted: Delete   # clean up PVCs when the CR is deleted
  gossipEncryption:
    secretName: consul-gossip-key
    secretKey: key
  tls:
    caSecretName: consul-ca-cert
    serverCertSecretName: consul-server-cert
```

The operator creates a Service named `<cr-name>-server` (e.g., `consul-server`). This is the exact name that `consul-server-connection-manager` and all other Helm-managed components (connect-inject, mesh gateway, ACL init job) already reference, so everything continues to work without modification.

## Out of Scope (Phase 1)

- **Backup/restore CRDs**: Can follow the etcd-operator pattern of separate `ConsulBackup` / `ConsulRestore` CRDs in a future phase
- **Vault integration**: The operator does not handle Vault agent injection in phase 1; users can add Vault annotations via `spec.pod.annotations`
- **ACL bootstrapping**: The existing `server-acl-init` job continues to handle this
- **Snapshot agent sidecar**: Can be added as a container in `spec.pod.extraContainers` or a future spec field
- **Enterprise license**: Can be injected via `spec.pod.env` initially; a dedicated field can be added later

## Running the Operator

The `ConsulClusterReconciler` is registered alongside existing controllers in the control-plane binary's manager setup. No new binary or deployment is required. RBAC is generated from kubebuilder markers on the reconciler struct using `controller-gen`.
