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

	if suite.Config().EnableMultiCluster {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping peering tests because -enable-multi-cluster is not set")
		os.Exit(0)
	}
}
