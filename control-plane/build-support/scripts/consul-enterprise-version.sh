#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ !"${VERSION}" == *"hashicorppreview/consul:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g")
elif [[ !"${VERSION}" == *"hashicorp/consul:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g" | sed "s/$/-ent/g")
fi

echo "${VERSION}"
