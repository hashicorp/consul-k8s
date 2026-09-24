// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gwv1beta1 "github.com/hashicorp/consul-k8s/control-plane/gateway07/gateway-api-0.7.1-custom/apis/v1beta1"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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

func TestEntryComparator_ExtProcProcessingDirectionEqual(t *testing.T) {
	t.Parallel()

	comparator := entryComparator{}
	base := api.ExtProcProcessingDirection{
		HeadersMode:  "SEND",
		BodyMode:     "BUFFERED",
		TrailersMode: "SKIP",
		MaxBodyBytes: 1024,
	}

	require.True(t, comparator.extProcProcessingDirectionEqual(base, base))

	for name, mutate := range map[string]func(d *api.ExtProcProcessingDirection){
		"different headers mode":  func(d *api.ExtProcProcessingDirection) { d.HeadersMode = "SKIP" },
		"different body mode":     func(d *api.ExtProcProcessingDirection) { d.BodyMode = "STREAMED" },
		"different trailers mode": func(d *api.ExtProcProcessingDirection) { d.TrailersMode = "SEND" },
		"different max body":      func(d *api.ExtProcProcessingDirection) { d.MaxBodyBytes = 2048 },
	} {
		t.Run(name, func(t *testing.T) {
			other := base
			mutate(&other)
			require.False(t, comparator.extProcProcessingDirectionEqual(base, other))
		})
	}
}

func TestEntryComparator_ExtProcProcessingEqual(t *testing.T) {
	t.Parallel()

	comparator := entryComparator{}
	base := api.ExtProcProcessing{
		Request:  &api.ExtProcProcessingDirection{HeadersMode: "SEND"},
		Response: &api.ExtProcProcessingDirection{HeadersMode: "SKIP"},
	}

	require.True(t, comparator.extProcProcessingEqual(base, base))

	differentRequest := base
	differentRequest.Request = &api.ExtProcProcessingDirection{HeadersMode: "SKIP"}
	require.False(t, comparator.extProcProcessingEqual(base, differentRequest))

	nilResponse := base
	nilResponse.Response = nil
	require.False(t, comparator.extProcProcessingEqual(base, nilResponse))

	bothNil := api.ExtProcProcessing{}
	require.True(t, comparator.extProcProcessingEqual(bothNil, bothNil))
}

func TestEntryComparator_ExtProcOverridesEqual(t *testing.T) {
	t.Parallel()

	comparator := entryComparator{}
	base := api.ExtProcOverrides{
		Processing: &api.ExtProcProcessing{
			Request: &api.ExtProcProcessingDirection{HeadersMode: "SEND"},
		},
	}

	require.True(t, comparator.extProcOverridesEqual(base, base))

	differentProcessing := api.ExtProcOverrides{
		Processing: &api.ExtProcProcessing{
			Request: &api.ExtProcProcessingDirection{HeadersMode: "SKIP"},
		},
	}
	require.False(t, comparator.extProcOverridesEqual(base, differentProcessing))

	nilProcessing := api.ExtProcOverrides{}
	require.False(t, comparator.extProcOverridesEqual(base, nilProcessing))
	require.True(t, comparator.extProcOverridesEqual(nilProcessing, nilProcessing))
}

func TestEntryComparator_ExtProcFiltersEqual(t *testing.T) {
	t.Parallel()

	comparator := entryComparator{}
	base := api.ExtProcFilter{
		StatPrefix: "prefix",
		Mode:       "override",
		Overrides: &api.ExtProcOverrides{
			Processing: &api.ExtProcProcessing{
				Request: &api.ExtProcProcessingDirection{HeadersMode: "SEND"},
			},
		},
	}

	require.True(t, comparator.extProcFiltersEqual(base, base))

	differentStatPrefix := base
	differentStatPrefix.StatPrefix = "other"
	require.False(t, comparator.extProcFiltersEqual(base, differentStatPrefix))

	differentMode := base
	differentMode.Mode = "enabled"
	require.False(t, comparator.extProcFiltersEqual(base, differentMode))

	nilOverrides := base
	nilOverrides.Overrides = nil
	require.False(t, comparator.extProcFiltersEqual(base, nilOverrides))

	// Filters with no overrides on both sides are equal.
	noOverrides := api.ExtProcFilter{StatPrefix: "prefix", Mode: "enabled"}
	require.True(t, comparator.extProcFiltersEqual(noOverrides, noOverrides))
}

