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
	t.Log("Installing Consul in a way that will fail")
	cluster.Create(t, "-timeout", "1s")

	// Try to upgrade Consul.
	t.Log("Attempting to upgrade Consul")
	cluster.Upgrade(t, helmValues)
	t.Log("Done")
}
