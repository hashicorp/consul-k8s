// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

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
		!r.Configuration.Global.Federation.CreateFederationSecret &&
		!r.Configuration.Global.SecretsBackend.Vault.Enabled
}

// FedSecret returns the name of the federation secret which should be created
// by the operator.
func (r *Release) FedSecret() string {
	return r.Name + "-federation"
}
