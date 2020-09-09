package catalog

import (
	"sync"

	"github.com/hashicorp/consul/api"
)

const (
	TestConsulK8STag = "k8s"
)

// testSyncer implements Syncer for tests, giving easy access to the
// set of registrations.
type testSyncer struct {
	sync.Mutex    // Lock should be held while accessing Registrations
	Registrations []*api.CatalogRegistration
}

// Sync implements Syncer
func (s *testSyncer) Sync(rs []*api.CatalogRegistration) {
	s.Lock()
	defer s.Unlock()
	s.Registrations = rs
}

func newTestSyncer() *testSyncer {
	return &testSyncer{}
}
