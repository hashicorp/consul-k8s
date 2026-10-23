// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package vault

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
	cfg := suite.Config()
	// TestVault and TestVault_TLSAutoReload need one cluster, and the multi-cluster tests skip
	// themselves without -enable-multi-cluster, so OpenShift can run this package on one cluster.
	if (cfg.EnableMultiCluster && cfg.IsExpectedClusterCount(expectedNumberOfClusters)) || cfg.EnableOpenshift || cfg.UseOpenshift {
		os.Exit(suite.Run())
	} else {
		fmt.Println(fmt.Sprintf("Skipping vault tests because either -enable-multi-cluster is "+
			"not set or the number of clusters did not match the expected count of %d", expectedNumberOfClusters))
		os.Exit(0)
	}
}
