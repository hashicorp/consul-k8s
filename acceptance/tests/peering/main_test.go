package peering

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	if suite.Config().EnableMultiCluster && !suite.Config().DisablePeering {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping peering tests because either -enable-multi-cluster is not set or -disable-peering is set")
		os.Exit(0)
	}
}
