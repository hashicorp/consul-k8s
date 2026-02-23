// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package openshift

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	cfg := suite.Config()
	if cfg.UseOpenshift {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping openshift tests because use-openshift not set")
		os.Exit(0)
	}
}
