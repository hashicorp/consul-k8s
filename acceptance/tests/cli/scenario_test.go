package cli

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
)

// TestInstallAfterFailedInstall exercises the install command after a failed
// install. This scenario tests this issue: https://github.com/hashicorp/consul-k8s/issues/1005
func TestInstallAfterFailedInstall(t *testing.T) {
	t.Skip("This is stuck on a pending install for now. I'd like to get it failing.")

	// Install Consul in a way that will fail.
	{
		// This config will work until we implement some precheck for valid CA Certs.
		helmValues := map[string]string{
			"server.replicas":              "1",
			"global.tls.enabled":           "true",
			"global.tls.caCert.secretName": "asdf",
			"global.tls.caCert.secretKey":  "asdf",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		cluster.Create(t)
	}

	// Try to install Consul again.
	{
		helmValues := map[string]string{
			"server.replicas": "1",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		cluster.Create(t)
	}
}

// TestUpgradeAfterFailedInstall exercises the upgrade command after a failed
// install. This scenario tests this issue: https://github.com/hashicorp/consul-k8s/issues/1005
func TestUpgradeAfterFailedInstall(t *testing.T) {
	t.Skip("This is stuck on a pending install for now. I'd like to get it failing.")

	// Install Consul in a way that will fail.
	{
		// This config will work until we implement some precheck for valid CA Certs.
		helmValues := map[string]string{
			"server.replicas":              "1",
			"global.tls.enabled":           "true",
			"global.tls.caCert.secretName": "asdf",
			"global.tls.caCert.secretKey":  "asdf",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		cluster.Create(t)

		// Try to upgrade Consul.
		helmValues = map[string]string{
			"server.replicas": "1",
		}
		cluster.Upgrade(t, helmValues)
	}
}

// TestReinstallingRecreatesCRDs tests the scenario where a user installs Consul
// on Kubernetes with CRDs enabled, deletes the installation, then installs
// Consul again with CRDs enabled. This scenario was created to address this
// issue: https://github.com/hashicorp/consul-k8s/issues/1062
func TestReinstallingRecreatesCRDs(t *testing.T) {
	t.Skip()

	// Install Consul with the Controller enabled, then delete it.
	{
		helmValues := map[string]string{
			"global.enabled":     "false",
			"server.replicas":    "1",
			"controller.enabled": "true",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()
		cfg.NoCleanupOnFailure = true

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		t.Log("Installing Consul...")
		cluster.Create(t)

		// Delete the Consul cluster.
		t.Log("Deleting Consul...")
		cluster.Destroy(t)
	}

	// Install Consul with the Controller enabled, then delete it.
	{
		helmValues := map[string]string{
			"global.enabled":     "false",
			"server.replicas":    "1",
			"controller.enabled": "true",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()
		cfg.NoCleanupOnFailure = true

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		t.Log("Installing Consul...")
		cluster.Create(t)
	}
}
