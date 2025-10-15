// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package constants

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetNormalizedConsulNamespace(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		expect string
	}{
		{
			name:   "expect contant",
			value:  "",
			expect: DefaultConsulNS,
		},
		{
			name:   "expect passed in value",
			value:  "some-value",
			expect: "some-value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := GetNormalizedConsulNamespace(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}

func TestGetNormalizedConsulPartition(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		expect string
	}{
		{
			name:   "expect contant",
			value:  "",
			expect: DefaultConsulPartition,
		},
		{
			name:   "expect passed in value",
			value:  "some-value",
			expect: "some-value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := GetNormalizedConsulPartition(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}

func TestGetNormalizedConsulPeer(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		expect string
	}{
		{
			name:   "expect contant",
			value:  "",
			expect: DefaultConsulPeer,
		},
		{
			name:   "expect passed in value",
			value:  "some-value",
			expect: "some-value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := GetNormalizedConsulPeer(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}

func TestIsDualStack(t *testing.T) {
	// Define test cases in a table-driven format.
	testCases := []struct {
		name        string
		envVarValue string // The value to set for the env var.
		setEnvVar   bool   // Whether to set the env var at all.
		expected    bool   // The expected boolean result.
	}{
		{
			name:        "should return true when env var is 'true'",
			envVarValue: "true",
			setEnvVar:   true,
			expected:    true,
		},
		{
			name:        "should return false when env var is 'false'",
			envVarValue: "false",
			setEnvVar:   true,
			expected:    false,
		},
		{
			name:        "should return false when env var is an empty string",
			envVarValue: "",
			setEnvVar:   true,
			expected:    false,
		},
		{
			name:        "should return false when env var is any other string",
			envVarValue: "enabled",
			setEnvVar:   true,
			expected:    false,
		},
		{
			name:      "should return false when env var is not set",
			setEnvVar: false,
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Ensure a clean environment for each sub-test.
			// t.Cleanup automatically calls this function when the test and all its subtests complete.
			t.Cleanup(func() {
				os.Unsetenv(ConsulDualStackEnvVar)
			})

			// Set the environment variable if the test case requires it.
			if tc.setEnvVar {
				err := os.Setenv(ConsulDualStackEnvVar, tc.envVarValue)
				require.NoError(t, err, "Setting environment variable should not fail")
			} else {
				// Explicitly unset to be sure it's not present from a previous run.
				os.Unsetenv(ConsulDualStackEnvVar)
			}

			// Act: Call the function we are testing.
			actual := IsDualStack()

			// Assert: Check if the actual result matches the expected result.
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestGetv4orv6(t *testing.T) {
	// Define test cases
	testCases := []struct {
		name          string
		envVarValue   string
		setEnvVar     bool
		ipv4Input     string
		ipv6Input     string
		expectedOuput string
	}{
		{
			name:          "should return IPv6 address when dual stack is enabled",
			envVarValue:   "true",
			setEnvVar:     true,
			ipv4Input:     "127.0.0.1",
			ipv6Input:     "::1",
			expectedOuput: "::1",
		},
		{
			name:          "should return IPv4 address when dual stack is disabled (set to 'false')",
			envVarValue:   "false",
			setEnvVar:     true,
			ipv4Input:     "192.168.1.1",
			ipv6Input:     "fe80::1",
			expectedOuput: "192.168.1.1",
		},
		{
			name:          "should return IPv4 address when dual stack env var is not set",
			setEnvVar:     false,
			ipv4Input:     "0.0.0.0",
			ipv6Input:     "::",
			expectedOuput: "0.0.0.0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Ensure a clean environment for each sub-test.
			t.Cleanup(func() {
				os.Unsetenv(ConsulDualStackEnvVar)
			})

			if tc.setEnvVar {
				err := os.Setenv(ConsulDualStackEnvVar, tc.envVarValue)
				require.NoError(t, err)
			} else {
				os.Unsetenv(ConsulDualStackEnvVar)
			}

			// Act: Call the function.
			actual := Getv4orv6Str(tc.ipv4Input, tc.ipv6Input)

			// Assert: Check the result.
			require.Equal(t, tc.expectedOuput, actual)
		})
	}
}
