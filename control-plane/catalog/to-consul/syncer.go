// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/armon/go-metrics"
	"github.com/armon/go-metrics/prometheus"
	"github.com/cenkalti/backoff"
	mapset "github.com/deckarep/golang-set"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
)

var (
	baseName            = []string{"consul", "sync_catalog", "to_consul"}
	registerName        = append(baseName, "register")
	deregisterName      = append(baseName, "deregister")
	registerErrorName   = append(baseName, "register", "error")
	deregisterErrorName = append(baseName, "deregister", "error")
	syncCatalogStatus   = append(baseName, "status")
)

var SyncToConsulCounters = []prometheus.CounterDefinition{
	{
		Name: registerName,
		Help: "Increments for each service instance registered to Consul via catalog sync",
	},
	{
		Name: deregisterName,
		Help: "Increments for each service deregistered from Consul via catalog sync",
	},
	{
		Name: registerErrorName,
		Help: "Increments whenever a Consul API client returns an error for a catalog sync register request",
	},
	{
		Name: deregisterErrorName,
		Help: "Increments whenever a Consul API client returns an error for a catalog sync deregister request request",
	},
}

var SyncCatalogGauge = []prometheus.GaugeDefinition{
	{
		Name: syncCatalogStatus,
		Help: "Status of the Consul Client endpoint. 1 for connected, 0 for disconnected",
	},
}

const (
	// ConsulSyncPeriod is how often the syncer will attempt to
	// reconcile the expected service states with the remote Consul server.
	ConsulSyncPeriod = 30 * time.Second

	// ConsulServicePollPeriod is how often a service is checked for
	// whether it has instances to reap.
	ConsulServicePollPeriod = 60 * time.Second
)

// Syncer is responsible for syncing a set of Consul catalog registrations.
// An external system manages the set of registrations and periodically
// updates the Syncer. The Syncer should keep the remote system in sync with
// the given set of registrations.
type Syncer interface {
	// Sync is called to sync the full set of registrations.
	Sync([]*api.CatalogRegistration)
}

// ConsulSyncer is a Syncer that takes the set of registrations and
// registers them with Consul. It also watches Consul for changes to the
// services and ensures the local set of registrations represents the
// source of truth, overwriting any external changes to the services.
type ConsulSyncer struct {
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager

	Log hclog.Logger

	// EnableNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which is namespace aware. It enables Consul namespaces,
	// with syncing into either a single Consul namespace or mirrored from
	// k8s namespaces.
	EnableNamespaces bool

	// CrossNamespaceACLPolicy is the name of the ACL policy to attach to
	// any created Consul namespaces to allow cross namespace service discovery.
	// Only necessary if ACLs are enabled.
	CrossNamespaceACLPolicy string

	// SyncPeriod is the interval between full catalog syncs. These will
	// re-register all services to prevent overwrites of data. This should
	// happen relatively infrequently and default to 30 seconds.
	//
	// ServicePollPeriod is the interval to look for invalid services to
	// deregister. One request will be made for each synced service in
	// Kubernetes.
	//
	// For both syncs, smaller more frequent and focused syncs may be
	// triggered by known drift or changes.
	SyncPeriod        time.Duration
	ServicePollPeriod time.Duration

	// ConsulK8STag is the tag value for services registered.
	ConsulK8STag string

	// The Consul node name to register services with.
	ConsulNodeName string

	lock sync.Mutex
	once sync.Once

	// initialSync is used to ensure that we have received our initial list
	// of services before we start reaping services. When it is closed,
	// the initial sync is complete.
	initialSync chan bool
	// initialSyncOnce controls the close operation on the initialSync channel
	// to ensure it isn't closed more than once.
	initialSyncOnce sync.Once

	// serviceNames is all namespaces mapped to a set of valid
	// Consul service names
	serviceNames map[string]mapset.Set

	// namespaces is all namespaces mapped to a map of Consul service
	// ids mapped to their CatalogRegistrations
	namespaces map[string]map[string]*api.CatalogRegistration
	deregs     map[string]*api.CatalogDeregistration

	// watchers is all namespaces mapped to a map of Consul service
	// names mapped to a cancel function for watcher routines
	watchers map[string]map[string]context.CancelFunc

	PrometheusSink *prometheus.PrometheusSink
}

