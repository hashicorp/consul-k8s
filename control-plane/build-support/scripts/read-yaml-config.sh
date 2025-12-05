#!/bin/bash

# Copyright IBM Corp. 2018, 2025
# SPDX-License-Identifier: MPL-2.0
INPUT_FILE=$1
FIELD=$2

VALUE=$(yq $FIELD $INPUT_FILE)

echo "${VALUE}"
