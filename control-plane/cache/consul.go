package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/translation"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
)

const namespaceWildcard = "*"

var ErrStaleEntry = errors.New("entry is stale")

var Kinds = []string{api.APIGateway, api.HTTPRoute, api.TCPRoute, api.InlineCertificate}

type Config struct {
	ConsulClientConfig  *consul.Config
	ConsulServerConnMgr consul.ServerConnectionManager
	NamespacesEnabled   bool
	Partition           string
	Logger              logr.Logger
}

type resourceCache map[api.ResourceReference]api.ConfigEntry

func (oldCache resourceCache) diff(newCache resourceCache) []api.ConfigEntry {
	diffs := make([]api.ConfigEntry, 0)

	for ref, entry := range newCache {
		oldRef, ok := oldCache[ref]
		// ref from the new cache doesn't exist in the old one
		// this means a resource was added
		if !ok {
			diffs = append(diffs, entry)
			continue
		}

		// the entry in the old cache has an older modify index than the ref
		// from the new cache
		if oldRef.GetModifyIndex() < entry.GetModifyIndex() {
			diffs = append(diffs, entry)
		}
	}

	// get all deleted entries, these are entries present in the old cache
	// that are not present in the new
	for ref, entry := range oldCache {
		if _, ok := newCache[ref]; !ok {
			diffs = append(diffs, entry)
		}
	}
	return diffs
}

// configEntryObject is used for generic k8s events so we maintain the consul name/namespace
type configEntryObject struct {
	client.Object // embed so we fufill the object interface

	Namespace string
	Name      string
}

func (c *configEntryObject) GetNamespace() string {
	return c.Namespace
}

func (c *configEntryObject) GetName() string {
	return c.Name
}

func newConfigEntryObject(namespacedName types.NamespacedName) *configEntryObject {
	return &configEntryObject{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}
}

// Subscription represents a watcher for events on a specific kind
type Subscription struct {
	translator translation.TranslatorFn
	ctx        context.Context
	cancelCtx  context.CancelFunc
	events     chan event.GenericEvent
}

func (s *Subscription) Cancel() {
	s.cancelCtx()
}

func (s *Subscription) Events() chan event.GenericEvent {
	return s.events
}

// Cache subscribes to and caches Consul objects, it also responsible for mainting subscriptions to
// resources that it caches
type Cache struct {
	config    *consul.Config
	serverMgr consul.ServerConnectionManager
	logger    logr.Logger

	cache      map[string]resourceCache
	cacheMutex *sync.Mutex

	subscribers     map[string][]*Subscription
	subscriberMutex *sync.Mutex

	partition         string
	namespacesEnabled bool

	synced chan struct{}

	kinds []string
}

func New(config Config) *Cache {
	cache := make(map[string]resourceCache, len(Kinds))
	for _, kind := range Kinds {
		cache[kind] = make(resourceCache)
	}

	return &Cache{
		config:            config.ConsulClientConfig,
		serverMgr:         config.ConsulServerConnMgr,
		namespacesEnabled: config.NamespacesEnabled,
		partition:         config.Partition,
		cache:             cache,
		cacheMutex:        &sync.Mutex{},
		subscribers:       make(map[string][]*Subscription),
		subscriberMutex:   &sync.Mutex{},
		kinds:             Kinds,
		synced:            make(chan struct{}, len(Kinds)),
		logger:            config.Logger,
	}
}

// WaitSynced is used to coordinate with the caller when the cache is initially filled
func (c *Cache) WaitSynced(ctx context.Context) {
	for range c.kinds {
		select {
		case <-c.synced:
		case <-ctx.Done():
			return
		}
	}
}

// Subscribe handles adding a new subscription for resources of a given kind
func (c *Cache) Subscribe(ctx context.Context, kind string, translator translation.TranslatorFn) *Subscription {
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

// Run starts the cache watch cycle, on the first call it will fill the cache with existing resources
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

	if c.partition != "" {
		opts.Partition = c.partition
	}

	for {
		client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
		if err != nil {
			c.logger.Error(err, "error initializing consul client")
			continue
		}

		entries, meta, err := client.ConfigEntries().List(kind, opts)
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
	defer c.cacheMutex.Unlock()

	cache := make(resourceCache)

	for _, entry := range entries {
		cache[translation.EntryToReference(entry)] = entry
	}

	diffs := c.cache[kind].diff(cache)

	c.cache[kind] = cache

	// we run this the first time the cache is filled to notify the waiter
	once.Do(func() {
		c.synced <- struct{}{}
	})

	// now notify all subscribers
	c.notifySubscribers(ctx, kind, diffs)
}

// notifySubscribers notifies each subscriber for a given kind on changes to a config entry of that kind. It also
// handles removing any subscribers that have marked themselves as done
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

// Write handles writing back the config entry back to consul, if the current reference of the
// config entry is stale then it returns an error
func (c *Cache) Write(entry api.ConfigEntry) error {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	options := &api.WriteOptions{}

	if c.namespacesEnabled {
		options.Namespace = namespaceWildcard
	}

	if c.partition != "" {
		options.Partition = c.partition
	}

	updated, _, err := client.ConfigEntries().CAS(entry, entry.GetModifyIndex(), options)
	if err != nil {
		return err
	}

	if !updated {
		return ErrStaleEntry
	}

	return nil
}

// Get returns a config entry from the cache that corresponds to the given resource reference
func (c *Cache) Get(ref api.ResourceReference) api.ConfigEntry {
	c.cacheMutex.Lock()
	defer c.cacheMutex.Unlock()

	entryMap, ok := c.cache[ref.Kind]
	if !ok {
		return nil
	}

	entry, ok := entryMap[ref]
	if !ok {
		return nil
	}

	return entry
}
