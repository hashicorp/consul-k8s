package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/stretchr/testify/require"
)

func TestProxyList(t *testing.T) {
	// Because the Kubernetes Pods will have a random string appended to their
	// names, the expected mapping only includes how the Pod name will start
	// to validate the test output.
	expected := map[string]string{
		"default/consul-consul-ingress-gateway-": "Ingress Gateway",
		"default/static-client-":                 "Sidecar",
		"default/static-server-":                 "Sidecar",
	}

	// Install Consul in the cluster
	helmValues := map[string]string{
		"controller.enabled":    "true",
		"connectInject.enabled": "true",

		"ingressGateways.enabled":           "true",
		"ingressGateways.defaults.replicas": "1",
	}
	cfg := suite.Config()
	ctx := suite.Environment().DefaultContext(t)
	consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, "consul")
	consulCluster.Create(t)

	// Deploy Pods into the Cluster
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-client-inject")
	k8s.DeployKustomize(t, ctx.KubectlOptions(t), cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

	// Run consul-k8s proxy list
	actual, err := cli.RunCmd("proxy", "list", "-A")
	require.NoError(t, err)
	fmt.Println(string(actual))

	// Verify the output
	for podName, proxyType := range translateListOutput(actual) {
		// Remove the 16 random character appended to each Pod name.
		podName = podName[:len(podName)-16]
		require.Equal(t, expected[podName], proxyType)
	}
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
