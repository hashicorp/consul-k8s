// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestEntryComparator_APIGatewayListenerTLSConfigurationsEqual_SDS(t *testing.T) {
	t.Parallel()

	comparator := entryComparator{}
	base := api.APIGatewayTLSConfiguration{
		SDS: &api.GatewayTLSSDSConfig{ClusterName: "sds", CertResource: "cert"},
	}

	require.True(t, comparator.apiGatewayListenerTLSConfigurationsEqual(base, base))

	differentResource := base
	differentResource.SDS = &api.GatewayTLSSDSConfig{ClusterName: "sds", CertResource: "different-cert"}
	require.False(t, comparator.apiGatewayListenerTLSConfigurationsEqual(base, differentResource))

	noSDS := base
	noSDS.SDS = nil
	require.False(t, comparator.apiGatewayListenerTLSConfigurationsEqual(base, noSDS))
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

func TestRouteExtProcStatusesEqual(t *testing.T) {
	t.Parallel()

	base := v1alpha1.Status{
		Conditions: []v1alpha1.Condition{
			{
				Type:    "Accepted",
				Status:  "True",
				Reason:  "Accepted",
				Message: "route ext_proc filter accepted",
			},
		},
	}

	require.True(t, RouteExtProcStatusesEqual(base, base))

	// LastTransitionTime is intentionally ignored.
	withTime := v1alpha1.Status{
		Conditions: []v1alpha1.Condition{
			{
				Type:               "Accepted",
				Status:             "True",
				Reason:             "Accepted",
				Message:            "route ext_proc filter accepted",
				LastTransitionTime: metav1.Now(),
			},
		},
	}
	require.True(t, RouteExtProcStatusesEqual(base, withTime))

	different := v1alpha1.Status{
		Conditions: []v1alpha1.Condition{
			{
				Type:    "Accepted",
				Status:  "False",
				Reason:  "Invalid",
				Message: `route ext_proc filter sets "overrides" but mode is not "override"`,
			},
		},
	}
	require.False(t, RouteExtProcStatusesEqual(base, different))

	require.False(t, RouteExtProcStatusesEqual(base, v1alpha1.Status{}))
}

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

// TestEntriesEqual_APIGateway_Protocol verifies that a protocol change on a
// Consul APIGatewayListener (e.g. http → grpc, http → http2) is detected by
// EntriesEqual so the reconciler writes the updated config entry to Consul.
func TestEntriesEqual_APIGateway_Protocol(t *testing.T) {
	t.Parallel()

	makeGateway := func(protocol string) *api.APIGatewayConfigEntry {
		return &api.APIGatewayConfigEntry{
			Kind:      api.APIGateway,
			Name:      "edge",
			Namespace: "default",
			Partition: "default",
			Listeners: []api.APIGatewayListener{
				{Name: "listener", Port: 9080, Protocol: protocol},
			},
		}
	}

	tests := []struct {
		name   string
		a, b   *api.APIGatewayConfigEntry
		wantEq bool
	}{
		{
			name:   "same protocol http is equal",
			a:      makeGateway("http"),
			b:      makeGateway("http"),
			wantEq: true,
		},
		{
			name:   "same protocol grpc is equal",
			a:      makeGateway("grpc"),
			b:      makeGateway("grpc"),
			wantEq: true,
		},
		{
			name:   "same protocol http2 is equal",
			a:      makeGateway("http2"),
			b:      makeGateway("http2"),
			wantEq: true,
		},
		{
			// protocols should be compared case-insensitively (EqualFold)
			name:   "protocol GRPC vs grpc is equal (case-insensitive)",
			a:      makeGateway("GRPC"),
			b:      makeGateway("grpc"),
			wantEq: true,
		},
		{
			// Day-2 update: operator adds grpc annotation → protocol changes http→grpc.
			// The reconciler must detect inequality and write the update to Consul.
			name:   "http vs grpc are NOT equal (day-2 protocol change)",
			a:      makeGateway("http"),
			b:      makeGateway("grpc"),
			wantEq: false,
		},
		{
			name:   "http vs http2 are NOT equal (day-2 protocol change)",
			a:      makeGateway("http"),
			b:      makeGateway("http2"),
			wantEq: false,
		},
		{
			name:   "grpc vs http2 are NOT equal",
			a:      makeGateway("grpc"),
			b:      makeGateway("http2"),
			wantEq: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantEq, EntriesEqual(tt.a, tt.b))
		})
	}
}

