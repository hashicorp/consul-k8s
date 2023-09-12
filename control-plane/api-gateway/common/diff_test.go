// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestEntriesEqual(t *testing.T) {
	testCases := map[string]struct {
		a              api.ConfigEntry
		b              api.ConfigEntry
		expectedResult bool
	}{
		"gateway_equal": {
			a: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway",
				Meta: map[string]string{
					"somekey": "somevalue",
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     "l1",
						Hostname: "host.com",
						Port:     590,
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.InlineCertificate,
									Name:        "cert",
									SectionName: "section",
									Partition:   "partition",
									Namespace:   "ns",
								},
							},
							MaxVersion:   "5",
							MinVersion:   "2",
							CipherSuites: []string{"cipher"},
						},
						Override: &api.APIGatewayPolicy{
							JWT: &api.APIGatewayJWTRequirement{
								Providers: []*api.APIGatewayJWTProvider{
									{
										Name: "okta",
										VerifyClaims: []*api.APIGatewayJWTClaimVerification{
											{
												Path:  []string{"role"},
												Value: "admin",
											},
										},
									},
								},
							},
						},
						Default: &api.APIGatewayPolicy{
							JWT: &api.APIGatewayJWTRequirement{
								Providers: []*api.APIGatewayJWTProvider{
									{
										Name: "okta",
										VerifyClaims: []*api.APIGatewayJWTClaimVerification{
											{
												Path:  []string{"aud"},
												Value: "consul.com",
											},
										},
									},
								},
							},
						},
					},
				},
				Partition: "partition",
				Namespace: "ns",
			},
			b: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway",
				Meta: map[string]string{
					"somekey": "somevalue",
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     "l1",
						Hostname: "host.com",
						Port:     590,
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.InlineCertificate,
									Name:        "cert",
									SectionName: "section",
									Partition:   "partition",
									Namespace:   "ns",
								},
							},
							MaxVersion:   "5",
							MinVersion:   "2",
							CipherSuites: []string{"cipher"},
						},
						Override: &api.APIGatewayPolicy{
							JWT: &api.APIGatewayJWTRequirement{
								Providers: []*api.APIGatewayJWTProvider{
									{
										Name: "okta",
										VerifyClaims: []*api.APIGatewayJWTClaimVerification{
											{
												Path:  []string{"role"},
												Value: "admin",
											},
										},
									},
								},
							},
						},
						Default: &api.APIGatewayPolicy{
							JWT: &api.APIGatewayJWTRequirement{
								Providers: []*api.APIGatewayJWTProvider{
									{
										Name: "okta",
										VerifyClaims: []*api.APIGatewayJWTClaimVerification{
											{
												Path:  []string{"aud"},
												Value: "consul.com",
											},
										},
									},
								},
							},
						},
					},
				},
				Partition: "partition",
				Namespace: "ns",
			},
			expectedResult: true,
		},
	}

	for name, tc := range testCases {
		name := name
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			actual := EntriesEqual(tc.a, tc.b)
			require.Equal(t, tc.expectedResult, actual)
		})
	}
}
