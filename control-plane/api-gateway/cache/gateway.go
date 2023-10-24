// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cache

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/cenkalti/backoff"
	"github.com/go-logr/logr"
	"github.com/hashicorp/consul/api"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

type GatewayCache struct {
	config    Config
	serverMgr consul.ServerConnectionManager
	logger    logr.Logger

	data      map[api.ResourceReference][]api.CatalogService
	dataMutex sync.RWMutex

	subscribedGateways map[api.ResourceReference]context.CancelFunc
	mutex              sync.RWMutex

	ctx context.Context
}

func NewGatewayCache(ctx context.Context, config Config) *GatewayCache {
	return &GatewayCache{
		config:             config,
		serverMgr:          config.ConsulServerConnMgr,
		logger:             config.Logger,
		data:               make(map[api.ResourceReference][]api.CatalogService),
		subscribedGateways: make(map[api.ResourceReference]context.CancelFunc),
		ctx:                ctx,
	}
}

func (r *GatewayCache) ServicesFor(ref api.ResourceReference) []api.CatalogService {
	r.dataMutex.RLock()
	defer r.dataMutex.RUnlock()

	return r.data[common.NormalizeMeta(ref)]
}

func (r *GatewayCache) FetchServicesFor(ctx context.Context, ref api.ResourceReference) ([]api.CatalogService, error) {
	client, err := consul.NewClientFromConnMgr(r.config.ConsulClientConfig, r.serverMgr)
	if err != nil {
		return nil, err
	}

	opts := &api.QueryOptions{}
	if r.config.NamespacesEnabled && ref.Namespace != "" {
		opts.Namespace = ref.Namespace
	}

	services, _, err := client.Catalog().Service(ref.Name, "", opts.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	return common.DerefAll(services), nil
}

func (r *GatewayCache) EnsureSubscribed(ref api.ResourceReference, resource types.NamespacedName) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if _, exists := r.subscribedGateways[common.NormalizeMeta(ref)]; exists {
		return
	}

	ctx, cancel := context.WithCancel(r.ctx)
	r.subscribedGateways[common.NormalizeMeta(ref)] = cancel
	go r.subscribeToGateway(ctx, ref, resource)
}

func (r *GatewayCache) RemoveSubscription(ref api.ResourceReference) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if cancel, exists := r.subscribedGateways[common.NormalizeMeta(ref)]; exists {
		cancel()
		delete(r.subscribedGateways, common.NormalizeMeta(ref))
	}
}

func (r *GatewayCache) subscribeToGateway(ctx context.Context, ref api.ResourceReference, resource types.NamespacedName) {
	opts := &api.QueryOptions{}
	if r.config.NamespacesEnabled && ref.Namespace != "" {
		opts.Namespace = ref.Namespace
	}

	var (
		services []*api.CatalogService
		meta     *api.QueryMeta
	)

	for {
		select {
		case <-ctx.Done():
			r.dataMutex.Lock()
			delete(r.data, ref)
			r.dataMutex.Unlock()
			return
		default:
		}

		retryBackoff := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 10)

		if err := backoff.Retry(func() error {
			client, err := consul.NewClientFromConnMgr(r.config.ConsulClientConfig, r.serverMgr)
			if err != nil {
				return err
			}

			services, meta, err = client.Catalog().Service(ref.Name, "", opts.WithContext(ctx))
			if err != nil {
				return err
			}

			return nil
		}, backoff.WithContext(retryBackoff, ctx)); err != nil {
			// if we timeout we don't care about the error message because it's expected to happen on long polls
			// any other error we want to alert on
			if !strings.Contains(strings.ToLower(err.Error()), "timeout") &&
				!strings.Contains(strings.ToLower(err.Error()), "no such host") &&
				!strings.Contains(strings.ToLower(err.Error()), "connection refused") {
				r.logger.Error(err, fmt.Sprintf("unable to fetch config entry for gateway: %s/%s", ref.Namespace, ref.Name))
			}
			continue
		}

		opts.WaitIndex = meta.LastIndex

		derefed := common.DerefAll(services)

		r.dataMutex.Lock()
		r.data[common.NormalizeMeta(ref)] = derefed
		r.dataMutex.Unlock()
	}
}