// TestEntriesEqual_APIGateway_ExtAuthz exercises the bug-fix path: changes to
// the gateway-wide ExtAuthz field must be detected as unequal so the reconciler
// writes the updated config entry to Consul.
func TestEntriesEqual_APIGateway_ExtAuthz(t *testing.T) {
	t.Parallel()

	makeGateway := func(extAuthz *api.APIGatewayExtAuthz) *api.APIGatewayConfigEntry {
		return &api.APIGatewayConfigEntry{
			Kind:      api.APIGateway,
			Name:      "edge",
			Namespace: "default",
			Partition: "default",
			Listeners: []api.APIGatewayListener{
				{Name: "listener", Port: 9080, Protocol: "http"},
			},
			ExtAuthz: extAuthz,
		}
	}

	enabled := &api.APIGatewayExtAuthz{Enabled: true}
	disabled := &api.APIGatewayExtAuthz{Enabled: false}

	tests := []struct {
		name   string
		a, b   *api.APIGatewayConfigEntry
		wantEq bool
	}{
		{
			name:   "both nil ExtAuthz are equal",
			a:      makeGateway(nil),
			b:      makeGateway(nil),
			wantEq: true,
		},
		{
			name:   "both enabled are equal",
			a:      makeGateway(enabled),
			b:      makeGateway(enabled),
			wantEq: true,
		},
		{
			name:   "both disabled are equal",
			a:      makeGateway(disabled),
			b:      makeGateway(disabled),
			wantEq: true,
		},
		{
			// Day-2 update: operator disables ext_authz on the gateway.
			name:   "enabled vs disabled are NOT equal",
			a:      makeGateway(enabled),
			b:      makeGateway(disabled),
			wantEq: false,
		},
		{
			name:   "disabled vs enabled are NOT equal",
			a:      makeGateway(disabled),
			b:      makeGateway(enabled),
			wantEq: false,
		},
		{
			name:   "nil vs enabled are NOT equal",
			a:      makeGateway(nil),
			b:      makeGateway(enabled),
			wantEq: false,
		},
		{
			name:   "nil vs disabled are NOT equal",
			a:      makeGateway(nil),
			b:      makeGateway(disabled),
			wantEq: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantEq, EntriesEqual(tt.a, tt.b))
		})
	}
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

	tests := []struct {
		name   string
		a, b   *api.HTTPRouteConfigEntry
		wantEq bool
	}{
		{
			name:   "both nil ExtAuthz are equal",
			a:      makeEntry(nil),
			b:      makeEntry(nil),
			wantEq: true,
		},
		{
			name:   "both enabled are equal",
			a:      makeEntry(enabled),
			b:      makeEntry(enabled),
			wantEq: true,
		},
		{
			name:   "both disabled are equal",
			a:      makeEntry(disabled),
			b:      makeEntry(disabled),
			wantEq: true,
		},
		{
			// This was the regression: toggling enabled→disabled was not detected.
			name:   "enabled vs disabled are NOT equal",
			a:      makeEntry(enabled),
			b:      makeEntry(disabled),
			wantEq: false,
		},
		{
			name:   "disabled vs enabled are NOT equal",
			a:      makeEntry(disabled),
			b:      makeEntry(enabled),
			wantEq: false,
		},
		{
			name:   "nil vs enabled are NOT equal",
			a:      makeEntry(nil),
			b:      makeEntry(enabled),
			wantEq: false,
		},
		{
			name:   "nil vs disabled are NOT equal",
			a:      makeEntry(nil),
			b:      makeEntry(disabled),
			wantEq: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantEq, EntriesEqual(tt.a, tt.b))
		})
	}
}