// Sync implements Syncer.
func (s *ConsulSyncer) Sync(rs []*api.CatalogRegistration) {
	// Grab the lock so we can replace the sync state
	s.lock.Lock()
	defer s.lock.Unlock()

	s.serviceNames = make(map[string]mapset.Set)
	s.namespaces = make(map[string]map[string]*api.CatalogRegistration)

	for _, r := range rs {
		// Determine the namespace the service is in to use for indexing
		// against the s.serviceNames and s.namespaces maps.
		// This will be "" for OSS.
		ns := r.Service.Namespace

		// Mark this as a valid service, initializing state if necessary
		if _, ok := s.serviceNames[ns]; !ok {
			s.serviceNames[ns] = mapset.NewSet()
		}
		s.serviceNames[ns].Add(r.Service.Service)
		s.Log.Debug("[Sync] adding service to serviceNames set", "service", r.Service, "service name", r.Service.Service)

		// Add service to namespaces map, initializing if necessary
		if _, ok := s.namespaces[ns]; !ok {
			s.namespaces[ns] = make(map[string]*api.CatalogRegistration)
		}
		s.namespaces[ns][r.Service.ID] = r
		s.Log.Debug("[Sync] adding service to namespaces map", "service", r.Service)
	}

	// Signal that the initial sync is complete and our maps have been populated.
	// We can now safely reap untracked services.
	s.initialSyncOnce.Do(func() { close(s.initialSync) })
}

// Run is the long-running runloop for reconciling the local set of
// services to register with the remote state.
func (s *ConsulSyncer) Run(ctx context.Context) {
	s.once.Do(s.init)

	// Start the background watchers
	go s.watchReapableServices(ctx)

	reconcileTimer := time.NewTimer(s.SyncPeriod)
	defer reconcileTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Log.Info("ConsulSyncer quitting")
			return

		case <-reconcileTimer.C:
			s.syncFull(ctx)
			reconcileTimer.Reset(s.SyncPeriod)
		}
	}
}

// watchReapableServices is a long-running task started by Run that
// holds blocking queries to the Consul server to watch for any services
// tagged with k8s that are no longer valid and need to be deleted.
// This task only marks them for deletion but doesn't perform the actual
// deletion.
func (s *ConsulSyncer) watchReapableServices(ctx context.Context) {
	// We must wait for the initial sync to be complete and our maps to be
	// populated. If we don't wait, we will reap all services tagged with k8s
	// because we have no tracked services in our maps yet.
	<-s.initialSync

	opts := &api.QueryOptions{
		AllowStale: true,
		WaitIndex:  1,
		WaitTime:   1 * time.Minute,
		Filter:     fmt.Sprintf("\"%s\" in Tags", s.ConsulK8STag),
	}

	if s.EnableNamespaces {
		opts.Namespace = "*"
	}

	// minWait is the minimum time to wait between scheduling service deletes.
	// This prevents a lot of churn in services causing high CPU usage.
	minWait := s.SyncPeriod / 4
	minWaitCh := time.After(0)
	for {
		// Create a new consul client.
		consulClient, err := consul.NewClientFromConnMgr(s.ConsulClientConfig, s.ConsulServerConnMgr)
		if err != nil {
			s.Log.Error("failed to create Consul API client", "err", err)
			return
		}

		var services *api.CatalogNodeServiceList
		var meta *api.QueryMeta
		err = backoff.Retry(func() error {
			services, meta, err = consulClient.Catalog().NodeServiceList(s.ConsulNodeName, opts)
			return err
		}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))

		if err != nil {
			s.Log.Warn("error querying services, will retry", "err", err)
		} else {
			s.Log.Debug("[watchReapableServices] services returned from catalog",
				"services", services)
		}

		// Wait our minimum time before continuing or retrying
		select {
		case <-minWaitCh:
			if err != nil {
				continue
			}

			minWaitCh = time.After(minWait)
		case <-ctx.Done():
			return
		}

		// Update our blocking index
		opts.WaitIndex = meta.LastIndex

		// Lock so we can modify the stored state
		s.lock.Lock()

		// Go through the service array and find services that should be reaped
		for _, service := range services.Services {
			// Check that the namespace exists in the valid service names map
			// before checking whether it contains the service
			svcNs := service.Namespace
			if !s.EnableNamespaces {
				// Set namespace to empty when namespaces are not enabled.
				svcNs = ""
			}
			if _, ok := s.serviceNames[svcNs]; ok {
				// We only care if we don't know about this service at all.
				if s.serviceNames[svcNs].Contains(service.Service) {
					s.Log.Debug("[watchReapableServices] serviceNames contains service",
						"namespace", svcNs,
						"service-name", service.Service)
					continue
				}
			}

			s.Log.Info("invalid service found, scheduling for delete",
				"service-name", service.Service, "service-id", service.ID, "service-consul-namespace", svcNs)
			if err = s.scheduleReapServiceLocked(service.Service, svcNs); err != nil {
				s.Log.Info("error querying service for delete",
					"service-name", service.Service,
					"service-consul-namespace", svcNs,
					"err", err)
			}
		}

		s.lock.Unlock()
	}
}