// TestEntriesEqual_HTTPRoute_ExtAuthz exercises the bug-fix path: changes to
// Filters.ExtAuthz on an HTTPRouteConfigEntry rule must be detected as unequal
// so the reconciler writes the updated config entry to Consul.
func TestEntriesEqual_HTTPRoute_ExtAuthz(t *testing.T) {
	t.Parallel()

	baseRule := func(extAuthz *api.HTTPRouteExtAuthzFilter) api.HTTPRouteRule {
		return api.HTTPRouteRule{
			Matches: []api.HTTPMatch{
				{Path: api.HTTPPathMatch{Match: api.HTTPPathMatchPrefix, Value: "/"}},
			},
			Services: []api.HTTPService{{Name: "echo", Namespace: "default", Weight: 1}},
			Filters:  api.HTTPFilters{ExtAuthz: extAuthz},
		}
	}

	enabled := &api.HTTPRouteExtAuthzFilter{Enabled: true}
	disabled := &api.HTTPRouteExtAuthzFilter{Enabled: false}

	makeEntry := func(extAuthz *api.HTTPRouteExtAuthzFilter) *api.HTTPRouteConfigEntry {
		return &api.HTTPRouteConfigEntry{
			Kind:      api.HTTPRoute,
			Name:      "test-route",
			Namespace: "default",
			Partition: "default",
			Rules:     []api.HTTPRouteRule{baseRule(extAuthz)},
		}
	}

	testCases := map[string]struct {
		a, b           *api.HTTPRouteConfigEntry
		expectedResult bool
	}{
		"both nil ExtAuthz are equal": {
			a:              makeEntry(nil),
			b:              makeEntry(nil),
			expectedResult: true,
		},
		"both enabled are equal": {
			a:              makeEntry(enabled),
			b:              makeEntry(enabled),
			expectedResult: true,
		},
		"both disabled are equal": {
			a:              makeEntry(disabled),
			b:              makeEntry(disabled),
			expectedResult: true,
		},
		// This was the regression: toggling enabled→disabled was not detected.
		"enabled vs disabled are NOT equal": {
			a:              makeEntry(enabled),
			b:              makeEntry(disabled),
			expectedResult: false,
		},
		"disabled vs enabled are NOT equal": {
			a:              makeEntry(disabled),
			b:              makeEntry(enabled),
			expectedResult: false,
		},
		// nil ExtAuthz (inherit gateway default) vs explicit override are NOT equal.
		"nil vs enabled are NOT equal": {
			a:              makeEntry(nil),
			b:              makeEntry(enabled),
			expectedResult: false,
		},
		"nil vs disabled are NOT equal": {
			a:              makeEntry(nil),
			b:              makeEntry(disabled),
			expectedResult: false,
		},
	}

	for name, tc := range testCases {
		name, tc := name, tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expectedResult, EntriesEqual(tc.a, tc.b))
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for gateway-status equality helpers (GatewayStatusesEqual, etc.)
// ---------------------------------------------------------------------------

func TestGatewayStatusesEqual(t *testing.T) {
	t.Parallel()

	addrType := gwv1beta1.IPAddressType
	addrTypeOther := gwv1beta1.HostnameAddressType
	groupA := gwv1beta1.Group("gateway.networking.k8s.io")
	groupB := gwv1beta1.Group("other.io")

	base := gwv1beta1.GatewayStatus{
		Addresses: []gwv1beta1.GatewayAddress{
			{Type: &addrType, Value: "1.2.3.4"},
		},
		Conditions: []metav1.Condition{
			{Type: "Ready", Status: "True", Reason: "OK", Message: "all good", ObservedGeneration: 1},
		},
		Listeners: []gwv1beta1.ListenerStatus{
			{
				Name:           "listener1",
				AttachedRoutes: 2,
				SupportedKinds: []gwv1beta1.RouteGroupKind{
					{Group: &groupA, Kind: "HTTPRoute"},
				},
				Conditions: []metav1.Condition{
					{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"},
				},
			},
		},
	}

	require.True(t, GatewayStatusesEqual(base, base))

	// Different address value
	diffAddrValue := base
	diffAddrValue.Addresses = []gwv1beta1.GatewayAddress{{Type: &addrType, Value: "9.9.9.9"}}
	require.False(t, GatewayStatusesEqual(base, diffAddrValue))

	// Different address type
	diffAddrType := base
	diffAddrType.Addresses = []gwv1beta1.GatewayAddress{{Type: &addrTypeOther, Value: "1.2.3.4"}}
	require.False(t, GatewayStatusesEqual(base, diffAddrType))

	// One address type nil
	nilAddrType := base
	nilAddrType.Addresses = []gwv1beta1.GatewayAddress{{Type: nil, Value: "1.2.3.4"}}
	require.False(t, GatewayStatusesEqual(base, nilAddrType))

	// Both address types nil — equal
	bothNilType := gwv1beta1.GatewayStatus{
		Addresses: []gwv1beta1.GatewayAddress{{Type: nil, Value: "1.2.3.4"}},
	}
	require.True(t, GatewayStatusesEqual(bothNilType, bothNilType))

	// Different condition message
	diffCond := base
	diffCond.Conditions = []metav1.Condition{
		{Type: "Ready", Status: "True", Reason: "OK", Message: "different"},
	}
	require.False(t, GatewayStatusesEqual(base, diffCond))

	// Different listener name
	diffListener := base
	diffListener.Listeners = []gwv1beta1.ListenerStatus{
		{
			Name:           "other-listener",
			AttachedRoutes: 2,
			SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: &groupA, Kind: "HTTPRoute"}},
			Conditions:     []metav1.Condition{{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"}},
		},
	}
	require.False(t, GatewayStatusesEqual(base, diffListener))

	// Different listener attached routes
	diffAttached := base
	diffAttached.Listeners = []gwv1beta1.ListenerStatus{
		{
			Name:           "listener1",
			AttachedRoutes: 99,
			SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: &groupA, Kind: "HTTPRoute"}},
			Conditions:     []metav1.Condition{{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"}},
		},
	}
	require.False(t, GatewayStatusesEqual(base, diffAttached))

	// Different supported-kind group
	diffGroup := base
	diffGroup.Listeners = []gwv1beta1.ListenerStatus{
		{
			Name:           "listener1",
			AttachedRoutes: 2,
			SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: &groupB, Kind: "HTTPRoute"}},
			Conditions:     []metav1.Condition{{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"}},
		},
	}
	require.False(t, GatewayStatusesEqual(base, diffGroup))

	// Nil group on one side
	nilGroup := base
	nilGroup.Listeners = []gwv1beta1.ListenerStatus{
		{
			Name:           "listener1",
			AttachedRoutes: 2,
			SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: nil, Kind: "HTTPRoute"}},
			Conditions:     []metav1.Condition{{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"}},
		},
	}
	require.False(t, GatewayStatusesEqual(base, nilGroup))

	// Different kind
	diffKind := base
	diffKind.Listeners = []gwv1beta1.ListenerStatus{
		{
			Name:           "listener1",
			AttachedRoutes: 2,
			SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: &groupA, Kind: "TCPRoute"}},
			Conditions:     []metav1.Condition{{Type: "Ready", Status: "True", Reason: "OK", Message: "ok"}},
		},
	}
	require.False(t, GatewayStatusesEqual(base, diffKind))
}

