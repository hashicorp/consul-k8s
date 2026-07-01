#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# Cuts a release branch, prepares it, and opens a release-preparation PR.
#
# Workflow:
#   1. Create   release/<version>           from a source branch (e.g. release/2.0.x)
#   2. Create   prepare-release-<version>   from release/<version>
#   3. Run      `make prepare-release`       on the prepare branch and commit the result
#   4. Open a PR: prepare-release-<version> -> release/<version> and print its URL
#
# Usage:
#   ./release-scripts/prepare-release-branch.sh [options]
#
# Options:
#   -n, --dry-run   Print the commands that would run; create/change/push nothing.
#   -y, --yes       Non-interactive: skip the prompts/confirmation and use the
#                   values from the environment (or the DEFAULT_* values below).
#   -h, --help      Show this help and exit.
#
# Values are prompted interactively with a [default]; press Enter to accept.
# Each prompt is seeded from its matching environment variable when set, else the
# DEFAULT_* value below. The remote defaults to "origin" (override REMOTE=<name>).

set -euo pipefail

usage() {
  sed -n '/^# Cuts /,/^set /p' "$0" | sed '/^set /d; s/^# \{0,1\}//; s/^#$//'
}

# -----------------------------------------------------------------------------
# Flags
# -----------------------------------------------------------------------------
DRY_RUN=false
INTERACTIVE=true
for arg in "$@"; do
  case "${arg}" in
    -n | --dry-run) DRY_RUN=true ;;
    -y | --yes) INTERACTIVE=false ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: ${arg}" >&2
      usage >&2
      exit 1
      ;;
  esac
done

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

REMOTE="${REMOTE:-origin}"

# -----------------------------------------------------------------------------
# Defaults offered at each prompt. Edit these to change the defaults.
# -----------------------------------------------------------------------------
DEFAULT_SOURCE_BRANCH=release/2.0.x
DEFAULT_CONSUL_K8S_RELEASE_VERSION=2.0.2
DEFAULT_CONSUL_K8S_RELEASE_DATE="July 8, 2026"
DEFAULT_CONSUL_K8S_LAST_RELEASE_GIT_TAG=v2.0.1
DEFAULT_CONSUL_K8S_CONSUL_VERSION=1.16.2
DEFAULT_CONSUL_K8S_CONSUL_DATAPLANE_VERSION=2.0.2
DEFAULT_CONSUL_K8S_PRERELEASE_VERSION=

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
# Prerequisite checks (git is always required; the rest only warn in dry-run)
# -----------------------------------------------------------------------------
for cmd in git make gh; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    if [[ "${cmd}" == "git" ]]; then
      echo "Error: required command 'git' not found in PATH." >&2
      exit 1
    fi
    fail_or_warn "required command '${cmd}' not found in PATH."
  fi
done

if ! gh auth status >/dev/null 2>&1; then
  fail_or_warn "GitHub CLI is not authenticated. Run 'gh auth login' and try again."
fi

# Operate from the repository root so `make` runs against the right tree.
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "${REPO_ROOT}"

# A dirty tree would be carried onto the new branches, so require a clean start.
if [[ -n "$(git status --porcelain)" ]]; then
  fail_or_warn "working tree is not clean. Commit or stash your changes first."
fi

# -----------------------------------------------------------------------------
# Collect inputs (seeded from the environment or the DEFAULT_* values above;
# prompts are shown only in interactive mode)
# -----------------------------------------------------------------------------
SOURCE_BRANCH="${SOURCE_BRANCH:-${DEFAULT_SOURCE_BRANCH}}"
CONSUL_K8S_RELEASE_VERSION="${CONSUL_K8S_RELEASE_VERSION:-${DEFAULT_CONSUL_K8S_RELEASE_VERSION}}"
CONSUL_K8S_RELEASE_DATE="${CONSUL_K8S_RELEASE_DATE:-${DEFAULT_CONSUL_K8S_RELEASE_DATE}}"
CONSUL_K8S_LAST_RELEASE_GIT_TAG="${CONSUL_K8S_LAST_RELEASE_GIT_TAG:-${DEFAULT_CONSUL_K8S_LAST_RELEASE_GIT_TAG}}"
CONSUL_K8S_CONSUL_VERSION="${CONSUL_K8S_CONSUL_VERSION:-${DEFAULT_CONSUL_K8S_CONSUL_VERSION}}"
CONSUL_K8S_CONSUL_DATAPLANE_VERSION="${CONSUL_K8S_CONSUL_DATAPLANE_VERSION:-${DEFAULT_CONSUL_K8S_CONSUL_DATAPLANE_VERSION}}"
CONSUL_K8S_PRERELEASE_VERSION="${CONSUL_K8S_PRERELEASE_VERSION:-${DEFAULT_CONSUL_K8S_PRERELEASE_VERSION}}"

