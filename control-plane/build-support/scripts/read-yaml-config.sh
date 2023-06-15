#!/bin/bash

# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
input_file=$1

# Convert YAML content to JSON as it is easier to deal with
JSON_CONTENTS=$(yq eval '. | tojson' "${input_file}")

echo "${JSON_CONTENTS}"