// watchService watches all instances of a service by name for changes
// and schedules re-registration or deletion if necessary.
func (s *ConsulSyncer) watchService(ctx context.Context, name, namespace string) {
	s.Log.Info("starting service watcher", "service-name", name, "service-consul-namespace", namespace)
	defer s.Log.Info("stopping service watcher", "service-name", name, "service-consul-namespace", namespace)

	for {
		select {
		// Quit if our context is over
		case <-ctx.Done():
			return

		// Wait for our poll period
		case <-time.After(s.SyncPeriod):
		}

		// Set up query options
		queryOpts := &api.QueryOptions{
			AllowStale: true,
		}
		if s.EnableNamespaces {
			// Sets the Consul namespace to query the catalog
			queryOpts.Namespace = namespace
		}

		// Create a new consul client.
		consulClient, err := consul.NewClientFromConnMgr(s.ConsulClientConfig, s.ConsulServerConnMgr)
		if err != nil {
			s.Log.Error("failed to create Consul API client; will retry", "err", err)
			continue
		}
		// Wait for service changes
		var services []*api.CatalogService
		err = backoff.Retry(func() error {
			services, _, err = consulClient.Catalog().Service(name, s.ConsulK8STag, queryOpts)
			return err
		}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))
		if err != nil {
			s.Log.Warn("error querying service, will retry",
				"service-name", name,
				"service-namespace", namespace, // will be "" if namespaces aren't enabled
				"err", err)
			continue
		}

		// Lock so we can modify the set of actions to take
		s.lock.Lock()

		for _, svc := range services {
			// Make sure the namespace exists before we run checks against it
			if _, ok := s.serviceNames[namespace]; ok {
				// If the service is valid and its info isn't nil, we don't deregister it
				if s.serviceNames[namespace].Contains(svc.ServiceName) && s.namespaces[namespace][svc.ServiceID] != nil {
					continue
				}
			}

			s.deregs[svc.ServiceID] = &api.CatalogDeregistration{
				Node:      svc.Node,
				ServiceID: svc.ServiceID,
			}
			if s.EnableNamespaces {
				s.deregs[svc.ServiceID].Namespace = namespace
			}
			s.Log.Debug("[watchService] service being scheduled for deregistration",
				"namespace", namespace,
				"service name", svc.ServiceName,
				"service id", svc.ServiceID,
				"service dereg", s.deregs[svc.ServiceID])
		}

		s.lock.Unlock()
	}
}

// scheduleReapService finds all the instances of the service with the given
// name that have the k8s tag and schedules them for removal.
//
// Precondition: lock must be held.
func (s *ConsulSyncer) scheduleReapServiceLocked(name, namespace string) error {
	// Set up query options
	opts := api.QueryOptions{AllowStale: true}
	if s.EnableNamespaces {
		opts.Namespace = namespace
	}

	// Create a new consul client.
	consulClient, err := consul.NewClientFromConnMgr(s.ConsulClientConfig, s.ConsulServerConnMgr)
	if err != nil {
		s.Log.Error("failed to create Consul API client", "err", err)
		return err
	}

	// Only consider services that are tagged from k8s
	services, _, err := consulClient.Catalog().Service(name, s.ConsulK8STag, &opts)
	if err != nil {
		return err
	}

	// Create deregistrations for all of these
	for _, svc := range services {
		s.deregs[svc.ServiceID] = &api.CatalogDeregistration{
			Node:      svc.Node,
			ServiceID: svc.ServiceID,
		}
		if s.EnableNamespaces {
			s.deregs[svc.ServiceID].Namespace = namespace
		}
		s.Log.Debug("[scheduleReapServiceLocked] service being scheduled for deregistration",
			"namespace", namespace,
			"service name", svc.ServiceName,
			"service id", svc.ServiceID,
			"service dereg", s.deregs[svc.ServiceID])
	}

	return nil
}

