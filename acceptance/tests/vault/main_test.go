package vault

import (
	"fmt"
	"os"
	"testing"
)

//var suite testsuite.Suite

func TestMain(m *testing.M) {
	fmt.Printf("Vault Agent Injector does not currently support Kubernetes-1.25 PSA, skipping.\n")
	os.Exit(0)
	//suite = testsuite.NewSuite(m)
	//os.Exit(suite.Run())
}
