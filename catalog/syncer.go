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
	services map[string]*consulSyncState
}

// consulSyncState keeps track of the state of syncing nodes/services.
type consulSyncState struct {
	// Registrations is the list of registrations for this service.
	Registrations []*api.CatalogRegistration

	// Deregistrations are a list of deregistrations for this service
	// to reconcile changes.
	Deregistrations []*api.CatalogDeregistration
}

// Sync implements Syncer
func (s *ConsulSyncer) Sync(rs []*api.CatalogRegistration) {
	// Build the new state, we don't need a lock for this.
	services := make(map[string]*consulSyncState, len(rs))
	for _, r := range rs {
		state, ok := services[r.Service.Service]
		if !ok {
			state = &consulSyncState{}
			services[r.Service.Service] = state
		}
		state.Registrations = append(state.Registrations, r)
	}

	// Grab the lock so we can replace the sync state
	s.lock.Lock()
	defer s.lock.Unlock()

	// First go through and find any services marked for deletion
	for k, state := range s.services {
		if state == nil || len(state.Deregistrations) == 0 {
			continue
		}

		if _, ok := services[k]; !ok {
			services[k] = state
		}
	}

	s.services = services
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
			s.syncFull()
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
					if s.services[name] != nil {
						continue
					}

					s.Log.Info("invalid service found, scheduling for delete",
						"service-name", name)
					if err := s.scheduleReapService(name); err != nil {
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

// scheduleReapService finds all the instances of the service with the given
// name that have the k8s tag and schedules them for removal.
func (s *ConsulSyncer) scheduleReapService(name string) error {
	services, _, err := s.Client.Catalog().Service(name, ConsulK8STag, &api.QueryOptions{
		AllowStale: true,
	})
	if err != nil {
		return err
	}

	s.services[name] = &consulSyncState{}
	state := s.services[name]
	for _, svc := range services {
		state.Deregistrations = append(state.Deregistrations, &api.CatalogDeregistration{
			Node:      svc.Node,
			ServiceID: svc.ServiceID,
		})
	}

	return nil
}

func (s *ConsulSyncer) syncFull() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.Log.Info("registering services")

	// Register all the services. This will overwrite any changes that
	// may have been made to the registered services.
	for name, state := range s.services {
		// Service is scheduled for deletion, delete it.
		for _, r := range state.Deregistrations {
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

		// Always clear deregistrations, any invalid ones will repopulate later
		state.Deregistrations = nil

		for _, r := range state.Registrations {
			_, err := s.Client.Catalog().Register(r, nil)
			if err != nil {
				s.Log.Warn("error registering service",
					"node-name", r.Node,
					"service-name", r.Service.Service,
					"err", err)
				continue
			}
		}

		s.Log.Debug("registered service", "service-name", name)
	}
}

func (s *ConsulSyncer) init() {
	if s.services == nil {
		s.services = make(map[string]*consulSyncState)
	}
	if s.ReconcilePeriod == 0 {
		s.ReconcilePeriod = ConsulReconcilePeriod
	}
}
