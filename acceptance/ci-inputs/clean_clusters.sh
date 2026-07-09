#!/usr/bin/env bash
# ocp_clean.sh — idempotent cleanup of stale consul installations, consul CRDs,
# and test namespaces (ns1/ns2) across OCP clusters.
#
# Preserved intentionally:
#   consul namespace itself (do NOT delete)
#   consul/license secret  (Enterprise license — no Helm ownership)
#   OCP-native SAs (builder/default/deployer) and their dockercfg pull secrets
#   gateway.networking.k8s.io CRDs (OCP-native)
#   consul-test namespace (pre-existing, unrelated to acceptance tests)
#
# Usage: bash /tmp/ocp_clean.sh
set -uo pipefail

CONTEXTS=(
  "consul/api-ocpate1-d2zv-p1-openshiftapps-com:6443/cluster-admin"
  "consul/api-ocpate2-6hog-p1-openshiftapps-com:6443/cluster-admin"
)

# CTX is set as a global by cleanup_cluster before any K() call.
K() { kubectl --context="$CTX" "$@"; }

# clear_resource_finalizers <namespace> <resource-type> <resource-name>
# Patches the finalizers array to [] so the apiserver can GC the object.
clear_resource_finalizers() {
  local ns="$1" res_type="$2" res_name="$3"
  if [[ -n "$ns" ]]; then
    K -n "$ns" patch "$res_type" "$res_name" \
      --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
  else
    K patch "$res_type" "$res_name" \
      --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
  fi
}

# clear_all_namespace_resource_finalizers <namespace>
# Iterates common resource types in the given namespace and clears any finalizers.
clear_all_namespace_resource_finalizers() {
  local ns="$1"
  echo "  Clearing finalizers on resources in namespace $ns..."
  local res_types=(pods services configmaps secrets deployments replicasets statefulsets daemonsets jobs)
  for res_type in "${res_types[@]}"; do
    while IFS= read -r res_name; do
      [[ -z "$res_name" ]] && continue
      clear_resource_finalizers "$ns" "$res_type" "$res_name"
    done < <(K -n "$ns" get "$res_type" \
      -o jsonpath='{range .items[?(@.metadata.finalizers)]}{.metadata.name}{"\n"}{end}' \
      2>/dev/null || true)
  done
}

# delete_stale_helm_managed_resources <namespace>
# Deletes ConfigMaps and Secrets that carry a meta.helm.sh/release-name annotation
# pointing to a release that no longer exists. These survive `helm uninstall` when
# the release was in a failed/pending state and cause "invalid ownership metadata"
# on the next install.
delete_stale_helm_managed_resources() {
  local ns="$1"
  echo "  Deleting stale Helm-managed ConfigMaps and Secrets in namespace $ns..."
  for res_type in configmaps secrets; do
    local names_and_releases
    names_and_releases=$(K -n "$ns" get "$res_type" \
      -o jsonpath='{range .items[*]}{.metadata.name}{"|"}{ .metadata.annotations.meta\.helm\.sh/release-name}{"\n"}{end}' \
      2>/dev/null || true)
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      local res_name rel_name
      res_name="${line%%|*}"
      rel_name="${line#*|}"
      # Skip if no Helm annotation.
      [[ -z "$rel_name" ]] && continue
      # Check if this release still exists.
      if ! helm --kube-context="$CTX" -n "$ns" status "$rel_name" &>/dev/null 2>&1; then
        echo "    Removing stale $res_type/$res_name (owned by gone release $rel_name)"
        K -n "$ns" patch "$res_type" "$res_name" \
          --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
        K -n "$ns" delete "$res_type" "$res_name" --ignore-not-found 2>/dev/null || true
      fi
    done <<< "$names_and_releases"
  done
}

# wait_for_terminating_crds_deleted
# Polls Consul CRDs still in Terminating state, re-clearing CR finalizers on each
# iteration so the apiextensions GC can proceed. Times out after 3 minutes.
wait_for_terminating_crds_deleted() {
  echo "  Waiting for Terminating consul CRDs to be fully deleted..."
  local deadline=$(( $(date +%s) + 180 ))
  while true; do
    local terminating_crds
    terminating_crds=$(K get crd \
      -o jsonpath='{range .items[?(@.metadata.deletionTimestamp)]}{.metadata.name}{"\n"}{end}' \
      2>/dev/null \
      | grep -E '\.(consul|auth\.consul)\.hashicorp\.com$' \
      || true)
    [[ -z "$terminating_crds" ]] && break
    echo "  Still Terminating CRDs: $(echo "$terminating_crds" | tr '\n' ' ')"
    while IFS= read -r crd; do
      [[ -z "$crd" ]] && continue
      clear_cr_finalizers_and_delete "$crd"
      K patch crd "$crd" --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
    done <<< "$terminating_crds"
    if (( $(date +%s) >= deadline )); then
      echo "  WARNING: timed out waiting for Terminating CRDs to be deleted: $(echo "$terminating_crds" | tr '\n' ' ')"
      break
    fi
    sleep 5
  done
}