func TestGatewayPolicyStatusesEqual(t *testing.T) {
	t.Parallel()

	base := v1alpha1.CustomGatewayPolicyStatus{
		Conditions: []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "Accepted", Message: "ok", ObservedGeneration: 1},
		},
	}

	require.True(t, GatewayPolicyStatusesEqual(base, base))

	diffMsg := v1alpha1.CustomGatewayPolicyStatus{
		Conditions: []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "Accepted", Message: "different", ObservedGeneration: 1},
		},
	}
	require.False(t, GatewayPolicyStatusesEqual(base, diffMsg))

	empty := v1alpha1.CustomGatewayPolicyStatus{}
	require.True(t, GatewayPolicyStatusesEqual(empty, empty))
}

func TestRouteAuthFilterStatusesEqual(t *testing.T) {
	t.Parallel()

	base := v1alpha1.RouteAuthFilterStatus{
		Conditions: []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "Accepted", Message: "ok"},
		},
	}

	require.True(t, RouteAuthFilterStatusesEqual(base, base))

	diffReason := v1alpha1.RouteAuthFilterStatus{
		Conditions: []metav1.Condition{
			{Type: "Accepted", Status: "True", Reason: "InvalidReason", Message: "ok"},
		},
	}
	require.False(t, RouteAuthFilterStatusesEqual(base, diffReason))
}

