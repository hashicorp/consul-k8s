package controller

import (
	"os"
	"testing"

	testSuite "github.com/hashicorp/consul-helm/test/acceptance/framework/suite"
)

var suite testSuite.Suite

func TestMain(m *testing.M) {
	suite = testSuite.NewSuite(m)
	os.Exit(suite.Run())
}
