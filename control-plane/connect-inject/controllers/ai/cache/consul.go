// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package cache provides a Consul config-entry cache for the AI gateway
// controllers. It mirrors the design of api-gateway/cache but is scoped to
// the ai-gateway config entry kind only.
//
// The Cache runs a background blocking-query long-poll against Consul.
// Whenever an ai-gateway entry is created, modified, or deleted out-of-band,
// any registered Subscription receives a controller-runtime GenericEvent so
// the InferenceGatewayController re-queues a Reconcile — exactly the mechanism
// used by api-gateway/cache for APIGatewayConfigEntry at
// api-gateway/controllers/gateway_controller.go:536.
package cache

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	capi "github.com/hashicorp/consul/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const (
	// apiTimeout is the long-poll timeout passed to Consul so each blocking
	// query blocks for at most this duration before re-connecting.
	apiTimeout = 5 * time.Minute

	// datacenterMetaKey is the metadata key stamped on every ai-gateway config
	// entry by toConsulConfigEntry. Matches api/common.DatacenterKey.
	datacenterMetaKey = "consul.hashicorp.com/source-datacenter"
)

// Config holds the constructor arguments for a Cache.
type Config struct {
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	// Datacenter filters out config entries owned by other datacenters,
	// matching the convention in api-gateway/cache/consul.go:updateAndNotify.
	Datacenter string
	Logger     logr.Logger
}

// Cache subscribes to and mirrors Consul ai-gateway config entries.
// It exposes a Subscribe method so controllers can receive GenericEvents
// whenever the Consul-side state diverges from the last known state.
type Cache struct {
	config     *consul.Config
	serverMgr  consul.ServerConnectionManager
	logger     logr.Logger
	datacenter string

	// mu protects entries.
	mu      sync.RWMutex
	entries map[string]capi.ConfigEntry // keyed by entry name

	subMu       sync.Mutex
	subscribers []*Subscription

	// synced is buffered-1; receives a struct{} after the first successful poll.
	synced chan struct{}
	once   sync.Once
}

// New constructs a Cache. Call Run in a goroutine to start the background poll.
func New(cfg Config) *Cache {
	cfg.ConsulClientConfig.APITimeout = apiTimeout
	return &Cache{
		config:     cfg.ConsulClientConfig,
		serverMgr:  cfg.ConsulServerConnMgr,
		logger:     cfg.Logger,
		datacenter: cfg.Datacenter,
		entries:    make(map[string]capi.ConfigEntry),
		synced:     make(chan struct{}, 1),
	}
}

// WaitSynced blocks until the first successful Consul poll completes or ctx
// is cancelled — matching api-gateway/cache/consul.go:WaitSynced.
func (c *Cache) WaitSynced(ctx context.Context) {
	select {
	case <-c.synced:
	case <-ctx.Done():
	}
}

// Subscribe registers a Subscription. The returned Subscription's Events()
// channel receives a GenericEvent each time an ai-gateway config entry
// changes in Consul and the translator maps it to a non-empty NamespacedName.
//
// translator is called with each diffed entry and returns the K8s
// NamespacedName(s) to enqueue for reconciliation — mirroring
// api-gateway/cache/consul.go:Subscribe + Subscription.
func (c *Cache) Subscribe(ctx context.Context, translator TranslatorFn) *Subscription {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	sub := &Subscription{
		translator: translator,
		ctx:        ctx,
		cancelCtx:  cancel,
		events:     make(chan event.GenericEvent),
	}
	c.subscribers = append(c.subscribers, sub)
	return sub
}

// Run starts the blocking-query loop. It must be called in a goroutine and
// exits when ctx is cancelled (manager shutdown).
func (c *Cache) Run(ctx context.Context) {
	c.subscribeToConsul(ctx)
}

func (c *Cache) subscribeToConsul(ctx context.Context) {
	opts := &capi.QueryOptions{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		consulClient, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
		if err != nil {
			c.logger.Error(err, "error initialising Consul client")
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}

		entries, meta, err := consulClient.ConfigEntries().List(capi.AIGateway, opts.WithContext(ctx))
		if err != nil {
			if !isLongPollErr(err) {
				c.logger.Error(err, "error listing ai-gateway config entries")
			}
			continue
		}

		opts.WaitIndex = meta.LastIndex
		c.updateAndNotify(ctx, entries)
	}
}