// ---------------------------------------------------------------------------
// Tests for nil-branch paths in apiGatewayPoliciesEqual / equalJWTProviders /
// providersEqual / equalClaims
// ---------------------------------------------------------------------------

func TestEntryComparator_NilPolicies(t *testing.T) {
	t.Parallel()

	e := entryComparator{}

	// Both nil → equal.
	require.True(t, e.apiGatewayPoliciesEqual(nil, nil))

	pol := &api.APIGatewayPolicy{JWT: nil}
	// One nil, one non-nil → not equal.
	require.False(t, e.apiGatewayPoliciesEqual(nil, pol))
	require.False(t, e.apiGatewayPoliciesEqual(pol, nil))
}

func TestEntryComparator_NilJWTProviders(t *testing.T) {
	t.Parallel()

	e := entryComparator{}

	// Both nil → equal.
	require.True(t, e.equalJWTProviders(nil, nil))

	req := &api.APIGatewayJWTRequirement{Providers: nil}
	// One nil → not equal.
	require.False(t, e.equalJWTProviders(nil, req))
	require.False(t, e.equalJWTProviders(req, nil))
}

func TestProvidersEqual_NilBranches(t *testing.T) {
	t.Parallel()

	// Both nil.
	require.True(t, providersEqual(nil, nil))

	p := &api.APIGatewayJWTProvider{Name: "okta"}
	require.False(t, providersEqual(nil, p))
	require.False(t, providersEqual(p, nil))
}

func TestEqualClaims_NilAndPathBranches(t *testing.T) {
	t.Parallel()

	// Both nil.
	require.True(t, equalClaims(nil, nil))

	c := &api.APIGatewayJWTClaimVerification{Value: "x", Path: []string{"a"}}
	require.False(t, equalClaims(nil, c))
	require.False(t, equalClaims(c, nil))

	// Different value.
	other := &api.APIGatewayJWTClaimVerification{Value: "y", Path: []string{"a"}}
	require.False(t, equalClaims(c, other))

	// Different path length.
	longerPath := &api.APIGatewayJWTClaimVerification{Value: "x", Path: []string{"a", "b"}}
	require.False(t, equalClaims(c, longerPath))

	// Same value, same path.
	same := &api.APIGatewayJWTClaimVerification{Value: "x", Path: []string{"a"}}
	require.True(t, equalClaims(c, same))

	// Same length but different path contents.
	diffPath := &api.APIGatewayJWTClaimVerification{Value: "x", Path: []string{"b"}}
	require.False(t, equalClaims(c, diffPath))
}

// ---------------------------------------------------------------------------
// EntriesEqual — TCPRoute, Certificate, and type-mismatch branches
// ---------------------------------------------------------------------------

func TestEntriesEqual_TCPRoute(t *testing.T) {
	t.Parallel()

	base := &api.TCPRouteConfigEntry{
		Kind:      api.TCPRoute,
		Name:      "tcp-route",
		Namespace: "default",
		Partition: "default",
		Parents: []api.ResourceReference{
			{Kind: api.APIGateway, Name: "gw", Namespace: "default", Partition: "default"},
		},
		Services: []api.TCPService{
			{Name: "backend", Namespace: "default", Partition: "default"},
		},
	}

	require.True(t, EntriesEqual(base, base))

	// Different name.
	diffName := *base
	diffName.Name = "other"
	require.False(t, EntriesEqual(base, &diffName))

	// Different service name.
	diffSvc := *base
	diffSvc.Services = []api.TCPService{{Name: "other-svc", Namespace: "default", Partition: "default"}}
	require.False(t, EntriesEqual(base, &diffSvc))

	// Type mismatch (TCPRoute vs HTTPRoute) → false.
	http := &api.HTTPRouteConfigEntry{Kind: api.HTTPRoute, Name: "tcp-route"}
	require.False(t, EntriesEqual(base, http))
}

