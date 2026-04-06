// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	consulapi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

func TestGatewayScalingEnterpriseCheckEnabled(t *testing.T) {
	t.Parallel()

	check := newGatewayScalingEnterpriseCheck(testGatewayScalingConsulConfig())
	check.fetchFn = func() (bool, error) {
		return true, nil
	}

	require.True(t, check.enabled(testr.New(t)))
}

func TestGatewayScalingEnterpriseCheckCachesResult(t *testing.T) {
	t.Parallel()

	requests := 0
	check := newGatewayScalingEnterpriseCheck(testGatewayScalingConsulConfig())
	check.fetchFn = func() (bool, error) {
		requests++
		return false, nil
	}

	log := testr.New(t)
	require.False(t, check.enabled(log))
	require.False(t, check.enabled(log))
	require.Equal(t, 1, requests)
}

func TestGatewayScalingEnterpriseCheckErrorsClosed(t *testing.T) {
	t.Parallel()

	check := newGatewayScalingEnterpriseCheck(testGatewayScalingConsulConfig())
	check.fetchFn = func() (bool, error) {
		return false, errors.New("boom")
	}

	require.False(t, check.enabled(testr.New(t)))
}

func TestGatewayControllerEffectiveHelmConfig(t *testing.T) {
	t.Parallel()

	controller := GatewayController{
		HelmConfig: common.HelmConfig{
			EnableGatewayScaling: true,
		},
		gatewayScalingEnterpriseCheck: staticGatewayScalingEnterpriseCheck{},
	}

	require.False(t, controller.effectiveHelmConfig(testr.New(t)).EnableGatewayScaling)
}

func testGatewayScalingConsulConfig() *consul.Config {
	return &consul.Config{
		APIClientConfig: &consulapi.Config{},
		APITimeout:      time.Second,
	}
}
