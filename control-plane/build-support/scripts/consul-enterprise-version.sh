#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ !"${VERSION}" == *"hashicorp/consul:"* ]]; then
	VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g" | sed "s/$/-ent/g")
elif [[ !"${VERSION}" == *"-rc"* ]]; then
  VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g" | sed "s/$/-ent/g")
else
  VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g")
fi

echo "${VERSION}"