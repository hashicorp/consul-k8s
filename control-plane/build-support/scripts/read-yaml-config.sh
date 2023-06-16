#!/bin/bash

# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
INPUT_FILE=$1

# Convert YAML content to JSON as it is easier to deal with
JSON_CONTENTS=$(yq eval '. | tojson' "${INPUT_FILE}")

echo "${JSON_CONTENTS}"
