package read

import (
	"bytes"
	"context"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/stretchr/testify/require"
)

func TestFormatClusters(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Name.*FQDN.*Endpoints.*Type.*Last Updated",
		"local_agent.*local_agent.*192\\.168\\.79\\.187:8502.*STATIC.*2022-05-13T04:22:39\\.553Z",
		"local_app.*local_app.*127\\.0\\.0\\.1:8080.*STATIC.*2022-05-13T04:22:39\\.655Z",
		"client.*client\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul.*EDS.*2022-06-09T00:39:12\\.948Z",
		"frontend.*frontend\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul.*EDS.*2022-06-09T00:39:12\\.855Z",
		"original-destination.*original-destination.*ORIGINAL_DST.*2022-05-13T04:22:39.743Z",
		"server.*server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul.*EDS.*2022-06-09T00:39:12\\.754Z",
	}

	given := []Cluster{
		{
			Name:                     "local_agent",
			FullyQualifiedDomainName: "local_agent",
			Endpoints:                []string{"192.168.79.187:8502"},
			Type:                     "STATIC",
			LastUpdated:              "2022-05-13T04:22:39.553Z",
		},
		{
			Name:                     "client",
			FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{},
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.948Z",
		},
		{
			Name:                     "frontend",
			FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{},
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.855Z",
		},
		{
			Name:                     "local_app",
			FullyQualifiedDomainName: "local_app",
			Endpoints:                []string{"127.0.0.1:8080"},
			Type:                     "STATIC",
			LastUpdated:              "2022-05-13T04:22:39.655Z",
		},
		{
			Name:                     "original-destination",
			FullyQualifiedDomainName: "original-destination",
			Endpoints:                []string{},
			Type:                     "ORIGINAL_DST",
			LastUpdated:              "2022-05-13T04:22:39.743Z",
		},
		{
			Name:                     "server",
			FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{},
			Type:                     "EDS",
			LastUpdated:              "2022-06-09T00:39:12.754Z",
		},
	}

	expectedHeaders := []string{"Name", "FQDN", "Endpoints", "Type", "Last Updated"}

	table := formatClusters(given)

	require.Equal(t, expectedHeaders, table.Headers)
	require.Equal(t, len(given), len(table.Rows))

	buf := new(bytes.Buffer)
	terminal.NewUI(context.Background(), buf).Table(table)

	actual := buf.String()
	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}

func TestFormatEndpoints(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Address:Port.*Cluster.*Weight.*Status",
		"192.168.79.187:8502.*local_agent.*1.00.*HEALTHY",
		"127.0.0.1:8080.*local_app.*1.00.*HEALTHY",
		"192.168.31.201:20000.*1.00.*HEALTHY",
		"192.168.47.235:20000.*1.00.*HEALTHY",
		"192.168.71.254:20000.*1.00.*HEALTHY",
		"192.168.63.120:20000.*1.00.*HEALTHY",
		"192.168.18.110:20000.*1.00.*HEALTHY",
		"192.168.52.101:20000.*1.00.*HEALTHY",
		"192.168.65.131:20000.*1.00.*HEALTHY",
	}

	given := []Endpoint{
		{
			Address: "192.168.79.187:8502",
			Cluster: "local_agent",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "127.0.0.1:8080",
			Cluster: "local_app",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.31.201:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.47.235:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.71.254:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.63.120:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.18.110:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.52.101:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
		{
			Address: "192.168.65.131:20000",
			Weight:  1,
			Status:  "HEALTHY",
		},
	}

	expectedHeaders := []string{"Address:Port", "Cluster", "Weight", "Status"}

	table := formatEndpoints(given)

	require.Equal(t, expectedHeaders, table.Headers)
	require.Equal(t, len(given), len(table.Rows))

	buf := new(bytes.Buffer)
	terminal.NewUI(context.Background(), buf).Table(table)

	actual := buf.String()
	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}

