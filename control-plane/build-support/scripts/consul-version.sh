#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ "${VERSION}" == *"consul-enterprise:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul-enterprise:/consul:/g")
fi

# global.image is pinned to the Consul Enterprise image (consul-enterprise:1.22.10)
# so that Enterprise acceptance tests deploy the Enterprise build. The control-plane
# unit tests instead pull the CE (community edition) image to obtain a license-free
# consul binary. The CE and Enterprise 1.22.x patch lines have diverged: the last
# published CE 1.22.x release is 1.22.7, while Enterprise is at 1.22.10. Map the
# derived (nonexistent) CE 1.22.x tag to the latest published CE 1.22.x release so
# the image can be pulled. Rolling tags such as consul:1.22-dev are left untouched.
CONSUL_CE_VERSION="1.22.7"
VERSION=$(echo ${VERSION} | sed -E "s/consul:1\.22\.[0-9]+/consul:${CONSUL_CE_VERSION}/g")

echo "${VERSION}"
