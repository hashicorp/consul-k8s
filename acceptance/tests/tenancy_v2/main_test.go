// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tenancy_v2

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	expectedNumberOfClusters := 1
	if suite.Config().IsExpectedClusterCount(expectedNumberOfClusters) {
		os.Exit(suite.Run())
	} else {
		fmt.Printf(
			"Skipping tenancy_v2 tests because the number of clusters, %d, did not match the expected count of %d\n",
			len(suite.Config().KubeEnvs),
			expectedNumberOfClusters,
		)
		os.Exit(0)
	}
}
