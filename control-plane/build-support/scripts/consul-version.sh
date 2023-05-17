#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.image $FILE)

# echo full string "hashicorp/consul:1.15.1" | remove first and last characters | cut everything before ':'
echo "${VERSION}" | sed 's/^.//;s/.$//' | cut -d ':' -f2-
