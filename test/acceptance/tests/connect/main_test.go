package connect

import (
	"os"
	"testing"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
)

var suite framework.Suite

func TestMain(m *testing.M) {
	suite = framework.NewSuite(m)
	os.Exit(suite.Run())
}
