// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

// +kubebuilder:skipversion

type ExternalRouteFilter interface {
	GetNamespace() string
}
