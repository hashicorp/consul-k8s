#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
#
# Cuts a release branch, prepares it, and opens a release-preparation PR.
#
# You provide just two things:
#   CONSUL_K8S_RELEASE_SERIES  MAJOR.MINOR to release from (e.g. 2.0 or 1.9)
#   CONSUL_K8S_RELEASE_TYPE    patch | minor  (case-insensitive)
# Everything else - the exact version, the last release tag, the source branch,
# the release date, and the Consul / Consul Dataplane versions - is derived from
# the repo's git tags and chart metadata.
#
#   patch : release the next patch on the given series. The newest vMAJOR.MINOR.*
#           tag is found and its patch is incremented (series 2.0, last tag v2.0.1
#           -> 2.0.2). Source branch: release/MAJOR.MINOR.x.
#   minor : release the next minor in the given major. The highest minor released
#           in MAJOR is found and incremented, with patch 0 (major 2, latest 2.0.x
#           -> 2.1.0). The series' minor part is informational; the next minor is
#           computed from the tags. Source branch: release/MAJOR.<next-minor>.x.
#
# Derived Consul / Consul Dataplane versions:
#   - Consul Dataplane always tracks the consul-k8s version being released.
#   - Consul tracks the consul-k8s version for 2.x and newer; for 1.x and older it
#     is read from charts/consul/Chart.yaml (appVersion) at the previous release
#     tag. Override either by exporting CONSUL_K8S_CONSUL_VERSION or
#     CONSUL_K8S_CONSUL_DATAPLANE_VERSION before running.
#
# Workflow:
#   1. Create   release/<version>           from release/<major>.<minor>.x
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
# The two inputs are prompted interactively with a [default]; press Enter to
# accept. Each is seeded from its matching environment variable when set, else the
# DEFAULT_* value below. Optional overrides (env only, auto-derived when unset):
# CONSUL_K8S_RELEASE_DATE, CONSUL_K8S_CONSUL_VERSION,
# CONSUL_K8S_CONSUL_DATAPLANE_VERSION, CONSUL_K8S_PRERELEASE_VERSION.
# The remote defaults to "origin" (override REMOTE=<name>).
#
# PR creation is retried on transient GitHub errors; tune with (optional):
#   PR_CREATE_MAX_ATTEMPTS = number of attempts (default 5)
#   PR_CREATE_RETRY_DELAY  = seconds to wait between attempts (default 10)

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

# remote_repo_slug REMOTE_NAME  Prints the "owner/repo" slug for a git remote,
# derived from its URL. Handles https/ssh forms and strips any embedded
# credentials (e.g. https://user:token@github.com/owner/repo.git) so a token in
# the remote URL is never passed on to "gh --repo".
remote_repo_slug() {
  local url
  url="$(git remote get-url "$1" 2>/dev/null)" || return 1
  url="${url%.git}"   # strip trailing ".git"
  url="${url#*://}"   # strip leading "scheme://" if present
  url="${url#*@}"     # strip any "user[:token]@" (or "git@")
  url="${url#*[:/]}"  # strip the host, up to the first ":" or "/"
  printf '%s\n' "${url}"
}

REMOTE="${REMOTE:-origin}"

# PR creation is retried because GitHub can briefly reject the createPullRequest
# call right after the base/head branches are pushed (e.g. "Base sha can't be
# blank") until the new refs register. Both knobs are overridable via the env.
PR_CREATE_MAX_ATTEMPTS="${PR_CREATE_MAX_ATTEMPTS:-5}"
PR_CREATE_RETRY_DELAY="${PR_CREATE_RETRY_DELAY:-10}"

# -----------------------------------------------------------------------------
# Defaults offered at each prompt. Edit these to change the defaults.
# -----------------------------------------------------------------------------
DEFAULT_CONSUL_K8S_RELEASE_SERIES="2.0"
DEFAULT_CONSUL_K8S_RELEASE_TYPE="patch"

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
# Collect the two inputs (seeded from the environment or the DEFAULT_* values
# above; prompts are shown only in interactive mode)
# -----------------------------------------------------------------------------
CONSUL_K8S_RELEASE_SERIES="${CONSUL_K8S_RELEASE_SERIES:-${DEFAULT_CONSUL_K8S_RELEASE_SERIES}}"
CONSUL_K8S_RELEASE_TYPE="${CONSUL_K8S_RELEASE_TYPE:-${DEFAULT_CONSUL_K8S_RELEASE_TYPE}}"

