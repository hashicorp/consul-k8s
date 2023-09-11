// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consul

import (
	"fmt"

	"github.com/hashicorp/consul/proto-public/pbdataplane"
)

// NewDataplaneServiceClient creates a pbdataplane.DataplaneServiceClient for gathering proxy bootstrap config.
// It is initialized with a consul-server-connection-manager Watcher to continuously find Consul
// server addresses.
func NewDataplaneServiceClient(watcher ServerConnectionManager) (pbdataplane.DataplaneServiceClient, error) {

	// We recycle the GRPC connection from the discovery client because it
	// should have all the necessary dial options, including the resolver that
	// continuously updates Consul server addresses. Otherwise, a lot of code from consul-server-connection-manager
	// would need to be duplicated
	state, err := watcher.State()
	if err != nil {
		return nil, fmt.Errorf("unable to get connection manager state: %w", err)
	}
	dpClient := pbdataplane.NewDataplaneServiceClient(state.GRPCConn)

	return dpClient, nil
}
