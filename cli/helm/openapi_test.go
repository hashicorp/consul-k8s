// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSkipOpenAPIValidation(t *testing.T) {
	for _, tc := range []struct {
		value   string
		enabled bool
		wantErr bool
	}{
		{value: ""},
		{value: "false"},
		{value: "true", enabled: true},
		{value: "1", enabled: true},
		{value: "invalid", wantErr: true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("CONSUL_K8S_SKIP_OPENAPI_VALIDATION", tc.value)
			enabled, err := skipOpenAPIValidation()
			if tc.wantErr {
				require.ErrorContains(t, err, "CONSUL_K8S_SKIP_OPENAPI_VALIDATION")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.enabled, enabled)
		})
	}
}
