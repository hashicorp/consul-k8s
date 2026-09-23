#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# Verifies that the released consul-k8s-control-plane container image is
# available in the HashiCorp registry and reports its version and image digest
# (SHA). Uses the Podman CLI.
#
# Workflow:
#   1. Pull  docker.io/hashicorp/consul-k8s-control-plane:<version>  (prints pull output)
#   2. Print the image digest (sha256) and image ID
#   3. Run   `consul-k8s-control-plane version` inside the image (prints output)
#
# Requirements:
#   - podman in PATH. On macOS/Windows, a running Podman machine
#     (`podman machine start`).
#   - For staging, be logged in to the Artifactory registry
#     (`podman login crt-core-staging-docker-local.artifactory.hashicorp.engineering`).
#
# Usage:
#   ./verify-release-image.sh [staging|production]
#   CONSUL_K8S_PRODUCT_VERSION=2.0.2 ./verify-release-image.sh staging
#
# If no environment argument is provided, it defaults to production.
# The version is prompted interactively with a [default]; press Enter to accept.
# Setting CONSUL_K8S_PRODUCT_VERSION in the environment pre-fills the default.

set -euo pipefail

# -----------------------------------------------------------------------------
# Defaults offered at the prompt. Edit these to change the defaults.
# -----------------------------------------------------------------------------
DEFAULT_CONSUL_K8S_PRODUCT_VERSION=2.0.2

# -----------------------------------------------------------------------------
# Arguments
# -----------------------------------------------------------------------------
ENVIRONMENT=""
for arg in "$@"; do
  case "${arg}" in
    staging | production) ENVIRONMENT="${arg}" ;;
    -h | --help)
      echo "Usage: ./verify-release-image.sh [staging|production]"
      exit 0
      ;;
    *)
      echo "Unknown argument: ${arg}" >&2
      exit 1
      ;;
  esac
done

ENVIRONMENT="${ENVIRONMENT:-production}"

# The image repository to verify (can be overridden via IMAGE_REPO).
# Use fully qualified names so Podman does not rely on short-name resolution.
if [[ "${ENVIRONMENT}" == "staging" ]]; then
  IMAGE_REPO="${IMAGE_REPO:-crt-core-staging-docker-local.artifactory.hashicorp.engineering/docker.io/hashicorp/consul-k8s-control-plane}"
else
  IMAGE_REPO="${IMAGE_REPO:-docker.io/hashicorp/consul-k8s-control-plane}"
fi

# prompt_var VAR_NAME DEFAULT_VALUE
# Prompts for a value, showing the default; an empty reply keeps the default.
prompt_var() {
  local var_name="$1"
  local default_value="$2"
  local input
  read -r -p "  ${var_name} [${default_value}]: " input || true
  printf -v "${var_name}" '%s' "${input:-${default_value}}"
  export "${var_name}"
}

# -----------------------------------------------------------------------------
# Prerequisite checks
# -----------------------------------------------------------------------------
if ! command -v podman >/dev/null 2>&1; then
  echo "Error: required command 'podman' not found in PATH." >&2
  exit 1
fi

if ! podman info >/dev/null 2>&1; then
  echo "Error: Podman is not running or not reachable." >&2
  echo "       On macOS/Windows, start it with: podman machine start" >&2
  exit 1
fi

# -----------------------------------------------------------------------------
# Collect inputs (an existing env var value pre-fills the default)
# -----------------------------------------------------------------------------
echo "Enter the version to verify (press Enter to accept the [default]):"
prompt_var CONSUL_K8S_PRODUCT_VERSION \
  "${CONSUL_K8S_PRODUCT_VERSION:-${DEFAULT_CONSUL_K8S_PRODUCT_VERSION}}"
echo

IMAGE_REF="${IMAGE_REPO}:${CONSUL_K8S_PRODUCT_VERSION}"

# -----------------------------------------------------------------------------
# 1. Pull the image (podman pull prints its own progress and final image ID)
# -----------------------------------------------------------------------------
echo "==> podman pull ${IMAGE_REF}"
echo
if ! podman pull "${IMAGE_REF}"; then
  echo >&2
  echo "Error: failed to pull ${IMAGE_REF}. Is this version published in the registry?" >&2
  if [[ "${ENVIRONMENT}" == "staging" ]]; then
    echo "       For staging, ensure you are logged in: podman login ${IMAGE_REPO%%/*}" >&2
  fi
  exit 1
fi
echo

# -----------------------------------------------------------------------------
# 2. Report the image digest (SHA) and image ID
# -----------------------------------------------------------------------------
echo "==> Image digest (SHA) for ${IMAGE_REF}:"
DIGEST="$(podman image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "${IMAGE_REF}" 2>/dev/null || true)"
if [[ -n "${DIGEST}" ]]; then
  printf '%s\n' "${DIGEST}"
else
  echo "  (no RepoDigests found for ${IMAGE_REF})"
fi

IMAGE_ID="$(podman image inspect --format '{{.Id}}' "${IMAGE_REF}" 2>/dev/null || true)"
echo "  Image ID: ${IMAGE_ID}"
echo

# -----------------------------------------------------------------------------
# 3. Print the consul-k8s-control-plane version from inside the image
# -----------------------------------------------------------------------------
# Only allocate a TTY (-t) when stdout is a terminal so this also works in CI,
# where attaching a TTY would fail with "the input device is not a TTY".
run_flags=(--rm -i)
if [[ -t 1 ]]; then
  run_flags+=(-t)
fi

echo "==> podman run --rm -ti ${IMAGE_REF} consul-k8s-control-plane version"
echo
podman run "${run_flags[@]}" "${IMAGE_REF}" consul-k8s-control-plane version
echo

echo "==> Verification complete for ${IMAGE_REF}."
