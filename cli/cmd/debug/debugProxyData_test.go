package debug

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/envoy"
)

func TestCaptureEnvoyStats(t *testing.T) {
	pod := pods[0]
	proxyPod := proxyPodData{
		pod:       pod,
		proxyType: "dummyProxyType",
		Namespace: pod.Namespace,
		Name:      pod.Name,
	}

	mockJSONResponse := `{"server":{"stats_recent_lookups":0}}`
	// indentation should match the output of json.MarshalIndent in command.go
	expectedFileContent := `{
	"server": {
		"stats_recent_lookups": 0
	}
}`
	server := startHttpServerForEnvoyStats(envoyDefaultAdminPort, mockJSONResponse)
	defer server.Close()

	c := initializeDebugCommands(new(bytes.Buffer))
	endpoint := "localhost:" + strconv.Itoa(envoyDefaultAdminPort)

	p := &EnvoyProxyCapture{
		output: t.TempDir(),
		ctx:    c.Ctx,
	}
	err := p.captureEnvoyStats(endpoint, proxyPod)

	require.NoError(t, err, "captureEnvoyStats should not return an error")
	expectedFilePath := filepath.Join(p.output, "proxy", pod.Namespace, "dummyProxyType", pod.Name, "stats.json")
	require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedFilePath)

	actualFileContent, err := os.ReadFile(expectedFilePath)
	require.NoError(t, err)
	require.Equal(t, expectedFileContent, string(actualFileContent))
}
func TestCaptureEnvoyConfig(t *testing.T) {
	pod := pods[0]
	proxyPod := proxyPodData{
		pod:       pod,
		proxyType: "dummyProxyType",
		Namespace: pod.Namespace,
		Name:      pod.Name,
	}

	expectedConfig := map[string]interface{}{
		proxyPod.Name: map[string]interface{}{
			"clusters":  testEnvoyConfig.Clusters,
			"endpoints": testEnvoyConfig.Endpoints,
			"listeners": testEnvoyConfig.Listeners,
			"routes":    testEnvoyConfig.Routes,
			"secrets":   testEnvoyConfig.Secrets,
		},
	}
	expectedConfigJSON, err := json.MarshalIndent(expectedConfig, "", "\t")
	require.NoError(t, err)

	expectedConfigDumps := []byte(`{
	"pod-ingress-gateway": {
		"configs": [
			{
				"id": 1
			}
		]
	}
}`)
	c := initializeDebugCommands(new(bytes.Buffer))

	p := &EnvoyProxyCapture{
		kubernetes: fake.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{pod}}),
		output:     t.TempDir(),
		ctx:        c.Ctx,
	}
	// Override fetchEnvoyConfig to return our test data instead of making an actual HTTP call.
	p.fetchEnvoyConfig = func(ctx context.Context, pf common.PortForwarder) (*envoy.EnvoyConfig, error) {
		return testEnvoyConfig, nil
	}

	err = p.captureEnvoyConfig(proxyPod)
	require.NoError(t, err, "captureEnvoyConfig should not return an error")

	expectedConfigFilePath := filepath.Join(p.output, "proxy", pod.Namespace, "dummyProxyType", pod.Name, "config.json")
	require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedConfigFilePath)

	expectedConfigDumpFilePath := filepath.Join(p.output, "proxy", pod.Namespace, "dummyProxyType", pod.Name, "config_dumps.json")
	require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedConfigDumpFilePath)

	actualConfigJSON, err := os.ReadFile(expectedConfigFilePath)
	require.NoError(t, err)
	require.Equal(t, string(expectedConfigJSON), string(actualConfigJSON))

	actualConfigDumpJSON, err := os.ReadFile(expectedConfigDumpFilePath)
	require.NoError(t, err)
	require.Equal(t, string(expectedConfigDumps), string(actualConfigDumpJSON))
}

