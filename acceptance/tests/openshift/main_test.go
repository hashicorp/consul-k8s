package openshift

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

func TestMain(m *testing.M) {
	suite = testsuite.NewSuite(m)

	cfg := suite.Config()
	if cfg.UseOpenshift {
		fmt.Println("openshift tests started")
		os.Exit(suite.Run())
	} else {
		fmt.Println("Skipping openshift tests because use-openshift not set")
		os.Exit(0)
	}
}
