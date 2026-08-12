#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ "${VERSION}" == *"consul-enterprise:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul-enterprise:/consul:/g")
fi

CONSUL_CE_VERSION="1.22.7"
VERSION=$(echo ${VERSION} | sed -E "s/consul:1\.22\.[0-9]+/consul:${CONSUL_CE_VERSION}/g")

echo "${VERSION}"
