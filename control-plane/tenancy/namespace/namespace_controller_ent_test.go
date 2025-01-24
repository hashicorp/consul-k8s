// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package namespace

import (
	"testing"
)

func TestReconcileCreateNamespace_ENT(t *testing.T) {
	testCases := []createTestCase{
		{
			name:                    "consul namespace is ap1/ns1",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			expectedConsulNamespace: "ns1",
		},
	}
	testReconcileCreateNamespace(t, testCases)
}

func TestReconcileDeleteNamespace_ENT(t *testing.T) {
	testCases := []deleteTestCase{
		{
			name:                    "non-default partition",
			kubeNamespace:           "ns1",
			partition:               "ap1",
			existingConsulNamespace: "ns1",
			expectNamespaceDeleted:  "ns1",
		},
	}
	testReconcileDeleteNamespace(t, testCases)
}
