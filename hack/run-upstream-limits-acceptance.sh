#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# hack/run-upstream-limits-acceptance.sh
#
# Runs TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck against a fresh
# kind cluster using locally-built consul and consul-k8s images.
#
# What it tests
# ─────────────
#   Day-1  – Apply a RouteUpstreamLimitsFilter + HTTPRoute backendRef and
#             assert the Consul HTTPRoute config entry has the expected Limits
#             block AND that the Envoy cluster config_dump contains
#             circuit-breaker (max_connections) + outlier_detection fields.
#   Day-2  – Patch the RouteUpstreamLimitsFilter with new values; re-assert
#             both the Consul config entry and the Envoy config_dump.
#   Day-1  – Annotate the Gateway with default-max-connections / PHC
#             annotations; assert the Consul APIGateway Defaults block.
#   Day-2  – Change the annotations; re-assert the Defaults block.
#
# Usage
# ─────
#   ./hack/run-upstream-limits-acceptance.sh [flags]
#
# Flags
#   --no-cleanup    Leave the kind cluster running after the test.
#   --skip-build    Skip rebuilding Docker images; use existing :local tags.
#
# Prerequisites
# ─────────────
#   kind  kubectl  helm  docker  go
#
# Running against an existing cluster
# ────────────────────────────────────
# If you already have a kind cluster loaded with the right images, pass both
# --no-cleanup and --skip-build to jump straight to the test step.

set -euo pipefail

BOLD='\033[1m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
log()  { echo -e "${BOLD}[$(date +%T)]${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}⚠${NC}  $*"; }
fail() { echo -e "${RED}✗${NC} $*" >&2; exit 1; }

# ── paths ───────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONSUL_REPO="/Users/bharath/hashicorp/consul"

# ── image / cluster constants ───────────────────────────────────────────────
CLUSTER_NAME="dc1"
CONSUL_IMAGE="consul:local"
K8S_IMAGE="consul-k8s-control-plane:local"
KIND_NODE_IMAGE="$(awk '/kindNodeImage:/{print $2}' \
  "${REPO_ROOT}/acceptance/ci-inputs/kind-inputs.yaml")"
ARCH="$(go env GOARCH)"   # arm64 on Apple Silicon

# consul-dataplane image used by the Helm chart.
DATAPLANE_IMAGE="hashicorp/consul-dataplane:2.0-arm64"

# ── flag parsing ────────────────────────────────────────────────────────────
CLEANUP=true
SKIP_BUILD=false
for arg in "$@"; do
  case "${arg}" in
    --no-cleanup)   CLEANUP=false ;;
    --skip-build)   SKIP_BUILD=true ;;
    *) warn "Unknown flag '${arg}' — ignored" ;;
  esac
done

# ── Docker Desktop socket ────────────────────────────────────────────────────
DOCKER_SOCK="/Users/bharath/.docker/run/docker.sock"
if [[ -S "${DOCKER_SOCK}" ]]; then
  export DOCKER_HOST="unix://${DOCKER_SOCK}"
  log "Using Docker Desktop socket: ${DOCKER_SOCK}"
else
  fail "Docker Desktop socket not found at ${DOCKER_SOCK}. Adjust DOCKER_SOCK in this script."
fi

# ── preflight ─────────────────────────────────────────────────────────────
log "Preflight checks"
for bin in kind kubectl helm docker go; do
  command -v "${bin}" >/dev/null 2>&1 || fail "'${bin}' not found on PATH"
done
[[ -d "${CONSUL_REPO}" ]] || fail "consul repo not found at ${CONSUL_REPO}"
ok "All required tools present"

# ── step 1: clean up any stale cluster ───────────────────────────────────────
log "Step 1 — Removing any stale kind cluster '${CLUSTER_NAME}'"
kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null && ok "Deleted stale cluster" || ok "No stale cluster found"

# ── step 2 (optional): build images ──────────────────────────────────────────
if [[ "${SKIP_BUILD}" == "false" ]]; then
  log "Step 2a — Building consul:local (linux/${ARCH})"
  (
    cd "${CONSUL_REPO}"
    make GOARCH="${ARCH}" linux
    make GOARCH="${ARCH}" dev-docker
  )
  ok "consul:local built"

  log "Step 2b — Building consul-k8s-control-plane:local (linux/${ARCH})"
  (
    cd "${REPO_ROOT}"
    make GOOS=linux GOARCH="${ARCH}" control-plane-dev
    make GOOS=linux GOARCH="${ARCH}" dev-docker
  )
  ok "consul-k8s-control-plane:local built"
