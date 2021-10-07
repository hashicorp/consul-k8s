package cli

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/tests/connect"
)

// TestCLIConnectInject is a smoke test that the CLI works with Helm hooks. It sets the
// connect.ConnectInjectConnectivityCheck cli flag to true, causing the Create() and Destroy() methods to use the CLI
// for installation/uninstallation. The connect.ConnectInjectConnectivityCheck test leverages secure mode which will
// enable ACLs and TLS, which are set up via Helm hooks. This allows us to verify that core service mesh functionality
// with non-trivial Helm settings are set up appropriately with the CLI.
func TestCLIConnectInject(t *testing.T) {
	cases := []struct {
		secure      bool
		autoEncrypt bool
	}{
		{false, false},
		{true, false},
		{true, true},
	}

	for _, c := range cases {
		name := fmt.Sprintf("secure: %t; auto-encrypt: %t", c.secure, c.autoEncrypt)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			connect.ConnectInjectConnectivityCheck(t, ctx, cfg, c.secure, c.autoEncrypt, true)

		})
	}
}
