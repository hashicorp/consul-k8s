package read

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterClusters(t *testing.T) {
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
			Name:                     "local_admin",
			FullyQualifiedDomainName: "local_admin",
			Endpoints:                []string{"127.0.0.1:5000"},
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
					Name:                     "local_admin",
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
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
			},
		},
		"Filter FQDN": {
			fqdn:    "default",
			address: "",
			port:    -1,
			expected: []Cluster{
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
					Name:                     "server",
					FullyQualifiedDomainName: "server.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul",
					Endpoints:                []string{},
					Type:                     "EDS",
					LastUpdated:              "2022-06-09T00:39:12.754Z",
				},
			},
		},
		"Filter address": {
			fqdn:    "",
			address: "127.0.",
			port:    -1,
			expected: []Cluster{
				{
					Name:                     "local_app",
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
				{
					Name:                     "local_admin",
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
			},
		},
		"Filter port": {
			fqdn:    "",
			address: "",
			port:    8080,
			expected: []Cluster{
				{
					Name:                     "local_app",
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
			},
		},
		"Filter fqdn and address": {
			fqdn:    "local",
			address: "127.0.0.1",
			port:    -1,
			expected: []Cluster{
				{
					Name:                     "local_app",
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
				{
					Name:                     "local_admin",
					FullyQualifiedDomainName: "local_admin",
					Endpoints:                []string{"127.0.0.1:5000"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
			},
		},
		"Filter fqdn and port": {
			fqdn:    "local",
			address: "",
			port:    8080,
			expected: []Cluster{
				{
					Name:                     "local_app",
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
			},
		},
		"Filter address and port": {
			fqdn:    "",
			address: "127.0.0.1",
			port:    8080,
			expected: []Cluster{
				{
					Name:                     "local_app",
					FullyQualifiedDomainName: "local_app",
					Endpoints:                []string{"127.0.0.1:8080"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.655Z",
				},
			},
		},
		"Filter fqdn, address, and port": {
			fqdn:    "local",
			address: "192.168.79.187",
			port:    8502,
			expected: []Cluster{
				{
					Name:                     "local_agent",
					FullyQualifiedDomainName: "local_agent",
					Endpoints:                []string{"192.168.79.187:8502"},
					Type:                     "STATIC",
					LastUpdated:              "2022-05-13T04:22:39.553Z",
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
			},
		},
		"Filter address": {
			address: "127.0.0.1",
			port:    -1,
			expected: []Endpoint{
				{
					Address: "127.0.0.1:8080",
					Cluster: "local_app",
					Weight:  1,
					Status:  "HEALTHY",
				},
			},
		},
		"Filter port": {
			address: "",
			port:    20000,
			expected: []Endpoint{
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
			},
		},
		"Filter address and port": {
			address: "235",
			port:    20000,
			expected: []Endpoint{
				{
					Address: "192.168.47.235:20000",
					Weight:  1,
					Status:  "HEALTHY",
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
			},
			Direction:   "OUTBOUND",
			LastUpdated: "2022-05-24T17:41:59.079Z",
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
					},
					Direction:   "OUTBOUND",
					LastUpdated: "2022-05-24T17:41:59.079Z",
				},
			},
		},
		"Filter address": {
			address: "127.0.0.1",
			port:    -1,
			expected: []Listener{
				{
					Name:    "outbound_listener",
					Address: "127.0.0.1:15001",
					FilterChain: []FilterChain{
						{
							FilterChainMatch: "10.100.134.173/32, 240.0.0.3/32",
							Filters:          []string{"-> client.default.dc1.internal.bc3815c2-1a0f-f3ff-a2e9-20d791f08d00.consul"},
						},
					},
					Direction:   "OUTBOUND",
					LastUpdated: "2022-05-24T17:41:59.079Z",
				},
			},
		},
		"Filter port": {
			address: "",
			port:    20000,
			expected: []Listener{
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
			},
		},
		"Filter address and port": {
			address: "192.168.69.179",
			port:    20000,
			expected: []Listener{
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