func TestEntriesEqual_Certificate(t *testing.T) {
	t.Parallel()

	base := &api.FileSystemCertificateConfigEntry{
		Kind:        api.FileSystemCertificate,
		Name:        "my-cert",
		Namespace:   "default",
		Partition:   "default",
		Certificate: "cert-data",
		PrivateKey:  "key-data",
	}

	require.True(t, EntriesEqual(base, base))

	// Different certificate.
	diffCert := *base
	diffCert.Certificate = "other-cert"
	require.False(t, EntriesEqual(base, &diffCert))

	// Different private key.
	diffKey := *base
	diffKey.PrivateKey = "other-key"
	require.False(t, EntriesEqual(base, &diffKey))

	// Different name.
	diffName := *base
	diffName.Name = "other-cert"
	require.False(t, EntriesEqual(base, &diffName))
}

func TestEntriesEqual_UnknownType(t *testing.T) {
	t.Parallel()

	// ConfigEntry type not handled → false.
	a := &api.IngressGatewayConfigEntry{Kind: "ingress-gateway", Name: "x"}
	b := &api.IngressGatewayConfigEntry{Kind: "ingress-gateway", Name: "x"}
	require.False(t, EntriesEqual(a, b))
}

// ---------------------------------------------------------------------------
// HTTPRoute sub-comparators: header, query, header-filter, urlRewrite, retry,
// timeout, jwtFilter
// ---------------------------------------------------------------------------

func TestEntryComparator_HTTPHeaderMatchesEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.HTTPHeaderMatch{Match: api.HTTPHeaderMatchExact, Name: "X-Foo", Value: "bar"}

	require.True(t, e.httpHeaderMatchesEqual(base, base))

	diffMatch := base
	diffMatch.Match = api.HTTPHeaderMatchPrefix
	require.False(t, e.httpHeaderMatchesEqual(base, diffMatch))

	diffName := base
	diffName.Name = "X-Other"
	require.False(t, e.httpHeaderMatchesEqual(base, diffName))

	diffValue := base
	diffValue.Value = "other"
	require.False(t, e.httpHeaderMatchesEqual(base, diffValue))
}

func TestEntryComparator_HTTPQueryMatchesEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.HTTPQueryMatch{Match: api.HTTPQueryMatchExact, Name: "q", Value: "v"}

	require.True(t, e.httpQueryMatchesEqual(base, base))

	diffMatch := base
	diffMatch.Match = api.HTTPQueryMatchRegularExpression
	require.False(t, e.httpQueryMatchesEqual(base, diffMatch))

	diffName := base
	diffName.Name = "other"
	require.False(t, e.httpQueryMatchesEqual(base, diffName))

	diffValue := base
	diffValue.Value = "other"
	require.False(t, e.httpQueryMatchesEqual(base, diffValue))
}

func TestEntryComparator_HTTPHeaderFiltersEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.HTTPHeaderFilter{
		Add:    map[string]string{"X-Add": "v"},
		Set:    map[string]string{"X-Set": "v"},
		Remove: []string{"X-Remove"},
	}

	require.True(t, e.httpHeaderFiltersEqual(base, base))

	diffAdd := base
	diffAdd.Add = map[string]string{"X-Add": "other"}
	require.False(t, e.httpHeaderFiltersEqual(base, diffAdd))

	diffSet := base
	diffSet.Set = map[string]string{"X-Set": "other"}
	require.False(t, e.httpHeaderFiltersEqual(base, diffSet))

	diffRemove := base
	diffRemove.Remove = []string{"X-Other"}
	require.False(t, e.httpHeaderFiltersEqual(base, diffRemove))
}

func TestEntryComparator_URLRewritesEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	a := api.URLRewrite{Path: "/prefix"}
	b := api.URLRewrite{Path: "/other"}

	require.True(t, e.urlRewritesEqual(a, a))
	require.False(t, e.urlRewritesEqual(a, b))
}

