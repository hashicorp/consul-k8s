// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

// TestMain for peering is DISABLED for 0.49.
func TestMain(m *testing.M) {

	fmt.Println("Skipping peering tests because this is a beta feature and not fully supported")
	os.Exit(0)

	//expectedNumberOfClusters := 2
	//if suite.Config().EnableMultiCluster && suite.Config().IsExpectedClusterCount(expectedNumberOfClusters) && !suite.Config().DisablePeering {
	//	os.Exit(suite.Run())
	//} else {
	//	fmt.Println(fmt.Sprintf("Skipping peerings tests because either -enable-multi-cluster is "+
	//		"not set, -disable-peering is set, or the number of clusters did not match the expected count of %d", expectedNumberOfClusters))
	//	os.Exit(0)
	//}
}
