// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldCleanupGatewayResources(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		gatewayMarkedDeleted    bool
		gatewayClassMismatch    bool
		gatewayClassConfigEmpty bool
		expected                bool
	}{
		"no cleanup for normal reconcile": {
			expected: false,
		},
		"cleanup for deleted gateway": {
			gatewayMarkedDeleted: true,
			expected:             true,
		},
		"cleanup for gateway class mismatch": {
			gatewayClassMismatch: true,
			expected:             true,
		},
		"cleanup for missing class config": {
			gatewayClassConfigEmpty: true,
			expected:                true,
		},
		"cleanup for multiple cleanup conditions": {
			gatewayMarkedDeleted: true,
			gatewayClassMismatch: true,
			expected:             true,
		},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := shouldCleanupGatewayResources(tc.gatewayMarkedDeleted, tc.gatewayClassMismatch, tc.gatewayClassConfigEmpty)
			require.Equal(t, tc.expected, got)
		})
	}
}
