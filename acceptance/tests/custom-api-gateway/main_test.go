// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var suite testsuite.Suite

const (
	customGatewayFixturesDir     = "../fixtures/bases/custom-api-gateway"
	customGatewayCertificatePath = customGatewayFixturesDir + "/certificate.yaml"
	customGatewayName            = "custom-gateway"
	customGatewayClassName       = "custom-gateway-class"
	customGatewayClassConfigName = "gateway-class-config"
	customGatewayHTTPRouteName   = "custom-http-route"
	customGatewayCertificateName = "certificate"
	consulGatewayResource        = "cgateways.consul.hashicorp.com"
	consulHTTPRouteResource      = "chttproutes.consul.hashicorp.com"
	consulTCPRouteResource       = "ctcproutes.consul.hashicorp.com"
)

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	cfg := suite.Config()
	if cfg.EnableOpenshift || cfg.UseOpenshift {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping custom API gateway tests because -enable-openshift and -use-openshift are not set")
		os.Exit(0)
	}
}

func waitForResource(t *testing.T, kubernetesClient client.Client, key client.ObjectKey, object client.Object) {
	t.Helper()
	retry.RunWith(&retry.Counter{Count: 15, Wait: 2 * time.Second}, t, func(r *retry.R) {
		require.NoError(r, kubernetesClient.Get(context.Background(), key, object))
	})
}

func waitForGatewayClassConfig(t *testing.T, kubernetesClient client.Client, name string) {
	t.Helper()
	waitForResource(t, kubernetesClient, client.ObjectKey{Name: name}, &v1alpha1.GatewayClassConfig{})
}