if [[ "${INTERACTIVE}" == "true" ]]; then
  echo "Enter release details (press Enter to accept each [default]):"
  echo
  prompt_var CONSUL_K8S_RELEASE_SERIES "${CONSUL_K8S_RELEASE_SERIES}"
  prompt_var CONSUL_K8S_RELEASE_TYPE   "${CONSUL_K8S_RELEASE_TYPE}"
  echo
fi

# Optional overrides: derived below when left unset. Kept out of the prompts so
# the common case only needs the two inputs above.
CONSUL_K8S_RELEASE_DATE="${CONSUL_K8S_RELEASE_DATE:-$(date "+%B %-d, %Y")}"
CONSUL_K8S_CONSUL_VERSION="${CONSUL_K8S_CONSUL_VERSION:-}"
CONSUL_K8S_CONSUL_DATAPLANE_VERSION="${CONSUL_K8S_CONSUL_DATAPLANE_VERSION:-}"
CONSUL_K8S_PRERELEASE_VERSION="${CONSUL_K8S_PRERELEASE_VERSION:-}"

# Validate the inputs (bad input is fatal even in dry-run). The release type is
# accepted in any case (e.g. PATCH, Minor) and normalized to lowercase here.
if [[ ! "${CONSUL_K8S_RELEASE_SERIES}" =~ ^[0-9]+\.[0-9]+$ ]]; then
  echo "Error: CONSUL_K8S_RELEASE_SERIES must be MAJOR.MINOR (e.g. 2.0), got '${CONSUL_K8S_RELEASE_SERIES}'." >&2
  exit 1
fi
CONSUL_K8S_RELEASE_TYPE="$(printf '%s' "${CONSUL_K8S_RELEASE_TYPE}" | tr '[:upper:]' '[:lower:]')"
case "${CONSUL_K8S_RELEASE_TYPE}" in
  patch | minor) ;;
  *)
    echo "Error: CONSUL_K8S_RELEASE_TYPE must be 'patch' or 'minor', got '${CONSUL_K8S_RELEASE_TYPE}'." >&2
    exit 1
    ;;
esac

release_major="${CONSUL_K8S_RELEASE_SERIES%%.*}"
release_minor="${CONSUL_K8S_RELEASE_SERIES#*.}"

# -----------------------------------------------------------------------------
# Fetch refs/tags up front so the version derivation and branch checks below see
# the latest state. This runs even in dry-run because it is read-only: only local
# tags and remote-tracking refs are updated - no branches, working tree, or the
# remote are touched - so the previewed version is accurate. A failure is fatal
# for a real run but only a warning in dry-run, so a dry-run still works offline
# (falling back to whatever tags exist locally).
# -----------------------------------------------------------------------------
echo "==> Fetching latest refs and tags from ${REMOTE}..."
if ! git fetch --tags "${REMOTE}"; then
  fail_or_warn "could not fetch from ${REMOTE}; version derivation and branch checks may use stale local data."
fi

# -----------------------------------------------------------------------------
# Derive the release version and the previous release tag from the git tags.
#   patch -> newest vMAJOR.MINOR.<patch> tag, patch + 1
#   minor -> newest minor released in MAJOR, minor + 1, patch 0
# Pre-release tags (e.g. -rc1) are ignored by the numeric-only match.
# -----------------------------------------------------------------------------
if [[ "${CONSUL_K8S_RELEASE_TYPE}" == "patch" ]]; then
  last_tag="$(git tag --list "v${release_major}.${release_minor}.*" \
    | grep -E "^v${release_major}\.${release_minor}\.[0-9]+$" \
    | sort -V | tail -n1 || true)"
  if [[ -z "${last_tag}" ]]; then
    fail_or_warn "no existing release tag found for series ${release_major}.${release_minor}; cannot compute the next patch."
  fi
  last_patch="${last_tag##*.}"
  CONSUL_K8S_RELEASE_VERSION="${release_major}.${release_minor}.$(( ${last_patch:-0} + 1 ))"
  CONSUL_K8S_LAST_RELEASE_GIT_TAG="${last_tag:-<none>}"
else
  last_minor="$(git tag --list "v${release_major}.*" \
    | grep -E "^v${release_major}\.[0-9]+\.[0-9]+$" \
    | sed -E "s/^v${release_major}\.([0-9]+)\..*/\1/" \
    | sort -n | tail -n1 || true)"
  if [[ -z "${last_minor}" ]]; then
    fail_or_warn "no existing release tag found for major ${release_major}; cannot compute the next minor."
  fi
  CONSUL_K8S_RELEASE_VERSION="${release_major}.$(( ${last_minor:-0} + 1 )).0"
  # Newest patch tag of the current (soon to be previous) minor: seeds the
  # changelog range and, for 1.x, the Consul version lookup below.
  CONSUL_K8S_LAST_RELEASE_GIT_TAG="$(git tag --list "v${release_major}.${last_minor:-0}.*" \
    | grep -E "^v${release_major}\.${last_minor:-0}\.[0-9]+$" \
    | sort -V | tail -n1 || true)"
  if [[ -z "${CONSUL_K8S_LAST_RELEASE_GIT_TAG}" ]]; then
    fail_or_warn "could not determine the previous release tag for major ${release_major}."
    CONSUL_K8S_LAST_RELEASE_GIT_TAG="<none>"
  fi
