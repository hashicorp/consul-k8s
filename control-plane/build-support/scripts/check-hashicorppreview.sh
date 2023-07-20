#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
echo "Checking charts for hashicorpreview images. . ."
if grep -rnw -e 'hashicorppreview'  './charts'; then
    echo Charts contain hashicorppreview images. If this is intended for release, please remove the preview images.
else
    echo Charts do not contain hashicorpreview images, ready for release!
fi