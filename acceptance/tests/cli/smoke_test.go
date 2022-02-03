package cli

import (
	"fmt"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
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

			helper := connect.ConnectHelper{
				ClusterGenerator: consul.NewCLICluster,
				ReleaseName:      consul.CLIReleaseName,
				Secure:           c.secure,
				AutoEncrypt:      c.autoEncrypt,
				T:                t,
				Ctx:              ctx,
				Cfg:              cfg,
			}

			helper.InstallThenCheckConnectInjection()
		})
	}
}

// TestUpgrade is a smoke test that the CLI handles upgrades correctly.
// It sets an initial set of Helm override values with `installation`, installs the chart using the CLI,
// then upgrades the chart with the `upgrade` Helm overrides and verifies that the upgrade was successful.
// Then the installed chart is uninstalled.
func TestCLIConnectInjectOnUpgrade(t *testing.T) {
	cases := map[string]struct {
		installation map[string]string
		upgrade      map[string]string
		secure       bool
		autoEncrypt  bool
	}{
		"Upgrade changes nothing": {
			installation: map[string]string{},
			upgrade:      map[string]string{},
			secure:       false,
			autoEncrypt:  false,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			conCheck := connect.ConnectHelper{
				ClusterGenerator: consul.NewCLICluster,
				ReleaseName:      consul.CLIReleaseName,
				Secure:           c.secure,
				AutoEncrypt:      c.autoEncrypt,
				T:                t,
				Ctx:              ctx,
				Cfg:              cfg,
			}

			conCheck.InstallThenCheckConnectInjection()
			// TODO upgrade
		})
	}
}
