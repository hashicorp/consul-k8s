// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

type ExternalRouteFilter interface {
	GetNamespace() string
}
