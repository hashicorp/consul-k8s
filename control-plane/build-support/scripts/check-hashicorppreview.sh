#!/bin/bash
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
if grep -rnw -e 'hashicorppreviewsadfsd'  './charts'; then
    echo charts contain hashicorppreview images
else
    echo charts do not contain hashicorpreview images
fi