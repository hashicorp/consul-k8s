package controller

import (
	"fmt"
	"os"
	"testing"

	testSuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testSuite.Suite

func TestMain(m *testing.M) {
	fmt.Println("Skipping controller tests because it's not supported with agentless yet")
	os.Exit(0)
	//suite = testSuite.NewSuite(m)
	//os.Exit(suite.Run())
}
