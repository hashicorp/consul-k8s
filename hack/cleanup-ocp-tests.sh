#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

context=""
namespace="consul"
kubeconfig=""
debug_directory=""
dry_run=false

usage() {
  echo "Usage: $0 --context CONTEXT [--namespace consul] [--kubeconfig FILE] [--debug-directory DIR] [--dry-run]"
}

while (($#)); do
  case "$1" in
    --context|--namespace|--kubeconfig|--debug-directory)
      if (($# < 2)) || [[ -z "$2" ]]; then
        usage >&2
        exit 1
      fi
      case "$1" in
        --context) context="$2" ;;
        --namespace) namespace="$2" ;;
        --kubeconfig) kubeconfig="$2" ;;
        --debug-directory) debug_directory="$2" ;;
      esac
      shift 2
      ;;
    --dry-run) dry_run=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage >&2; exit 1 ;;
  esac
done

if [[ -z "$context" ]]; then
  echo "An explicit Kubernetes context is required." >&2
  exit 1
fi
case "$namespace" in
  ""|default|kube-*|openshift|openshift-*|dedicated-admin)
    echo "Refusing to clean protected namespace '$namespace'." >&2
    exit 1
    ;;
esac
if [[ ! "$namespace" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || ((${#namespace} > 63)); then
  echo "Invalid test namespace '$namespace'." >&2
  exit 1
fi

kubectl=(kubectl --context "$context" --request-timeout=30s)
helm=(helm --kube-context "$context")
if [[ -n "$kubeconfig" ]]; then
  kubectl+=(--kubeconfig "$kubeconfig")
  helm+=(--kubeconfig "$kubeconfig")
fi

retry_command() {
  local output result attempt error_file error_output
  for attempt in 1 2 3; do
    error_file=$(mktemp)
    if output=$("$@" 2>"$error_file"); then
      cat "$error_file" >&2
      rm "$error_file"
      printf '%s\n' "$output"
      return 0
    else
      result=$?
    fi
    error_output=$(cat "$error_file")
    rm "$error_file"
    printf '%s\n' "$error_output" >&2
    printf '%s\n' "$output" >&2
    output+=$'\n'"$error_output"
    if [[ "$output" != *"TLS handshake timeout"* && "$output" != *"context deadline exceeded"* && \
          "$output" != *"connection reset by peer"* && "$output" != *"Client.Timeout exceeded"* && \
          "$output" != *"read: operation timed out"* && "$output" != *"i/o timeout"* && \
          "$output" != *"http2: client connection lost"* && \
          "$output" != *"unexpected error when reading response body"* ]]; then
      return "$result"
    fi
    echo "Transient cluster connection failure (attempt $attempt/3)." >&2
    sleep 5
  done
  return "$result"
}
kubectl=(retry_command "${kubectl[@]}")
helm=(retry_command "${helm[@]}")

mutate() {
  if "$dry_run"; then
    printf 'DRY RUN:'
    printf ' %q' "$@"
    printf '\n'
  else
    "$@"
  fi
}

"${kubectl[@]}" get clusterversion version -o jsonpath='{.status.desired.version}{"\n"}'
if [[ -n "$debug_directory" && -f "$debug_directory/ocp-dns-backup.json" ]]; then
  backup="$debug_directory/ocp-dns-backup.json"
  if ! jq -e --arg context "$context" --arg ns "$namespace" \
    '.context == $context and .namespace == $ns and has("servers") and (.upstream | type == "string")' "$backup" > /dev/null; then
    echo "DNS backup does not match the requested context and namespace: $backup" >&2
    exit 1
  fi
  current_dns=$("${kubectl[@]}" get dns.operator.openshift.io default -o json)
  upstream=$(jq -r '.upstream' "$backup")
  original_servers=$(jq -c '.servers' "$backup")
  patch=$(jq -ce --arg upstream "$upstream" --argjson original "$original_servers" '
    (.spec.servers // []) as $servers |
    if any($servers[]; .name == "consul-acceptance" and .forwardPlugin.upstreams != [$upstream]) then
      error("the acceptance DNS forwarder changed; refusing to overwrite another update")
    else
      ($servers | map(select(.name != "consul-acceptance"))) as $restored |
      {metadata: {resourceVersion: .metadata.resourceVersion},
       spec: {servers: (if ($restored | length) == 0 and $original == null then null else $restored end)}}
    end' <<< "$current_dns")
  mutate "${kubectl[@]}" patch dns.operator.openshift.io default --type=merge --patch "$patch"
  if ! "$dry_run"; then rm "$backup"; fi
fi
namespace_json=$("${kubectl[@]}" get namespace "$namespace" --ignore-not-found -o json)
if [[ -n "$namespace_json" ]]; then
  owner=$(jq -r '.metadata.labels["consul.hashicorp.com/acceptance-tests"] // ""' <<< "$namespace_json")
  if [[ "$owner" != "true" ]]; then
    echo "Namespace '$namespace' must be explicitly labeled consul.hashicorp.com/acceptance-tests=true before cleanup." >&2
    exit 1
  fi
fi

echo "Cleaning OCP acceptance resources in context '$context', namespace '$namespace'."
releases=$("${helm[@]}" list --namespace "$namespace" --output json)
unexpected=$(jq -r '.[] | select(.chart | test("^(consul(-helm)?|vault)-") | not) | .name' <<< "$releases")
if [[ -n "$unexpected" ]]; then
  echo "Refusing to remove namespace containing non-test Helm releases: $unexpected" >&2
  exit 1
fi
crds=$("${kubectl[@]}" get crd -o json)
owned_releases=$(jq -c --arg ns "$namespace" --argjson releases "$releases" '
  ([$releases[].name] +
   [.items[] |
    select((.spec.group == "consul.hashicorp.com" or .spec.group == "auth.consul.hashicorp.com") and
           .metadata.annotations["meta.helm.sh/release-namespace"] == $ns) |
    .metadata.annotations["meta.helm.sh/release-name"]]) |
  map(select(type == "string" and length > 0)) | unique' <<< "$crds")
auxiliary_namespaces=$("${kubectl[@]}" get namespaces -l "consul.hashicorp.com/acceptance-namespace=$namespace" -o json)
test_namespaces=$(jq -r '.items[].metadata.name' <<< "$auxiliary_namespaces")
if [[ -n "$namespace_json" ]]; then test_namespaces+=$'\n'"$namespace"; fi
for test_namespace in $test_namespaces; do
  case "$test_namespace" in
    default|kube-*|openshift|openshift-*|dedicated-admin)
      echo "Refusing to clean protected auxiliary namespace '$test_namespace'." >&2
      exit 1
      ;;
  esac
done

for resource in mutatingwebhookconfigurations validatingwebhookconfigurations; do
  objects=$("${kubectl[@]}" get "$resource" -o json)
  names=$(jq -r --arg ns "$namespace" '.items[] | select(.metadata.annotations["meta.helm.sh/release-namespace"] == $ns) | .metadata.name' <<< "$objects")
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    mutate "${kubectl[@]}" delete "$resource" "$name" --ignore-not-found --wait=false
  done <<< "$names"
done

resource_namespaces="$test_namespaces"$'\n'default
for test_namespace in $resource_namespaces; do
  for group in consul.hashicorp.com auth.consul.hashicorp.com gateway.networking.k8s.io; do
    resources=$("${kubectl[@]}" api-resources --api-group="$group" --namespaced=true --verbs=list,delete -o name)
    while IFS= read -r resource; do
      [[ -n "$resource" ]] || continue
      get_args=(get "$resource" --namespace "$test_namespace" -o name)
      if [[ "$test_namespace" == "default" ]]; then
        get_args+=(--selector "consul.hashicorp.com/acceptance-namespace=$namespace")
      fi
      objects=$("${kubectl[@]}" "${get_args[@]}")
      while IFS= read -r object; do
        [[ -n "$object" ]] || continue
        mutate "${kubectl[@]}" patch "$object" --namespace "$test_namespace" --type=merge \
          --patch '{"metadata":{"finalizers":[]}}'
        mutate "${kubectl[@]}" delete "$object" --namespace "$test_namespace" --ignore-not-found --wait=false
      done <<< "$objects"
    done <<< "$resources"
  done
done

for group in consul.hashicorp.com auth.consul.hashicorp.com gateway.networking.k8s.io; do
  resources=$("${kubectl[@]}" api-resources --api-group="$group" --namespaced=false --verbs=list,delete -o name)
  while IFS= read -r resource; do
    [[ -n "$resource" ]] || continue
    objects=$("${kubectl[@]}" get "$resource" -o json)
    names=$(jq -r --arg ns "$namespace" --argjson owned "$owned_releases" '.items[] |
      select(.metadata.annotations["meta.helm.sh/release-namespace"] == $ns or
             .metadata.labels["consul.hashicorp.com/acceptance-namespace"] == $ns or
             (.metadata.labels.release as $release | ($owned | index($release)) != null)) |
      .metadata.name' <<< "$objects")
    while IFS= read -r name; do
      [[ -n "$name" ]] || continue
      mutate "${kubectl[@]}" patch "$resource" "$name" --type=merge --patch '{"metadata":{"finalizers":[]}}'
      mutate "${kubectl[@]}" delete "$resource" "$name" --ignore-not-found --wait=false
    done <<< "$names"
  done <<< "$resources"
done

release_names=$(jq -r '.[].name' <<< "$releases")
while IFS= read -r release; do
  [[ -n "$release" ]] || continue
  mutate "${helm[@]}" uninstall "$release" --namespace "$namespace" --no-hooks --timeout 2m
done <<< "$release_names"

# Tests create GatewayClasses without ownership metadata, and Consul finalizes every class it controls,
# so classes left by a failed run block a later test that recreates them. Only remove them when no
# Consul installation outside the test namespaces could still own them.
if jq -e 'any(.items[]; .metadata.name == "gatewayclasses.gateway.networking.k8s.io")' <<< "$crds" > /dev/null; then
  injectors=$("${kubectl[@]}" get deployments --all-namespaces -l app=consul,component=connect-injector -o json)
  foreign_injectors=$(jq -r --arg namespaces "$test_namespaces" '($namespaces | split("\n")) as $owned |
    [.items[].metadata.namespace | select(. as $n | any($owned[]; . == $n) | not)] | unique | join(", ")' <<< "$injectors")
  if [[ -n "$foreign_injectors" ]]; then
    echo "Skipping unowned Consul GatewayClasses because Consul also runs in: $foreign_injectors." >&2
  else
    gateway_classes=$("${kubectl[@]}" get gatewayclasses.gateway.networking.k8s.io -o json)
    names=$(jq -r --arg ns "$namespace" --argjson owned "$owned_releases" '.items[] |
      select((.spec.controllerName // "") | startswith("consul.hashicorp.com/")) |
      select((.metadata.annotations["meta.helm.sh/release-namespace"] // $ns) == $ns) |
      select(.metadata.labels.release as $release | $release == null or ($owned | index($release)) != null) |
      .metadata.name' <<< "$gateway_classes")
    while IFS= read -r name; do
      [[ -n "$name" ]] || continue
      mutate "${kubectl[@]}" patch gatewayclasses.gateway.networking.k8s.io "$name" --type=merge \
        --patch '{"metadata":{"finalizers":[]}}'
      mutate "${kubectl[@]}" delete gatewayclasses.gateway.networking.k8s.io "$name" --ignore-not-found --wait=false
    done <<< "$names"
  fi
fi

for resource in clusterrolebindings clusterroles securitycontextconstraints; do
  objects=$("${kubectl[@]}" get "$resource" -o json)
  names=$(jq -r --arg ns "$namespace" '.items[] | select(.metadata.annotations["meta.helm.sh/release-namespace"] == $ns) | .metadata.name' <<< "$objects")
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    mutate "${kubectl[@]}" delete "$resource" "$name" --ignore-not-found --wait=false
  done <<< "$names"
done

for test_namespace in $test_namespaces; do
  if [[ "$test_namespace" != "$namespace" ]]; then
    mutate "${kubectl[@]}" delete namespace "$test_namespace" --ignore-not-found --timeout=3m
  fi
done

crds=$("${kubectl[@]}" get crd -o json)
# Delete all consul CRDs regardless of which release namespace owns them.
# This handles orphaned CRDs from runs that used a different namespace (e.g. 'default').
crd_names=$(jq -r '.items[] |
  select(.spec.group == "consul.hashicorp.com" or .spec.group == "auth.consul.hashicorp.com") |
  .metadata.name' <<< "$crds")
while IFS= read -r crd; do
  [[ -n "$crd" ]] || continue
  # Deleting a CRD deletes every instance, but instances keep their Consul finalizers once the
  # controller is uninstalled, which leaves the CRD terminating forever.
  if ! instances=$("${kubectl[@]}" get "$crd" --all-namespaces -o json); then
    if [[ -z "$("${kubectl[@]}" get crd "$crd" --ignore-not-found -o name)" ]]; then continue; fi
    exit 1
  fi
  finalized=$(jq -r '.items[] | select((.metadata.finalizers // []) | length > 0) |
    [.metadata.name, .metadata.namespace // ""] | @tsv' <<< "$instances")
  while IFS=$'\t' read -r name instance_namespace; do
    [[ -n "$name" ]] || continue
    patch_args=(patch "$crd" "$name" --type=merge --patch '{"metadata":{"finalizers":[]}}')
    if [[ -n "$instance_namespace" ]]; then patch_args+=(--namespace "$instance_namespace"); fi
    mutate "${kubectl[@]}" "${patch_args[@]}"
  done <<< "$finalized"
  mutate "${kubectl[@]}" delete crd "$crd" --ignore-not-found --timeout=2m
done <<< "$crd_names"

if [[ -n "$namespace_json" ]]; then
  mutate "${kubectl[@]}" delete namespace "$namespace" --ignore-not-found --timeout=3m
fi
if ! "$dry_run"; then
  "${kubectl[@]}" create namespace "$namespace"
  "${kubectl[@]}" label namespace "$namespace" consul.hashicorp.com/acceptance-tests=true
fi
