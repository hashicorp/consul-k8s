// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package cli

import (
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/connhelper"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestConnectInjectOnUpgrade tests that Connect works before and after an
// upgrade is performed on the cluster.
func TestUpgrade(t *testing.T) {
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)

	connHelper := connhelper.ConnectHelper{
		ClusterKind: consul.CLI,
		ReleaseName: consul.CLIReleaseName,
		Ctx:         ctx,
		Cfg:         cfg,
	}

	connHelper.Setup(t)

	connHelper.Install(t)

	// Change a value on the connect-injector to force an update.
	connHelper.HelmValues = map[string]string{
		"ingressGateways.enabled":           "true",
		"ingressGateways.defaults.replicas": "1",
	}

	connHelper.Upgrade(t)

	t.Log("checking that the ingress gateway was install as a result of the upgrade")
	k8sClient := ctx.KubernetesClient(t)
	igwPods, err := k8sClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{LabelSelector: "component=ingress-gateway"})
	require.NoError(t, err)
	require.Len(t, igwPods.Items, 1)
}
