// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"fmt"
	"path"
	"strconv"
	"testing"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
)

func TestAPIGateway_Tenancy(t *testing.T) {
	cases := []struct {
		secure             bool
		namespaceMirroring bool
	}{
		{
			secure:             false,
			namespaceMirroring: false,
		},
		{
			secure:             true,
			namespaceMirroring: false,
		},
		{
			secure:             false,
			namespaceMirroring: true,
		},
		{
			secure:             true,
			namespaceMirroring: true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, namespaces: %t", c.secure, c.namespaceMirroring)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()

			if !cfg.EnableEnterprise && c.namespaceMirroring {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.enableConsulNamespaces":               strconv.FormatBool(c.namespaceMirroring),
				"global.acls.manageSystemACLs":                strconv.FormatBool(c.secure),
				"global.tls.enabled":                          strconv.FormatBool(c.secure),
				"global.logLevel":                             "trace",
				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": strconv.FormatBool(c.namespaceMirroring),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			serviceNamespace, serviceK8SOptions := createNamespace(t, ctx, cfg)
			certificateNamespace, certificateK8SOptions := createNamespace(t, ctx, cfg)
			gatewayNamespace, gatewayK8SOptions := createNamespace(t, ctx, cfg)
			routeNamespace, routeK8SOptions := createNamespace(t, ctx, cfg)

			logger.Logf(t, "creating target server in %s namespace", serviceNamespace)
			k8s.DeployKustomize(t, serviceK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Logf(t, "creating certificate resources in %s namespace", certificateNamespace)
			applyFixture(t, cfg, certificateK8SOptions, "cases/api-gateways/certificate")

			logger.Logf(t, "creating gateway in %s namespace", gatewayNamespace)
			applyFixture(t, cfg, gatewayK8SOptions, "cases/bases/api-gateway")

			logger.Logf(t, "creating route resources in %s namespace", routeNamespace)
			applyFixture(t, cfg, routeK8SOptions, "cases/api-gateways/httproute")
		})
	}
}

func applyFixture(t *testing.T, cfg *config.TestConfig, k8sOptions *terratestk8s.KubectlOptions, fixture string) {
	t.Helper()

	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", path.Join("../fixtures", fixture))
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", path.Join("../fixtures", fixture))
	})
}

func createNamespace(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig) (string, *terratestk8s.KubectlOptions) {
	t.Helper()

	namespace := helpers.RandomName()

	logger.Logf(t, "creating Kubernetes namespace %s", namespace)
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", namespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", namespace)
	})

	return namespace, &terratestk8s.KubectlOptions{
		ContextName: ctx.KubectlOptions(t).ContextName,
		ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
		Namespace:   namespace,
	}
}
