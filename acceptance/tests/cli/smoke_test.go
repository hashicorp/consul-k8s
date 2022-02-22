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
				Secure:           c.secure,
				AutoEncrypt:      c.autoEncrypt,
				ReleaseName:      consul.CLIReleaseName,
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
	type TestCase struct {
		secure      bool
		autoEncrypt bool
		helmValues  map[string]string
	}

	cases := map[string]struct {
		initialState  TestCase
		upgradedState TestCase
	}{
		"Upgrade changes nothing": {
			initialState:  TestCase{},
			upgradedState: TestCase{},
		},
		// "Upgrade to secure": {
		// 	initialState: TestCase{
		// 		secure: false,
		// 	},
		// 	upgradedState: TestCase{
		// 		secure: true,
		// 	},
		// },
		// "Upgrade to auto-encrypt": {
		// 	initialState: TestCase{
		// 		autoEncrypt: false,
		// 	},
		// 	upgradedState: TestCase{
		// 		autoEncrypt: true,
		// 	},
		// },
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()
			ctx := suite.Environment().DefaultContext(t)

			conCheck := connect.ConnectHelper{
				ClusterGenerator:     consul.NewCLICluster,
				AdditionalHelmValues: c.initialState.helmValues,
				Secure:               c.initialState.secure,
				ReleaseName:          consul.CLIReleaseName,
				T:                    t,
				Ctx:                  ctx,
				Cfg:                  cfg,
			}

			conCheck.InstallThenCheckConnectInjection()

			conCheck.AdditionalHelmValues = c.upgradedState.helmValues
			conCheck.Secure = c.upgradedState.secure
			conCheck.UpgradeThenCheckConnectInjection()
		})
	}
}
