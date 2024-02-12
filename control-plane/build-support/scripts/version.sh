#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0


version_file=$1
version=$(awk '$1 == "Version" && $2 == "=" { gsub(/"/, "", $3); print $3 }' < "${version_file}")
prerelease=$(awk '$1 == "VersionPrerelease" && $2 == "=" { gsub(/"/, "", $3); print $3 }' < "${version_file}")

if [ -n "$prerelease" ]; then
    echo "${version}-${prerelease}"
else
    echo "${version}"
fi