// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package serviceaccount

import (
	"testing"
)

// TODO: ConsulDestinationNamespace and EnableNSMirroring +/- prefix

// TODO(zalimeni)
// Tests new WorkloadIdentity registration in a non-default NS and Partition with namespaces set to mirroring
func TestReconcile_CreateWorkloadIdentity_WithNamespaces(t *testing.T) {

}

// TODO(zalimeni)
// Tests removing WorkloadIdentity registration in a non-default NS and Partition with namespaces set to mirroring
func TestReconcile_DeleteWorkloadIdentity_WithNamespaces(t *testing.T) {

}
