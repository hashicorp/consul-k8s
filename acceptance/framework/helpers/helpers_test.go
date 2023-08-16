package helpers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeMaps(t *testing.T) {
	cases := map[string]struct {
		a, b, expected map[string]string
	}{
		"b overwrites a": {
			a: map[string]string{
				"foo": "bar",
			},
			b: map[string]string{
				"foo": "baz",
			},
			expected: map[string]string{
				"foo": "baz",
			},
		},
		"no overlap": {
			a: map[string]string{
				"foo": "bar",
			},
			b: map[string]string{
				"bar": "baz",
			},
			expected: map[string]string{
				"foo": "bar",
				"bar": "baz",
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			actual := c.a
			MergeMaps(actual, c.b)
			require.Equal(t, c.expected, actual)
		})
	}
}
