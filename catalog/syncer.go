package catalog

import (
	"context"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
)

const (
	// ConsulReconcilePeriod is how often the syncer will attempt to
	// reconcile the expected service states with the remote Consul server.
	ConsulReconcilePeriod = 30 * time.Second
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

	// ReconcilePeriod is the duration that the syncer will wait (at most)
	// to reconcile the remote catalog with the local catalog. We may sync
	// more frequently in certain situations.
	ReconcilePeriod time.Duration

	lock     sync.Mutex
	once     sync.Once
	services map[string]struct{} // set of valid service names
	nodes    map[string]*consulSyncState
	deregs   map[string]*api.CatalogDeregistration
	watchers map[string]context.CancelFunc
}

// consulSyncState keeps track of the state of syncing nodes/services.
type consulSyncState struct {
	// Services keeps track of the valid services on this node (by service ID)
	Services map[string]*api.CatalogRegistration
}

// Sync implements Syncer
func (s *ConsulSyncer) Sync(rs []*api.CatalogRegistration) {
	// Grab the lock so we can replace the sync state
	s.lock.Lock()
	defer s.lock.Unlock()

	s.services = make(map[string]struct{})
	s.nodes = make(map[string]*consulSyncState)
	for _, r := range rs {
		// Mark this as a valid service
		s.services[r.Service.Service] = struct{}{}

		// Initialize the state if we don't have it
		state, ok := s.nodes[r.Node]
		if !ok {
			state = &consulSyncState{
				Services: make(map[string]*api.CatalogRegistration),
			}

			s.nodes[r.Node] = state
		}

		// Add our registration
		state.Services[r.Service.ID] = r
	}
}

// Run is the long-running runloop for reconciling the local set of
// services to register with the remote state.
func (s *ConsulSyncer) Run(ctx context.Context) {
	s.once.Do(s.init)

	// Start the background watchers
	go s.watchReapableServices(ctx)

	reconcileTimer := time.NewTimer(s.ReconcilePeriod)
	defer reconcileTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Log.Info("ConsulSyncer quitting")
			return

		case <-reconcileTimer.C:
			s.syncFull(ctx)
			reconcileTimer.Reset(s.ReconcilePeriod)
		}
	}
}

// watchReapableServices is a long-running task started by Run that
// holds blocking queries to the Consul server to watch for any services
// tagged with k8s that are no longer valid and need to be deleted.
// This task only marks them for deletion but doesn't perform the actual
// deletion.
func (s *ConsulSyncer) watchReapableServices(ctx context.Context) {
	opts := api.QueryOptions{
		AllowStale: true,
		WaitIndex:  1,
		WaitTime:   1 * time.Minute,
	}

	// minWait is the minimum time to wait between scheduling service deletes.
	// This prevents a lot of churn in services causing high CPU usage.
	minWait := s.ReconcilePeriod / 4
	minWaitCh := time.After(0)
	for {
		// Get all services with tags.
		serviceMap, meta, err := s.Client.Catalog().Services(&opts)
		if err != nil {
			s.Log.Warn("error querying services, will retry", "err", err)
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

		// Lock so we can modify the
		s.lock.Lock()

		// Go through the service map and find services that should be reaped
		for name, tags := range serviceMap {
			for _, tag := range tags {
				if tag == ConsulK8STag {
					// We only care if we don't know about this service at all.
					if _, ok := s.services[name]; ok {
						continue
					}

					s.Log.Info("invalid service found, scheduling for delete",
						"service-name", name)
					if err := s.scheduleReapServiceLocked(name); err != nil {
						s.Log.Info("error querying service for delete",
							"service-name", name,
							"err", err)
					}

					// We're done searching this service, let it go
					break
				}
			}
		}

		s.lock.Unlock()
	}
}

// watchService watches all instances of a service by name for changes
// and schedules re-registration or deletion if necessary.
func (s *ConsulSyncer) watchService(ctx context.Context, name string) {
	s.Log.Info("starting service watcher", "service-name", name)
	defer s.Log.Info("stopping service watcher", "service-name", name)

	opts := api.QueryOptions{
		AllowStale: true,
		WaitIndex:  1,
		WaitTime:   1 * time.Minute,
	}

	for {
		// Wait for service changes
		services, meta, err := s.Client.Catalog().Service(name, ConsulK8STag, &opts)
		if err != nil {
			s.Log.Warn("error querying service, will retry",
				"service-name", name,
				"err", err)
			continue
		}

		// Quit if our context is over
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Update our blocking index
		opts.WaitIndex = meta.LastIndex

		// Lock so we can modify the set of actions to take
		s.lock.Lock()

		for _, svc := range services {
			// We delete unless we have a service and the node mapping
			delete := true
			if _, ok := s.services[svc.ServiceName]; ok {
				nodeSvc := s.nodes[svc.Node]
				delete = nodeSvc == nil || nodeSvc.Services[svc.ServiceID] == nil
			}

			if delete {
				s.deregs[svc.ServiceID] = &api.CatalogDeregistration{
					Node:      svc.Node,
					ServiceID: svc.ServiceID,
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
func (s *ConsulSyncer) scheduleReapServiceLocked(name string) error {
	services, _, err := s.Client.Catalog().Service(name, ConsulK8STag, &api.QueryOptions{
		AllowStale: true,
	})
	if err != nil {
		return err
	}

	for _, svc := range services {
		s.deregs[svc.ServiceID] = &api.CatalogDeregistration{
			Node:      svc.Node,
			ServiceID: svc.ServiceID,
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

	// Start the service watchers
	for k, cf := range s.watchers {
		if _, ok := s.services[k]; !ok {
			cf()
			delete(s.watchers, k)
		}
	}
	for k := range s.services {
		if _, ok := s.watchers[k]; !ok {
			svcCtx, cancelF := context.WithCancel(ctx)
			go s.watchService(svcCtx, k)
			s.watchers[k] = cancelF
		}
	}

	// Do all deregistrations first
	for _, r := range s.deregs {
		s.Log.Info("deregistering service",
			"node-name", r.Node,
			"service-id", r.ServiceID)
		_, err := s.Client.Catalog().Deregister(r, nil)
		if err != nil {
			s.Log.Warn("error deregistering service",
				"node-name", r.Node,
				"service-id", r.ServiceID,
				"err", err)
		}
	}

	// Always clear deregistrations, they'll repopulate if we had errors
	s.deregs = make(map[string]*api.CatalogDeregistration)

	// Register all the services. This will overwrite any changes that
	// may have been made to the registered services.
	for _, state := range s.nodes {
		for _, r := range state.Services {
			_, err := s.Client.Catalog().Register(r, nil)
			if err != nil {
				s.Log.Warn("error registering service",
					"node-name", r.Node,
					"service-name", r.Service.Service,
					"err", err)
				continue
			}

			s.Log.Debug("registered service instance",
				"node-name", r.Node,
				"service-name", r.Service.Service)
		}
	}
}

func (s *ConsulSyncer) init() {
	if s.services == nil {
		s.services = make(map[string]struct{})
	}
	if s.nodes == nil {
		s.nodes = make(map[string]*consulSyncState)
	}
	if s.deregs == nil {
		s.deregs = make(map[string]*api.CatalogDeregistration)
	}
	if s.watchers == nil {
		s.watchers = make(map[string]context.CancelFunc)
	}
	if s.ReconcilePeriod == 0 {
		s.ReconcilePeriod = ConsulReconcilePeriod
	}
}