func TestFormatListeners(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Name.*Address:Port.*Direction.*Filter Chain Match.*Filters.*Last Updated",
		"public_listener.*192\\.168\\.69\\.179:20000.*INBOUND.*Any.*\\* -> local_app/.*2022-06-09T00:39:27\\.668Z",
		"outbound_listener.*127.0.0.1:15001.*OUTBOUND.*10\\.100\\.134\\.173/32, 240\\.0\\.0\\.3/32.*-> client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul.*2022-05-24T17:41:59\\.079Z",
		"10\\.100\\.254\\.176/32, 240\\.0\\.0\\.4/32.*\\* -> server\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul/",
		"10\\.100\\.31\\.2/32, 240\\.0\\.0\\.2/32.*-> frontend\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul",
		"Any.*-> original-destination",
	}

	given := []Listener{
		{
			Name:    "public_listener",
			Address: "192.168.69.179:20000",
			FilterChain: []FilterChain{
				{
					FilterChainMatch: "Any",
					Filters:          []string{"* -> local_app/"},
				},
			},
			Direction:   "INBOUND",
			LastUpdated: "2022-06-09T00:39:27.668Z",
		},
		{
			Name:    "outbound_listener",
			Address: "127.0.0.1:15001",
			FilterChain: []FilterChain{
				{
					FilterChainMatch: "10.100.134.173/32, 240.0.0.3/32",
					Filters:          []string{"-> client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul"},
				},
				{
					FilterChainMatch: "10.100.254.176/32, 240.0.0.4/32",
					Filters:          []string{"* -> server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul/"},
				},
				{
					FilterChainMatch: "10.100.31.2/32, 240.0.0.2/32",
					Filters: []string{
						"-> frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					},
				},
				{
					FilterChainMatch: "Any",
					Filters:          []string{"-> original-destination"},
				},
			},
			Direction:   "OUTBOUND",
			LastUpdated: "2022-05-24T17:41:59.079Z",
		},
	}

	expectedHeaders := []string{"Name", "Address:Port", "Direction", "Filter Chain Match", "Filters", "Last Updated"}

	// Listeners tables split filter chain information across rows.
	expectedRowCount := 0
	for _, element := range given {
		expectedRowCount += len(element.FilterChain)
	}

	table := formatListeners(given)

	require.Equal(t, expectedHeaders, table.Headers)
	require.Equal(t, expectedRowCount, len(table.Rows))

	buf := new(bytes.Buffer)
	terminal.NewUI(context.Background(), buf).Table(table)

	actual := buf.String()
	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}

func TestFormatRoutes(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Name.*Destination Cluster.*Last Updated",
		"public_listener.*local_app/.*2022-06-09T00:39:27.667Z",
		"server.*server\\.default\\.dc1\\.internal\\.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00\\.consul/.*2022-05-24T17:41:59\\.078Z",
	}

	given := []Route{
		{
			Name:               "public_listener",
			DestinationCluster: "local_app/",
			LastUpdated:        "2022-06-09T00:39:27.667Z",
		},
		{
			Name:               "server",
			DestinationCluster: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul/",
			LastUpdated:        "2022-05-24T17:41:59.078Z",
		},
	}

	expectedHeaders := []string{"Name", "Destination Cluster", "Last Updated"}

	table := formatRoutes(given)

	require.Equal(t, expectedHeaders, table.Headers)
	require.Equal(t, len(given), len(table.Rows))

	buf := new(bytes.Buffer)
	terminal.NewUI(context.Background(), buf).Table(table)

	actual := buf.String()
	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}

func TestFormatSecrets(t *testing.T) {
	// These regular expressions must be present in the output.
	expected := []string{
		"Name.*Type.*Last Updated",
		"default.*Dynamic Active.*2022-05-24T17:41:59.078Z",
		"ROOTCA.*Dynamic Warming.*2022-03-15T05:14:22.868Z",
	}

	given := []Secret{
		{
			Name:        "default",
			Type:        "Dynamic Active",
			LastUpdated: "2022-05-24T17:41:59.078Z",
		},
		{
			Name:        "ROOTCA",
			Type:        "Dynamic Warming",
			LastUpdated: "2022-03-15T05:14:22.868Z",
		},
	}

	expectedHeaders := []string{"Name", "Type", "Last Updated"}

	table := formatSecrets(given)

	require.Equal(t, expectedHeaders, table.Headers)
	require.Equal(t, len(given), len(table.Rows))

	buf := new(bytes.Buffer)
	terminal.NewUI(context.Background(), buf).Table(table)

	actual := buf.String()
	for _, expression := range expected {
		require.Regexp(t, expression, actual)
	}
}
