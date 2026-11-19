#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

if [[ -z "${KUBE_CONTEXT:-}" || -z "${CONSUL_ENT_LICENSE:-}" ]]; then
  echo "Set KUBE_CONTEXT and CONSUL_ENT_LICENSE before running this script." >&2
  exit 1
fi
namespace="${KUBE_NAMESPACE:-consul}"
debug_directory="${DEBUG_DIRECTORY:-/tmp/rosa-tests-$(date +%Y%m%d-%H%M%S)}"
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_directory/.." && pwd)"
if (($# == 0)); then
  packages=(basic server cli config-entries connect consul-dns metrics snapshot-agent sync terminating-gateway api-gateway custom-api-gateway openshift ingress-gateway segments vault)
else
  packages=("$@")
fi

for package in "${packages[@]}"; do
  case "$package" in
    basic|server|cli|config-entries|connect|consul-dns|metrics|snapshot-agent|sync|terminating-gateway|api-gateway|custom-api-gateway|openshift|ingress-gateway|segments|vault) ;;
    *) echo "Unsupported single-cluster acceptance package: $package" >&2; exit 1 ;;
  esac
  if [[ "$namespace" != "consul" && ( "$package" == "cli" || "$package" == "openshift" ) ]]; then
    echo "The $package acceptance package requires KUBE_NAMESPACE=consul." >&2
    exit 1
  fi
done
export CONSUL_K8S_SKIP_OPENAPI_VALIDATION=true
mkdir -p "$debug_directory"
debug_directory="$(cd "$debug_directory" && pwd)"
echo "OCP acceptance logs: $debug_directory"
for package in "${packages[@]}"; do
  if [[ "$package" == "cli" ]]; then
    mkdir -p "$debug_directory/bin"
    (
      cd "$repo_root/cli"
      go build -o "$debug_directory/bin/consul-k8s" .
    )
    export PATH="$debug_directory/bin:$PATH"
    break
  fi
done

cleanup_args=(--context "$KUBE_CONTEXT" --namespace "$namespace" --debug-directory "$debug_directory")
test_args=(
  -use-openshift
  -enable-openshift
  -enable-enterprise
  "-kube-contexts=$KUBE_CONTEXT"
  "-kube-namespaces=$namespace"
  "-debug-directory=$debug_directory"
  -no-cleanup-on-failure
)
if [[ -n "${KUBECONFIG_FILE:-}" ]]; then
  cleanup_args+=(--kubeconfig "$KUBECONFIG_FILE")
  test_args+=("-kubeconfigs=$KUBECONFIG_FILE")
fi
case "${ENABLE_TRANSPARENT_PROXY:-true}" in
  true) test_args+=(-enable-transparent-proxy) ;;
  false) ;;
  *) echo "ENABLE_TRANSPARENT_PROXY must be true or false." >&2; exit 1 ;;
esac
case "${ENABLE_CNI:-false}" in
  true) test_args+=(-enable-cni) ;;
  false) ;;
  *) echo "ENABLE_CNI must be true or false." >&2; exit 1 ;;
esac
if [[ -n "${CONSUL_IMAGE:-}" ]]; then test_args+=("-consul-image=$CONSUL_IMAGE"); fi
if [[ -n "${CONSUL_K8S_IMAGE:-}" ]]; then test_args+=("-consul-k8s-image=$CONSUL_K8S_IMAGE"); fi
if [[ -n "${CONSUL_DATAPLANE_IMAGE:-}" ]]; then test_args+=("-consul-dataplane-image=$CONSUL_DATAPLANE_IMAGE"); fi
go_args=(-count=1 -v -p 1 -timeout 3h -failfast)
if [[ -n "${TEST_RUN:-}" ]]; then go_args+=(-run "$TEST_RUN"); fi

cd "$repo_root/acceptance/tests"
for package in "${packages[@]}"; do
  "$script_directory/cleanup-ocp-tests.sh" "${cleanup_args[@]}"
  echo "Running single-cluster OCP acceptance package: $package"
  go test "./$package" "${go_args[@]}" -args "${test_args[@]}" 2>&1 | tee "$debug_directory/$package.log"
done