# clear_cr_finalizers_and_delete <crd-name>
# Gets all CRs for the given CRD (cluster-wide), clears their finalizers, then
# deletes them. Works for both Cluster- and Namespaced-scoped CRDs.
clear_cr_finalizers_and_delete() {
  local crd="$1"
  local cr_list
  cr_list=$(K get "$crd" -A \
    -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{"\n"}{end}' \
    2>/dev/null || true)
  [[ -z "$cr_list" ]] && return 0

  while IFS= read -r cr; do
    [[ -z "$cr" ]] && continue
    local cr_ns cr_name
    cr_ns="${cr%%/*}"
    cr_name="${cr#*/}"
    [[ -z "$cr_name" ]] && continue

    if [[ -n "$cr_ns" ]]; then
      echo "    patch finalizers: $crd/$cr_name (ns=$cr_ns)"
      K -n "$cr_ns" patch "$crd" "$cr_name" \
        --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
      K -n "$cr_ns" delete "$crd" "$cr_name" \
        --ignore-not-found --wait=false 2>/dev/null || true
    else
      echo "    patch finalizers: $crd/$cr_name (cluster-scoped)"
      K patch "$crd" "$cr_name" \
        --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
      K delete "$crd" "$cr_name" \
        --ignore-not-found --wait=false 2>/dev/null || true
    fi
  done <<< "$cr_list"
}

cleanup_cluster() {
  CTX="$1"
  echo ""
  echo "============================================================"
  echo " Cleaning: $CTX"
  echo "============================================================"

  # ── Step 1: Delete app workloads while webhook is still alive ────────
  # Ensures connect-inject finalizers are honoured before we nuke the webhook.
  echo "[1] Deleting app workloads in consul/ns1/ns2..."
  for ns in consul ns1 ns2; do
    K -n "$ns" delete deployment replicaset statefulset daemonset job \
      -l 'app in (static-server,static-client,multiport,multiport-admin,job-client)' \
      --ignore-not-found --wait=false 2>/dev/null || true
    K -n "$ns" delete pod \
      -l 'app in (static-server,static-client,multiport,multiport-admin,job-client)' \
      --ignore-not-found --wait=false --grace-period=0 2>/dev/null || true
  done

  # ── Step 2: Helm uninstall all releases in consul namespace ──────────
  echo "[2] Helm uninstall (--no-hooks)..."
  local releases
  releases=$(helm --kube-context="$CTX" list -n consul -q 2>/dev/null || true)
  if [[ -n "$releases" ]]; then
    while IFS= read -r rel; do
      [[ -z "$rel" ]] && continue
      echo "  Uninstalling helm release: $rel"
      helm --kube-context="$CTX" uninstall "$rel" -n consul \
        --no-hooks --wait=false 2>/dev/null || true
    done <<< "$releases"
    # Brief wait so the apiserver processes deletes before we proceed.
    sleep 5
  else
    echo "  No Helm releases found."
  fi

  # ── Step 2b: Delete stale Helm-managed resources ───────────────────
  # Removes ConfigMaps/Secrets annotated with a release name that no longer
  # exists. These cause "invalid ownership metadata" on the next install.
  echo "[2b] Removing stale Helm-managed resources in consul namespace..."
  delete_stale_helm_managed_resources consul

  # ── Step 2c: Clear finalizers on remaining namespace resources ───────
  echo "[2c] Clearing resource finalizers in consul/ns1/ns2..."
  for ns in consul ns1 ns2; do
    clear_all_namespace_resource_finalizers "$ns"
  done

  # ── Step 3: Clear finalizers + delete all consul CRDs ────────────────
  # Covers *.consul.hashicorp.com and *.auth.consul.hashicorp.com.
  # Also handles CRDs already in Terminating state (finalizer-held CRs
  # still served; patch unblocks CRD GC).
  echo "[3] Clearing and deleting consul CRDs..."
  local consul_crds
  consul_crds=$(K get crd \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
    2>/dev/null \
    | grep -E '\.(consul|auth\.consul)\.hashicorp\.com$' \
    || true)

  if [[ -n "$consul_crds" ]]; then
    while IFS= read -r crd; do
      [[ -z "$crd" ]] && continue
      echo "  CRD: $crd"
      clear_cr_finalizers_and_delete "$crd"
      K delete crd "$crd" --ignore-not-found --wait=false 2>/dev/null || true
    done <<< "$consul_crds"
  else
    echo "  No consul CRDs found."
  fi

  # ── Step 3b: Wait for Terminating consul CRDs ───────────────────────
  wait_for_terminating_crds_deleted

  # ── Step 4: Delete consul-owned gateway.networking.k8s.io CRs ───────
  # Deletes only the OBJECTS (gateways/routes etc.) in test namespaces and
  # consul-named GatewayClasses. The gateway.networking.k8s.io CRDs are
  # OCP-native and must NOT be deleted.
  echo "[4] Deleting consul-owned gateway.networking.k8s.io CRs..."
  local gw_resources=(
    gateways httproutes grpcroutes tcproutes tlsroutes udproutes referencegrants
  )
  for ns in consul ns1 ns2; do
    for res in "${gw_resources[@]}"; do
      # Check if the CRD exists before trying to delete to avoid noisy errors.
      if K get crd "${res}.gateway.networking.k8s.io" &>/dev/null 2>&1; then
        # Clear finalizers first then delete.
        local gw_list
        gw_list=$(K -n "$ns" get "${res}.gateway.networking.k8s.io" \
          -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' \
          2>/dev/null || true)
        while IFS= read -r obj; do
          [[ -z "$obj" ]] && continue
          K -n "$ns" patch "${res}.gateway.networking.k8s.io" "$obj" \
            --type=merge -p '{"metadata":{"finalizers":[]}}' 2>/dev/null || true
          K -n "$ns" delete "${res}.gateway.networking.k8s.io" "$obj" \
            --ignore-not-found --wait=false 2>/dev/null || true
        done <<< "$gw_list"
      fi
    done
  done
  # Delete GatewayClasses (cluster-scoped) whose controllerName contains "consul".
  if K get crd gatewayclasses.gateway.networking.k8s.io &>/dev/null 2>&1; then
    local consul_gcs
    consul_gcs=$(K get gatewayclasses.gateway.networking.k8s.io -o json 2>/dev/null \
      | python3 -c "
import sys, json
items = json.load(sys.stdin).get('items', [])
for i in items:
    ctrl = i.get('spec', {}).get('controllerName', '')
    if 'consul' in ctrl:
        print(i['metadata']['name'])
" 2>/dev/null || true)
    while IFS= read -r gc; do
      [[ -z "$gc" ]] && continue
      echo "  Deleting consul GatewayClass: $gc"
      K delete gatewayclass "$gc" --ignore-not-found 2>/dev/null || true
    done <<< "$consul_gcs"
  fi

  # ── Step 5: Delete consul webhooks, ClusterRoles, ClusterRoleBindings, SCCs ──
  echo "[5] Deleting consul webhooks/RBAC/SCC..."
  for wh in $(K get mutatingwebhookconfiguration -o name 2>/dev/null \
      | grep consul | awk -F'/' '{print $2}'); do
    K delete mutatingwebhookconfiguration "$wh" --ignore-not-found 2>/dev/null || true
  done
  for wh in $(K get validatingwebhookconfiguration -o name 2>/dev/null \
      | grep consul | awk -F'/' '{print $2}'); do
    K delete validatingwebhookconfiguration "$wh" --ignore-not-found 2>/dev/null || true
  done
  for cr in $(K get clusterrole -o name 2>/dev/null \
      | grep consul | awk -F'/' '{print $2}'); do
    K delete clusterrole "$cr" --ignore-not-found 2>/dev/null || true
  done
  for crb in $(K get clusterrolebinding -o name 2>/dev/null \
      | grep consul | awk -F'/' '{print $2}'); do
    K delete clusterrolebinding "$crb" --ignore-not-found 2>/dev/null || true
  done
  # SCCs are OpenShift-specific (non-fatal if the API doesn't exist on vanilla k8s).
  for scc in $(K get scc -o name 2>/dev/null \
      | grep consul | awk -F'/' '{print $2}'); do
    K delete scc "$scc" --ignore-not-found 2>/dev/null || true
  done

  # ── Step 6: Clean consul namespace ───────────────────────────────────
  # Delete all consul-test workload leftovers. Preserve:
  #   secret/license — Enterprise license, no Helm ownership, must survive.
  #   builder/default/deployer SAs and their OCP-auto dockercfg secrets.
  echo "[6] Cleaning consul namespace..."
  if K get namespace consul &>/dev/null; then
    # Helm release metadata secrets (sh.helm.release.v1.*)
    K -n consul delete secret -l 'owner=helm' --ignore-not-found 2>/dev/null || true

    # PVCs — stale consul-server data volumes
    K -n consul delete pvc --all --ignore-not-found --wait=false 2>/dev/null || true

    # Resources whose name matches consul/static/multiport/job-client patterns.
    # Excludes OCP-native SA names (builder, default, deployer).
    for res_type in serviceaccount role rolebinding job configmap; do
      while IFS= read -r name; do
        [[ -z "$name" ]] && continue
        K -n consul delete "$res_type" "$name" --ignore-not-found 2>/dev/null || true
      done < <(K -n consul get "$res_type" -o name 2>/dev/null \
        | awk -F'/' '{print $2}' \
        | grep -E '(-consul|-static-|-multiport|-job-client)' \
        | grep -vE '^(builder|default|deployer)$' \
        || true)
    done

    # Secrets: same pattern but also exclude 'license' and dockercfg pull secrets.
    while IFS= read -r name; do
      [[ -z "$name" ]] && continue
      K -n consul delete secret "$name" --ignore-not-found 2>/dev/null || true
    done < <(K -n consul get secret -o name 2>/dev/null \
      | awk -F'/' '{print $2}' \
      | grep -E '(-consul|-static-|-multiport|-job-client)' \
      | grep -vE '^(license)$' \
      | grep -vE '\-dockercfg\-' \
      || true)

    # Services matching consul pattern.
    while IFS= read -r name; do
      [[ -z "$name" ]] && continue
      K -n consul delete service "$name" --ignore-not-found 2>/dev/null || true
    done < <(K -n consul get service -o name 2>/dev/null \
      | awk -F'/' '{print $2}' \
      | grep consul \
      || true)

    # Endpoints + EndpointSlices left by consul services.
    while IFS= read -r name; do
      [[ -z "$name" ]] && continue
      K -n consul delete endpoints "$name" --ignore-not-found 2>/dev/null || true
    done < <(K -n consul get endpoints -o name 2>/dev/null \
      | awk -F'/' '{print $2}' \
      | grep consul \
      || true)
  else
    echo "  consul namespace not found."
  fi

  # ── Step 7: Force-delete test namespaces ns1 and ns2 ─────────────────
  # Pattern: delete first (sets deletionTimestamp), then force-finalize only
  # if still present (Terminating). NEVER clear spec.finalizers BEFORE Delete.
  echo "[7] Deleting test namespaces ns1/ns2..."
  for ns in ns1 ns2; do
    if K get namespace "$ns" &>/dev/null; then
      echo "  Deleting namespace: $ns"
      K delete namespace "$ns" --wait=false 2>/dev/null || true
      sleep 3
      if K get namespace "$ns" &>/dev/null; then
        echo "  Namespace $ns still present — force-finalizing via /finalize subresource..."
        K get namespace "$ns" -o json 2>/dev/null \
          | python3 -c "
import sys, json
d = json.load(sys.stdin)
d['spec']['finalizers'] = []
print(json.dumps(d))
" | K replace --raw "/api/v1/namespaces/$ns/finalize" -f - 2>/dev/null || true
      fi
    else
      echo "  Namespace $ns not found — skipping."
    fi
  done

  # ── Step 8: Recreate consul namespace ────────────────────────────────
  echo "[8] Recreating consul namespace..."
  if ! K get namespace consul &>/dev/null; then
    K create namespace consul 2>/dev/null || true
    echo "  Created namespace consul."
  else
    echo "  Namespace consul already exists."
  fi

  # ── Step 9: Summary ───────────────────────────────────────────────────
  echo ""
  echo "  Post-cleanup counts for $CTX:"
  echo "    Helm releases  : $(helm --kube-context="$CTX" list -n consul -q 2>/dev/null | wc -l | tr -d ' ')"
  echo "    Consul CRDs    : $(K get crd 2>/dev/null | grep -cE '\.(consul|auth\.consul)\.hashicorp\.com$' || echo 0)"
  echo "    Webhooks       : $(K get mutatingwebhookconfiguration,validatingwebhookconfiguration -o name 2>/dev/null | grep -c consul || echo 0)"
  echo "    ClusterRoles   : $(K get clusterrole -o name 2>/dev/null | grep -c consul || echo 0)"
  echo "    ClusterRoleBdg : $(K get clusterrolebinding -o name 2>/dev/null | grep -c consul || echo 0)"
  echo "    SCCs           : $(K get scc -o name 2>/dev/null | grep -c consul || echo 0)"
  echo "    ns1 exists     : $(K get namespace ns1 &>/dev/null && echo yes || echo no)"
  echo "    ns2 exists     : $(K get namespace ns2 &>/dev/null && echo yes || echo no)"
  echo ""
  echo "Cluster done: $CTX"
}

# ── Main ──────────────────────────────────────────────────────────────────
for ctx in "${CONTEXTS[@]}"; do
  cleanup_cluster "$ctx" || echo ""
  echo "============================= continuing to next cluster... ============================="
done

echo ""
echo "All 8 clusters processed."