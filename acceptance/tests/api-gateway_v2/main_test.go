// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigatewayv2

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	runTests := os.Getenv("TEST_APIGW_V2")
	if runTests != "TRUE" {
		fmt.Println("skipping")
		os.Exit(0)
	}
	suite = testsuite.NewSuite(m)
	os.Exit(suite.Run())
}