func TestGetEnvoyProxyPodsList(t *testing.T) {
	c := initializeDebugCommands(new(bytes.Buffer))

	p := &EnvoyProxyCapture{
		kubernetes: fake.NewSimpleClientset(&v1.PodList{Items: pods}),
		ctx:        c.Ctx,
	}
	err := p.getEnvoyProxyPodsList()
	require.NoError(t, err)

	// Calculate the total number of pods found across all groups.
	totalPodsFound := 0
	for _, group := range p.proxyPodsList {
		totalPodsFound += len(group.ProxyPodData)
		// Check that "Not-a-proxy-pod" is not in the list.
		for _, pod := range group.ProxyPodData {
			require.NotEqual(t, "Not-a-proxy-pod", pod.Name, "Not-a-proxy-pod should not be in the returned list")
		}
	}

	// "Not-a-proxy-pod" is not a proxy pod and should be filtered out.
	require.Equal(t, totalPodsFound, len(pods)-1)
}

func TestGetAndWriteEnvoyProxyPodList(t *testing.T) {
	c := initializeDebugCommands(new(bytes.Buffer))
	p := &EnvoyProxyCapture{
		kubernetes: fake.NewSimpleClientset(&v1.PodList{Items: pods}),
		output:     t.TempDir(),
		ctx:        c.Ctx,
	}

	// getproxypods
	err := p.getEnvoyProxyPodsList()
	require.NoError(t, err)

	// Calculate the total number of pods found across all groups.
	totalPodsFound := 0
	for _, group := range p.proxyPodsList {
		totalPodsFound += len(group.ProxyPodData)
		// Check that "Not-a-proxy-pod" is not in the list.
		for _, pod := range group.ProxyPodData {
			require.NotEqual(t, "Not-a-proxy-pod", pod.Name, "Not-a-proxy-pod should not be in the returned list")
		}
	}
	// "Not-a-proxy-pod" is not a proxy pod and should be filtered out.
	require.Equal(t, totalPodsFound, len(pods)-1)

	// writeproxypods
	err = p.writeEnvoyProxyPodList()
	require.NoError(t, err)

	expectedFilePath := filepath.Join(p.output, "proxy", "proxyList.json")
	_, err = os.Stat(expectedFilePath)
	require.NoError(t, err, "expected output file '%s' to be created, but it was not", expectedFilePath)

	actualFileContent, err := os.ReadFile(expectedFilePath)
	require.NoError(t, err)

	// Unmarshal into the new struct type.
	var proxyPodsFromFile []proxyPods
	err = json.Unmarshal(actualFileContent, &proxyPodsFromFile)
	require.NoError(t, err)

	// Check that the counts for each proxy type are correct.
	var sidecars, ingressGW, apiGW, deprecated_apiGW, meshGW, termGW = 1, 2, 1, 1, 1, 1
	for _, proxyGroup := range proxyPodsFromFile {
		switch proxyGroup.ProxyPodType {
		case "Sidecar":
			require.Equal(t, sidecars, len(proxyGroup.ProxyPodData), "sidecars mismatched")
		case "Mesh Gateway":
			require.Equal(t, meshGW, len(proxyGroup.ProxyPodData), "meshGW mismatched")
		case "API Gateway":
			require.Equal(t, apiGW, len(proxyGroup.ProxyPodData), "apiGW mismatched")
		case "API Gateway(Deprecated)":
			require.Equal(t, deprecated_apiGW, len(proxyGroup.ProxyPodData), "deprecated_apiGW mismatched")
		case "Terminating Gateway":
			require.Equal(t, termGW, len(proxyGroup.ProxyPodData), "termGW mismatched")
		case "Ingress Gateway":
			require.Equal(t, ingressGW, len(proxyGroup.ProxyPodData), "ingressGW mismatched")
		default:
			t.Fatalf("unexpected proxy type found in JSON: %s", proxyGroup.ProxyPodType)
		}
	}
}

func startHttpServerForEnvoyStats(port int, jsonResponse string) *http.Server {
	srv := &http.Server{Addr: ":" + strconv.Itoa(port)}

	handler := http.NewServeMux()
	handler.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") == "json" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, jsonResponse)
		} else {
			http.Error(w, "format must be json", http.StatusBadRequest)
		}
	})
	srv.Handler = handler

	go func() {
		srv.ListenAndServe()
	}()
	return srv
}