fi

# Long-lived source branch for the release line: release/<major>.<minor>.x.
CONSUL_K8S_SOURCE_BRANCH="release/${CONSUL_K8S_RELEASE_VERSION%.*}.x"

# Consul Dataplane always tracks the consul-k8s version being released.
if [[ -z "${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}" ]]; then
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION="${CONSUL_K8S_RELEASE_VERSION}"
fi

# Consul tracks the consul-k8s version for 2.x+; for older lines it comes from
# charts/consul/Chart.yaml (appVersion) at the previous release tag.
if [[ -z "${CONSUL_K8S_CONSUL_VERSION}" ]]; then
  if (( release_major >= 2 )); then
    CONSUL_K8S_CONSUL_VERSION="${CONSUL_K8S_RELEASE_VERSION}"
  else
    CONSUL_K8S_CONSUL_VERSION="$(git show "${CONSUL_K8S_LAST_RELEASE_GIT_TAG}:charts/consul/Chart.yaml" 2>/dev/null \
      | awk -F': ' '/^appVersion:/ { gsub(/[[:space:]]/, "", $2); print $2; exit }' || true)"
    if [[ -z "${CONSUL_K8S_CONSUL_VERSION}" ]]; then
      fail_or_warn "could not read appVersion from ${CONSUL_K8S_LAST_RELEASE_GIT_TAG}:charts/consul/Chart.yaml for the Consul version."
      CONSUL_K8S_CONSUL_VERSION="<unresolved>"
    fi
  fi
fi

# Export so `make prepare-release` and any child processes inherit the values.
export CONSUL_K8S_SOURCE_BRANCH CONSUL_K8S_RELEASE_VERSION CONSUL_K8S_RELEASE_DATE \
  CONSUL_K8S_LAST_RELEASE_GIT_TAG CONSUL_K8S_CONSUL_VERSION \
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION CONSUL_K8S_PRERELEASE_VERSION

RELEASE_BRANCH="release/${CONSUL_K8S_RELEASE_VERSION}"
PREPARE_BRANCH="prepare-release-${CONSUL_K8S_RELEASE_VERSION}"

# Resolve the GitHub repo the PR will target from the push remote's URL, so
# "gh pr create --repo" never depends on a configured default (this checkout can
# have multiple remotes, e.g. origin and upstream).
PR_REPO="$(remote_repo_slug "${REMOTE}")" \
  || fail_or_warn "could not determine the GitHub repo from remote '${REMOTE}' (is it set?)."

# -----------------------------------------------------------------------------
# Show the plan and confirm before any push / PR (these are not reversible)
# -----------------------------------------------------------------------------
cat <<EOF
The following actions will be performed on remote '${REMOTE}':

  Release series (input)               = ${CONSUL_K8S_RELEASE_SERIES}
  Release type (input)                 = ${CONSUL_K8S_RELEASE_TYPE}
  Source branch (derived)              = ${CONSUL_K8S_SOURCE_BRANCH}
  Release branch (created/used)        = ${RELEASE_BRANCH}
  Prepare branch (new)                 = ${PREPARE_BRANCH}
  PR target repo (gh --repo)           = ${PR_REPO:-<unknown>}

  CONSUL_K8S_RELEASE_VERSION           = ${CONSUL_K8S_RELEASE_VERSION}
  CONSUL_K8S_RELEASE_DATE              = ${CONSUL_K8S_RELEASE_DATE}
  CONSUL_K8S_LAST_RELEASE_GIT_TAG      = ${CONSUL_K8S_LAST_RELEASE_GIT_TAG}
  CONSUL_K8S_CONSUL_VERSION            = ${CONSUL_K8S_CONSUL_VERSION}
  CONSUL_K8S_CONSUL_DATAPLANE_VERSION  = ${CONSUL_K8S_CONSUL_DATAPLANE_VERSION}
  CONSUL_K8S_PRERELEASE_VERSION        = ${CONSUL_K8S_PRERELEASE_VERSION:-<none>}

