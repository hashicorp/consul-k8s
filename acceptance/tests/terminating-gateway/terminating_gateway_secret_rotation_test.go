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
				"connectInject.enabled":                             "true",
				"terminatingGateways.enabled":                       "true",
				"terminatingGateways.gateways[0].name":              "tg",
				"terminatingGateways.gateways[0].replicas":          "1",
				"terminatingGateways.defaults.extraVolumes[0].type": "secret",
				"terminatingGateways.defaults.extraVolumes[0].name": "tg-client-tls",
				"global.acls.manageSystemACLs":                      strconv.FormatBool(tc.secure),
				"global.tls.enabled":                                strconv.FormatBool(tc.secure),
			}

			logger.Log(t, "applying referenced gateway secret")
			k8s.KubectlApply(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/secret.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDelete(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/secret.yaml")
			})

			logger.Log(t, "creating consul cluster")
			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, tc.secure)

			logger.Log(t, "applying terminating gateway fixture")
			k8s.KubectlApply(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/terminating-gateway.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDelete(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/terminating-gateway.yaml")
			})
			sleepTime := 10 * time.Second
			time.Sleep(sleepTime)

			logger.Log(t, "applying unrelated secret")
			k8s.KubectlApply(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/secret-unrelated.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.KubectlDelete(t, ctx.KubectlOptions(t), "../fixtures/cases/terminating-gateway-secret-rotation/secret-unrelated.yaml")
			})

			time.Sleep(sleepTime)

			logger.Log(t, "patching referenced secret to trigger gateway reconcile")
			secretPatch := fmt.Sprintf(`{"metadata":{"annotations":{"rotation-trigger":"%s"}}}`, strconv.FormatInt(time.Now().UnixNano(), 10))
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "tg-client-tls", "--type=merge", "-p", secretPatch)

			metaKey := "consul.hashicorp.com/secret/tg-client-tls/last-rotation"

			var firstRotation string
			retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "tg", nil)
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
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "tg", nil)
				require.NoError(t, err)

				tgEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
				require.True(t, ok)

				current := tgEntry.Meta[metaKey]
				require.Equal(t, firstRotation, current)
			}

			logger.Log(t, "patching referenced secret to trigger gateway reconcile")
			secretPatch = fmt.Sprintf(`{"metadata":{"annotations":{"rotation-trigger":"%s"}}}`, strconv.FormatInt(time.Now().UnixNano(), 10))
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "patch", "secret", "tg-client-tls", "--type=merge", "-p", secretPatch)

			retry.RunWith(&retry.Counter{Wait: 5 * time.Second, Count: 60}, t, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, "tg", nil)
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
