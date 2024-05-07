// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"

	"github.com/stretchr/testify/require"
)

func TestToCatalogRegistration(tt *testing.T) {
	cases := map[string]struct {
		registration *Registration
		expected     *capi.CatalogRegistration
	}{
		"minimal registration": {
			registration: &Registration{
				Spec: RegistrationSpec{
					ID:         "node-id",
					Node:       "node-virtual",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: Service{
						ID:      "service-id",
						Name:    "service-name",
						Port:    8080,
						Address: "127.0.0.1",
					},
				},
			},
			expected: &capi.CatalogRegistration{
				ID:         "node-id",
				Node:       "node-virtual",
				Address:    "127.0.0.1",
				Datacenter: "dc1",
				Service: &capi.AgentService{
					ID:      "service-id",
					Service: "service-name",
					Port:    8080,
					Address: "127.0.0.1",
				},
			},
		},
		"maximal registration": {
			registration: &Registration{
				Spec: RegistrationSpec{
					ID:      "node-id",
					Node:    "node-virtual",
					Address: "127.0.0.1",
					TaggedAddresses: map[string]string{
						"lan": "8080",
					},
					NodeMeta: map[string]string{
						"n1": "m1",
					},
					Datacenter: "dc1",
					Service: Service{
						ID:   "service-id",
						Name: "service-name",
						Tags: []string{"tag1", "tag2"},
						Meta: map[string]string{
							"m1": "1",
							"m2": "2",
						},
						Port:    8080,
						Address: "127.0.0.1",
						TaggedAddresses: map[string]ServiceAddress{
							"lan": {
								Address: "10.0.0.10",
								Port:    5000,
							},
						},
						Weights: Weights{
							Passing: 50,
							Warning: 100,
						},
						EnableTagOverride: true,
						Locality: &Locality{
							Region: "us-east-1",
							Zone:   "auto",
						},
						Namespace: "n1",
						Partition: "p1",
					},
					Partition: "p1",
					HealthCheck: &HealthCheck{
						Node:        "node-virtual",
						CheckID:     "service-check",
						Name:        "service-health",
						Status:      "passing",
						Notes:       "all about that service",
						Output:      "healthy",
						ServiceID:   "service-id",
						ServiceName: "service-name",
						Type:        "readiness",
						ExposedPort: 19000,
						Definition: HealthCheckDefinition{
							HTTP: "/health",
							TCP:  "tcp-check",
							Header: map[string][]string{
								"Content-Type": {"application/json"},
							},
							Method:                                 "GET",
							TLSServerName:                          "my-secure-tls-server",
							TLSSkipVerify:                          true,
							Body:                                   "some-body",
							GRPC:                                   "/grpc-health-check",
							GRPCUseTLS:                             true,
							OSService:                              "osservice-name",
							IntervalDuration:                       "5s",
							TimeoutDuration:                        "10s",
							DeregisterCriticalServiceAfterDuration: "30s",
						},
						Namespace: "n1",
						Partition: "p1",
					},
					Locality: &Locality{
						Region: "us-east-1",
						Zone:   "auto",
					},
				},
			},
			expected: &capi.CatalogRegistration{
				ID:      "node-id",
				Node:    "node-virtual",
				Address: "127.0.0.1",
				TaggedAddresses: map[string]string{
					"lan": "8080",
				},
				NodeMeta: map[string]string{
					"n1": "m1",
				},
				Datacenter: "dc1",
				Service: &capi.AgentService{
					ID:      "service-id",
					Service: "service-name",
					Tags:    []string{"tag1", "tag2"},
					Meta: map[string]string{
						"m1": "1",
						"m2": "2",
					},
					Port:    8080,
					Address: "127.0.0.1",
					TaggedAddresses: map[string]capi.ServiceAddress{
						"lan": {
							Address: "10.0.0.10",
							Port:    5000,
						},
					},
					Weights: capi.AgentWeights{
						Passing: 50,
						Warning: 100,
					},
					EnableTagOverride: true,
					Locality: &capi.Locality{
						Region: "us-east-1",
						Zone:   "auto",
					},
					Namespace: "n1",
					Partition: "p1",
				},
				Check: &capi.AgentCheck{
					Node:        "node-virtual",
					CheckID:     "service-check",
					Name:        "service-health",
					Status:      "passing",
					Notes:       "all about that service",
					Output:      "healthy",
					ServiceID:   "service-id",
					ServiceName: "service-name",
					Type:        "readiness",
					ExposedPort: 19000,
					Definition: capi.HealthCheckDefinition{
						HTTP: "/health",
						TCP:  "tcp-check",
						Header: map[string][]string{
							"Content-Type": {"application/json"},
						},
						Method:                                 "GET",
						TLSServerName:                          "my-secure-tls-server",
						TLSSkipVerify:                          true,
						Body:                                   "some-body",
						GRPC:                                   "/grpc-health-check",
						GRPCUseTLS:                             true,
						OSService:                              "osservice-name",
						IntervalDuration:                       toDuration(tt, "5s"),
						TimeoutDuration:                        toDuration(tt, "10s"),
						DeregisterCriticalServiceAfterDuration: toDuration(tt, "30s"),
					},
					Namespace: "n1",
					Partition: "p1",
				},
				SkipNodeUpdate: false,
				Partition:      "p1",
				Locality: &capi.Locality{
					Region: "us-east-1",
					Zone:   "auto",
				},
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			actual, err := tc.registration.ToCatalogRegistration()
			require.NoError(t, err)
			require.Equal(t, tc.expected, actual)
		})
	}
}

func TestToCatalogDeregistration(tt *testing.T) {
	cases := map[string]struct {
		registration *Registration
		expected     *capi.CatalogDeregistration
	}{
		"with health check": {
			registration: &Registration{
				Spec: RegistrationSpec{
					ID:         "node-id",
					Node:       "node-virtual",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: Service{
						ID:        "service-id",
						Namespace: "n1",
						Partition: "p1",
					},
					HealthCheck: &HealthCheck{
						CheckID: "checkID",
					},
				},
			},
			expected: &capi.CatalogDeregistration{
				Node:       "node-virtual",
				Address:    "127.0.0.1",
				Datacenter: "dc1",
				ServiceID:  "service-id",
				CheckID:    "checkID",
				Namespace:  "n1",
				Partition:  "p1",
			},
		},
		"no health check": {
			registration: &Registration{
				Spec: RegistrationSpec{
					ID:         "node-id",
					Node:       "node-virtual",
					Address:    "127.0.0.1",
					Datacenter: "dc1",
					Service: Service{
						ID:        "service-id",
						Namespace: "n1",
						Partition: "p1",
					},
				},
			},
			expected: &capi.CatalogDeregistration{
				Node:       "node-virtual",
				Address:    "127.0.0.1",
				Datacenter: "dc1",
				ServiceID:  "service-id",
				CheckID:    "",
				Namespace:  "n1",
				Partition:  "p1",
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		tt.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := tc.registration.ToCatalogDeregistration()
			require.Equal(t, tc.expected, actual)
		})
	}
}

func toDuration(t *testing.T, d string) time.Duration {
	t.Helper()
	duration, err := time.ParseDuration(d)
	if err != nil {
		t.Fatal(err)
	}
	return duration
}
