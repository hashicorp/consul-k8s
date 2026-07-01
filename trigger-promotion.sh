#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# Triggers a consul-k8s release promotion to staging or production via `bob`.
#
# Usage:
#   ./trigger-promotion.sh [staging|production] [--dry-run]
#
# The promotion target defaults to "staging" when no argument is provided.
#
# Dry-run mode:
#   Pass --dry-run (or -n), or set DRY_RUN=true, to resolve all inputs and print
#   the env vars and the exact `bob` command WITHOUT executing it.

set -euo pipefail

# -----------------------------------------------------------------------------
# Release variables (defaults shown at each prompt; press Enter to accept)
# -----------------------------------------------------------------------------
# Default values offered at each prompt. Edit these to change the defaults.
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

echo "Enter release values (press Enter to accept each [default]):"
echo
prompt_var CONSUL_K8S_RELEASE_VERSION          "${DEFAULT_CONSUL_K8S_RELEASE_VERSION}"
prompt_var CONSUL_K8S_PRODUCT_VERSION          "${DEFAULT_CONSUL_K8S_PRODUCT_VERSION}"
prompt_var CONSUL_K8S_RELEASE_BRANCH           "${DEFAULT_CONSUL_K8S_RELEASE_BRANCH}"
prompt_var CONSUL_K8S_RELEASE_DATE             "${DEFAULT_CONSUL_K8S_RELEASE_DATE}"
prompt_var CONSUL_K8S_LAST_RELEASE_GIT_TAG     "${DEFAULT_CONSUL_K8S_LAST_RELEASE_GIT_TAG}"
prompt_var CONSUL_K8S_CONSUL_DATAPLANE_VERSION "${DEFAULT_CONSUL_K8S_CONSUL_DATAPLANE_VERSION}"
prompt_var CONSUL_K8S_CONSUL_VERSION           "${DEFAULT_CONSUL_K8S_CONSUL_VERSION}"
echo

# -----------------------------------------------------------------------------
# Arguments: promotion target (staging|production) and optional --dry-run
# -----------------------------------------------------------------------------
DRY_RUN="${DRY_RUN:-false}"
PROMOTION_TARGET=""

for arg in "$@"; do
  case "${arg}" in
    --dry-run | -n)
      DRY_RUN=true
      ;;
    staging | production)
      PROMOTION_TARGET="${arg}"
      ;;
    *)
      echo "Error: unrecognized argument '${arg}'." >&2
      echo "Usage: $0 [staging|production] [--dry-run]" >&2
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

# -----------------------------------------------------------------------------
# Prerequisite checks (bob is only required when actually executing)
# -----------------------------------------------------------------------------
required_cmds=(git)
if [[ "${DRY_RUN}" != "true" ]]; then
  required_cmds+=(bob)
fi
for cmd in "${required_cmds[@]}"; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Error: required command '${cmd}' not found in PATH." >&2
    exit 1
  fi
done

# Ensure the remote ref is current so we resolve the latest commit SHA.
echo "Fetching latest refs for ${CONSUL_K8S_RELEASE_BRANCH} from origin..."
if ! git fetch origin "${CONSUL_K8S_RELEASE_BRANCH}"; then
  if [[ "${DRY_RUN}" == "true" ]]; then
    echo "[DRY RUN] Warning: 'git fetch' failed for ${CONSUL_K8S_RELEASE_BRANCH}; continuing." >&2
  else
    echo "Error: 'git fetch' failed for ${CONSUL_K8S_RELEASE_BRANCH}." >&2
    exit 1
  fi
fi

# Resolve the latest commit SHA of the release branch.
if ! CONSUL_K8S_RELEASE_SHA="$(git rev-parse "origin/${CONSUL_K8S_RELEASE_BRANCH}" 2>/dev/null)"; then
  if [[ "${DRY_RUN}" == "true" ]]; then
    echo "[DRY RUN] Warning: unable to resolve origin/${CONSUL_K8S_RELEASE_BRANCH}; using placeholder SHA." >&2
    CONSUL_K8S_RELEASE_SHA="<unresolved-sha-for-origin/${CONSUL_K8S_RELEASE_BRANCH}>"
  else
    echo "Error: unable to resolve origin/${CONSUL_K8S_RELEASE_BRANCH}. Does the branch exist on origin?" >&2
    exit 1
  fi
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
  --environment=consul
  --slack-channel=C09KX8B2KC6
  --org hashicorp
  --branch "${CONSUL_K8S_RELEASE_BRANCH}"
  "${PROMOTION_TARGET}"
)

# Print the command in a readable, copy-pasteable form.
print_promotion_cmd() {
  printf '%s' "${promotion_cmd[0]}"
  local i
  for ((i = 1; i < ${#promotion_cmd[@]}; i++)); do
    printf ' \\\n  %q' "${promotion_cmd[i]}"
  done
  printf '\n'
}

if [[ "${DRY_RUN}" == "true" ]]; then
  echo "[DRY RUN] Skipping execution. The following command would be run:"
  echo
  print_promotion_cmd
  echo
  echo "[DRY RUN] No promotion was triggered."
  exit 0
fi

read -r -p "Proceed with promotion to ${PROMOTION_TARGET}? [y/N] " response
case "${response}" in
  [yY] | [yY][eE][sS]) ;;
  *)
    echo "Aborted. No promotion was triggered."
    exit 1
    ;;
esac

# -----------------------------------------------------------------------------
# Trigger the promotion
# -----------------------------------------------------------------------------
"${promotion_cmd[@]}"
