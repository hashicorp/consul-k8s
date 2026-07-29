#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# Triggers a consul-k8s release promotion to staging or production via `bob`.
#
# Usage:
#   ./release-scripts/trigger-promotion.sh [staging|production] [options]
#
# The promotion target defaults to "staging" when no argument is provided.
#
# Options:
#   -n, --dry-run   Resolve all inputs and print the exact `bob` command WITHOUT
#                   executing it (also enabled by setting DRY_RUN=true).
#   -y, --yes       Non-interactive: skip the prompts/confirmation and use the
#                   values from the environment (or the DEFAULT_* values below).
#   -h, --help      Show this help and exit.
#
# Values are prompted interactively with a [default]; press Enter to accept.
# Each prompt is seeded from its matching environment variable when set, else the
# DEFAULT_* value below.

set -euo pipefail

usage() {
  sed -n '/^# Triggers /,/^set /p' "$0" | sed '/^set /d; s/^# \{0,1\}//; s/^#$//'
}

# -----------------------------------------------------------------------------
# Flags / arguments (promotion target plus dry-run / non-interactive switches)
# -----------------------------------------------------------------------------
DRY_RUN="${DRY_RUN:-false}"
INTERACTIVE=true
PROMOTION_TARGET=""
for arg in "$@"; do
  case "${arg}" in
    -n | --dry-run) DRY_RUN=true ;;
    -y | --yes) INTERACTIVE=false ;;
    -h | --help)
      usage
      exit 0
      ;;
    staging | production) PROMOTION_TARGET="${arg}" ;;
    *)
      echo "Unknown argument: ${arg}" >&2
      usage >&2
      exit 1
      ;;
  esac
done

# Normalize DRY_RUN to strictly "true" or "false" (accepts true/1/yes from env).
case "${DRY_RUN}" in
  true | TRUE | True | 1 | yes | YES) DRY_RUN=true ;;
  *) DRY_RUN=false ;;
esac

PROMOTION_TARGET="${PROMOTION_TARGET:-staging}"

# run CMD...  Runs CMD, or in dry-run mode prints it (shell-quoted) instead.
run() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    { printf '  [dry-run] $'; printf ' %q' "$@"; printf '\n'; }
  else
    "$@"
  fi
}

# fail_or_warn MESSAGE  Errors out normally; in dry-run only warns so the preview
# can run to completion.
fail_or_warn() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    echo "Warning (dry-run): $1" >&2
  else
    echo "Error: $1" >&2
    exit 1
  fi
}

# -----------------------------------------------------------------------------
# Release variables (defaults offered at each prompt; edit to change them)
# -----------------------------------------------------------------------------
DEFAULT_CONSUL_K8S_RELEASE_VERSION=2.0.2
# DEFAULT_CONSUL_K8S_PRERELEASE_VERSION=rc1
DEFAULT_CONSUL_K8S_PRODUCT_VERSION=2.0.2
DEFAULT_CONSUL_K8S_RELEASE_BRANCH=release/2.0.2
DEFAULT_CONSUL_K8S_RELEASE_DATE="July 8, 2026"
DEFAULT_CONSUL_K8S_LAST_RELEASE_GIT_TAG=v2.0.1
DEFAULT_CONSUL_K8S_CONSUL_DATAPLANE_VERSION=2.0.2
# NOTE!: This should be the latest Consul **CE** version this release supports.
DEFAULT_CONSUL_K8S_CONSUL_VERSION=2.0.2

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
# Collect inputs (seeded from the environment or the DEFAULT_* values above;
# prompts are shown only in interactive mode)
# -----------------------------------------------------------------------------
CONSUL_K8S_RELEASE_VERSION="${CONSUL_K8S_RELEASE_VERSION:-${DEFAULT_CONSUL_K8S_RELEASE_VERSION}}"
CONSUL_K8S_PRODUCT_VERSION="${CONSUL_K8S_PRODUCT_VERSION:-${DEFAULT_CONSUL_K8S_PRODUCT_VERSION}}"
CONSUL_K8S_RELEASE_BRANCH="${CONSUL_K8S_RELEASE_BRANCH:-${DEFAULT_CONSUL_K8S_RELEASE_BRANCH}}"
CONSUL_K8S_RELEASE_DATE="${CONSUL_K8S_RELEASE_DATE:-${DEFAULT_CONSUL_K8S_RELEASE_DATE}}"
CONSUL_K8S_LAST_RELEASE_GIT_TAG="${CONSUL_K8S_LAST_RELEASE_GIT_TAG:-${DEFAULT_CONSUL_K8S_LAST_RELEASE_GIT_TAG}}"
CONSUL_K8S_CONSUL_DATAPLANE_VERSION="${CONSUL_K8S_CONSUL_DATAPLANE_VERSION:-${DEFAULT_CONSUL_K8S_CONSUL_DATAPLANE_VERSION}}"
CONSUL_K8S_CONSUL_VERSION="${CONSUL_K8S_CONSUL_VERSION:-${DEFAULT_CONSUL_K8S_CONSUL_VERSION}}"

