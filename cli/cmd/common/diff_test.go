package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiff(t *testing.T) {
	type args struct {
		a map[string]interface{}
		b map[string]interface{}
	}

	tests := []struct {
		name     string
		args     args
		expected string
	}{
		{
			name: "Two empty maps should return an empty string",
			args: args{
				a: map[string]interface{}{},
				b: map[string]interface{}{},
			},
			expected: "",
		},
		{
			name: "New elements should be prefixed with a plus sign",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
				},
				b: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
				},
			},
			expected: "  foo: bar\n+ baz: qux\n",
		},
		{
			name: "New non-string elements should be prefixed with a plus sign",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
				},
				b: map[string]interface{}{
					"foo": "bar",
					"baz": []string{"qux"},
					"qux": map[string]string{
						"quux": "corge",
					},
				},
			},
			expected: "  foo: bar\n+ baz: [qux]\n+ qux:\n+     quux: corge\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := Diff(tt.args.a, tt.args.b)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestCollectKeys(t *testing.T) {
	type args struct {
		a map[string]interface{}
		b map[string]interface{}
	}

	tests := []struct {
		name     string
		args     args
		expected []string
	}{
		{
			name: "Two empty maps should return an empty slice",
			args: args{
				a: map[string]interface{}{},
				b: map[string]interface{}{},
			},
			expected: []string{},
		},
		{
			name: "Two maps without repeated keys should return the union of the keys",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
				},
				b: map[string]interface{}{
					"liz": "qux",
				},
			},
			expected: []string{"foo", "baz", "liz"},
		},
		{
			name: "Two maps with repeated keys should return the deduplicated union of the keys",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
				},
				b: map[string]interface{}{
					"baz": "qux",
					"liz": "qux",
				},
			},
			expected: []string{"foo", "baz", "liz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := collectKeys(tt.args.a, tt.args.b)
			for _, key := range tt.expected {
				require.Contains(t, actual, key)
			}
		})
	}
}
