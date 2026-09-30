# CRD Sample Manifests

Sample CR manifests for the Consul AI CRDs. Apply these against a cluster where
the CRDs are installed (see `../bases/`) and the connect-inject controller is
running with `--enable-ai=true`.

## Files

| File | Description |
|---|---|
| `inferencepoolconfig-minimal.yaml` | Required fields only — the smallest valid `InferencePoolConfig` |
| `inferencepoolconfig-full.yaml` | Every optional field populated — `rateLimit`, `routing`, `matchRules`, `complianceMap`, open-ended `budget`/`cache`/`mirror` |
| `inferencepoolconfig-disabled.yaml` | `spec.enabled=false` — pool staged but not active; `Ready=False` |
| `inferencepoolconfig-multi-namespace.yaml` | Pool in `team-a` namespace with a cross-namespace `parentRef` pointing to `default` |

## Quick start

```bash
# 1. Install the CRD
kubectl apply -f ../bases/consul.hashicorp.com_inferencepoolconfigs.yaml

# 2. Apply a sample
kubectl apply -f inferencepoolconfig-minimal.yaml

# 3. Watch status (short name: ipc)
kubectl get ipc -A -w

# 4. Inspect conditions
kubectl get ipc test-pool-minimal -n default \
  -o jsonpath='{range .status.conditions[*]}{.type}={.status} ({.reason}){"\n"}{end}'
```

## Expected conditions

| Scenario | `Accepted` | `ParentResolved` | `Ready` |
|---|---|---|---|
| `enabled=true` and parent exists | `True` | `True` | `True` |
| `enabled=true` and parent missing | `True` | `False` (reason: `ParentNotFound`) | `False` |
| `enabled=false` | `True` | `True` | `False` |
