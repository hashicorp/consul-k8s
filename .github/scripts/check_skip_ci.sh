#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

# first argument is the list, second is the item to check
function contains() {
  list=($1)
  for item in "${list[@]}"; do
    if [ "$item" == "$2" ]; then
      return 0
    fi
  done
  return 1
}

# Get the list of changed files
# Using `git merge-base` ensures that we're always comparing against the correct branch point.
#For example, given the commits:
#
# A---B---C---D---W---X---Y---Z # origin/main
#             \---E---F         # feature/branch
#
# ... `git merge-base origin/$SKIP_CHECK_BRANCH HEAD` would return commit `D`
# `...HEAD` specifies from the common ancestor to the latest commit on the current branch (HEAD)..
skip_check_branch=${SKIP_CHECK_BRANCH:?SKIP_CHECK_BRANCH is required}
files_to_check=$(git diff --name-only "$(git merge-base origin/$skip_check_branch HEAD~)"...HEAD)

# Define the directories to check
skipped_directories=("assets" ".changelog" "version")

files_to_skip=("LICENSE" ".copywrite.hcl" ".gitignore")

# Loop through the changed files and find directories/files outside the skipped ones
files_to_check_array=($files_to_check)
for file_to_check in "${files_to_check_array[@]}"; do
  file_is_skipped=false
  echo "checking file: $file_to_check"

  # Allow changes to:
  # - This script
  # - Files in the skipped directories
  # - Markdown files
  for dir in "${skipped_directories[@]}"; do
    if [[ "$file_to_check" == */check_skip_ci.sh ]] ||
      [[ "$file_to_check" == "$dir/"* ]] ||
      [[ "$file_to_check" == *.md ]] ||
      contains "${files_to_skip[*]}" "$file_to_check"; then
      file_is_skipped=true
      break
    fi
  done

  if [ "$file_is_skipped" != "true" ]; then
    echo -e "non-skippable file changed: $file_to_check"
    SKIP_CI=false
    echo "Changes detected in non-documentation files - will not skip tests and build"
    echo "skip-ci=false" >>"$GITHUB_OUTPUT"
    exit 0 ## if file is outside of the skipped_directory exit script
  fi
done

echo "Changes detected in only documentation files - skipping tests and build"
echo "skip-ci=true" >>"$GITHUB_OUTPUT"
