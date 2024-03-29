// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Rename package to your test package.
// NOTE: Remember to add your test package to acceptance/ci-inputs so it gets run in CI.
package example

import (
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	// First, uncomment the line below to create a new suite so that all flags are parsed.
	/*
		suite = framework.NewSuite(m)
	*/

	// If the test suite needs to run only when certain test flags are passed,
	// you need to handle that in the TestMain function.
	// Uncomment and modify example code below if that is the case.
	/*
		if suite.Config().EnableExampleFeature {
			os.Exit(suite.Run())
		} else {
			fmt.Println("Skipping example feature tests because -enable-example-feature is not set")
			os.Exit(0)
		}
	*/

	// If the test suite should run in every case, uncomment the line below.
	/*
		os.Exit(suite.Run())
	*/
}
