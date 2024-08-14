// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package godiscover

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/version"
	"github.com/hashicorp/go-discover"
	discoverk8s "github.com/hashicorp/go-discover/provider/k8s"
	"github.com/hashicorp/go-hclog"
)

// ConsulServerAddresses uses go-discover to discover Consul servers
// provided by the 'discoverString' and returns them.
func ConsulServerAddresses(discoverString string, providers map[string]discover.Provider, logger hclog.Logger) ([]string, error) {
	// If it's a cloud-auto join string, discover server addresses through the cloud provider.
	// This code was adapted from
	// https://github.com/hashicorp/consul/blob/c5fe112e59f6e8b03159ec8f2dbe7f4a026ce823/agent/retry_join.go#L55-L89.
	disco, err := newDiscover(providers)
	if err != nil {
		return nil, err
	}
	logger.Debug("using cloud auto-join", "server-addr", discoverString)
	servers, err := disco.Addrs(discoverString, logger.StandardLogger(&hclog.StandardLoggerOptions{
		InferLevels: true,
	}))
	if err != nil {
		return nil, err
	}

	// check if we discovered any servers
	if len(servers) == 0 {
		return nil, fmt.Errorf("could not discover any Consul servers with %q", discoverString)
	}

	logger.Debug("discovered servers", "servers", strings.Join(servers, " "))

	return servers, nil
}

// newDiscover initializes the new Discover object
// set up with all predefined providers, as well as
// the k8s provider.
// This code was adapted from
// https://github.com/hashicorp/consul/blob/c5fe112e59f6e8b03159ec8f2dbe7f4a026ce823/agent/retry_join.go#L42-L53
func newDiscover(providers map[string]discover.Provider) (*discover.Discover, error) {
	if providers == nil {
		providers = make(map[string]discover.Provider)
	}

	for k, v := range discover.Providers {
		providers[k] = v
	}
	providers["k8s"] = &discoverk8s.Provider{}

	userAgent := fmt.Sprintf("consul-k8s/%s (https://www.consul.io/)", version.GetHumanVersion())
	return discover.New(
		discover.WithUserAgent(userAgent),
		discover.WithProviders(providers),
	)
}
