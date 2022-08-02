package read

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterClusters(t *testing.T) {
	given := []Cluster{
		{
			FullyQualifiedDomainName: "local_agent",
			Endpoints:                []string{"192.168.79.187:8502"},
		},
		{
			FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{},
		},
		{
			FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{},
		},
		{
			FullyQualifiedDomainName: "local_app",
			Endpoints:                []string{"127.0.0.1:8080"},
		},
		{
			FullyQualifiedDomainName: "local_admin",
			Endpoints:                []string{"127.0.0.1:5000"},
		},
		{
			FullyQualifiedDomainName: "original-destination",
			Endpoints:                []string{},
		},
		{
			FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
			Endpoints:                []string{"123.45.67.890:8080", "111.30.2.39:8080"},
		},
	}

	cases := map[string]struct {
		fqdn     string
		address  string
		port     int
		expected []Cluster
	}{
		"No filter": {
			fqdn:    "",
			address: "",
			port:    -1,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_agent",
					Endpoints:                []string{"192.168.79.187:8502"},
				},
				{
					FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{},
				},
				{
					FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{},
				},
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
				{
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
				},
				{
					FullyQualifiedDomainName: "original-destination",
					Endpoints:                []string{},
				},
				{
					FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{"123.45.67.890:8080", "111.30.2.39:8080"},
				},
			},
		},
		"Filter FQDN": {
			fqdn:    "default",
			address: "",
			port:    -1,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{},
				},
				{
					FullyQualifiedDomainName: "frontend.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{},
				},
				{
					FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{"123.45.67.890:8080", "111.30.2.39:8080"},
				},
			},
		},
		"Filter address": {
			fqdn:    "",
			address: "127.0.",
			port:    -1,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
				{
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
				},
			},
		},
		"Filter port": {
			fqdn:    "",
			address: "",
			port:    8080,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
				{
					FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{"123.45.67.890:8080", "111.30.2.39:8080"},
				},
			},
		},
		"Filter fqdn and address": {
			fqdn:    "local",
			address: "127.0.0.1",
			port:    -1,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
				{
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
				},
			},
		},
		"Filter fqdn and port": {
			fqdn:    "local",
			address: "",
			port:    8080,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
			},
		},
		"Filter address and port": {
			fqdn:    "",
			address: "127.0.0.1",
			port:    8080,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
				},
			},
		},
		"Filter fqdn, address, and port": {
			fqdn:    "local",
			address: "192.168.79.187",
			port:    8502,
			expected: []Cluster{
				{
					FullyQualifiedDomainName: "local_agent",
					Endpoints:                []string{"192.168.79.187:8502"},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := FilterClusters(given, tc.fqdn, tc.address, tc.port)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestFilterEndpoints(t *testing.T) {
	given := []Endpoint{
		{
			Address: "192.168.79.187:8502",
		},
		{
			Address: "127.0.0.1:8080",
		},
		{
			Address: "192.168.31.201:20000",
		},
		{
			Address: "192.168.47.235:20000",
		},
		{
			Address: "192.168.71.254:20000",
		},
	}

	cases := map[string]struct {
		address  string
		port     int
		expected []Endpoint
	}{
		"No filter": {
			address: "",
			port:    -1,
			expected: []Endpoint{
				{
					Address: "192.168.79.187:8502",
				},
				{
					Address: "127.0.0.1:8080",
				},
				{
					Address: "192.168.31.201:20000",
				},
				{
					Address: "192.168.47.235:20000",
				},
				{
					Address: "192.168.71.254:20000",
				},
			},
		},
		"Filter address": {
			address: "127.0.0.1",
			port:    -1,
			expected: []Endpoint{
				{
					Address: "127.0.0.1:8080",
				},
			},
		},
		"Filter port": {
			address: "",
			port:    20000,
			expected: []Endpoint{
				{
					Address: "192.168.31.201:20000",
				},
				{
					Address: "192.168.47.235:20000",
				},
				{
					Address: "192.168.71.254:20000",
				},
			},
		},
		"Filter address and port": {
			address: "235",
			port:    20000,
			expected: []Endpoint{
				{
					Address: "192.168.47.235:20000",
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := FilterEndpoints(given, tc.address, tc.port)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestFilterListeners(t *testing.T) {
	given := []Listener{
		{
			Address: "192.168.69.179:20000",
		},
		{
			Address: "127.0.0.1:15001",
		},
	}

	cases := map[string]struct {
		address  string
		port     int
		expected []Listener
	}{
		"No filter": {
			address: "",
			port:    -1,
			expected: []Listener{
				{
					Address: "192.168.69.179:20000",
				},
				{
					Address: "127.0.0.1:15001",
				},
			},
		},
		"Filter address": {
			address: "127.0.0.1",
			port:    -1,
			expected: []Listener{
				{
					Address: "127.0.0.1:15001",
				},
			},
		},
		"Filter port": {
			address: "",
			port:    20000,
			expected: []Listener{
				{
					Address: "192.168.69.179:20000",
				},
			},
		},
		"Filter address and port": {
			address: "192.168.69.179",
			port:    20000,
			expected: []Listener{
				{
					Address: "192.168.69.179:20000",
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := FilterListeners(given, tc.address, tc.port)
			require.Equal(t, tc.expected, actual)
		})
	}
}
