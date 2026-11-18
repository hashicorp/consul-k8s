// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package k8s

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoredOpenShiftDNSServers(t *testing.T) {
	const testServer = `{"name":"consul-acceptance","zones":["consul"],"forwardPlugin":{"upstreams":["10.0.0.1"]}}`
	const unrelatedServer = `{"name":"unrelated","zones":["example.com"],"forwardPlugin":{"upstreams":["10.0.0.2"]},"unknown":"preserved"}`
	for _, tc := range []struct {
		name     string
		current  string
		original string
		want     string
		wantErr  bool
	}{
		{name: "restore absent servers", current: "[" + testServer + "]", original: "null", want: "null"},
		{name: "restore empty array", current: "[" + testServer + "]", original: "[]", want: "[]"},
		{name: "preserve unrelated updates", current: "[" + testServer + "," + unrelatedServer + "]", original: "null", want: "[" + unrelatedServer + "]"},
		{name: "already restored", current: "[" + unrelatedServer + "]", original: "null", want: "[" + unrelatedServer + "]"},
		{name: "conflicting change", current: "[" + testServer + "]", original: "null", wantErr: true},
		{name: "invalid JSON", current: "{", original: "null", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := "10.0.0.1"
			if tc.name == "conflicting change" {
				upstream = "10.0.0.9"
			}
			result, err := restoredOpenShiftDNSServers(json.RawMessage(tc.current), json.RawMessage(tc.original), upstream)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(result))
		})
	}
}
