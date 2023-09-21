// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package constants

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetDefaultConsulNamespace(t *testing.T) {
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
			actual := GetDefaultConsulNamespace(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}

func TestGetDefaultConsulPartition(t *testing.T) {
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
			actual := GetDefaultConsulPartition(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}

func TestGetDefaultConsulPeer(t *testing.T) {
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
			actual := GetDefaultConsulPeer(tc.value)
			require.Equal(t, actual, tc.expect)
		})
	}
}
