// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"strings"
)

func isModifiedError(err error) bool {
	if strings.Contains(err.Error(), "the object has been modified") {
		return true
	}
	return false
}