// pods for k8s clientset fake testing
var pods = []v1.Pod{
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-ingress-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"component": "ingress-gateway",
				"chart":     "consul-helm",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "another-pod-ingress-gateway",
			Namespace: "default",
			Labels: map[string]string{
				"component": "ingress-gateway",
				"chart":     "consul-helm",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-mesh-gateway",
			Namespace: "consul",
			Labels: map[string]string{
				"component": "mesh-gateway",
				"chart":     "consul-helm",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-terminating-gateway",
			Namespace: "consul",
			Labels: map[string]string{
				"component": "terminating-gateway",
				"chart":     "consul-helm",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-api-gateway",
			Namespace: "consul",
			Labels: map[string]string{
				"component":                            "api-gateway",
				"gateway.consul.hashicorp.com/managed": "true",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-deprecated-api-gateway",
			Namespace: "consul",
			Labels: map[string]string{
				"api-gateway.consul.hashicorp.com/managed": "true",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "Not-a-proxy-pod",
			Namespace: "default",
			Labels:    map[string]string{},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-sidecar",
			Namespace: "default",
			Labels: map[string]string{
				"consul.hashicorp.com/connect-inject-status": "injected",
			},
		},
	},
}

// testEnvoyConfig is what we expect the config at `test_config_dump.json` to be.
var testEnvoyConfig = &envoy.EnvoyConfig{
	Clusters: []envoy.Cluster{
		{Name: "local_agent", FullyQualifiedDomainName: "local_agent", Endpoints: []string{"192.168.79.187:8502"}, Type: "STATIC", LastUpdated: "2022-05-13T04:22:39.553Z"},

		{Name: "client", FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul", Endpoints: []string{"192.168.18.110:20000", "192.168.52.101:20000", "192.168.65.131:20000"}, Type: "EDS", LastUpdated: "2022-08-10T12:30:32.326Z"},

		{Name: "frontend", FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul", Endpoints: []string{"192.168.63.120:20000"}, Type: "EDS", LastUpdated: "2022-08-10T12:30:32.233Z"},

		{Name: "local_app", FullyQualifiedDomainName: "local_app", Endpoints: []string{"127.0.0.1:8080"}, Type: "STATIC", LastUpdated: "2022-05-13T04:22:39.655Z"},

		{Name: "original-destination", FullyQualifiedDomainName: "original-destination", Endpoints: []string{}, Type: "ORIGINAL_DST", LastUpdated: "2022-05-13T04:22:39.743Z"},
	},

	Endpoints: []envoy.Endpoint{
		{Address: "192.168.79.187:8502", Cluster: "local_agent", Weight: 1, Status: "HEALTHY"},

		{Address: "192.168.18.110:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},

		{Address: "192.168.52.101:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},

		{Address: "192.168.65.131:20000", Cluster: "client", Weight: 1, Status: "HEALTHY"},

		{Address: "192.168.63.120:20000", Cluster: "frontend", Weight: 1, Status: "HEALTHY"},

		{Address: "127.0.0.1:8080", Cluster: "local_app", Weight: 1, Status: "HEALTHY"},
	},

	Listeners: []envoy.Listener{
		{Name: "public_listener", Address: "192.168.69.179:20000", FilterChain: []envoy.FilterChain{{Filters: []string{"HTTP: * -> local_app/"}, FilterChainMatch: "Any"}}, Direction: "INBOUND", LastUpdated: "2022-08-10T12:30:47.142Z"},

		{Name: "outbound_listener", Address: "127.0.0.1:15001", FilterChain: []envoy.FilterChain{
			{Filters: []string{"TCP: -> client"}, FilterChainMatch: "10.100.134.173/32, 240.0.0.3/32"},

			{Filters: []string{"TCP: -> frontend"}, FilterChainMatch: "10.100.31.2/32, 240.0.0.5/32"},

			{Filters: []string{"TCP: -> original-destination"}, FilterChainMatch: "Any"},
		}, Direction: "OUTBOUND", LastUpdated: "2022-07-18T15:31:03.246Z"},
	},

	Routes: []envoy.Route{
		{
			Name: "public_listener",

			DestinationCluster: "local_app/",

			LastUpdated: "2022-08-10T12:30:47.141Z",
		},
	},

	Secrets: []envoy.Secret{
		{
			Name: "default",

			Type: "Dynamic Active",

			LastUpdated: "2022-05-24T17:41:59.078Z",
		},

		{
			Name: "ROOTCA",

			Type: "Dynamic Warming",

			LastUpdated: "2022-03-15T05:14:22.868Z",
		},
	},
	RawCfg: []byte(`{
  "configs": [
    {
      "id": 1
    }
  ]
}`),
}
