// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These are 1.8 replica-management tests, not the enterprise annotation/HPA
// autoscaling tests introduced in 2.0. No enterprise license is required.
func TestCustomAPIGateway_Replicas(t *testing.T) {
	h := newGatewayHarness(t, false)
	cases := []struct {
		name       string
		deployment v1alpha1.DeploymentSpec
		initial    int32
		scaleTo    *int32
	}{
		{name: "implicit default", initial: 1},
		{name: "explicit default", deployment: replicaConfig(2, 1, 3), initial: 2},
		{name: "initial default capped by maximum", deployment: replicaConfig(4, 1, 3), initial: 3},
		{name: "initial default raised to minimum", deployment: replicaConfig(1, 2, 3), initial: 2},
		{name: "manual scale up within bounds", deployment: replicaConfig(2, 1, 3), initial: 2, scaleTo: ptr.To(int32(3))},
		{name: "manual scale down within bounds", deployment: replicaConfig(2, 1, 3), initial: 2, scaleTo: ptr.To(int32(1))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, class := h.createClass(t, tc.deployment)
			gateway := h.createGateway(t, class)
			h.checkInstances(t, gateway, tc.initial)
			if tc.scaleTo != nil {
				h.scale(t, gateway, *tc.scaleTo)
				h.checkInstances(t, gateway, *tc.scaleTo)
				h.requireStableReplicas(t, gateway, *tc.scaleTo)
			}
		})
	}
}

func TestCustomAPIGateway_UpdatedDefaultPreservesExistingReplicas(t *testing.T) {
	h := newGatewayHarness(t, false)
	gcc, class := h.createClass(t, replicaConfig(1, 1, 3))
	existing := h.createGateway(t, class)
	h.checkInstances(t, existing, 1)
	h.scale(t, existing, 2)
	h.checkInstances(t, existing, 2)

	retryGateway(t, func(r *retry.R) {
		var current v1alpha1.GatewayClassConfig
		require.NoError(r, h.k8s.Get(context.Background(), client.ObjectKeyFromObject(gcc), &current))
		current.Spec.DeploymentSpec.DefaultInstances = ptr.To(int32(3))
		require.NoError(r, h.k8s.Update(context.Background(), &current))
	})
	// Creating a second gateway exercises the updated class configuration without
	// assuming the controller watches GatewayClassConfig changes directly.
	created := h.createGateway(t, class)
	h.checkInstances(t, created, 3)
	h.checkInstances(t, existing, 2)
	h.requireStableReplicas(t, existing, 2)
}

func replicaConfig(defaultInstances, minInstances, maxInstances int32) v1alpha1.DeploymentSpec {
	return v1alpha1.DeploymentSpec{
		DefaultInstances: ptr.To(defaultInstances),
		MinInstances:     ptr.To(minInstances),
		MaxInstances:     ptr.To(maxInstances),
	}
}
