// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
)

const (
	namespaceWildcard = "*"
	apiTimeout        = 5 * time.Minute
)

var Kinds = []string{api.APIGateway, api.HTTPRoute, api.TCPRoute, api.InlineCertificate}

type Config struct {
	ConsulClientConfig      *consul.Config
	ConsulServerConnMgr     consul.ServerConnectionManager
	NamespacesEnabled       bool
	CrossNamespaceACLPolicy string
	Logger                  logr.Logger
}

// Cache subscribes to and caches Consul objects, it also responsible for mainting subscriptions to
// resources that it caches.
type Cache struct {
	config    *consul.Config
	serverMgr consul.ServerConnectionManager
	logger    logr.Logger

	cache      map[string]*common.ReferenceMap
	cacheMutex *sync.Mutex

	subscribers     map[string][]*Subscription
	subscriberMutex *sync.Mutex

	namespacesEnabled       bool
	crossNamespaceACLPolicy string

	synced chan struct{}

	kinds []string
}

func New(config Config) *Cache {
	cache := make(map[string]*common.ReferenceMap, len(Kinds))
	for _, kind := range Kinds {
		cache[kind] = common.NewReferenceMap()
	}
	config.ConsulClientConfig.APITimeout = apiTimeout

	return &Cache{
		config:                  config.ConsulClientConfig,
		serverMgr:               config.ConsulServerConnMgr,
		namespacesEnabled:       config.NamespacesEnabled,
		cache:                   cache,
		cacheMutex:              &sync.Mutex{},
		subscribers:             make(map[string][]*Subscription),
		subscriberMutex:         &sync.Mutex{},
		kinds:                   Kinds,
		synced:                  make(chan struct{}, len(Kinds)),
		logger:                  config.Logger,
		crossNamespaceACLPolicy: config.CrossNamespaceACLPolicy,
	}
}

// WaitSynced is used to coordinate with the caller when the cache is initially filled.
func (c *Cache) WaitSynced(ctx context.Context) {
	for range c.kinds {
		select {
		case <-c.synced:
		case <-ctx.Done():
			return
		}
	}
}

// Subscribe handles adding a new subscription for resources of a given kind.
func (c *Cache) Subscribe(ctx context.Context, kind string, translator TranslatorFn) *Subscription {
	c.subscriberMutex.Lock()
	defer c.subscriberMutex.Unlock()

	// check that kind is registered with cache
	if !slices.Contains(c.kinds, kind) {
		return &Subscription{}
	}

	subscribers, ok := c.subscribers[kind]
	if !ok {
		subscribers = []*Subscription{}
	}

	ctx, cancel := context.WithCancel(ctx)
	events := make(chan event.GenericEvent)
	sub := &Subscription{
		translator: translator,
		ctx:        ctx,
		cancelCtx:  cancel,
		events:     events,
	}

	subscribers = append(subscribers, sub)

	c.subscribers[kind] = subscribers

	return sub
}

// Run starts the cache watch cycle, on the first call it will fill the cache with existing resources.
func (c *Cache) Run(ctx context.Context) {
	wg := &sync.WaitGroup{}

	for i := range c.kinds {
		kind := c.kinds[i]

		wg.Add(1)
		go func() {
			defer wg.Done()
			c.subscribeToConsul(ctx, kind)
		}()
	}

	wg.Wait()
}

func (c *Cache) subscribeToConsul(ctx context.Context, kind string) {
	once := &sync.Once{}

	opts := &api.QueryOptions{}
	if c.namespacesEnabled {
		opts.Namespace = namespaceWildcard
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
		if err != nil {
			c.logger.Error(err, "error initializing consul client")
			continue
		}

		entries, meta, err := client.ConfigEntries().List(kind, opts.WithContext(ctx))
		if err != nil {
			c.logger.Error(err, fmt.Sprintf("error fetching config entries for kind: %s", kind))
			continue
		}

		opts.WaitIndex = meta.LastIndex

		c.updateAndNotify(ctx, once, kind, entries)

		select {
		case <-ctx.Done():
			return
		default:
			continue
		}
	}
}

func (c *Cache) updateAndNotify(ctx context.Context, once *sync.Once, kind string, entries []api.ConfigEntry) {
	c.cacheMutex.Lock()

	cache := common.NewReferenceMap()

	for _, entry := range entries {
		cache.Set(common.EntryToReference(entry), entry)
	}

	diffs := c.cache[kind].Diff(cache)

	c.cache[kind] = cache

	// we run this the first time the cache is filled to notify the waiter
	once.Do(func() {
		c.logger.Info("sync mark for " + kind)
		c.synced <- struct{}{}
	})

	c.cacheMutex.Unlock()

	// now notify all subscribers
	c.notifySubscribers(ctx, kind, diffs)
}

