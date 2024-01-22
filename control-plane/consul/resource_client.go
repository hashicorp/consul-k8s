// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"fmt"

	"github.com/hashicorp/consul/proto-public/pbresource"
)

// NewResourceServiceClient creates a pbresource.ResourceServiceClient for creating V2 Consul resources.
// It is initialized with a consul-server-connection-manager Watcher to continuously find Consul
// server addresses.
func NewResourceServiceClient(watcher ServerConnectionManager) (pbresource.ResourceServiceClient, error) {

	// We recycle the GRPC connection from the discovery client because it
	// should have all the necessary dial options, including the resolver that
	// continuously updates Consul server addresses. Otherwise, a lot of code from consul-server-connection-manager
	// would need to be duplicated
	state, err := watcher.State()
	if err != nil {
		return nil, fmt.Errorf("unable to get connection manager state: %w", err)
	}
	resourceClient := pbresource.NewResourceServiceClient(state.GRPCConn)

	return resourceClient, nil
}
