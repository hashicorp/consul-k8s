#!/usr/bin/env bash
# Copyright IBM Corp. 2018, 2025
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ "${VERSION}" == *"consul-enterprise:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul-enterprise:/consul:/g")
fi

echo "${VERSION}"