// notifySubscribers notifies each subscriber for a given kind on changes to a config entry of that kind. It also
// handles removing any subscribers that have marked themselves as done.
func (c *Cache) notifySubscribers(ctx context.Context, kind string, entries []api.ConfigEntry) {
	c.subscriberMutex.Lock()
	defer c.subscriberMutex.Unlock()

	for _, entry := range entries {
		// this will hold the new list of current subscribers after we finish notifying
		subscribers := make([]*Subscription, 0, len(c.subscribers[kind]))
		for _, subscriber := range c.subscribers[kind] {
			addSubscriber := false

			for _, namespaceName := range subscriber.translator(entry) {
				event := event.GenericEvent{
					Object: newConfigEntryObject(namespaceName),
				}

				select {
				case <-ctx.Done():
					return
				case <-subscriber.ctx.Done():
					// don't add this subscriber to current list because it is done
					addSubscriber = false
				case subscriber.events <- event:
					// keep this one since we can send events to it
					addSubscriber = true
				}
			}

			if addSubscriber {
				subscribers = append(subscribers, subscriber)
			}
		}
		c.subscribers[kind] = subscribers
	}
}

// Write handles writing the config entry back to Consul, if the current reference of the
// config entry is stale then it returns an error.
func (c *Cache) Write(ctx context.Context, entry api.ConfigEntry) error {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	entryMap, ok := c.cache[entry.GetKind()]
	if !ok {
		return nil
	}

	ref := common.EntryToReference(entry)

	old := entryMap.Get(ref)
	if old != nil && common.EntriesEqual(old, entry) {
		return nil
	}

	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	if c.namespacesEnabled {
		if _, err := namespaces.EnsureExists(client, entry.GetNamespace(), c.crossNamespaceACLPolicy); err != nil {
			return err
		}
	}

	options := &api.WriteOptions{}

	_, _, err = client.ConfigEntries().Set(entry, options.WithContext(ctx))
	if err != nil {
		return err
	}

	return nil
}

// Get returns a config entry from the cache that corresponds to the given resource reference.
func (c *Cache) Get(ref api.ResourceReference) api.ConfigEntry {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	entryMap, ok := c.cache[ref.Kind]
	if !ok {
		return nil
	}

	return entryMap.Get(ref)
}

// Delete handles deleting the config entry from consul, if the current reference of the config entry is stale then
// it returns an error.
func (c *Cache) Delete(ctx context.Context, ref api.ResourceReference) error {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	entryMap, ok := c.cache[ref.Kind]
	if !ok {
		return nil
	}

	if entryMap.Get(ref) == nil {
		c.logger.Info("cached object not found, not deleting")
		return nil
	}

	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	options := &api.WriteOptions{}

	_, err = client.ConfigEntries().Delete(ref.Kind, ref.Name, options.WithContext(ctx))
	return err
}

// List returns a list of config entries from the cache that corresponds to the given kind.
func (c *Cache) List(kind string) []api.ConfigEntry {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	refMap, ok := c.cache[kind]
	if !ok {
		return nil
	}

	return refMap.Entries()
}

// LinkPolicy links a mesh write policy to a token associated with the service.
func (c *Cache) LinkPolicy(ctx context.Context, name, namespace string) error {
	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	options := &api.QueryOptions{}

	if c.namespacesEnabled {
		options.Namespace = namespace
	}

	policies, _, err := client.ACL().PolicyList(options.WithContext(ctx))
	if err != nil {
		return ignoreACLsDisabled(err)
	}

	links := []*api.ACLLink{}
	for _, policy := range policies {
		if strings.HasPrefix(policy.Name, "connect-inject-policy") {
			links = append(links, &api.ACLLink{
				Name: policy.Name,
			})
		}
	}

	tokens, _, err := client.ACL().TokenList(options.WithContext(ctx))
	if err != nil {
		return ignoreACLsDisabled(err)
	}

	for _, token := range tokens {
		for _, identity := range token.ServiceIdentities {
			if identity.ServiceName == name {
				token, _, err := client.ACL().TokenRead(token.AccessorID, options.WithContext(ctx))
				if err != nil {
					return ignoreACLsDisabled(err)
				}
				token.Policies = links

				_, _, err = client.ACL().TokenUpdate(token, &api.WriteOptions{})
				if err != nil {
					return ignoreACLsDisabled(err)
				}
			}
		}
	}

	return nil
}

// Register registers a service in Consul.
func (c *Cache) Register(ctx context.Context, registration api.CatalogRegistration) error {
	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	options := &api.WriteOptions{}

	_, err = client.Catalog().Register(&registration, options.WithContext(ctx))
	return err
}

// Deregister deregisters a service in Consul.
func (c *Cache) Deregister(ctx context.Context, deregistration api.CatalogDeregistration) error {
	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	options := &api.WriteOptions{}

	_, err = client.Catalog().Deregister(&deregistration, options.WithContext(ctx))
	return err
}

func ignoreACLsDisabled(err error) error {
	if err.Error() == "Unexpected response code: 401 (ACL support disabled)" {
		return nil
	}
	return err
}