else
  log "Step 2 — Skipping image build (--skip-build)"
  docker image inspect "${CONSUL_IMAGE}"  >/dev/null 2>&1 || fail "${CONSUL_IMAGE} not found — remove --skip-build to build it"
  docker image inspect "${K8S_IMAGE}"     >/dev/null 2>&1 || fail "${K8S_IMAGE} not found — remove --skip-build to build it"
  ok "Using existing local images"
fi

# ── step 3: create kind cluster ──────────────────────────────────────────────
log "Step 3 — Creating kind cluster '${CLUSTER_NAME}' (${KIND_NODE_IMAGE})"
kind create cluster \
  --name "${CLUSTER_NAME}" \
  --image "${KIND_NODE_IMAGE}" \
  --config "${REPO_ROOT}/acceptance/framework/environment/kind/kind.config"
kubectl cluster-info --context "kind-${CLUSTER_NAME}"
ok "kind cluster '${CLUSTER_NAME}' ready"

# Register cleanup trap AFTER the cluster exists.
cleanup() {
  if [[ "${CLEANUP}" == "true" ]]; then
    log "Cleanup — deleting kind cluster '${CLUSTER_NAME}'"
    kind delete cluster --name "${CLUSTER_NAME}" 2>/dev/null || true
    ok "Cluster deleted"
  else
    warn "--no-cleanup: cluster '${CLUSTER_NAME}' is still running."
    warn "Delete later with:  kind delete cluster --name ${CLUSTER_NAME}"
  fi
}
trap cleanup EXIT

# ── step 4: load images into kind ─────────────────────────────────────────────
log "Step 4 — Loading images into kind cluster"
kind load docker-image --name "${CLUSTER_NAME}" "${CONSUL_IMAGE}"
ok "Loaded ${CONSUL_IMAGE}"
kind load docker-image --name "${CLUSTER_NAME}" "${K8S_IMAGE}"
ok "Loaded ${K8S_IMAGE}"

log "  Loading consul-dataplane image"
docker pull "${DATAPLANE_IMAGE}" 2>/dev/null || warn "Could not pull ${DATAPLANE_IMAGE} — will rely on registry pull inside kind"
kind load docker-image --name "${CLUSTER_NAME}" "${DATAPLANE_IMAGE}" 2>/dev/null || warn "Dataplane image load failed — continuing"
ok "consul-dataplane loaded (or skipped)"

# Pre-load the static-server image used by the backend deployment.
STATIC_SERVER_IMAGE="docker.mirror.hashicorp.services/hashicorp/http-echo:alpine"
log "  Pre-loading ${STATIC_SERVER_IMAGE}"
docker pull "${STATIC_SERVER_IMAGE}" 2>/dev/null || warn "Could not pull ${STATIC_SERVER_IMAGE}"
kind load docker-image --name "${CLUSTER_NAME}" "${STATIC_SERVER_IMAGE}" 2>/dev/null || warn "static-server image load failed — continuing"

# ── step 5: helm repo ─────────────────────────────────────────────────────────
log "Step 5 — Updating Helm repo"
helm repo add hashicorp https://helm.releases.hashicorp.com --force-update >/dev/null
helm repo update >/dev/null
ok "Helm repo ready"

# ── step 6: run the acceptance test ───────────────────────────────────────────
log "Step 6 — Running TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck"
kubectl config use-context "kind-${CLUSTER_NAME}"

LOG_FILE="/tmp/test-upstream-limits-$(date +%Y%m%d-%H%M%S).log"
log "Log file: ${LOG_FILE}"

cd "${REPO_ROOT}/acceptance"

go test \
  ./tests/api-gateway/... \
  -v \
  -run "TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck" \
  -timeout 60m \
  -p 1 \
  -consul-image="${CONSUL_IMAGE}" \
  -consul-k8s-image="${K8S_IMAGE}" \
  -consul-dataplane-image="${DATAPLANE_IMAGE}" \
  -use-kind \
  -no-cleanup-on-failure \
  2>&1 | tee "${LOG_FILE}"

TEST_EXIT=${PIPESTATUS[0]}

echo ""
if [[ ${TEST_EXIT} -eq 0 ]]; then
  ok "TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck PASSED"
else
  fail "TestAPIGateway_UpstreamLimits_And_PassiveHealthCheck FAILED (exit ${TEST_EXIT}) — see ${LOG_FILE}"
fi
