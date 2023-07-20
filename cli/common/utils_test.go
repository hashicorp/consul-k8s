package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeMaps(t *testing.T) {
	cases := map[string]struct {
		a        map[string]interface{}
		b        map[string]interface{}
		expected map[string]interface{}
	}{
		"a is empty": {
			a:        map[string]interface{}{},
			b:        map[string]interface{}{"foo": "bar"},
			expected: map[string]interface{}{"foo": "bar"},
		},
		"b is empty": {
			a:        map[string]interface{}{"foo": "bar"},
			b:        map[string]interface{}{},
			expected: map[string]interface{}{"foo": "bar"},
		},
		"b overrides a": {
			a:        map[string]interface{}{"foo": "bar"},
			b:        map[string]interface{}{"foo": "baz"},
			expected: map[string]interface{}{"foo": "baz"},
		},
		"b partially overrides a": {
			a:        map[string]interface{}{"foo": "bar", "baz": "qux"},
			b:        map[string]interface{}{"foo": "baz"},
			expected: map[string]interface{}{"foo": "baz", "baz": "qux"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := MergeMaps(tc.a, tc.b)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestIsValidLabel(t *testing.T) {
	cases := []struct {
		name     string
		label    string
		expected bool
	}{
		{"Valid label", "such-a-good-label", true},
		{"Valid label with leading numbers", "123-such-a-good-label", true},
		{"Invalid label empty", "", false},
		{"Invalid label contains capital letters", "Peppertrout", false},
		{"Invalid label contains underscores", "this_is_not_python", false},
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