// syncFull is called periodically to perform all the write-based API
// calls to sync the data with Consul. This may also start background
// watchers for specific services.
func (s *ConsulSyncer) syncFull(ctx context.Context) {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Create a new consul client.
	consulClient, err := consul.NewClientFromConnMgr(s.ConsulClientConfig, s.ConsulServerConnMgr)
	if err != nil {
		s.Log.Error("failed to create Consul API client", "err", err)
		return
	}

	s.Log.Info("registering services")

	// Update the service watchers
	for ns, watchers := range s.watchers {
		// If the service the watcher is watching is no longer valid,
		// cancel the watcher
		for svc, cf := range watchers {
			if s.serviceNames[ns] == nil || !s.serviceNames[ns].Contains(svc) {
				cf()
				delete(s.watchers[ns], svc)
				s.Log.Debug("[syncFull] deleting service watcher", "namespace", ns, "service", svc)
			}
		}
	}

	// Start watchers for all services if they're not already running
	for ns, services := range s.serviceNames {
		for svc := range services.Iter() {
			if _, ok := s.watchers[ns][svc.(string)]; !ok {
				svcCtx, cancelF := context.WithCancel(ctx)
				go s.watchService(svcCtx, svc.(string), ns)
				s.Log.Debug("[syncFull] starting watchService routine", "namespace", ns, "service", svc)

				// Create watcher map if it doesn't exist for this namespace
				if s.watchers[ns] == nil {
					s.watchers[ns] = make(map[string]context.CancelFunc)
				}

				// Add the watcher to our tracking
				s.watchers[ns][svc.(string)] = cancelF
			}
		}
	}

	// Do all deregistrations first.
	for _, r := range s.deregs {
		s.Log.Info("deregistering service",
			"node-name", r.Node,
			"service-id", r.ServiceID,
			"service-consul-namespace", r.Namespace)

		_, err = consulClient.Catalog().Deregister(r, nil)
		if err != nil {
			// metric count for error deregistering k8s services from Consul
			labels := []metrics.Label{
				{Name: "error", Value: err.Error()},
			}
			s.PrometheusSink.IncrCounterWithLabels(deregisterErrorName, 1, labels)

			s.Log.Warn("error deregistering service",
				"node-name", r.Node,
				"service-id", r.ServiceID,
				"service-consul-namespace", r.Namespace,
				"err", err)
			continue
		}

		// metric count for deregistering k8s services from Consul
		labels := []metrics.Label{
			{Name: "id", Value: r.ServiceID},
			{Name: "node", Value: r.Node},
			{Name: "namespace", Value: r.Namespace},
		}
		s.PrometheusSink.IncrCounterWithLabels(deregisterName, 1, labels)
	}

	// Always clear deregistrations, they'll repopulate if we had errors
	s.deregs = make(map[string]*api.CatalogDeregistration)

	// Register all the services. This will overwrite any changes that
	// may have been made to the registered services.
	for _, services := range s.namespaces {
		for _, r := range services {
			if s.EnableNamespaces {
				_, err = namespaces.EnsureExists(consulClient, r.Service.Namespace, s.CrossNamespaceACLPolicy)
				if err != nil {
					s.Log.Warn("error checking and creating Consul namespace",
						"node-name", r.Node,
						"service-name", r.Service.Service,
						"consul-namespace-name", r.Service.Namespace,
						"err", err)
					continue
				}
			}

			// Register the service.
			_, err = consulClient.Catalog().Register(r, nil)
			if err != nil {
				// metric count for error syncing K8S services to Consul
				label := []metrics.Label{
					{Name: "error", Value: err.Error()},
				}
				s.PrometheusSink.IncrCounterWithLabels(registerErrorName, 1, label)
				// Set to 0 if the endpoint is down or returns an error
				s.PrometheusSink.SetGauge(syncCatalogStatus, 0)

				s.Log.Warn("error registering service",
					"node-name", r.Node,
					"service-name", r.Service.Service,
					"service", r.Service,
					"err", err)
				continue
			}

			s.Log.Debug("registered service instance",
				"node-name", r.Node,
				"service-name", r.Service.Service,
				"consul-namespace-name", r.Service.Namespace,
				"service", r.Service)

			// metric count and service metadata syncing k8s services to Consul
			labels := []metrics.Label{
				{Name: "id", Value: r.Service.ID},
				{Name: "service", Value: r.Service.Service},
				{Name: "node", Value: r.Node},
				{Name: "namespace", Value: r.Service.Namespace},
				{Name: "datacenter", Value: r.Datacenter},
			}

			if val, exists := r.Service.Meta["external-k8s-ref-name"]; exists && val != "" {
				labels = append(labels, metrics.Label{Name: "external_k8s_ref_name", Value: val})
			}
			if r.Check != nil {
				labels = append(labels, metrics.Label{Name: "status", Value: r.Check.Status})
			}
			s.PrometheusSink.IncrCounterWithLabels(registerName, 1, labels)
			// Set to 1 if the endpoint is healthy
			s.PrometheusSink.SetGauge(syncCatalogStatus, 1)
		}
	}
}

func (s *ConsulSyncer) init() {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.serviceNames == nil {
		s.serviceNames = make(map[string]mapset.Set)
	}
	if s.namespaces == nil {
		s.namespaces = make(map[string]map[string]*api.CatalogRegistration)
	}
	if s.deregs == nil {
		s.deregs = make(map[string]*api.CatalogDeregistration)
	}
	if s.watchers == nil {
		s.watchers = make(map[string]map[string]context.CancelFunc)
	}
	if s.SyncPeriod == 0 {
		s.SyncPeriod = ConsulSyncPeriod
	}
	if s.ServicePollPeriod == 0 {
		s.ServicePollPeriod = ConsulServicePollPeriod
	}
	if s.initialSync == nil {
		s.initialSync = make(chan bool)
	}
}
