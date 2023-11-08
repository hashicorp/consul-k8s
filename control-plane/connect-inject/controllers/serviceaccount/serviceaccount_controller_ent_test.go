// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package serviceaccount

import (
	"testing"
)

// TODO(NET-5719): ConsulDestinationNamespace and EnableNSMirroring +/- prefix

// TODO(NET-5719)
// Tests new WorkloadIdentity registration in a non-default NS and Partition with namespaces set to mirroring
func TestReconcile_CreateWorkloadIdentity_WithNamespaces(t *testing.T) {
	//TODO(NET-5719): Add test case to cover Consul namespace missing and check for backoff
}

// TODO(NET-5719)
// Tests removing WorkloadIdentity registration in a non-default NS and Partition with namespaces set to mirroring
func TestReconcile_DeleteWorkloadIdentity_WithNamespaces(t *testing.T) {
	//TODO(NET-5719): Add test case to cover Consul namespace missing and check for backoff
}
