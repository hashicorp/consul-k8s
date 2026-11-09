// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	terratestLogger "github.com/gruntwork-io/terratest/modules/logger"
	"github.com/hashicorp/consul-k8s/acceptance/framework/portforward"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

// ListenerTracingJSON overrides HCM sampling independently of the tracer provider.
const ListenerTracingJSON = `{
  "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager.Tracing",
  "random_sampling": {"value": 100},
  "overall_sampling": {"value": 100},
  "spawn_upstream_span": true
}`

// ListenerTracingTarget identifies the application HTTP filter chains to check.
// StatPrefix excludes unrelated HCMs, such as exposed health-check listeners.
type ListenerTracingTarget struct {
	Options       *terratestk8s.KubectlOptions
	LabelSelector string
	StatPrefix    string
}

// SetGlobalListenerTracing preserves the existing global proxy defaults.
// Use the owning CRD instead when proxy defaults are managed by Kubernetes.
func SetGlobalListenerTracing(t *testing.T, client *api.Client, enabled bool) {
	t.Helper()
	entry, _, err := client.ConfigEntries().Get(api.ProxyDefaults, api.ProxyConfigGlobal, nil)
	require.NoError(t, err)
	defaults, ok := entry.(*api.ProxyConfigEntry)
	require.True(t, ok)
	if defaults.Config == nil {
		defaults.Config = make(map[string]interface{})
	}
	if enabled {
		defaults.Config["envoy_listener_tracing_json"] = ListenerTracingJSON
	} else {
		delete(defaults.Config, "envoy_listener_tracing_json")
	}
	written, _, err := client.ConfigEntries().Set(defaults, nil)
	require.NoError(t, err)
	require.True(t, written)
}

// CheckListenerTracing verifies default, enabled, and removed settings on live
// proxies without restarting them. update must modify the owning config entry.
func CheckListenerTracing(t *testing.T, update func(*testing.T, bool), targets ...ListenerTracingTarget) {
	t.Helper()
	require.NotEmpty(t, targets)
	for _, phase := range []struct {
		name    string
		enabled bool
	}{
		{name: "default"},
		{name: "enabled", enabled: true},
		{name: "removed"},
	} {
		if !t.Run("listener-tracing/"+phase.name, func(t *testing.T) {
			update(t, phase.enabled)
			for _, target := range targets {
				checkListenerTracingTarget(t, target, phase.enabled)
			}
		}) {
			return
		}
	}
}

func checkListenerTracingTarget(t *testing.T, target ListenerTracingTarget, enabled bool) {
	t.Helper()
	var pods corev1.PodList
	retry.Run(t, func(r *retry.R) {
		out, err := terratestk8s.RunKubectlAndGetOutputE(r, target.Options,
			"get", "pods", "-l", target.LabelSelector, "-o", "json")
		require.NoError(r, err)
		require.NoError(r, json.Unmarshal([]byte(out), &pods))
		require.NotEmpty(r, pods.Items, "no pods match %s", target.LabelSelector)
	})
	for _, pod := range pods.Items {
		addr := portforward.CreateTunnelToResourcePort(t, pod.Name, 19000, target.Options, terratestLogger.Discard)
		client := &http.Client{Timeout: 10 * time.Second}
		retry.RunWith(&retry.Timer{Timeout: 2 * time.Minute, Wait: time.Second}, t, func(r *retry.R) {
			resp, err := client.Get("http://" + addr + "/config_dump")
			require.NoError(r, err)
			defer resp.Body.Close()
			require.Equal(r, http.StatusOK, resp.StatusCode)
			var dump listenerTracingDump
			require.NoError(r, json.NewDecoder(resp.Body).Decode(&dump))
			require.NoError(r, dump.check(target.StatPrefix, enabled), "pod %s", pod.Name)
		})
	}
}

type listenerTracingDump struct {
	Configs []struct {
		Listeners []struct {
			Active struct {
				Listener struct {
					FilterChains []struct {
						Filters []struct {
							Name   string `json:"name"`
							Config struct {
								StatPrefix string `json:"stat_prefix"`
								Tracing    *struct {
									Random *struct {
										Value float64 `json:"value"`
									} `json:"random_sampling"`
									Overall *struct {
										Value float64 `json:"value"`
									} `json:"overall_sampling"`
									SpawnUpstreamSpan bool `json:"spawn_upstream_span"`
								} `json:"tracing"`
							} `json:"typed_config"`
						} `json:"filters"`
					} `json:"filter_chains"`
				} `json:"listener"`
			} `json:"active_state"`
		} `json:"dynamic_listeners"`
	} `json:"configs"`
}

func (d listenerTracingDump) check(statPrefix string, enabled bool) error {
	matched := 0
	for _, config := range d.Configs {
		for _, listener := range config.Listeners {
			for _, chain := range listener.Active.Listener.FilterChains {
				for _, filter := range chain.Filters {
					if filter.Name != "envoy.filters.network.http_connection_manager" ||
						!strings.HasPrefix(filter.Config.StatPrefix, statPrefix) {
						continue
					}
					matched++
					tracing := filter.Config.Tracing
					if tracing == nil || tracing.Random == nil {
						return fmt.Errorf("HCM %q is missing explicit random sampling", filter.Config.StatPrefix)
					}
					if enabled {
						if tracing.Random.Value != 100 || tracing.Overall == nil ||
							tracing.Overall.Value != 100 || !tracing.SpawnUpstreamSpan {
							return fmt.Errorf("HCM %q has not applied the tracing override", filter.Config.StatPrefix)
						}
					} else if tracing.Random.Value != 0 || tracing.Overall != nil || tracing.SpawnUpstreamSpan {
						return fmt.Errorf("HCM %q has not restored default tracing", filter.Config.StatPrefix)
					}
				}
			}
		}
	}
	if matched == 0 {
		return fmt.Errorf("no active HTTP filter chains match stat prefix %q", statPrefix)
	}
	return nil
}