if [[ "${INTERACTIVE}" == "true" ]]; then
  echo "Enter release details (press Enter to accept each [default]):"
  echo
  prompt_var SOURCE_BRANCH                       "${SOURCE_BRANCH}"
  prompt_var CONSUL_K8S_RELEASE_VERSION          "${CONSUL_K8S_RELEASE_VERSION}"
  prompt_var CONSUL_K8S_RELEASE_DATE             "${CONSUL_K8S_RELEASE_DATE}"
  prompt_var CONSUL_K8S_LAST_RELEASE_GIT_TAG     "${CONSUL_K8S_LAST_RELEASE_GIT_TAG}"
  prompt_var CONSUL_K8S_CONSUL_VERSION           "${CONSUL_K8S_CONSUL_VERSION}"
  prompt_var CONSUL_K8S_CONSUL_DATAPLANE_VERSION "${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}"
  prompt_var CONSUL_K8S_PRERELEASE_VERSION       "${CONSUL_K8S_PRERELEASE_VERSION}"
  echo
fi

# Export so `make prepare-release` and any child processes inherit the values.
export SOURCE_BRANCH CONSUL_K8S_RELEASE_VERSION CONSUL_K8S_RELEASE_DATE \
  CONSUL_K8S_LAST_RELEASE_GIT_TAG CONSUL_K8S_CONSUL_VERSION \
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION CONSUL_K8S_PRERELEASE_VERSION

RELEASE_BRANCH="release/${CONSUL_K8S_RELEASE_VERSION}"
PREPARE_BRANCH="prepare-release-${CONSUL_K8S_RELEASE_VERSION}"

# -----------------------------------------------------------------------------
# Show the plan and confirm before any push / PR (these are not reversible)
# -----------------------------------------------------------------------------
cat <<EOF
The following actions will be performed on remote '${REMOTE}':

  Source branch                        = ${SOURCE_BRANCH}
  Release branch (created/used)        = ${RELEASE_BRANCH}
  Prepare branch (new)                 = ${PREPARE_BRANCH}

  CONSUL_K8S_RELEASE_VERSION           = ${CONSUL_K8S_RELEASE_VERSION}
  CONSUL_K8S_RELEASE_DATE              = ${CONSUL_K8S_RELEASE_DATE}
  CONSUL_K8S_LAST_RELEASE_GIT_TAG      = ${CONSUL_K8S_LAST_RELEASE_GIT_TAG}
  CONSUL_K8S_CONSUL_VERSION            = ${CONSUL_K8S_CONSUL_VERSION}
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION  = ${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}
  CONSUL_K8S_PRERELEASE_VERSION        = ${CONSUL_K8S_PRERELEASE_VERSION:-<none>}

Steps:
  1. Create ${RELEASE_BRANCH} from ${REMOTE}/${SOURCE_BRANCH} (and push if new)
  2. Create ${PREPARE_BRANCH} from ${RELEASE_BRANCH}
  3. Run 'make prepare-release' and commit the changes
  4. Push ${PREPARE_BRANCH} and open a PR into ${RELEASE_BRANCH}

EOF

if [[ "${DRY_RUN}" == "true" ]]; then
  echo ">>> DRY RUN: nothing will be created, changed, pushed, or opened."
  echo ">>> The commands that would run are printed below, prefixed with [dry-run]."
  echo ">>> Branch-existence checks use your local clone; run 'git fetch ${REMOTE}' first for accuracy."
  echo