// updateAndNotify diffs the new poll response against the local cache,
// replaces the cache, and notifies subscribers of every changed/deleted entry.
func (c *Cache) updateAndNotify(ctx context.Context, entries []capi.ConfigEntry) {
	c.mu.Lock()

	// Build a fresh snapshot, honouring the datacenter ownership check that
	// api-gateway/cache/consul.go:updateAndNotify uses to ignore foreign entries.
	newEntries := make(map[string]capi.ConfigEntry, len(entries))
	for _, e := range entries {
		m := e.GetMeta()
		if m[constants.MetaKeyKubeName] == "" {
			// Not managed by any consul-k8s controller — no k8s-name stamp.
			continue
		}
		if c.datacenter != "" && m[datacenterMetaKey] != c.datacenter {
			// Belongs to a different datacenter.
			continue
		}
		newEntries[e.GetName()] = e
	}

	// Diff: new or modified entries.
	var changed []capi.ConfigEntry
	for name, newE := range newEntries {
		old, ok := c.entries[name]
		if !ok || old.GetModifyIndex() < newE.GetModifyIndex() {
			changed = append(changed, newE)
		}
	}
	// Diff: deleted entries (present in old cache, absent from new snapshot).
	for name, oldE := range c.entries {
		if _, ok := newEntries[name]; !ok {
			changed = append(changed, oldE)
		}
	}

	c.entries = newEntries

	// Signal initial sync complete (once).
	c.once.Do(func() {
		c.logger.Info("consul ai-gateway cache: initial sync complete")
		c.synced <- struct{}{}
	})

	c.mu.Unlock()

	c.notifySubscribers(ctx, changed)
}

// notifySubscribers fans out changed entries to all live subscriptions,
// pruning any whose context has been cancelled — including pruning subs
// with no changed entries so that Cancel() is observed quickly.
func (c *Cache) notifySubscribers(ctx context.Context, entries []capi.ConfigEntry) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	live := c.subscribers[:0]
	for _, sub := range c.subscribers {
		// First, check whether this subscription has already been cancelled.
		select {
		case <-sub.ctx.Done():
			// Subscription was cancelled — drop it without delivering events.
			continue
		default:
		}

		keep := true
		for _, e := range entries {
			for _, nn := range sub.translator(e) {
				ge := event.GenericEvent{Object: newEntryObject(nn.Namespace, nn.Name)}
				select {
				case <-ctx.Done():
					return
				case <-sub.ctx.Done():
					keep = false
				case sub.events <- ge:
				}
			}
		}
		if keep {
			live = append(live, sub)
		}
	}
	c.subscribers = live
}

// Write upserts an ai-gateway config entry in Consul. It skips the Consul
// call when the locally cached entry already has the same ModifyIndex,
// mirroring the dedup guard in api-gateway/cache/consul.go:Write.
func (c *Cache) Write(ctx context.Context, entry capi.ConfigEntry) error {
	c.mu.RLock()
	old, ok := c.entries[entry.GetName()]
	c.mu.RUnlock()

	if ok && entriesEqual(old, entry) {
		return nil
	}

	consulClient, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	opts := &capi.WriteOptions{}
	if entry.GetNamespace() != "" {
		opts.Namespace = entry.GetNamespace()
	}
	if entry.GetPartition() != "" {
		opts.Partition = entry.GetPartition()
	}
	_, _, err = consulClient.ConfigEntries().Set(entry, opts.WithContext(ctx))
	return err
}

// Delete removes an ai-gateway config entry from Consul. It is a no-op when
// the entry is absent from the local cache, mirroring the guard in
// api-gateway/cache/consul.go:Delete to prevent spurious deletes on cold start.
func (c *Cache) Delete(ctx context.Context, name, namespace, partition string) error {
	c.mu.RLock()
	_, ok := c.entries[name]
	c.mu.RUnlock()

	if !ok {
		c.logger.Info("ai-gateway cache: entry not found locally, skipping Consul delete", "name", name)
		return nil
	}

	consulClient, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	opts := &capi.WriteOptions{Namespace: namespace, Partition: partition}
	_, err = consulClient.ConfigEntries().Delete(capi.AIGateway, name, opts.WithContext(ctx))
	return err
}

// Get returns the locally cached ai-gateway config entry for name, or nil.
func (c *Cache) Get(name string) capi.ConfigEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[name]
}

// List returns all locally cached ai-gateway config entries.
func (c *Cache) List() []capi.ConfigEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]capi.ConfigEntry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	return out
}

// ── helpers ───────────────────────────────────────────────────────────────────

// entryObject is a minimal client.Object used to carry a name/namespace into
// a GenericEvent without importing a full K8s resource type — mirroring
// api-gateway/cache/kubernetes.go:configEntryObject.
type entryObject struct {
	client.Object
	namespace string
	name      string
}

func (o *entryObject) GetNamespace() string { return o.namespace }
func (o *entryObject) GetName() string      { return o.name }

func newEntryObject(namespace, name string) *entryObject {
	return &entryObject{namespace: namespace, name: name}
}

// isLongPollErr returns true for errors that are routine during Consul
// blocking queries (timeouts, startup races) and should not be logged as errors.
// Mirrors api-gateway/cache/consul.go:subscribeToConsul error filter.
func isLongPollErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "invalid config entry kind")
}

// entriesEqual is a lightweight equality check using ModifyIndex — sufficient
// for write-dedup. ModifyIndex advances on every Consul write.
func entriesEqual(a, b capi.ConfigEntry) bool {
	return a.GetModifyIndex() == b.GetModifyIndex()
}
