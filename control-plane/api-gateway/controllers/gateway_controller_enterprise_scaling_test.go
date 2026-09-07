// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"testing"

	"github.com/go-logr/logr/testr"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	apicommon "github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/stretchr/testify/require"
)

func TestGatewayControllerEffectiveHelmConfig(t *testing.T) {
	t.Parallel()

	controller := GatewayController{
		HelmConfig: common.HelmConfig{
			EnableGatewayScaling: true,
		},
		ConsulMeta: apicommon.ConsulMeta{},
	}

	require.False(t, controller.effectiveHelmConfig(testr.New(t)).EnableGatewayScaling)

	controller.ConsulMeta.IsEnterpriseDistribution = true
	require.True(t, controller.effectiveHelmConfig(testr.New(t)).EnableGatewayScaling)
}
