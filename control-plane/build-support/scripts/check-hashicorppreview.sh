#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
echo "Checking charts for hashicorppreview images. . ."
if grep -rnw -e 'hashicorppreview'  './charts'; then
    echo "ERROR: Charts contain hashicorppreview images. If this is intended for release, please remove the preview images." >&2
    exit 1
else
    echo Charts do not contain hashicorppreview images, ready for release!
fi