#!/bin/bash

# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

INPUT_FILE=$1

# convert readable yaml to json for github actions consumption
# do not include any pretty print, print to single line with -I 0
VALUE=$(yq eval 'select(fileIndex == 0)' "$INPUT_FILE" -o json -I 0)

echo "$VALUE"