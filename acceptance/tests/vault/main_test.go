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
	if suite.Config().UseKind {
		fmt.Println("Skipping vault tests because they are currently flaky")
		os.Exit(0)
	}
	os.Exit(suite.Run())
}
