#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0


CHECK=false

# Check if the --check argument is passed
for arg in "$@"
do
    if [ "$arg" == "--check" ]
    then
        CHECK=true
    fi
done

# Find directories containing a go.mod file
for dir in $(find . -type f -name go.mod -exec dirname {} \;); do
    # Change into the directory
    cd "$dir" || exit

    # Run go mod tidy
    echo "Running go mod tidy in $dir"
    go mod tidy

    # Change back to the original directory
    cd - || exit
done

# Check for differences if the --check argument was passed
if [ "$CHECK" = true ]; then
    if [[ -n "$(git status --porcelain)" ]]; then
        echo "differences were found in go.mod or go.sum, run go mod tidy to fix them"
        exit 1
    fi
fi