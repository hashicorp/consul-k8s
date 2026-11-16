// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package customapigateway

import (
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
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
	consulGatewayResource        = "cgateways.consul.hashicorp.com"
	consulHTTPRouteResource      = "chttproutes.consul.hashicorp.com"
	consulTCPRouteResource       = "ctcproutes.consul.hashicorp.com"
)

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}

// withOpenShiftCRDs enables the consul.hashicorp.com gateway CRDs, which the chart
// only renders on OpenShift when global.openshift.crds.consulapi.enabled is set.
func withOpenShiftCRDs(cfg *config.TestConfig, values map[string]string) map[string]string {
	if cfg.EnableOpenshift || cfg.UseOpenshift {
		values["global.openshift.crds.consulapi.enabled"] = "true"
	}
	return values
}
