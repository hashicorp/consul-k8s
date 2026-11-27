// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestSetGlobalListenerTracing(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			original := &api.ProxyConfigEntry{
				Kind: api.ProxyDefaults,
				Name: api.ProxyConfigGlobal,
				Config: map[string]interface{}{
					"protocol":                    "http",
					"envoy_listener_tracing_json": ListenerTracingJSON,
				},
				MeshGateway: api.MeshGatewayConfig{Mode: api.MeshGatewayModeLocal},
			}
			var updated api.ProxyConfigEntry
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/config/proxy-defaults/global":
					if err := json.NewEncoder(w).Encode(original); err != nil {
						t.Error(err)
					}
				case r.Method == http.MethodPut && r.URL.Path == "/v1/config":
					if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
						t.Error(err)
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					fmt.Fprint(w, "true")
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			config := api.DefaultConfig()
			config.Address = server.URL
			client, err := api.NewClient(config)
			require.NoError(t, err)

			SetGlobalListenerTracing(t, client, enabled)
			require.Equal(t, "http", updated.Config["protocol"])
			require.Equal(t, original.MeshGateway, updated.MeshGateway)
			if enabled {
				require.Equal(t, ListenerTracingJSON, updated.Config["envoy_listener_tracing_json"])
			} else {
				require.NotContains(t, updated.Config, "envoy_listener_tracing_json")
			}
		})
	}
}

func TestListenerTracingDump(t *testing.T) {
	const defaults = `{"random_sampling": {}}`
	const enabled = `{
		"random_sampling": {"value": 100},
		"overall_sampling": {"value": 100},
		"spawn_upstream_span": true
	}`
	hcm := func(prefix, tracing string) string {
		return fmt.Sprintf(`{
			"name": "envoy.filters.network.http_connection_manager",
			"typed_config": {"stat_prefix": %q, "tracing": %s}
		}`, prefix, tracing)
	}
	dump := func(filters string) string {
		return fmt.Sprintf(`{"configs": [{"dynamic_listeners": [{
			"active_state": {"listener": {"filter_chains": [{"filters": [%s]}]}}
		}]}]}`, filters)
	}

	for _, tc := range []struct {
		name    string
		json    string
		enabled bool
		wantErr string
	}{
		{
			name: "default",
			json: dump(hcm("upstream.service", defaults)),
		},
		{
			name:    "enabled",
			json:    dump(hcm("upstream.service", enabled)),
			enabled: true,
		},
		{
			name:    "override not applied",
			json:    dump(hcm("upstream.service", defaults)),
			enabled: true,
			wantErr: "has not applied",
		},
		{
			name:    "override not removed",
			json:    dump(hcm("upstream.service", enabled)),
			wantErr: "has not restored",
		},
		{
			name:    "missing overall sampling",
			json:    dump(hcm("upstream.service", `{"random_sampling": {"value": 100}, "spawn_upstream_span": true}`)),
			enabled: true,
			wantErr: "has not applied",
		},
		{
			name:    "missing spawn upstream span",
			json:    dump(hcm("upstream.service", `{"random_sampling": {"value": 100}, "overall_sampling": {"value": 100}}`)),
			enabled: true,
			wantErr: "has not applied",
		},
		{
			name:    "missing tracing",
			json:    dump(hcm("upstream.service", "null")),
			wantErr: "missing explicit random sampling",
		},
		{
			name:    "implicit random sampling is not zero",
			json:    dump(hcm("upstream.service", `{}`)),
			wantErr: "missing explicit random sampling",
		},
		{
			name: "ignores unrelated HCM and TCP filters",
			json: dump(hcm("exposed_check", defaults) + "," +
				`{"name": "envoy.filters.network.tcp_proxy"}` + "," +
				hcm("upstream.service", enabled)),
			enabled: true,
		},
		{
			name:    "checks every matching HCM",
			json:    dump(hcm("upstream.first", enabled) + "," + hcm("upstream.second", defaults)),
			enabled: true,
			wantErr: "has not applied",
		},
		{
			name:    "no matching HCM",
			json:    dump(hcm("exposed_check", enabled)),
			enabled: true,
			wantErr: "no active HTTP filter chains",
		},
		{
			name:    "empty dump",
			json:    `{}`,
			wantErr: "no active HTTP filter chains",
		},
		{
			name: "warming is not active",
			json: fmt.Sprintf(`{"configs": [{"dynamic_listeners": [{
				"warming_state": {"listener": {"filter_chains": [{"filters": [%s]}]}}
			}]}]}`, hcm("upstream.service", enabled)),
			enabled: true,
			wantErr: "no active HTTP filter chains",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dump listenerTracingDump
			require.NoError(t, json.Unmarshal([]byte(tc.json), &dump))
			err := dump.check("upstream.", tc.enabled)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
