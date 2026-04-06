// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"

	consulclient "github.com/hashicorp/consul-k8s/control-plane/consul"
)

const gatewayScalingEnterpriseCheckTTL = 5 * time.Minute

type gatewayScalingEnterpriseCheck struct {
	consulConfig *consulclient.Config
	now          func() time.Time
	ttl          time.Duration
	fetchFn      func() (bool, error)

	mu         sync.Mutex
	lastCheck  time.Time
	lastResult bool
}

func newGatewayScalingEnterpriseCheck(consulConfig *consulclient.Config) *gatewayScalingEnterpriseCheck {
	check := &gatewayScalingEnterpriseCheck{
		consulConfig: consulConfig,
		now:          time.Now,
		ttl:          gatewayScalingEnterpriseCheckTTL,
	}
	check.fetchFn = check.fetch
	return check
}

func (c *gatewayScalingEnterpriseCheck) enabled(log logr.Logger) bool {
	if c == nil || c.consulConfig == nil || c.consulConfig.APIClientConfig == nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.lastCheck.IsZero() && c.now().Sub(c.lastCheck) < c.ttl {
		return c.lastResult
	}

	enabled, err := c.fetchFn()
	c.lastCheck = c.now()
	c.lastResult = enabled
	if err != nil {
		log.Error(err, "Enterprise API Gateway scaling is enabled in Helm values but unavailable from the connected Consul cluster; annotation-driven scaling will be ignored until a valid enterprise license is detected")
		return false
	}
	if !enabled {
		log.Info("Enterprise API Gateway scaling is enabled in Helm values but the connected Consul cluster does not report a valid enterprise license; annotation-driven scaling will be ignored")
	}

	return enabled
}

func (c *gatewayScalingEnterpriseCheck) fetch() (bool, error) {
	clientConfig := *c.consulConfig.APIClientConfig
	clientConfig.HttpClient = nil
	clientConfig.Transport = nil

	client, err := consulclient.NewClient(&clientConfig, c.consulConfig.APITimeout)
	if err != nil {
		return false, fmt.Errorf("create Consul client: %w", err)
	}

	reply, err := client.Operator().LicenseGet(nil)
	if err != nil {
		return false, fmt.Errorf("query /v1/operator/license: %w", err)
	}

	return reply != nil && reply.Valid && reply.License != nil && reply.License.Product != "", nil
}

type staticGatewayScalingEnterpriseCheck struct {
	scalingEnabled bool
}

func (c staticGatewayScalingEnterpriseCheck) enabled(logr.Logger) bool {
	return c.scalingEnabled
}
