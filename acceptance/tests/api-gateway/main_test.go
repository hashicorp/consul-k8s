// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

const (
	gatewayGatewayResource   = "gateways.gateway.networking.k8s.io"
	gatewayHTTPRouteResource = "httproutes.gateway.networking.k8s.io"
)

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}
