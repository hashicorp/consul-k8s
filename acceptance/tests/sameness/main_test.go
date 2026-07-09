// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package sameness

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	expectedNumberOfClusters := 4
	cfg := suite.Config()
	supportedClusterType := cfg.UseKind || cfg.UseOpenshift || cfg.EnableOpenshift

	if cfg.EnableMultiCluster && cfg.IsExpectedClusterCount(expectedNumberOfClusters) && supportedClusterType {
		os.Exit(suite.Run())
	} else {
		fmt.Println(fmt.Sprintf("Skipping sameness tests because either -enable-multi-cluster is "+
			"not set, the number of clusters did not match the expected count of %d, or neither Kind nor OpenShift is enabled.", expectedNumberOfClusters))
	}
}
