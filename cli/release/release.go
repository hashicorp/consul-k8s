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

// FedSecret fetches the federation secret for the release or an empty string
// if the federation secret is not set. The federation secret could be set
// in several places in the configuration. This method returns the first
// secret name that is set.
func (r *Release) FedSecret() string {
	if s := r.Configuration.Global.TLS.CaCert.SecretName; s != "" {
		return s
	}

	if s := r.Configuration.Global.TLS.CaKey.SecretName; s != "" {
		return s
	}

	if s := r.Configuration.Global.Acls.ReplicationToken.SecretName; s != "" {
		return s
	}

	if s := r.Configuration.Global.GossipEncryption.SecretName; s != "" {
		return s
	}

	return ""
}
