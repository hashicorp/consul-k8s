package peering

import (
	"fmt"
	"os"
	"testing"

	testsuite "github.com/hashicorp/consul-k8s/acceptance/framework/suite"
)

var suite testsuite.Suite

// TestMain for peering is DISABLED for 0.49.
func TestMain(m *testing.M) {

	fmt.Println("Skipping peering tests because this is a beta feature and not fully supported")
	os.Exit(0)

	//suite = testsuite.NewSuite(m)
	//
	//if suite.Config().EnableMultiCluster && !suite.Config().DisablePeering {
	//	os.Exit(suite.Run())
	//} else {
	//	fmt.Println("Skipping peering tests because either -enable-multi-cluster is not set or -disable-peering is set")
	//	os.Exit(0)
	//}
}
