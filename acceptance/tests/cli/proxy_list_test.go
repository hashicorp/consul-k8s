package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/cli"
	"github.com/stretchr/testify/require"
)

func TestProxyList(t *testing.T) {
	helmValues := map[string]string{
		"connectInject.enabled":   "true",
		"ingressGateways.enabled": "true",
	}
	fmt.Println(helmValues)

	expected := map[string]string{
		"default/pod1": "Sidecar",
	}

	// Run proxy proxy list
	actual, err := cli.RunCmd("proxy", "list")
	fmt.Println(err)
	require.NoError(t, err)

	// Verify the output
	fmt.Println(expected, actual)
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
	for _, pod := range strings.Split(string(raw), "\n")[3:] {
		row := strings.Split(pod, "\t")

		var name string
		if len(row) == 3 { // Handle the case where namespace is present
			name = fmt.Sprintf("%s/%s", strings.TrimSpace(row[0]), strings.TrimSpace(row[1]))
		} else {
			name = strings.TrimSpace(row[0])
		}
		formatted[name] = row[len(row)-1]
	}

	return formatted
}