Steps:
  1. Create ${RELEASE_BRANCH} from ${REMOTE}/${CONSUL_K8S_SOURCE_BRANCH} (and push if new)
  2. Create ${PREPARE_BRANCH} from ${RELEASE_BRANCH}
  3. Run 'make prepare-release' and commit the changes
  4. Push ${PREPARE_BRANCH} and open a PR into ${RELEASE_BRANCH}

EOF

if [[ "${DRY_RUN}" == "true" ]]; then
  echo ">>> DRY RUN: nothing will be created, changed, pushed, or opened."
  echo ">>> The commands that would run are printed below, prefixed with [dry-run]."
  echo ">>> Refs and tags were fetched (read-only) above, so the derived version and branch checks reflect ${REMOTE}."
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
#    (refs/tags were already fetched above, before the version derivation)
# -----------------------------------------------------------------------------
if ! git rev-parse --verify --quiet "refs/remotes/${REMOTE}/${CONSUL_K8S_SOURCE_BRANCH}" >/dev/null; then
  fail_or_warn "source branch '${CONSUL_K8S_SOURCE_BRANCH}' not found on ${REMOTE}."
fi

if git show-ref --verify --quiet "refs/heads/${PREPARE_BRANCH}"; then
  fail_or_warn "local branch '${PREPARE_BRANCH}' already exists. Delete it or pick another version."
fi

if git rev-parse --verify --quiet "refs/remotes/${REMOTE}/${RELEASE_BRANCH}" >/dev/null; then
  echo "==> ${RELEASE_BRANCH} already exists on ${REMOTE}; using it as-is."
  run git checkout -B "${RELEASE_BRANCH}" "${REMOTE}/${RELEASE_BRANCH}"
else
  echo "==> Creating ${RELEASE_BRANCH} from ${REMOTE}/${CONSUL_K8S_SOURCE_BRANCH}..."
  run git checkout -b "${RELEASE_BRANCH}" "${REMOTE}/${CONSUL_K8S_SOURCE_BRANCH}"
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

Base branch \`${RELEASE_BRANCH}\` was created from \`${CONSUL_K8S_SOURCE_BRANCH}\`.
EOF
)"

echo "==> Opening pull request: ${PREPARE_BRANCH} -> ${RELEASE_BRANCH} (repo ${PR_REPO})..."
if [[ "${DRY_RUN}" == "true" ]]; then
  printf '  [dry-run] $ gh pr create --repo %q --base %q --head %q --title %q --body <PR body>\n' \
    "${PR_REPO}" "${RELEASE_BRANCH}" "${PREPARE_BRANCH}" "${PR_TITLE}"
  printf '  [dry-run] (retried up to %s time(s), %ss apart, on transient failures)\n' \
    "${PR_CREATE_MAX_ATTEMPTS}" "${PR_CREATE_RETRY_DELAY}"
  echo
  echo "Dry run complete. No branches were created and nothing was pushed."
  exit 0
fi

# Create the PR, retrying on transient GitHub errors (e.g. a blank base SHA while
# the just-pushed refs settle). If a PR for this branch already exists, reuse it.
pr_err_file="$(mktemp "${TMPDIR:-/tmp}/prepare-release-pr.XXXXXX")"
PR_URL=""
attempt=1
while :; do
  if PR_URL="$(gh pr create \
    --repo "${PR_REPO}" \
    --base "${RELEASE_BRANCH}" \
    --head "${PREPARE_BRANCH}" \
    --title "${PR_TITLE}" \
    --body "${PR_BODY}" 2>"${pr_err_file}")"; then
    break
  fi
  cat "${pr_err_file}" >&2
  if grep -qi 'already exists' "${pr_err_file}"; then
    PR_URL="$(gh pr view "${PREPARE_BRANCH}" --repo "${PR_REPO}" --json url -q .url 2>/dev/null || true)"
    if [[ -n "${PR_URL}" ]]; then
      echo "==> A pull request for ${PREPARE_BRANCH} already exists; using it." >&2
      break
    fi
  fi
  if (( attempt >= PR_CREATE_MAX_ATTEMPTS )); then
    rm -f "${pr_err_file}"
    echo "Error: 'gh pr create' failed after ${PR_CREATE_MAX_ATTEMPTS} attempt(s)." >&2
    exit 1
  fi
  echo "==> PR creation attempt ${attempt}/${PR_CREATE_MAX_ATTEMPTS} failed; retrying in ${PR_CREATE_RETRY_DELAY}s..." >&2
  sleep "${PR_CREATE_RETRY_DELAY}"
  attempt=$((attempt + 1))
done
rm -f "${pr_err_file}"

echo
echo "Pull request created:"
echo "${PR_URL}"
