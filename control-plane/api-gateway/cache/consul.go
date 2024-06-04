// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
)

func init() {
	gatewayTpl = template.Must(template.New("root").Parse(strings.TrimSpace(gatewayRulesTpl)))
}

type templateArgs struct {
	EnableNamespaces bool
	APIGatewayName   string
}

var (
	gatewayTpl      *template.Template
	gatewayRulesTpl = `
mesh = "read"
{{- if .EnableNamespaces }}
  namespace_prefix "" {
{{- end }}
		node_prefix "" {
			policy = "read"
		}
		service_prefix "" {
			policy = "read"
		}
    service "{{.APIGatewayName}}" {
      policy = "write"
    }
{{- if .EnableNamespaces }}
	}
{{- end }}
`
)

const (
	namespaceWildcard = "*"
	apiTimeout        = 5 * time.Minute
)

var Kinds = []string{api.APIGateway, api.HTTPRoute, api.TCPRoute, api.FileSystemCertificate, api.JWTProvider}

type Config struct {
	ConsulClientConfig      *consul.Config
	ConsulServerConnMgr     consul.ServerConnectionManager
	NamespacesEnabled       bool
	Datacenter              string
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

	gatewayNameToACLPolicy map[string]*api.ACLPolicy
	policyMutex            *sync.Mutex

	gatewayNameToACLRole map[string]*api.ACLRole
	aclRoleMutex         *sync.Mutex

	gatewayNameToACLBindingRule map[string]*api.ACLBindingRule
	bindingRuleMutex            *sync.Mutex

	namespacesEnabled       bool
	crossNamespaceACLPolicy string

	synced chan struct{}

	kinds []string

	datacenter string
}

