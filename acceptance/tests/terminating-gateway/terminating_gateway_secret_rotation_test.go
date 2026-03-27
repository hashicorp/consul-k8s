// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package terminatinggateway

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
)

// Test that updating a Secret referenced by a terminating gateway rotates
// the metadata timestamp key on the Consul config entry.
func TestTerminatingGatewaySecretRotation(t *testing.T) {
	cases := []struct {
		name   string
		secure bool
	}{
		{name: "secure=false", secure: false},
		{name: "secure=true", secure: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			helmValues := map[string]string{
				"connectInject.enabled":                    "true",
				"terminatingGateways.enabled":              "true",
				"terminatingGateways.gateways[0].name":     "terminating-gateway",
				"terminatingGateways.gateways[0].replicas": "1",
				"global.acls.manageSystemACLs":             strconv.FormatBool(tc.secure),
				"global.tls.enabled":                       strconv.FormatBool(tc.secure),
			}

			logger.Log(t, "creating consul cluster")
			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, tc.secure)

			logger.Log(t, "applying terminating gateway + secret fixture")
			k8s.KubectlApplyK(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDeleteK(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation")
			})

			metaKey := "consul.hashicorp.com/secret/tgw-rotation-secret/last-rotation"

			var firstRotation string
			retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", nil)
				require.NoError(r, err)

				tgEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
				require.True(r, ok)

				firstRotation = tgEntry.Meta[metaKey]
				require.NotEmpty(r, firstRotation)
				_, err = time.Parse(time.RFC3339, firstRotation)
				require.NoError(r, err)
			})

			logger.Log(t, "patching unrelated secret and verifying metadata does not rotate")
			unrelatedPatch := fmt.Sprintf(`{"metadata":{"annotations":{"rotation-trigger":"%s"}}}`, strconv.FormatInt(time.Now().UnixNano(), 10))
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "tgw-unrelated-secret", "--type=merge", "-p", unrelatedPatch)

			for i := 0; i < 3; i++ {
				time.Sleep(5 * time.Second)
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", nil)
				require.NoError(t, err)

				tgEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
				require.True(t, ok)

				current := tgEntry.Meta[metaKey]
				require.Equal(t, firstRotation, current)
			}

			logger.Log(t, "patching referenced secret to trigger gateway reconcile")
			secretPatch := fmt.Sprintf(`{"metadata":{"annotations":{"rotation-trigger":"%s"}}}`, strconv.FormatInt(time.Now().UnixNano(), 10))
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "tgw-rotation-secret", "--type=merge", "-p", secretPatch)

			retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "terminating-gateway", nil)
				require.NoError(r, err)

				tgEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
				require.True(r, ok)

				secondRotation := tgEntry.Meta[metaKey]
				require.NotEmpty(r, secondRotation)
				require.NotEqual(r, firstRotation, secondRotation)
				_, err = time.Parse(time.RFC3339, secondRotation)
				require.NoError(r, err)
			})
		})
	}
}
