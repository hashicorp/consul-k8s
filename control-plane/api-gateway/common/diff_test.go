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
		"gateway equal": {
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
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
		"gateway name different": {
			a: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway-2",
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
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway meta different": {
			a: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway",
				Meta: map[string]string{
					"somekey2": "somevalue",
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
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different name": {
			a: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway",
				Meta: map[string]string{
					"somekey": "somevalue",
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     "l2",
						Hostname: "host.com",
						Port:     590,
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different hostname": {
			a: &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: "api-gateway",
				Meta: map[string]string{
					"somekey": "somevalue",
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     "l1",
						Hostname: "host-different.com",
						Port:     590,
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different port": {
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
						Port:     123,
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different protocol": {
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
						Protocol: "https",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different TLS max version": {
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
									Kind:        api.FileSystemCertificate,
									Name:        "cert",
									SectionName: "section",
									Partition:   "partition",
									Namespace:   "ns",
								},
							},
							MaxVersion:   "15",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different TLS min version": {
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
									Kind:        api.FileSystemCertificate,
									Name:        "cert",
									SectionName: "section",
									Partition:   "partition",
									Namespace:   "ns",
								},
							},
							MaxVersion:   "5",
							MinVersion:   "0",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different TLS cipher suites": {
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
									Kind:        api.FileSystemCertificate,
									Name:        "cert",
									SectionName: "section",
									Partition:   "partition",
									Namespace:   "ns",
								},
							},
							MaxVersion:   "5",
							MinVersion:   "2",
							CipherSuites: []string{"cipher", "another one"},
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different TLS certificate references": {
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
									Kind:        api.FileSystemCertificate,
									Name:        "cert-2",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different override policies jwt provider name": {
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
									Kind:        api.FileSystemCertificate,
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
										Name: "auth0",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different override policy jwt claims path": {
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
									Kind:        api.FileSystemCertificate,
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
												Path:  []string{"roles"},
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different override policy jwt claims value": {
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
									Kind:        api.FileSystemCertificate,
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
												Value: "user",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different default policies jwt provider name": {
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
									Kind:        api.FileSystemCertificate,
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
										Name: "auth0",
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
									Kind:        api.FileSystemCertificate,
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
			expectedResult: false,
		},
		"gateway listeners different default policy jwt claims path": {
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
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
												Path:  []string{"roles"},
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
			expectedResult: false,
		},
		"gateway listeners different default policy jwt claims value": {
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
									Kind:        api.FileSystemCertificate,
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
									Kind:        api.FileSystemCertificate,
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
												Value: "user",
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
			expectedResult: false,
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
