package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
			actual := TranslateListOutput(tc.raw)

			require.Equal(t, len(tc.expected), len(actual))
			for podName, proxyType := range actual {
				require.Equal(t, tc.expected[podName], proxyType)
			}
		})
	}
}
