package read

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterFQDN(t *testing.T) {
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

	cases := map[string]struct {
		substring string
		expected  []Cluster
	}{
		"No filter": {
			substring: "",
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
		"Filter default": {
			substring: "default",
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
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			actual := FilterFQDN(given, tc.substring)
			require.Equal(t, tc.expected, actual)
		})
	}

}
