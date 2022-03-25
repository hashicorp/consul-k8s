package cli

import (
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
)

// TestUpgradeAfterFailedInstall exercises the upgrade command after a failed
// install. This scenario tests this issue: https://github.com/hashicorp/consul-k8s/issues/1005
func TestUpgradeAfterFailedInstall(t *testing.T) {
	helmValues := map[string]string{
		"server.replicas": "1",
	}
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
	cluster.Create(t, "-timeout", "1s")

	// Try to upgrade Consul.
	cluster.Upgrade(t, helmValues)
}

// TestUpgradeInNonConsulNamespace exercises the scenario where Consul is
// installed in a namespace other than "consul". The upgrade command should
// find the Consul cluster in the namespace and upgrade it.
func TestUpgradeInNonConsulNamespace(t *testing.T) {
	helmValues := map[string]string{
		"server.replicas": "1",
	}
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()
	cfg.KubeNamespace = "default"

	cluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
	cluster.Create(t)

	// Try to upgrade Consul.
	cluster.Upgrade(t, helmValues)
}
