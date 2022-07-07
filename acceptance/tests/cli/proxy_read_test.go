package cli

import (
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/stretchr/testify/require"
)

func TestProxyRead(t *testing.T) {
	// These regular expressions must be present in the output table.
	expected := []string{
		"Envoy configuration for static-client-[a-z0-9]{10}-[a-z0-9]{5} in Namespace default:",
		"==> Clusters \\(3\\)",
		"Name.*FQDN.*Endpoints.*Type.*Last Updated",
		"local_agent.*local_agent.*STATIC",
		"local_app.*local_app.*127\\.0\\.0\\.1:0.*STATIC",
		"static-server.*static-server\\.default\\.dc1\\.internal\\.[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}\\.consul.*EDS",
		"==> Endpoints (3)",
		// "Address:Port    	Cluster    	Weight	Status",
		// "172.18.0.2:8502 	local_agent	1.00  	HEALTHY",
		// "127.0.0.1:0     	local_app  	1.00  	HEALTHY",
		// "10.244.0.6:20000	           	1.00  	HEALTHY",
		// "==> Listeners (2)",
		// "Name           	Address:Port    	Direction	Filter Chain Match	Filters                                                                          	Last Updated",
		// "public_listener	10.244.0.5:20000	INBOUND  	Any               	                                                                                 	2022-07-07T15:17:46.681Z",
		// "static-server  	127.0.0.1:1234  	OUTBOUND 	Any               	-> static-server.default.dc1.internal.64fcd045-12fe-f48a-fc08-1c29b9c98966.consul	2022-07-07T15:17:46.683Z",
		// "==> Routes (0)",
		// "Name	Destination Cluster	Last Updated",
		// "==> Secrets (0)",
		// "Name	Type	Last Updated",
	}

	// Install Consul in the cluster.
	helmValues := map[string]string{
		"controller.enabled":    "true",
		"connectInject.enabled": "true",
		"global.tls.enabled":    "true",
	}
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, "consul")
	consulCluster.Create(t)

	// Deploy Pods into the Cluster.
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	// Get the name of the Static Client pod.
	output, err := k8s.RunKubectlAndGetOutputE(t, ctx.KubectlOptions(t), "get", "pods", "--selector=app=static-client")
	require.NoError(t, err)
	outputRows := strings.Split(strings.TrimSpace(output), "\n")
	staticClientName := strings.Fields(outputRows[len(outputRows)-1])[0]

	// Call consul-k8s proxy read <staticClientName>.
	actual, err := cli.RunCmd("proxy", "read", staticClientName)
	require.NoError(t, err)

	for _, expression := range expected {
		require.Regexp(t, expression, string(actual))
	}
}
