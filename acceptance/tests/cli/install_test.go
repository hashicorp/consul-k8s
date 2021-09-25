package cli

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/tests/connect"
)

const staticClientName = "static-client"
const staticServerName = "static-server"

// Test that Connect works in a default and a secure installation.
func TestConnectInject(t *testing.T) {
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

			connect.ConnectInject(t, ctx, cfg, c.secure, c.autoEncrypt, true)

		})
	}
}
