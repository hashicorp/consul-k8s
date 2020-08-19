package catalog

import (
	"sync"

	"github.com/hashicorp/consul/api"
)

const (
	TestConsulK8STag = "k8s"
)

// TestSyncer implements Syncer for tests, giving easy access to the
// set of registrations.
type TestSyncer struct {
	sync.Mutex    // Lock should be held while accessing Registrations
	Registrations []*api.CatalogRegistration
	// The Consul node name to register for this syncer.
	NodeName string
}

func (s *TestSyncer) Node() string {
	return s.NodeName
}

// Sync implements Syncer
func (s *TestSyncer) Sync(rs []*api.CatalogRegistration) {
	s.Lock()
	defer s.Unlock()
	s.Registrations = rs
}

func NewTestSyncer() *TestSyncer {
	return &TestSyncer{NodeName: "k8s-sync"}
}
