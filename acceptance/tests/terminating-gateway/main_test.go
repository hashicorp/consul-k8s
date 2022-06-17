package terminatinggateway

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	fmt.Println("Skipping terminating gateway tests because it's not supported with agentless yet")
	os.Exit(0)
	//suite = testsuite.NewSuite(m)
	//os.Exit(suite.Run())
}
