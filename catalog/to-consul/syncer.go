package catalog

import (
	"context"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/deckarep/golang-set"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
)

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
	Client *api.Client
	Log    hclog.Logger

	// AllowK8sNamespacesSet is a set of k8s namespaces to explicitly allow for
	// syncing. It supports the special character `*` which indicates that
	// all k8s namespaces are eligible unless explicitly denied. This filter
	// is applied before checking pod annotations.
	AllowK8sNamespacesSet mapset.Set

	// DenyK8sNamespacesSet is a set of k8s namespaces to explicitly deny
	// syncing and thus service registration with Consul. An empty set
	// means that no namespaces are removed from consideration. This filter
	// takes precedence over AllowK8sNamespacesSet.
	DenyK8sNamespacesSet mapset.Set

	// EnableNamespaces indicates that a user is running Consul Enterprise
	// with version 1.7+ which is namespace aware. It enables Consul namespaces,
	// with syncing into either a single Consul namespace or mirrored from
	// k8s namespaces.
	EnableNamespaces bool

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

	lock sync.Mutex
	once sync.Once

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
}

// Sync implements Syncer
func (s *ConsulSyncer) Sync(rs []*api.CatalogRegistration) {
	// Grab the lock so we can replace the sync state
	s.lock.Lock()
	defer s.lock.Unlock()

	s.serviceNames = make(map[string]mapset.Set)
	s.namespaces = make(map[string]map[string]*api.CatalogRegistration)

	for _, r := range rs {
		// Determine the namespace we're working in
		// If namespaces aren't enabled, using `default` as the namespace key
		ns := "default"
		if s.EnableNamespaces {
			ns = r.Service.Namespace
		}

		// Mark this as a valid service, initializing state if necessary
		if _, ok := s.serviceNames[ns]; !ok {
			s.serviceNames[ns] = mapset.NewSet()
		}
		s.serviceNames[ns].Add(r.Service.Service)

		// Add service to namespaces map, initializing if necessary
		if _, ok := s.namespaces[ns]; !ok {
			s.namespaces[ns] = make(map[string]*api.CatalogRegistration)
		}
		s.namespaces[ns][r.Service.ID] = r
	}
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
	// minWait is the minimum time to wait between scheduling service deletes.
	// This prevents a lot of churn in services causing high CPU usage.
	minWait := s.SyncPeriod / 4
	minWaitCh := time.After(0)
	for {
		// Get services within all the namespaces we're tracking
		serviceMap := make(map[string]map[string][]string)
		var err error
		for ns, _ := range s.namespaces {
			// Set up default query options
			opts := api.QueryOptions{
				AllowStale: true,
				WaitTime:   15 * time.Second,
			}

			// Add the namespace to the query options if namespaces are enabled
			if s.EnableNamespaces {
				opts.Namespace = ns
			}

			// Get all of the services from this namespace
			err = backoff.Retry(func() error {
				var err error
				serviceMap[ns], _, err = s.Client.Catalog().Services(&opts)
				return err
			}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))
			if err != nil {
				s.Log.Warn("error querying services, will retry", "namespace", ns, "err", err)
				continue
			}
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

		// Lock so we can modify the stored state
		s.lock.Lock()

		// Go through the service map and find services that should be reaped
		for ns, services := range serviceMap {
			for name, tags := range services {
				for _, tag := range tags {
					if tag == s.ConsulK8STag {
						// We only care if we don't know about this service at all.
						if s.serviceNames[ns].Contains(name) {
							continue
						}

						s.Log.Info("invalid service found, scheduling for delete",
							"service-name", name, "service-consul-namespace", ns)
						if err := s.scheduleReapServiceLocked(name, ns); err != nil {
							s.Log.Info("error querying service for delete",
								"service-name", name,
								"service-consul-namespace", ns,
								"err", err)
						}

						// We're done searching this service, let it go
						break
					}
				}
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

		// Wait for service changes
		var services []*api.CatalogService
		err := backoff.Retry(func() error {
			var err error
			services, _, err = s.Client.Catalog().Service(name, s.ConsulK8STag, queryOpts)
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
			if !s.serviceNames[namespace].Contains(svc.ServiceName) || s.namespaces[namespace][svc.ServiceID] == nil {
				s.deregs[svc.ServiceID] = &api.CatalogDeregistration{
					Node:      svc.Node,
					ServiceID: svc.ServiceID,
				}
				if s.EnableNamespaces {
					s.deregs[svc.ServiceID].Namespace = namespace
				}
			}
		}

		s.lock.Unlock()
	}
}

// scheduleReapService finds all the instances of the service with the given
// name that have the k8s tag and schedules them for removal.
//
// Precondition: lock must be held
func (s *ConsulSyncer) scheduleReapServiceLocked(name, namespace string) error {
	// Set up query options
	opts := api.QueryOptions{AllowStale: true}
	if s.EnableNamespaces {
		opts.Namespace = namespace
	}

	// Only consider services that are tagged from k8s
	services, _, err := s.Client.Catalog().Service(name, s.ConsulK8STag, &opts)
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
	}

	return nil
}

// syncFull is called periodically to perform all the write-based API
// calls to sync the data with Consul. This may also start background
// watchers for specific services.
func (s *ConsulSyncer) syncFull(ctx context.Context) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.Log.Info("registering services")

	// Update the service watchers
	for ns, watchers := range s.watchers {
		// If the service the watcher is watching is no longer valid,
		// cancel the watcher
		for svc, cf := range watchers {
			if s.serviceNames[ns] == nil || !s.serviceNames[ns].Contains(svc) {
				cf()
				delete(s.watchers[ns], svc)
			}
		}
	}

	// Start watchers for all services if they're not already running
	for ns, services := range s.serviceNames {
		for svc := range services.Iter() {
			if _, ok := s.watchers[ns][svc.(string)]; !ok {
				svcCtx, cancelF := context.WithCancel(ctx)
				go s.watchService(svcCtx, svc.(string), ns)

				// Create watcher map if it doesn't exist for this namespace
				if s.watchers[ns] == nil {
					s.watchers[ns] = make(map[string]context.CancelFunc)
				}

				// Add the watcher to our tracking
				s.watchers[ns][svc.(string)] = cancelF
			}
		}
	}

	// Do all deregistrations first
	for _, r := range s.deregs {
		s.Log.Info("deregistering service",
			"node-name", r.Node,
			"service-id", r.ServiceID,
			"service-consul-namespace", r.Namespace)
		_, err := s.Client.Catalog().Deregister(r, nil)
		if err != nil {
			s.Log.Warn("error deregistering service",
				"node-name", r.Node,
				"service-id", r.ServiceID,
				"service-consul-namespace", r.Namespace,
				"err", err)
		}
	}

	// Always clear deregistrations, they'll repopulate if we had errors
	s.deregs = make(map[string]*api.CatalogDeregistration)

	// Register all the services. This will overwrite any changes that
	// may have been made to the registered services.
	for _, services := range s.namespaces {
		for _, r := range services {
			if s.EnableNamespaces {
				// Check and potentially create the service's namespace if
				// it doesn't already exist
				err := s.checkAndCreateNamespace(r.Service.Namespace)
				if err != nil {
					s.Log.Warn("error checking and creating Consul namespace",
						"node-name", r.Node,
						"service-name", r.Service.Service,
						"consul-namespace-name", r.Service.Namespace,
						"err", err)
					continue
				}
			}

			// Register the service
			_, err := s.Client.Catalog().Register(r, nil)
			if err != nil {
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
		}
	}
}

func (s *ConsulSyncer) init() {
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
}

func (s *ConsulSyncer) checkAndCreateNamespace(ns string) error {
	// Check if the Consul namespace exists
	namespaceInfo, _, err := s.Client.Namespaces().Read(ns, nil)
	if err != nil {
		return err
	}

	// If not, create it
	if namespaceInfo == nil {
		consulNamespace := api.Namespace{
			Name:        ns,
			Description: "Auto-generated by a Catalog Sync Process",
			// TODO: when metadata is added to the api
			// Meta:        map[string]string{"external-source": "kubernetes"},
		}

		_, _, err = s.Client.Namespaces().Create(&consulNamespace, nil)
		if err != nil {
			return err
		}
	}

	return nil
}
