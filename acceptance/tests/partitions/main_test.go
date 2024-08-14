// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package partitions

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	expectedNumberOfClusters := 2
	if suite.Config().EnableMultiCluster && suite.Config().IsExpectedClusterCount(expectedNumberOfClusters) {
		os.Exit(suite.Run())
	} else {
		fmt.Println(fmt.Sprintf("Skipping partitions tests because either -enable-multi-cluster is "+
			"not set or the number of clusters, %d, did not match the expected count of %d", len(suite.Config().KubeEnvs), expectedNumberOfClusters))
		os.Exit(0)
	}
}
