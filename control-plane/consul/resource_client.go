package consul

import (
	"context"
	"fmt"

	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/go-hclog"
)

// NewResourceServiceClient creates a pbresource.ResourceServiceClient for creating V2 Consul resources.
// It is initialized with a consul-server-connection-manager discovery config to continuously find Consul
// server addresses.
// The caller should make sure to Stop() the returned `watcher` (preferably with a `defer`) to clean up the gRPC
// connection and the discovery client.
// The caller can also set `config.ServerWatchDisabled=false` to prevent subscribing to Consul server address
// changes, as is the case with single-shot operations.
func NewResourceServiceClient(ctx context.Context, config discovery.Config, logger hclog.Logger, hack int) (pbresource.ResourceServiceClient, *discovery.Watcher, error) {

	watcher, err := discovery.NewWatcher(ctx, config, logger.Named("consul-server-connection-manager"))
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create Consul server watcher: %w", err)
	}

	go watcher.Run()

	// We recycle the GRPC connection from the discovery client because it
	// should have all the necessary dial options, including the resolver that
	// continuously updates Consul server addresses. Otherwise, a lot of code from consul-server-connection-manager
	// would need to be duplicated
	state, err := watcher.State()
	if err != nil {
		watcher.Stop()
		return nil, nil, fmt.Errorf("unable to get connection manager state: %w", err)
	}
	resourceClient := pbresource.NewResourceServiceClient(state.GRPCConn)

	return resourceClient, watcher, nil
}
