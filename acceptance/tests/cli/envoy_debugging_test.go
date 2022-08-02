package cli_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	// Run proxy read for each Pod and check the expected output
	for podName := range list {
		// TODO (waiting for other PRs to merge) use -o json and check that the output is correct.
		readOut, err := cli.Run("proxy", "read", podName)
		require.NoError(t, err)
		fmt.Println(string(readOut))
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

/*

	expected := map[string]string {
// Envoy configuration for static-server-7487b98997-5m26g in namespace default:

// ==> Clusters (2)
// Name       	FQDN       	Endpoints      	Type  	Last Updated
// local_agent	local_agent	172.18.0.2:8502	STATIC	2022-08-02T18:57:21.776Z
// local_app  	local_app  	127.0.0.1:8080 	STATIC	2022-08-02T18:57:21.905Z


// ==> Endpoints (2)
// Address:Port   	Cluster    	Weight	Status
// 172.18.0.2:8502	local_agent	1.00  	HEALTHY
// 127.0.0.1:8080 	local_app  	1.00  	HEALTHY


// ==> Listeners (1)
// Name           	Address:Port     	Direction	Filter Chain Match	Filters     	Last Updated
// public_listener	10.244.0.26:20000	INBOUND  	Any               	            	2022-08-02T18:57:22.175Z
//                	                 	         	                  	-> local_app


// ==> Routes (0)
// Name	Destination Cluster	Last Updated


// ==> Secrets (0)
// Name	Type	Last Updated


// Envoy configuration for static-client-689b6676cf-42x5m in Namespace default:

// ==> Clusters (3)
// Name         	FQDN                                                                          	Endpoints      	Type  	Last Updated
// local_agent  	local_agent                                                                   	172.18.0.2:8502	STATIC	2022-08-02T18:57:41.755Z
// local_app    	local_app                                                                     	127.0.0.1:0    	STATIC	2022-08-02T18:57:41.791Z
// static-server	static-server.default.dc1.internal.f2284f59-5e7a-79c8-b853-3fe9764f8d7c.consul	               	EDS   	2022-08-02T18:57:41.850Z


// ==> Endpoints (3)
// Address:Port     	Cluster    	Weight	Status
// 172.18.0.2:8502  	local_agent	1.00  	HEALTHY
// 10.244.0.26:20000	           	1.00  	HEALTHY
// 127.0.0.1:0      	local_app  	1.00  	HEALTHY


// ==> Listeners (2)
// Name           	Address:Port     	Direction	Filter Chain Match	Filters                                                                          	Last Updated
// public_listener	10.244.0.27:20000	INBOUND  	Any               	                                                                                 	2022-08-02T18:57:41.879Z
//                	                 	         	                  	-> local_app
// static-server  	127.0.0.1:1234   	OUTBOUND 	Any               	-> static-server.default.dc1.internal.f2284f59-5e7a-79c8-b853-3fe9764f8d7c.consul	2022-08-02T18:57:41.879Z


// ==> Routes (0)
// Name	Destination Cluster	Last Updated


// ==> Secrets (0)
// Name	Type	Last Updated
	}
*/
