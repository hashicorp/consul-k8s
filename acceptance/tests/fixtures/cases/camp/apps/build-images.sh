#!/usr/bin/env bash
# build-images.sh — build the three CAMP Docker images and load them into
# one or more named kind clusters.
#
# Usage:
#   TAG=latest ./build-images.sh <cluster-name> [<cluster-name> ...]
#
# This is the manual convenience script. The acceptance test
# (acceptance/tests/camp/mcp_gateway_basic_test.go) calls docker build + kind
# load directly from Go, so this script is only needed if you want to pre-build
# images by hand before running tests.
set -euo pipefail
cd "$(dirname "$0")"

TAG="${TAG:-latest}"

APPS=(
  "camp-apps:camp-apps:${TAG}"
  "ameduss:camp-ameduss:${TAG}"
  "weatherly:camp-weatherly:${TAG}"
)

for entry in "${APPS[@]}"; do
  dir="${entry%%:*}"
  rest="${entry#*:}"
  image="${rest}"
  echo "==> docker build -t ${image} ${dir}"
  docker build -t "${image}" "${dir}"
done

for cluster in "$@"; do
  for entry in "${APPS[@]}"; do
    rest="${entry#*:}"
    image="${rest}"
    echo "==> kind load docker-image ${image} --name ${cluster}"
    kind load docker-image "${image}" --name "${cluster}"
  done
done
