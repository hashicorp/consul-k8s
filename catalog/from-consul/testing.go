package catalog

import (
	"sync"
)

// TestSink implements Sink for tests by just storing the services.
// Reading/writing the services should be done only while the lock is held.
type TestSink struct {
	sync.Mutex
	Services map[string]string
}

func (s *TestSink) SetServices(raw map[string]string) {
	s.Lock()
	defer s.Unlock()
	s.Services = raw
}
