// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// aiGatewayEntry returns a minimal AIGatewayConfigEntry stamped with the
// standard k8s metadata used by toConsulConfigEntry.
func aiGatewayEntry(name, kubeNS, datacenter string, modifyIndex uint64) *capi.AIGatewayConfigEntry {
	return &capi.AIGatewayConfigEntry{
		Kind:        capi.AIGateway,
		Name:        name,
		ModifyIndex: modifyIndex,
		Meta: map[string]string{
			constants.MetaKeyKubeName: name,
			constants.MetaKeyKubeNS:   kubeNS,
			datacenterMetaKey:         datacenter,
		},
	}
}

// mockConnMgr builds a MockServerConnectionManager whose State() returns the
// given host:port so that consul.NewClientFromConnMgr dials the httptest server.
func mockConnMgr(t *testing.T, host string, port int) consul.ServerConnectionManager {
	t.Helper()

	ip := net.ParseIP(host)
	if ip == nil {
		// Resolve hostname to IP (handles "127.0.0.1" and "localhost").
		addrs, err := net.LookupHost(host)
		require.NoError(t, err)
		ip = net.ParseIP(addrs[0])
	}

	m := consul.NewMockServerConnectionManager(t)
	m.On("State").Return(discovery.State{
		Address: discovery.Addr{
			TCPAddr: net.TCPAddr{IP: ip, Port: port},
		},
	}, nil)
	return m
}

// consulTestServer starts an httptest server that returns a fixed list of
// ai-gateway entries and advances WaitIndex on each call (simulating Consul
// blocking queries). serverCallCount is incremented on every request so tests
// can synchronise.
//
// It returns the httptest server, a consul.Config that points at it (with
// HTTPPort set so NewClientFromConnMgrState dials correctly), and a watcher
// that satisfies ServerConnectionManager.
func consulTestServer(
	t *testing.T,
	entries []capi.ConfigEntry,
	serverCallCount *atomic.Int32,
) (*httptest.Server, *consul.Config, consul.ServerConnectionManager) {
	t.Helper()

	var index uint64 = 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCallCount.Add(1)
		index++

		w.Header().Set("X-Consul-Index", strconv.FormatUint(index, 10))
		w.Header().Set("Content-Type", "application/json")

		// Wrap each entry as a raw JSON object so the Consul client can decode it.
		type wireEntry struct {
			Kind        string            `json:"Kind"`
			Name        string            `json:"Name"`
			ModifyIndex uint64            `json:"ModifyIndex"`
			Meta        map[string]string `json:"Meta,omitempty"`
		}
		var wire []wireEntry
		for _, e := range entries {
			wire = append(wire, wireEntry{
				Kind:        e.GetKind(),
				Name:        e.GetName(),
				ModifyIndex: e.GetModifyIndex(),
				Meta:        e.GetMeta(),
			})
		}
		_ = json.NewEncoder(w).Encode(wire)
	}))
	t.Cleanup(srv.Close)

	// Parse host and port from httptest URL.
	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	cfg := &consul.Config{
		APIClientConfig: &capi.Config{
			Address: fmt.Sprintf("%s:%s", host, portStr),
			Scheme:  "http",
		},
		HTTPPort: port,
	}

	watcher := mockConnMgr(t, host, port)
	return srv, cfg, watcher
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestCache_WaitSynced(t *testing.T) {
	t.Parallel()

	entry := aiGatewayEntry("my-gw", "default", "dc1", 1)
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)
	c.WaitSynced(ctx)
	require.NoError(t, ctx.Err(), "WaitSynced timed out")
}

func TestCache_Get(t *testing.T) {
	t.Parallel()

	entry := aiGatewayEntry("my-gw", "default", "dc1", 10)
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)
	c.WaitSynced(ctx)

	got := c.Get("my-gw")
	require.NotNil(t, got)
	require.Equal(t, "my-gw", got.GetName())
}

func TestCache_List(t *testing.T) {
	t.Parallel()

	entries := []capi.ConfigEntry{
		aiGatewayEntry("gw-1", "default", "dc1", 1),
		aiGatewayEntry("gw-2", "default", "dc1", 2),
	}
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, entries, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)
	c.WaitSynced(ctx)

	list := c.List()
	require.Len(t, list, 2)
}

