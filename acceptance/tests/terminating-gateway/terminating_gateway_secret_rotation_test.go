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

func TestTerminatingGatewaySecretRotation(t *testing.T) {
	cases := []struct {
		secure bool
	}{
		{secure: false},
		{secure: true},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("secure: %t", tc.secure), func(t *testing.T) {
			ctx := suite.Environment().DefaultContext(t)
			cfg := suite.Config()

			logger.Log(t, "preinstalling initial secret for terminating gateway")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/terminating-gateway-secret-rotation/secret-initial.yaml")
			helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
				k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "-f", "../fixtures/cases/terminating-gateway-secret-rotation/secret-initial.yaml")
			})

			helmValues := map[string]string{
				"connectInject.enabled":                             "true",
				"terminatingGateways.enabled":                       "true",
				"terminatingGateways.gateways[0].name":              "tg",
				"terminatingGateways.gateways[0].replicas":          "1",
				"terminatingGateways.defaults.extraVolumes[0].type": "secret",
				"terminatingGateways.defaults.extraVolumes[0].name": "tgw-rotation-secret",
				"global.image":                        "hashicorp/consul-enterprise:local",
				"global.imageK8S":                     "consul-k8s-control-plane:local",
				"global.enterpriseLicense.secretName": "consul-hcl",
				"global.enterpriseLicense.secretKey":  "consul.hclic",
				//"global.imageConsulDataplane":  "consul-dataplane:local",
				"global.acls.manageSystemACLs": strconv.FormatBool(tc.secure),
				"global.tls.enabled":           strconv.FormatBool(tc.secure),
			}

			logger.Log(t, "creating consul cluster")
			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)
			consulCluster.Create(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, tc.secure)

			logger.Log(t, "deploying static server and external registration")
			k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/bases/static-server")
			k8sOpts := helpers.K8sOptions{
				Options:             ctx.KubectlOptions(t),
				NoCleanupOnFailure:  cfg.NoCleanupOnFailure,
				NoCleanup:           cfg.NoCleanup,
				KustomizeConfigPath: "../fixtures/bases/external-service-registration",
			}
			consulOpts := helpers.ConsulOptions{
				ConsulClient:                    consulClient,
				ExternalServiceNameRegistration: "static-server-registration",
			}
			helpers.RegisterExternalServiceCRD(t, k8sOpts, consulOpts)

			logger.Log(t, "creating terminating gateway with linked secretRef")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/terminating-gateway-secret-rotation/terminating-gateway.yaml")

			retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
				waitForTerminatingGatewayEntry(r, consulClient, "tg")
			})

			logger.Log(t, "rotating terminating gateway secret first")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/terminating-gateway-secret-rotation/secret-rotated.yaml")

			time.Sleep(5 * time.Second)

			metaKey := fmt.Sprintf("consul.hashicorp.com/secret/%s/last-rotation", "tgw-rotation-secret")
			var firstRotation string
			retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
				firstRotation = readRotationTimestamp(r, consulClient, "tg", metaKey)
			})

			logger.Log(t, "rotating terminating gateway secret second time")
			k8s.RunKubectl(t, ctx.KubectlOptions(t), "apply", "-f", "../fixtures/cases/terminating-gateway-secret-rotation/secret-rotated-1.yaml")

			time.Sleep(5 * time.Second)

			var secondRotation string
			retry.RunWith(&retry.Counter{Wait: 2 * time.Second, Count: 60}, t, func(r *retry.R) {
				secondRotation = readRotationTimestamp(r, consulClient, "tg", metaKey)
				require.NotEqual(r, firstRotation, secondRotation)
			})
		})
	}
}

func readRotationTimestamp(t require.TestingT, consulClient *api.Client, gatewayName, metaKey string) string {
	entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, gatewayName, nil)
	require.NoError(t, err)

	tgEntry, ok := entry.(*api.TerminatingGatewayConfigEntry)
	require.True(t, ok, "could not cast to TerminatingGatewayConfigEntry")
	require.NotNil(t, tgEntry.Meta)

	rotation := tgEntry.Meta[metaKey]
	require.NotEmpty(t, rotation)

	_, parseErr := time.Parse(time.RFC3339Nano, rotation)
	require.NoError(t, parseErr)

	return rotation
}

func waitForTerminatingGatewayEntry(t require.TestingT, consulClient *api.Client, gatewayName string) {
	entry, _, err := consulClient.ConfigEntries().Get(api.TerminatingGateway, gatewayName, nil)
	require.NoError(t, err)

	_, ok := entry.(*api.TerminatingGatewayConfigEntry)
	require.True(t, ok, "could not cast to TerminatingGatewayConfigEntry")
}
