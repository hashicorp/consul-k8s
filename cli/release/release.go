package release

import (
	"github.com/hashicorp/consul-k8s/cli/helm"
)

// Release represents a Consul cluster and its associated configuration.
type Release struct {
	// Name is the name of the release.
	Name string

	// Namespace is the Kubernetes namespace in which the release is deployed.
	Namespace string

	// Configuration is the Helm configuration for the release.
	Configuration helm.Values
}

// ShouldExpectFederationSecret returns true if the non-primary DC in a
// federated cluster.
func (r *Release) ShouldExpectFederationSecret() bool {
	return r.Configuration.Global.Federation.Enabled &&
		r.Configuration.Global.Datacenter != r.Configuration.Global.Federation.PrimaryDatacenter &&
		!r.Configuration.Global.Federation.CreateFederationSecret
}
