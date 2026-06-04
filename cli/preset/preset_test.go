// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package preset

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetHCPPresetFromEnv(t *testing.T) {
	const (
		scadaAddress = "scada-address"
		clientID     = "client-id"
		clientSecret = "client-secret"
		apiHost      = "api-host"
		authURL      = "auth-url"
		resourceID   = "resource-id"
	)

	testCases := []struct {
		description        string
		resourceID         string
		preProcessingFunc  func()
		postProcessingFunc func()
		expectedPreset     *HCPConfig
	}{
		{
			"Should populate properties in addition to resourceID on HCPConfig when environment variables are set.",
			resourceID,
			func() {
				os.Setenv(EnvHCPClientID, clientID)
				os.Setenv(EnvHCPClientSecret, clientSecret)
				os.Setenv(EnvHCPAPIHost, apiHost)
				os.Setenv(EnvHCPAuthURL, authURL)
				os.Setenv(EnvHCPScadaAddress, scadaAddress)
			},
			func() {
				os.Unsetenv(EnvHCPClientID)
				os.Unsetenv(EnvHCPClientSecret)
				os.Unsetenv(EnvHCPAPIHost)
				os.Unsetenv(EnvHCPAuthURL)
				os.Unsetenv(EnvHCPScadaAddress)
			},
			&HCPConfig{
				ResourceID:   resourceID,
				ClientID:     clientID,
				ClientSecret: clientSecret,
				AuthURL:      authURL,
				APIHostname:  apiHost,
				ScadaAddress: scadaAddress,
			},
		},
		{
			"Should only populate resourceID on HCPConfig when environment variables are not set.",
			resourceID,
			func() {
				os.Unsetenv(EnvHCPClientID)
				os.Unsetenv(EnvHCPClientSecret)
				os.Unsetenv(EnvHCPAPIHost)
				os.Unsetenv(EnvHCPAuthURL)
				os.Unsetenv(EnvHCPScadaAddress)
			},
			func() {},
			&HCPConfig{
				ResourceID: resourceID,
			},
		},
	}

	for _, testCase := range testCases {
		testCase.preProcessingFunc()
		defer testCase.postProcessingFunc()
		t.Run(testCase.description, func(t *testing.T) {
			hcpPreset := GetHCPPresetFromEnv(testCase.resourceID)
			require.Equal(t, testCase.expectedPreset, hcpPreset)
		})
	}
}
