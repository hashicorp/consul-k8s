package meshgateway

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
)

var suite framework.Suite

func TestMain(m *testing.M) {
	suite = framework.NewSuite(m)

	if suite.Config().EnableMultiCluster {
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping mesh gateway tests because -enable-multi-cluster is not set")
		os.Exit(0)
	}
}
