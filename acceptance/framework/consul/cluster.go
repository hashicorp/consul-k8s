package consul

import (
	"testing"

	"github.com/hashicorp/consul/api"
)

// Cluster represents a consul cluster object.
type Cluster interface {
	// SetupConsulClient returns a new Consul client.
	SetupConsulClient(t *testing.T, secure bool) *api.Client

	// Create creates a new Consul Cluster.
	Create(t *testing.T, args ...string)

	// Upgrade modifies the cluster in-place by merging the helm values
	// from the initial install with helmValues. Any keys that were previously set
	// will be overridden by the helmValues keys.
	Upgrade(t *testing.T, helmValues map[string]string, args ...string)

	// Destroy destroys the cluster
	Destroy(t *testing.T, args ...string)
}

// ClusterKind represents the kind of Consul cluster being used (e.g. "Helm" or "CLI").
type ClusterKind int

const (
	Helm ClusterKind = iota
	CLI
)