func New(config Config) *Cache {
	cache := make(map[string]*common.ReferenceMap, len(Kinds))
	for _, kind := range Kinds {
		cache[kind] = common.NewReferenceMap()
	}

	config.ConsulClientConfig.APITimeout = apiTimeout

	return &Cache{
		config:                      config.ConsulClientConfig,
		serverMgr:                   config.ConsulServerConnMgr,
		namespacesEnabled:           config.NamespacesEnabled,
		cache:                       cache,
		cacheMutex:                  &sync.Mutex{},
		subscribers:                 make(map[string][]*Subscription),
		subscriberMutex:             &sync.Mutex{},
		gatewayNameToACLPolicy:      make(map[string]*api.ACLPolicy),
		policyMutex:                 &sync.Mutex{},
		gatewayNameToACLRole:        make(map[string]*api.ACLRole),
		aclRoleMutex:                &sync.Mutex{},
		gatewayNameToACLBindingRule: make(map[string]*api.ACLBindingRule),
		bindingRuleMutex:            &sync.Mutex{},
		kinds:                       Kinds,
		synced:                      make(chan struct{}, len(Kinds)),
		logger:                      config.Logger,
		crossNamespaceACLPolicy:     config.CrossNamespaceACLPolicy,
		datacenter:                  config.Datacenter,
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
			// if we timeout we don't care about the error message because it's expected to happen on long polls
			// any other error we want to alert on
			if !strings.Contains(strings.ToLower(err.Error()), "timeout") &&
				!strings.Contains(strings.ToLower(err.Error()), "no such host") &&
				!strings.Contains(strings.ToLower(err.Error()), "connection refused") {
				c.logger.Error(err, fmt.Sprintf("error fetching config entries for kind: %s", kind))
			}
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
		meta := entry.GetMeta()
		if kind != api.JWTProvider {
			if meta[constants.MetaKeyKubeName] == "" || meta[constants.MetaKeyDatacenter] != c.datacenter {
				// Don't process things that don't belong to us. The main reason
				// for this is so that we don't garbage collect config entries that
				// are either user-created or that another controller running in a
				// federated datacenter creates. While we still allow for competing controllers
				// syncing/overriding each other due to conflicting Kubernetes objects in
				// two federated clusters (which is what the rest of the controllers also allow
				// for), we don't want to delete a config entry just because we don't have
				// its corresponding Kubernetes object if we know it belongs to another datacenter.
				continue
			}
		}

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

func (c *Cache) ensurePolicy(client *api.Client, gatewayName string) (string, error) {
	c.policyMutex.Lock()
	defer c.policyMutex.Unlock()

	createPolicy := func() (string, error) {
		policy := c.gatewayPolicy(gatewayName)

		created, _, err := client.ACL().PolicyCreate(&policy, &api.WriteOptions{})

		if isPolicyExistsErr(err, policy.Name) {
			existing, _, err := client.ACL().PolicyReadByName(policy.Name, &api.QueryOptions{})
			if err != nil {
				return "", err
			}

			// on an upgrade the cache will be empty so we need to write the policy to the cache
			c.gatewayNameToACLPolicy[gatewayName] = existing
			return existing.ID, nil
		}

		if err != nil {
			return "", err
		}

		c.gatewayNameToACLPolicy[gatewayName] = created
		return created.ID, nil
	}

	cachedPolicy, found := c.gatewayNameToACLPolicy[gatewayName]

	if !found {
		return createPolicy()
	}

	existing, _, err := client.ACL().PolicyReadByName(cachedPolicy.Name, &api.QueryOptions{})

	if existing == nil {
		return createPolicy()
	}

	if err != nil {
		return "", err
	}

	// update cache with existing policy
	c.gatewayNameToACLPolicy[gatewayName] = existing
	return existing.ID, nil
}

func getACLRoleName(gatewayName string) string {
	return fmt.Sprint("managed-gateway-acl-role-", gatewayName)
}

func (c *Cache) ensureRole(client *api.Client, gatewayName string) (string, error) {
	policyID, err := c.ensurePolicy(client, gatewayName)
	if err != nil {
		return "", err
	}

	c.aclRoleMutex.Lock()
	defer c.aclRoleMutex.Unlock()

	createRole := func() (string, error) {
		aclRoleName := getACLRoleName(gatewayName)
		role := &api.ACLRole{
			Name:        aclRoleName,
			Description: "ACL Role for Managed API Gateways",
			Policies:    []*api.ACLLink{{ID: policyID}},
		}

		_, _, err = client.ACL().RoleCreate(role, &api.WriteOptions{})
		if err != nil && !isRoleExistsErr(err, aclRoleName) {
			// don't error out in the case that the role already exists.
			return "", err
		}

		if err != nil && isRoleExistsErr(err, aclRoleName) {
			role, _, err := client.ACL().RoleReadByName(role.Name, &api.QueryOptions{})
			if err != nil {
				return "", err
			}

			role.Policies = []*api.ACLLink{{ID: policyID}}
			role, _, err = client.ACL().RoleUpdate(role, &api.WriteOptions{})
			if err != nil {
				return "", err
			}

			c.gatewayNameToACLRole[gatewayName] = role
			return aclRoleName, err
		}

		c.gatewayNameToACLRole[gatewayName] = role
		return aclRoleName, nil
	}

	cachedRole, found := c.gatewayNameToACLRole[gatewayName]

	if !found {
		return createRole()
	}

	aclRole, _, err := client.ACL().RoleReadByName(cachedRole.Name, &api.QueryOptions{})
	if err != nil {
		return "", err
	}

	if aclRole != nil {
		c.gatewayNameToACLRole[gatewayName] = aclRole
		return aclRole.Name, nil
	}

	return createRole()
}

func (c *Cache) gatewayPolicy(gatewayName string) api.ACLPolicy {
	var data bytes.Buffer
	if err := gatewayTpl.Execute(&data, templateArgs{
		EnableNamespaces: c.namespacesEnabled,
		APIGatewayName:   gatewayName,
	}); err != nil {
		// just panic if we can't compile the simple template
		// as it means something else is going severly wrong.
		panic(err)
	}

	return api.ACLPolicy{
		Name:        fmt.Sprint("api-gateway-policy-for-", gatewayName),
		Description: "API Gateway token Policy",
		Rules:       data.String(),
	}
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

	options := &api.WriteOptions{Namespace: ref.Namespace, Partition: ref.Partition}

	_, err = client.ConfigEntries().Delete(ref.Kind, ref.Name, options.WithContext(ctx))
	if err != nil {
		c.logger.Info("delete error", "err", err)
	}
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

func (c *Cache) EnsureRoleBinding(authMethod, service, namespace string) error {
	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	role, err := c.ensureRole(client, service)
	if err != nil {
		return ignoreACLsDisabled(err)
	}

	bindingRule := &api.ACLBindingRule{
		Description: fmt.Sprintf("Binding Rule for %s/%s", namespace, service),
		AuthMethod:  authMethod,
		Selector:    fmt.Sprintf("serviceaccount.name==%q and serviceaccount.namespace==%q", service, namespace),
		BindType:    api.BindingRuleBindTypeRole,
		BindName:    role,
	}

	existingRules, _, err := client.ACL().BindingRuleList(authMethod, &api.QueryOptions{})
	if err != nil {
		return err
	}

	for _, existingRule := range existingRules {
		if existingRule.BindName == bindingRule.BindName && existingRule.Description == bindingRule.Description {
			bindingRule.ID = existingRule.ID
		}
	}

	if bindingRule.ID == "" {
		_, _, err := client.ACL().BindingRuleCreate(bindingRule, &api.WriteOptions{})
		if err != nil {
			return err
		}

		c.bindingRuleMutex.Lock()
		defer c.bindingRuleMutex.Unlock()
		c.gatewayNameToACLBindingRule[service] = bindingRule

		return nil
	}
	_, _, err = client.ACL().BindingRuleUpdate(bindingRule, &api.WriteOptions{})
	if err != nil {
		return err
	}

	c.bindingRuleMutex.Lock()
	defer c.bindingRuleMutex.Unlock()
	c.gatewayNameToACLBindingRule[service] = bindingRule

	return nil
}

var (
	ErrFailedToDeleteBindingRule = errors.New("failed to delete ACLBindingRule")
	ErrFailedToDeleteRole        = errors.New("failed to delete ACLRole")
	ErrFailedToDeletePolicy      = errors.New("failed to delete ACLPolicy")
	ErrACLSDisabled              = errors.New("ACLs are disabled")
)

func (c *Cache) RemoveRoleBinding(authMethod, service, namespace string) error {
	client, err := consul.NewClientFromConnMgr(c.config, c.serverMgr)
	if err != nil {
		return err
	}

	// acquire locks
	c.bindingRuleMutex.Lock()
	defer c.bindingRuleMutex.Unlock()

	c.aclRoleMutex.Lock()
	defer c.aclRoleMutex.Unlock()

	c.policyMutex.Lock()
	defer c.policyMutex.Unlock()

	deleteFns := make([]func() error, 0, 3)

	if rule, ok := c.gatewayNameToACLBindingRule[service]; ok {
		deleteFns = append(deleteFns, c.bindingRuleDelete(client, rule, service))
	}

	if role, ok := c.gatewayNameToACLRole[service]; ok {
		deleteFns = append(deleteFns, c.roleDelete(client, role, service))
	}

	if policy, ok := c.gatewayNameToACLPolicy[service]; ok {
		deleteFns = append(deleteFns, c.policyDelete(client, policy, service))
	}

	for _, fn := range deleteFns {
		err := fn()
		if errors.Is(err, ErrACLSDisabled) {
			return nil
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Cache) bindingRuleDelete(client *api.Client, rule *api.ACLBindingRule, service string) func() error {
	return func() error {
		_, err := client.ACL().BindingRuleDelete(rule.ID, &api.WriteOptions{})
		if err != nil {
			if ignoreNotFound(err) == nil {
				delete(c.gatewayNameToACLBindingRule, service)
				return nil
			}

			if ignoreACLsDisabled(err) == nil {
				delete(c.gatewayNameToACLBindingRule, service)
				return ErrACLSDisabled
			}
			return fmt.Errorf("%w: %s", ErrFailedToDeleteBindingRule, err)
		}

		delete(c.gatewayNameToACLBindingRule, service)

		return nil
	}
}

func (c *Cache) roleDelete(client *api.Client, role *api.ACLRole, service string) func() error {
	return func() error {
		_, err := client.ACL().RoleDelete(role.ID, &api.WriteOptions{})
		if err != nil {
			if ignoreNotFound(err) == nil {
				delete(c.gatewayNameToACLRole, service)
				return nil
			}

			if ignoreACLsDisabled(err) == nil {
				delete(c.gatewayNameToACLBindingRule, service)
				return ErrACLSDisabled
			}
			return fmt.Errorf("%w: %s", ErrFailedToDeleteRole, err)
		}
		delete(c.gatewayNameToACLRole, service)

		return nil
	}
}

func (c *Cache) policyDelete(client *api.Client, policy *api.ACLPolicy, service string) func() error {
	return func() error {
		_, err := client.ACL().PolicyDelete(policy.ID, &api.WriteOptions{})
		if err != nil {
			if ignoreNotFound(err) == nil {
				delete(c.gatewayNameToACLPolicy, service)
				return nil
			}

			if ignoreACLsDisabled(err) == nil {
				delete(c.gatewayNameToACLBindingRule, service)
				return ErrACLSDisabled
			}
			return fmt.Errorf("%w: %s", ErrFailedToDeletePolicy, err)
		}
		delete(c.gatewayNameToACLPolicy, service)

		return nil
	}
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

func ignoreNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Unexpected response code: 404") {
		return nil
	}

	return err
}

func ignoreACLsDisabled(err error) error {
	if err == nil {
		return nil
	}
	if err.Error() == "Unexpected response code: 401 (ACL support disabled)" {
		return nil
	}
	return err
}

// isPolicyExistsErr returns true if err is due to trying to call the
// policy create API when the policy already exists.
func isPolicyExistsErr(err error, policyName string) bool {
	return isExistsErr(err, "Policy", policyName)
}

// isExistsErr returns true if err is due to trying to call an API for a given type and it already exists.
func isExistsErr(err error, typeName, name string) bool {
	return err != nil &&
		strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), fmt.Sprintf("Invalid %s: A %s with Name %q already exists", typeName, typeName, name))
}

// isRoleExistsErr returns true if err is due to trying to call the
// role create API when the role already exists.
func isRoleExistsErr(err error, roleName string) bool {
	return isExistsErr(err, "Role", roleName)
}
