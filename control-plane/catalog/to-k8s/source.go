// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
)

// Source is the source for the sync that watches Consul services and
// updates a Sink whenever the set of services to register changes.
type Source struct {
	// ConsulClientConfig is the config for the Consul API client.
	ConsulClientConfig *consul.Config
	// ConsulServerConnMgr is the watcher for the Consul server addresses.
	ConsulServerConnMgr consul.ServerConnectionManager
	Domain              string       // Consul DNS domain
	Sink                Sink         // Sink is the sink to update with services
	Prefix              string       // Prefix is a prefix to prepend to services
	Log                 hclog.Logger // Logger
	ConsulK8STag        string       // The tag value for services registered
}

// Run is the long-running runloop for watching Consul services and
// updating the Sink.
func (s *Source) Run(ctx context.Context) {
	opts := (&api.QueryOptions{
		AllowStale: true,
		WaitIndex:  1,
		WaitTime:   1 * time.Minute,
	}).WithContext(ctx)
	for {
		consulClient, err := consul.NewClientFromConnMgr(s.ConsulClientConfig, s.ConsulServerConnMgr)
		if err != nil {
			s.Log.Error("failed to create Consul API client", "err", err)
			return
		}

		// Get all services with tags.
		var serviceMap map[string][]string
		var meta *api.QueryMeta
		err = backoff.Retry(func() error {
			serviceMap, meta, err = consulClient.Catalog().Services(opts)
			return err
		}, backoff.WithContext(backoff.NewExponentialBackOff(), ctx))

		// If the context is ended, then we end
		if ctx.Err() != nil {
			return
		}

		// If there was an error, handle that
		if err != nil {
			s.Log.Warn("error querying services, will retry", "err", err)
			continue
		}

		// Update our blocking index
		opts.WaitIndex = meta.LastIndex

		// Setup the services
		services := make(map[string]string, len(serviceMap))
		for name, tags := range serviceMap {
			// We ignore services that are synced from k8s so we can avoid
			// circular syncing. Realistically this shouldn't happen since
			// we won't register services that already exist but we double
			// check here.
			k8s := false
			for _, t := range tags {
				if t == s.ConsulK8STag {
					k8s = true
					break
				}
			}

			if !k8s {
				services[s.Prefix+name] = fmt.Sprintf("%s.service.%s", name, s.Domain)
			}
		}
		s.Log.Info("received services from Consul", "count", len(services))

		s.Sink.SetServices(services)
	}
}
