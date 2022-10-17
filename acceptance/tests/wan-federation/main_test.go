package wanfederation

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	// todo(agentless): Re-enable tproxy tests once we support it for multi-cluster.
	if suite.Config().EnableMultiCluster && !suite.Config().EnableTransparentProxy {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping wan federation tests because -enable-multi-cluster is not set")
		os.Exit(0)
	}
}
