#!/usr/bin/env bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.imageConsulDataplane $FILE)

echo "${VERSION}"
