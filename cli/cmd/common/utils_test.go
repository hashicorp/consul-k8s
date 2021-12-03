package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsValidLabel(t *testing.T) {
	cases := []struct {
		name     string
		label    string
		expected bool
	}{
		{"Valid label", "such-a-good-label", true},
		{"Invalid label empty", "", false},
		{"Invalid label too long", "a-very-very-very-long-label-that-is-more-than-63-characters-long", false},
		{"Invalid label starts with a dash", "-invalid-label", false},
		{"Invalid label ends with a dash", "invalid-label-", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsValidLabel(tc.label)
			require.Equal(t, tc.expected, actual)
		})
	}
}
