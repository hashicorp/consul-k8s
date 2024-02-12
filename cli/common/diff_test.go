// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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
			name: "Deleted elements should be prefixed with a minus sign",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
					"baz": "qux",
				},
				b: map[string]interface{}{
					"foo": "bar",
				},
			},
			expected: "- baz: qux\n  foo: bar\n",
		},
		{
			name: "Deleted non-string elements should be prefixed with a minus sign",
			args: args{
				a: map[string]interface{}{
					"foo": "bar",
					"baz": []string{"qux"},
					"qux": map[string]string{
						"quux": "corge",
					},
				},
				b: map[string]interface{}{
					"foo": "bar",
				},
			},
			expected: "- baz:\n- - qux\n  foo: bar\n- qux:\n-   quux: corge\n",
		},
		{
			name: "Diff between two complex maps should be correct",
			args: args{
				a: map[string]interface{}{
					"global": map[string]interface{}{
						"name": "consul",
						"metrics": map[string]interface{}{
							"enabled":            true,
							"enableAgentMetrics": true,
						},
					},
					"connectInject": map[string]interface{}{
						"enabled": true,
						"metrics": map[string]interface{}{
							"defaultEnabled":       true,
							"defaultEnableMerging": true,
							"enableGatewayMetrics": true,
						},
					},
					"server": map[string]interface{}{
						"replicas": 1,
					},
					"controller": map[string]interface{}{
						"enabled": true,
					},
					"ui": map[string]interface{}{
						"enabled": true,
						"service": map[string]interface{}{
							"enabled": true,
						},
					},
					"prometheus": map[string]interface{}{
						"enabled": true,
					},
				},
				b: map[string]interface{}{
					"global": map[string]interface{}{
						"name": "consul",
						"gossipEncryption": map[string]interface{}{
							"autoGenerate": true,
						},
						"tls": map[string]interface{}{
							"enabled":           true,
							"enableAutoEncrypt": true,
						},
						"acls": map[string]interface{}{
							"manageSystemACLs": true,
						},
					},
					"server": map[string]interface{}{"replicas": 1},
					"connectInject": map[string]interface{}{
						"enabled": true,
					},
					"controller": map[string]interface{}{
						"enabled": true,
					},
				},
			},
			expected: "  connectInject:\n    enabled: true\n-   metrics:\n-     defaultEnableMerging: true\n-     defaultEnabled: true\n-     enableGatewayMetrics: true\n  controller:\n    enabled: true\n  global:\n+   acls:\n+     manageSystemACLs: true\n+   gossipEncryption:\n+     autoGenerate: true\n-   metrics:\n-     enableAgentMetrics: true\n-     enabled: true\n    name: consul\n+   tls:\n+     enableAutoEncrypt: true\n+     enabled: true\n- prometheus:\n-   enabled: true\n  server:\n    replicas: 1\n- ui:\n-   enabled: true\n-   service:\n-     enabled: true\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := Diff(tt.args.a, tt.args.b)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}
