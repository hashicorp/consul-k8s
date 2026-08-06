#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# Builds the ext-proc failover test images from the vendored app sources in this
# directory and (optionally) loads them into one or more kind clusters.
#
# The images match the tags referenced by the fixtures under ../common, ../single
# and ../two:
#   local/ext-proc-http:0.1
#   local/route-decider:0.1
#   local/service-d:0.1
#   local/service-e:0.1
#   local/ext-proc-http-connect-proxy:0.1
#   local/http-decider-connect-proxy:0.1
#
# Usage:
#   ./build-images.sh                       # build all images
#   ./build-images.sh kind-cluster-a kind-b # build, then `kind load` into each cluster
#
# Requires: docker, and (when cluster names are given) kind.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

TAG="${IMAGE_TAG:-0.1}"
APPS=(
  ext-proc-http
  route-decider
  service-d
  service-e
  ext-proc-http-connect-proxy
  http-decider-connect-proxy
)

for app in "${APPS[@]}"; do
  echo "==> building local/${app}:${TAG}"
  docker build -t "local/${app}:${TAG}" "${app}"
done

# Any positional args are treated as kind cluster names to load the images into.
for cluster in "$@"; do
  for app in "${APPS[@]}"; do
    echo "==> kind load local/${app}:${TAG} -> ${cluster}"
    kind load docker-image "local/${app}:${TAG}" --name "${cluster}"
  done
done

echo "done"