func TestCache_ForeignDatacenter_Filtered(t *testing.T) {
	t.Parallel()

	// Entry from dc2 — should be filtered out by the dc1 cache.
	entry := aiGatewayEntry("foreign-gw", "default", "dc2", 1)
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)
	c.WaitSynced(ctx)

	require.Nil(t, c.Get("foreign-gw"), "foreign-datacenter entry must be filtered out")
	require.Empty(t, c.List())
}

func TestCache_NoKubeName_Filtered(t *testing.T) {
	t.Parallel()

	// Entry with no k8s-name meta — not managed by us, should be filtered.
	entry := &capi.AIGatewayConfigEntry{
		Kind:        capi.AIGateway,
		Name:        "user-created",
		ModifyIndex: 1,
		Meta:        map[string]string{datacenterMetaKey: "dc1"},
	}
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.Run(ctx)
	c.WaitSynced(ctx)

	require.Nil(t, c.Get("user-created"), "entry without k8s-name must be filtered out")
}

func TestCache_Subscribe_NotifiesOnChange(t *testing.T) {
	t.Parallel()

	entry := aiGatewayEntry("my-gw", "default", "dc1", 1)
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// translator maps the Consul entry name back to a K8s NamespacedName using
	// the k8s-namespace meta key — same logic as the controller's translator.
	translator := func(e capi.ConfigEntry) []types.NamespacedName {
		m := e.GetMeta()
		return []types.NamespacedName{{
			Name:      m[constants.MetaKeyKubeName],
			Namespace: m[constants.MetaKeyKubeNS],
		}}
	}

	sub := c.Subscribe(ctx, translator)
	defer sub.Cancel()

	go c.Run(ctx)

	// The first poll must deliver an event for "my-gw".
	select {
	case ge := <-sub.Events():
		require.Equal(t, "my-gw", ge.Object.GetName())
		require.Equal(t, "default", ge.Object.GetNamespace())
	case <-ctx.Done():
		t.Fatal("timed out waiting for subscription event")
	}
}

func TestCache_Subscribe_Cancel(t *testing.T) {
	t.Parallel()

	entry := aiGatewayEntry("my-gw", "default", "dc1", 1)
	var callCount atomic.Int32
	_, consulCfg, watcher := consulTestServer(t, []capi.ConfigEntry{entry}, &callCount)

	c := New(Config{
		ConsulClientConfig:  consulCfg,
		ConsulServerConnMgr: watcher,
		Datacenter:          "dc1",
		Logger:              logrtest.NewTestLogger(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	translator := func(e capi.ConfigEntry) []types.NamespacedName {
		m := e.GetMeta()
		return []types.NamespacedName{{Name: m[constants.MetaKeyKubeName], Namespace: m[constants.MetaKeyKubeNS]}}
	}

	sub := c.Subscribe(ctx, translator)

	go c.Run(ctx)

	// Drain the first event then cancel the subscription.
	select {
	case <-sub.Events():
	case <-ctx.Done():
		t.Fatal("timed out waiting for first event")
	}

	sub.Cancel()

	// After cancel, the subscription should be pruned from the live list on
	// the next poll cycle. Use Eventually so we don't depend on exact timing.
	require.Eventually(t, func() bool {
		c.subMu.Lock()
		defer c.subMu.Unlock()
		for _, s := range c.subscribers {
			if s == sub {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond, "cancelled subscription must be pruned")
}

// TestEntriesEqual verifies the dedup helper.
func TestEntriesEqual(t *testing.T) {
	t.Parallel()

	a := aiGatewayEntry("gw", "default", "dc1", 5)
	b := aiGatewayEntry("gw", "default", "dc1", 5)
	c := aiGatewayEntry("gw", "default", "dc1", 6)

	require.True(t, entriesEqual(a, b), "same ModifyIndex must be equal")
	require.False(t, entriesEqual(a, c), "different ModifyIndex must not be equal")
}
