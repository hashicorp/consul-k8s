// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helmvalues

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringMap_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected StringMap
		errorMsg string
	}{
		{name: "null", input: `null`, expected: nil},
		{name: "empty object", input: `{}`, expected: StringMap{}},
		{name: "object", input: `{"foo":"bar","baz":"qux"}`, expected: StringMap{"foo": "bar", "baz": "qux"}},
		{name: "object with number and bool values", input: `{"prometheus.io/port":20200,"enabled":true}`, expected: StringMap{"prometheus.io/port": "20200", "enabled": "true"}},
		{name: "object with null value", input: `{"foo":null}`, expected: StringMap{"foo": ""}},
		{name: "empty string", input: `""`, expected: nil},
		{name: "whitespace string", input: `"  \n"`, expected: nil},
		{name: "yaml string", input: `"\"foo\": \"bar\"\n\"vault.hashicorp.com/namespace\": \"team-a\"\n"`, expected: StringMap{"foo": "bar", "vault.hashicorp.com/namespace": "team-a"}},
		{name: "yaml string with unquoted values", input: `"foo: bar\nport: 20200\n"`, expected: StringMap{"foo": "bar", "port": "20200"}},
		{name: "yaml string that is not a map", input: `"just-a-scalar"`, errorMsg: "expected a YAML map of strings"},
		{name: "invalid yaml string", input: `"foo: [bar"`, errorMsg: "expected a YAML map of strings"},
		{name: "nested map value", input: `{"foo":{"bar":"baz"}}`, errorMsg: `value for key "foo" must be a scalar`},
		{name: "list value in yaml string", input: `"foo:\n  - bar\n"`, errorMsg: `value for key "foo" must be a scalar`},
		{name: "array", input: `["foo"]`, errorMsg: "cannot unmarshal array"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m StringMap
			err := json.Unmarshal([]byte(tc.input), &m)
			if tc.errorMsg != "" {
				require.ErrorContains(t, err, tc.errorMsg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, m)
		})
	}
}
