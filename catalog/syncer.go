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

	lock sync.Mutex
	rs   []*api.CatalogRegistration
}

// Sync implements Syncer
func (s *ConsulSyncer) Sync(rs []*api.CatalogRegistration) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.rs = rs
}

// Run is the long-running runloop for reconciling the local set of
// services to register with the remote state.
func (s *ConsulSyncer) Run(ctx context.Context) {
	if s.ReconcilePeriod == 0 {
		s.ReconcilePeriod = ConsulReconcilePeriod
	}

	reconcileTimer := time.NewTimer(s.ReconcilePeriod)
	defer reconcileTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.Log.Info("ConsulSyncer quitting")
			return

		case <-reconcileTimer.C:
			s.register()
			reconcileTimer.Reset(s.ReconcilePeriod)
		}
	}
}

func (s *ConsulSyncer) register() {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Register all the services. This will overwrite any changes that
	// may have been made to the registered services.
	for _, r := range s.rs {
		_, err := s.Client.Catalog().Register(r, nil)
		if err != nil {
			s.Log.Warn("error registering service",
				"node-name", r.Node,
				"service-name", r.Service.Service,
				"err", err)
			continue
		}
	}
}
