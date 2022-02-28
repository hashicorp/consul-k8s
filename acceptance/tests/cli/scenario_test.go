package cli

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
)

// TestInstallAfterFailedInstall tests the scenario where a user installs Consul
// on Kubernetes, that install fails, then the user attempts a second install
// which should succeed. This scenario was created to address this issue:
// https://github.com/hashicorp/consul-k8s/issues/1005
func TestInstallAfterFailedInstall(t *testing.T) {
	// Install Consul with the Controller enabled.
	{
		helmValues := map[string]string{
			"server.replicas":    "1",
			"controller.enabled": "true",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		cluster.Create(t)

		// Create an arbitrary service default resource in the cluster.
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "apply", "-k", "../fixtures/bases/service-default")
		helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
			// Ignore errors here because if the test ran as expected
			// the custom resources will have been deleted.
			k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "-k", "../fixtures/bases/service-default")
		})

		// Delete the Consul cluster.
		cluster.Destroy(t)

		// Delete the service defaults CRD
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "servicedefaults.consul.hashicorp.com", "defaults")
		k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "delete", "crds", "servicedefaults.consul.hashicorp.com")
	}

	// Attempt to install Consul using the CLI without the Controller enabled.
	// This install should fail.
	{
		helmValues := map[string]string{
			"server.replicas":    "1",
			"controller.enabled": "false",
		}
		ctx := suite.Environment().DefaultContext(t)
		cfg := suite.Config()

		cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
		cluster.Create(t)
	}
}

// TestReinstallingRecreatesCRDs tests the scenario where a user installs Consul
// on Kubernetes with CRDs enabled, deletes the installation, then installs
// Consul again with CRDs enabled. This scenario was created to address this
// issue: https://github.com/hashicorp/consul-k8s/issues/1062
func TestReinstallingRecreatesCRDs(t *testing.T) {
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