if [[ "${INTERACTIVE}" == "true" ]]; then
  echo "Enter release values (press Enter to accept each [default]):"
  echo
  prompt_var CONSUL_K8S_RELEASE_VERSION          "${CONSUL_K8S_RELEASE_VERSION}"
  prompt_var CONSUL_K8S_PRODUCT_VERSION          "${CONSUL_K8S_PRODUCT_VERSION}"
  prompt_var CONSUL_K8S_RELEASE_BRANCH           "${CONSUL_K8S_RELEASE_BRANCH}"
  prompt_var CONSUL_K8S_RELEASE_DATE             "${CONSUL_K8S_RELEASE_DATE}"
  prompt_var CONSUL_K8S_LAST_RELEASE_GIT_TAG     "${CONSUL_K8S_LAST_RELEASE_GIT_TAG}"
  prompt_var CONSUL_K8S_CONSUL_DATAPLANE_VERSION "${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}"
  prompt_var CONSUL_K8S_CONSUL_VERSION           "${CONSUL_K8S_CONSUL_VERSION}"
  echo
fi

# Export so child processes (e.g. bob) inherit the resolved values.
export CONSUL_K8S_RELEASE_VERSION CONSUL_K8S_PRODUCT_VERSION CONSUL_K8S_RELEASE_BRANCH \
  CONSUL_K8S_RELEASE_DATE CONSUL_K8S_LAST_RELEASE_GIT_TAG \
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION CONSUL_K8S_CONSUL_VERSION

# -----------------------------------------------------------------------------
# Prerequisite checks (git is always required; bob only for a real promotion)
# -----------------------------------------------------------------------------
if ! command -v git >/dev/null 2>&1; then
  echo "Error: required command 'git' not found in PATH." >&2
  exit 1
fi
if ! command -v bob >/dev/null 2>&1; then
  fail_or_warn "required command 'bob' not found in PATH."
fi

# Ensure the remote ref is current so we resolve the latest commit SHA.
echo "Fetching latest refs for ${CONSUL_K8S_RELEASE_BRANCH} from origin..."
if ! git fetch origin "${CONSUL_K8S_RELEASE_BRANCH}"; then
  fail_or_warn "'git fetch' failed for ${CONSUL_K8S_RELEASE_BRANCH}."
fi

# Resolve the latest commit SHA of the release branch.
if ! CONSUL_K8S_RELEASE_SHA="$(git rev-parse "origin/${CONSUL_K8S_RELEASE_BRANCH}" 2>/dev/null)"; then
  fail_or_warn "unable to resolve origin/${CONSUL_K8S_RELEASE_BRANCH}. Does the branch exist on origin?"
  CONSUL_K8S_RELEASE_SHA="<unresolved-sha-for-origin/${CONSUL_K8S_RELEASE_BRANCH}>"
fi
export CONSUL_K8S_RELEASE_SHA

# -----------------------------------------------------------------------------
# Print configuration and confirm
# -----------------------------------------------------------------------------
cat <<EOF

The following variables are set:

  CONSUL_K8S_RELEASE_VERSION           = ${CONSUL_K8S_RELEASE_VERSION}
  CONSUL_K8S_PRODUCT_VERSION           = ${CONSUL_K8S_PRODUCT_VERSION}
  CONSUL_K8S_RELEASE_BRANCH            = ${CONSUL_K8S_RELEASE_BRANCH}
  CONSUL_K8S_RELEASE_DATE              = ${CONSUL_K8S_RELEASE_DATE}
  CONSUL_K8S_LAST_RELEASE_GIT_TAG      = ${CONSUL_K8S_LAST_RELEASE_GIT_TAG}
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION  = ${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}
  CONSUL_K8S_CONSUL_VERSION            = ${CONSUL_K8S_CONSUL_VERSION}
  CONSUL_K8S_RELEASE_SHA               = ${CONSUL_K8S_RELEASE_SHA}

  Promotion target                     = ${PROMOTION_TARGET}
  Dry run                              = ${DRY_RUN}

EOF

# -----------------------------------------------------------------------------
# Build the promotion command (single source of truth for run + dry-run)
# -----------------------------------------------------------------------------
promotion_cmd=(
  bob trigger-promotion
  --product-name=consul-k8s
  --repo=consul-k8s
  --product-version="${CONSUL_K8S_PRODUCT_VERSION}"
  --sha="${CONSUL_K8S_RELEASE_SHA}"
  --environment=consul-k8s-oss
  --slack-channel=C09KX8B2KC6
  --org hashicorp
  --branch "${CONSUL_K8S_RELEASE_BRANCH}"
  "${PROMOTION_TARGET}"
)

if [[ "${DRY_RUN}" == "true" ]]; then
  echo ">>> DRY RUN: no promotion will be triggered."
  echo ">>> The command that would run is printed below, prefixed with [dry-run]."
  echo
elif [[ "${INTERACTIVE}" == "true" ]]; then
  read -r -p "Proceed with promotion to ${PROMOTION_TARGET}? [y/N] " response || true
  case "${response}" in
    [yY] | [yY][eE][sS]) ;;
    *)
      echo "Aborted. No promotion was triggered."
      exit 1
      ;;
  esac
fi

# -----------------------------------------------------------------------------
# Trigger the promotion (printed in dry-run, executed otherwise)
# -----------------------------------------------------------------------------
echo "==> Triggering promotion to ${PROMOTION_TARGET}..."
run "${promotion_cmd[@]}"

if [[ "${DRY_RUN}" == "true" ]]; then
  echo
  echo "Dry run complete. No promotion was triggered."
fi
