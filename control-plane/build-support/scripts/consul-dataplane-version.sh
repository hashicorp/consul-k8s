#!/usr/bin/env bash
# Copyright IBM Corp. 2018, 2025
# SPDX-License-Identifier: MPL-2.0
FILE=$1
VERSION=$(yq .global.imageConsulDataplane $FILE)

echo "${VERSION}"
