// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
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
	consulGatewayResource        = "gateways.consul.hashicorp.com"
	consulHTTPRouteResource      = "httproutes.consul.hashicorp.com"
	consulTCPRouteResource       = "tcproutes.consul.hashicorp.com"
)

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}