func TestEntryComparator_RetryFiltersEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.RetryFilter{
		NumRetries:            uint32(3),
		RetryOnConnectFailure: true,
		RetryOn:               []string{"5xx"},
		RetryOnStatusCodes:    []uint32{503},
	}

	require.True(t, e.retryFiltersEqual(base, base))

	diffRetries := base
	diffRetries.NumRetries = 5
	require.False(t, e.retryFiltersEqual(base, diffRetries))

	diffConnFail := base
	diffConnFail.RetryOnConnectFailure = false
	require.False(t, e.retryFiltersEqual(base, diffConnFail))

	diffRetryOn := base
	diffRetryOn.RetryOn = []string{"reset"}
	require.False(t, e.retryFiltersEqual(base, diffRetryOn))

	diffStatusCodes := base
	diffStatusCodes.RetryOnStatusCodes = []uint32{500}
	require.False(t, e.retryFiltersEqual(base, diffStatusCodes))
}

func TestEntryComparator_TimeoutFiltersEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.TimeoutFilter{RequestTimeout: 10, IdleTimeout: 30}

	require.True(t, e.timeoutFiltersEqual(base, base))

	diffReq := base
	diffReq.RequestTimeout = 5
	require.False(t, e.timeoutFiltersEqual(base, diffReq))

	diffIdle := base
	diffIdle.IdleTimeout = 60
	require.False(t, e.timeoutFiltersEqual(base, diffIdle))
}

func TestEntryComparator_JWTFiltersEqual(t *testing.T) {
	t.Parallel()

	e := entryComparator{}
	base := api.JWTFilter{
		Providers: []*api.APIGatewayJWTProvider{
			{Name: "okta", VerifyClaims: []*api.APIGatewayJWTClaimVerification{
				{Path: []string{"role"}, Value: "admin"},
			}},
		},
	}

	require.True(t, e.jwtFiltersEqual(base, base))

	// Different number of providers → false via len check.
	extraProvider := api.JWTFilter{
		Providers: []*api.APIGatewayJWTProvider{
			{Name: "okta"},
			{Name: "auth0"},
		},
	}
	require.False(t, e.jwtFiltersEqual(base, extraProvider))

	// Same count, different provider name.
	diffProvider := api.JWTFilter{
		Providers: []*api.APIGatewayJWTProvider{
			{Name: "auth0"},
		},
	}
	require.False(t, e.jwtFiltersEqual(base, diffProvider))

	// Empty on both sides → equal.
	empty := api.JWTFilter{}
	require.True(t, e.jwtFiltersEqual(empty, empty))
}

// ---------------------------------------------------------------------------
// Nil-pointer guards on outer wrapper functions
// ---------------------------------------------------------------------------

func TestEntriesEqual_NilPointers(t *testing.T) {
	t.Parallel()

	// apiGatewaysEqual: nil a or b → false.
	require.False(t, EntriesEqual((*api.APIGatewayConfigEntry)(nil), &api.APIGatewayConfigEntry{}))
	require.False(t, EntriesEqual(&api.APIGatewayConfigEntry{}, (*api.APIGatewayConfigEntry)(nil)))

	// httpRoutesEqual: nil a or b → false.
	require.False(t, EntriesEqual((*api.HTTPRouteConfigEntry)(nil), &api.HTTPRouteConfigEntry{}))
	require.False(t, EntriesEqual(&api.HTTPRouteConfigEntry{}, (*api.HTTPRouteConfigEntry)(nil)))

	// tcpRoutesEqual: nil a or b → false.
	require.False(t, EntriesEqual((*api.TCPRouteConfigEntry)(nil), &api.TCPRouteConfigEntry{}))
	require.False(t, EntriesEqual(&api.TCPRouteConfigEntry{}, (*api.TCPRouteConfigEntry)(nil)))

	// certificatesEqual: nil a or b → false.
	require.False(t, EntriesEqual((*api.FileSystemCertificateConfigEntry)(nil), &api.FileSystemCertificateConfigEntry{}))
	require.False(t, EntriesEqual(&api.FileSystemCertificateConfigEntry{}, (*api.FileSystemCertificateConfigEntry)(nil)))
}
