#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

if [[ "${VERSION}" == *"hashicorp/consul:"* ]]; then
  # for matching release image repos with a -ent label
	VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g" | sed "s/$/-ent/g")
else
  # for matching preview image repos
  VERSION=$(echo ${VERSION} | sed "s/consul:/consul-enterprise:/g")
fi

echo "${VERSION}"