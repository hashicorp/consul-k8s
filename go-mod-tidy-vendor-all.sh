#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! command -v go >/dev/null 2>&1; then
  echo "go is required but not found in PATH" >&2
  exit 1
fi

cd "$ROOT_DIR"

GO_MOD_FILES=()
while IFS= read -r go_mod_file; do
  GO_MOD_FILES+=("$go_mod_file")
done < <(find . -type f -name 'go.mod' -not -path '*/vendor/*' | LC_ALL=C sort)

if [[ ${#GO_MOD_FILES[@]} -eq 0 ]]; then
  echo "No Go modules found under $ROOT_DIR"
  exit 0
fi

echo "Found ${#GO_MOD_FILES[@]} modules"

for go_mod_file in "${GO_MOD_FILES[@]}"; do
  module_dir="$(dirname "$go_mod_file")"
  echo "==> $module_dir"
  (
    cd "$module_dir"
    go mod tidy
    go mod vendor
  )
done

echo "Completed go mod tidy + vendor for all modules."
