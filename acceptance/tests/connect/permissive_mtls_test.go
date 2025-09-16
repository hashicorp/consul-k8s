// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package connect

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInject_PermissiveMTLS verifies that permissive mTLS works as expected when enabled in the MeshConfig.
func TestConnectInject_PermissiveMTLS(t *testing.T) {
	cfg := suite.Config()
	if !cfg.EnableTransparentProxy {
		t.Skipf("skipping this because -enable-transparent-proxy is not set")
	}
	cfg.SkipWhenOpenshiftAndCNI(t)

	ctx := suite.Environment().DefaultContext(t)

	releaseName := helpers.RandomName()
	connHelper := connhelper.ConnectHelper{
		ClusterKind: consul.Helm,
		Secure:      true,
		ReleaseName: releaseName,
		Ctx:         ctx,
		Cfg:         cfg,
	}
	connHelper.Setup(t)
	connHelper.Install(t)

	deployNonMeshClient(t, connHelper)
	deployStaticServer(t, cfg, connHelper)

	kubectlOpts := connHelper.Ctx.KubectlOptions(t)
	logger.Logf(t, "Check that incoming non-mTLS connection fails in MutualTLSMode = strict")
	k8s.CheckStaticServerConnectionFailing(t, kubectlOpts, "static-client", "http://static-server")

	logger.Log(t, "Set allowEnablingPermissiveMutualTLS = true")
	writeCrd(t, connHelper, "../fixtures/cases/permissive-mtls/mesh-config-permissive-allowed.yaml")

	logger.Log(t, "Set mutualTLSMode = permissive for static-server")
	writeCrd(t, connHelper, "../fixtures/cases/permissive-mtls/service-defaults-static-server-permissive.yaml")

	logger.Log(t, "Check that incoming mTLS connection is successful in MutualTLSMode = permissive")
	k8s.CheckStaticServerConnectionSuccessful(t, kubectlOpts, "static-client", "http://static-server")
}

func deployNonMeshClient(t *testing.T, ch connhelper.ConnectHelper) {
	t.Helper()

	logger.Log(t, "Creating static-client deployment with connect-inject=false")
	k8s.DeployKustomize(t, ch.Ctx.KubectlOptions(t), ch.Cfg.NoCleanupOnFailure, ch.Cfg.NoCleanup, ch.Cfg.DebugDirectory, "../fixtures/bases/static-client")
	requirePodContainers(t, ch, "app=static-client", 1)
}

func deployStaticServer(t *testing.T, cfg *config.TestConfig, ch connhelper.ConnectHelper) {
	t.Helper()

	logger.Log(t, "Creating static-server deployment with connect-inject=true")
	k8s.DeployKustomize(t, ch.Ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
	requirePodContainers(t, ch, "app=static-server", 2)
}

func writeCrd(t *testing.T, ch connhelper.ConnectHelper, path string) {
	t.Helper()

	t.Cleanup(func() {
		_, _ = k8s.RunKubectlAndGetOutputE(t, ch.Ctx.KubectlOptions(t), "delete", "-f", path)
	})

	_, err := k8s.RunKubectlAndGetOutputE(t, ch.Ctx.KubectlOptions(t), "apply", "-f", path)
	require.NoError(t, err)
}

func requirePodContainers(t *testing.T, ch connhelper.ConnectHelper, selector string, nContainers int) {
	t.Helper()

	opts := ch.Ctx.KubectlOptions(t)
	client := ch.Ctx.KubernetesClient(t)
	retry.Run(t, func(r *retry.R) {
		podList, err := client.CoreV1().
			Pods(opts.Namespace).
			List(context.Background(), metav1.ListOptions{LabelSelector: selector})
		require.NoError(r, err)
		require.Len(r, podList.Items, 1)
		require.Len(r, podList.Items[0].Spec.Containers, nContainers)
	})
}
