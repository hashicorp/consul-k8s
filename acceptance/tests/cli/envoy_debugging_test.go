package cli_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/serf/testutil/retry"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ipv4RegEx = "(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)"

func TestEnvoyDebugging(t *testing.T) {
	ctx := suite.Environment().DefaultContext(t)
	cfg := suite.Config()

	helmValues := map[string]string{
		"connectInject.enabled": "true",
	}

	cliConsulCluster := consul.NewCLICluster(t, helmValues, ctx, cfg, "consul")
	cli := cliConsulCluster.CLI()

	// Install Consul to the Kubernetes cluster
	cliConsulCluster.Create(t)

	// Install a client and server
	{
		logger.Log(t, "creating static-server and static-client deployments")
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")
		k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")

		// Check that both static-server and static-client have been injected and now have 2 containers.
		for _, labelSelector := range []string{"app=static-server", "app=static-client"} {
			podList, err := ctx.KubernetesClient(t).CoreV1().Pods(ctx.KubectlOptions(t).Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: labelSelector,
			})
			require.NoError(t, err)
			require.Len(t, podList.Items, 1)
			require.Len(t, podList.Items[0].Spec.Containers, 2)
		}
	}

	// Run proxy list and get the two results
	listOut, err := cli.Run("proxy", "list")
	require.NoError(t, err)
	logger.Log(t, string(listOut))

	list := translateListOutput(listOut)
	require.Equal(t, 2, len(list))
	for _, proxyType := range list {
		require.Equal(t, "Sidecar", proxyType)
	}

	// Run proxy read for each Pod and store the output
	outputs := make(map[string]string)
	for podName := range list {
		output, err := cli.Run("proxy", "read", podName)
		require.NoError(t, err)
		outputs[podName] = string(output)
	}

	// Check that the read command returned the correct output.
	// A retry accounts for variations in the connect injection time to succeed.
	retrier := &retry.Timer{Timeout: 160 * time.Second, Wait: 2 * time.Second}
	retry.RunWith(retrier, t, func(r *retry.R) {
		for podName, output := range outputs {
			logger.Log(t, output)
			// Both proxies must see their own local agent and app as clusters
			require.Regexp(r, "local_agent.*STATIC", output)
			require.Regexp(r, "local_app.*STATIC", output)

			// Static Client must have Static Server as a cluster and endpoint.
			if strings.Contains(podName, "static-client") {
				require.Regexp(r, "static-server.*static-server\\.default\\.dc1\\.internal.*EDS", output)
				require.Regexp(r, ipv4RegEx+".*static-server.default.dc1.internal", output)
			}
		}
	})
}

func TestTranslateListOutput(t *testing.T) {
	cases := map[string]struct {
		raw      []byte
		expected map[string]string
	}{
		"For single namespace": {
			raw: []byte(`Namespace: default
Name                     	Type
backend-658b679b45-d5xlb 	Sidecar
client-767ccfc8f9-6f6gx  	Sidecar
frontend-676564547c-v2mfq	Sidecar
server-7685b4fc97-9kt9c  	Sidecar
server-7685b4fc97-v78gm  	Sidecar
server-7685b4fc97-vphq9  	Sidecar`),
			expected: map[string]string{
				"backend-658b679b45-d5xlb":  "Sidecar",
				"client-767ccfc8f9-6f6gx":   "Sidecar",
				"frontend-676564547c-v2mfq": "Sidecar",
				"server-7685b4fc97-9kt9c":   "Sidecar",
				"server-7685b4fc97-v78gm":   "Sidecar",
				"server-7685b4fc97-vphq9":   "Sidecar",
			},
		},
		"Across multiple namespaces": {
			raw: []byte(`Namespace: All Namespaces
Namespace	Name                                   	Type
consul   	consul-ingress-gateway-6fb5544485-br6fl	Ingress Gateway
consul   	consul-ingress-gateway-6fb5544485-m54sp	Ingress Gateway
default  	backend-658b679b45-d5xlb               	Sidecar
default  	client-767ccfc8f9-6f6gx                	Sidecar
default  	frontend-676564547c-v2mfq              	Sidecar
default  	server-7685b4fc97-9kt9c                	Sidecar
default  	server-7685b4fc97-v78gm                	Sidecar
default  	server-7685b4fc97-vphq9                	Sidecar`),
			expected: map[string]string{
				"consul/consul-ingress-gateway-6fb5544485-br6fl": "Ingress Gateway",
				"consul/consul-ingress-gateway-6fb5544485-m54sp": "Ingress Gateway",
				"default/backend-658b679b45-d5xlb":               "Sidecar",
				"default/client-767ccfc8f9-6f6gx":                "Sidecar",
				"default/frontend-676564547c-v2mfq":              "Sidecar",
				"default/server-7685b4fc97-9kt9c":                "Sidecar",
				"default/server-7685b4fc97-v78gm":                "Sidecar",
				"default/server-7685b4fc97-vphq9":                "Sidecar",
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := translateListOutput(tc.raw)
			require.Equal(t, tc.expected, actual)
		})
	}
}

// translateListOutput takes the raw output from the proxy list command and
// translates the table into a map.
func translateListOutput(raw []byte) map[string]string {
	formatted := make(map[string]string)
	for _, pod := range strings.Split(strings.TrimSpace(string(raw)), "\n")[3:] {
		row := strings.Split(strings.TrimSpace(pod), "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else if len(row) == 2 {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
