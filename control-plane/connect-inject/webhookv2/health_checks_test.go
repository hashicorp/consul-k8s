// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReady(t *testing.T) {

	var cases = []struct {
		name             string
		certFileContents *string
		keyFileContents  *string
		expectError      bool
	}{
		{"Both cert and key files not present.", nil, nil, true},
		{"Cert file not empty and key file missing.", ptrToString("test"), nil, true},
		{"Key file not empty and cert file missing.", nil, ptrToString("test"), true},
		{"Both cert and key files are present and not empty.", ptrToString("test"), ptrToString("test"), false},
		{"Both cert and key files are present but both are empty.", ptrToString(""), ptrToString(""), true},
		{"Both cert and key files are present but key file is empty.", ptrToString("test"), ptrToString(""), true},
		{"Both cert and key files are present but cert file is empty.", ptrToString(""), ptrToString("test"), true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "")
			require.NoError(t, err)
			if tt.certFileContents != nil {
				err := os.WriteFile(filepath.Join(tmpDir, "tls.crt"), []byte(*tt.certFileContents), 0666)
				require.NoError(t, err)
			}
			if tt.keyFileContents != nil {
				err := os.WriteFile(filepath.Join(tmpDir, "tls.key"), []byte(*tt.keyFileContents), 0666)
				require.NoError(t, err)
			}
			rc := ReadinessCheck{tmpDir}
			err = rc.Ready(nil)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func ptrToString(s string) *string {
	return &s
}
