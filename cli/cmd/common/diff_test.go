package common

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/config"
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
			name: "Two equal maps should return the map parsed as YAML",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
					"liz": map[string]interface{}{"qux": []string{"quux", "quuz"}},
				},
				b: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
					"liz": map[string]interface{}{"qux": []string{"quux", "quuz"}},
				},
			},
			expected: "  baz: qux\n  foo: bar\n  liz:\n    qux:\n    - quux\n    - quuz\n",
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
			expected: "+ baz: qux\n  foo: bar\n",
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
			expected: "+ baz:\n+ - qux\n  foo: bar\n+ qux:\n+   quux: corge\n",
		},
		{
			name: "Upgrade from demo to secure",
			args: args{
				a: config.Presets["demo"].(map[string]interface{}),
				b: config.Presets["secure"].(map[string]interface{}),
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := Diff(tt.args.a, tt.args.b)
			require.NoError(t, err)
			fmt.Println(tt.expected)
			fmt.Println(actual)
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
