#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ "${VERSION}" == *"consul-enterprise:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul-enterprise:/consul:/g")
fi

echo "${VERSION}"