elif [[ "${INTERACTIVE}" == "true" ]]; then
  read -r -p "Proceed? [y/N] " response || true
  case "${response}" in
    [yY] | [yY][eE][sS]) ;;
    *)
      echo "Aborted. No changes were made."
      exit 1
      ;;
  esac
fi

# -----------------------------------------------------------------------------
# 1. Create (or reuse) the release branch from the source branch
# -----------------------------------------------------------------------------
echo "==> Fetching latest from ${REMOTE}..."
run git fetch "${REMOTE}"

if ! git rev-parse --verify --quiet "refs/remotes/${REMOTE}/${SOURCE_BRANCH}" >/dev/null; then
  fail_or_warn "source branch '${SOURCE_BRANCH}' not found on ${REMOTE}."
fi

if git show-ref --verify --quiet "refs/heads/${PREPARE_BRANCH}"; then
  fail_or_warn "local branch '${PREPARE_BRANCH}' already exists. Delete it or pick another version."
fi

if git rev-parse --verify --quiet "refs/remotes/${REMOTE}/${RELEASE_BRANCH}" >/dev/null; then
  echo "==> ${RELEASE_BRANCH} already exists on ${REMOTE}; using it as-is."
  run git checkout -B "${RELEASE_BRANCH}" "${REMOTE}/${RELEASE_BRANCH}"
else
  echo "==> Creating ${RELEASE_BRANCH} from ${REMOTE}/${SOURCE_BRANCH}..."
  run git checkout -b "${RELEASE_BRANCH}" "${REMOTE}/${SOURCE_BRANCH}"
  run git push -u "${REMOTE}" "${RELEASE_BRANCH}"
fi

# -----------------------------------------------------------------------------
# 2. Create the prepare branch from the release branch
# -----------------------------------------------------------------------------
echo "==> Creating ${PREPARE_BRANCH} from ${RELEASE_BRANCH}..."
run git checkout -b "${PREPARE_BRANCH}"

# -----------------------------------------------------------------------------
# 3. Prepare the release and commit the result
# -----------------------------------------------------------------------------
echo "==> Running 'make prepare-release'..."
run make prepare-release

if [[ "${DRY_RUN}" != "true" && -z "$(git status --porcelain)" ]]; then
  echo "Error: 'make prepare-release' produced no changes; nothing to commit." >&2
  exit 1
fi

run git add -A
run git commit -m "Prepare release ${CONSUL_K8S_RELEASE_VERSION}"

# -----------------------------------------------------------------------------
# 4. Push the prepare branch and open the PR
# -----------------------------------------------------------------------------
echo "==> Pushing ${PREPARE_BRANCH} to ${REMOTE}..."
run git push -u "${REMOTE}" "${PREPARE_BRANCH}"

PR_TITLE="Prepare release ${CONSUL_K8S_RELEASE_VERSION}"
PR_BODY="$(cat <<EOF
Automated release preparation for consul-k8s ${CONSUL_K8S_RELEASE_VERSION}.

Generated by \`prepare-release-branch.sh\` (output of \`make prepare-release\`).

- Release version: ${CONSUL_K8S_RELEASE_VERSION}
- Release date: ${CONSUL_K8S_RELEASE_DATE}
- Consul version: ${CONSUL_K8S_CONSUL_VERSION}
- Consul Dataplane version: ${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}
- Last release tag: ${CONSUL_K8S_LAST_RELEASE_GIT_TAG}

Base branch \`${RELEASE_BRANCH}\` was created from \`${SOURCE_BRANCH}\`.
EOF
)"

echo "==> Opening pull request: ${PREPARE_BRANCH} -> ${RELEASE_BRANCH}..."
if [[ "${DRY_RUN}" == "true" ]]; then
  printf '  [dry-run] $ gh pr create --base %q --head %q --title %q --body <PR body>\n' \
    "${RELEASE_BRANCH}" "${PREPARE_BRANCH}" "${PR_TITLE}"
  echo
  echo "Dry run complete. No branches were created and nothing was pushed."
  exit 0
fi

PR_URL="$(gh pr create \
  --base "${RELEASE_BRANCH}" \
  --head "${PREPARE_BRANCH}" \
  --title "${PR_TITLE}" \
  --body "${PR_BODY}")"

echo
echo "Pull request created:"
echo "${PR_URL}"
