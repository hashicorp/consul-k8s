// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// Test MatchesConsul for cases that should return true.
func TestJWTProvider_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    JWTProvider
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta",
				},
				Spec: JWTProviderSpec{},
			},
			Theirs: &capi.JWTProviderConfigEntry{
				Kind:        capi.JWTProvider,
				Name:        "test-okta",
				Namespace:   "default",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			Matches: true,
		},
		"all fields set matches": {
			Ours: JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta2",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Local: &LocalJWKS{
							JWKS:     "jwks-string",
							Filename: "jwks-file",
						},
						Remote: &RemoteJWKS{
							URI:                 "https://jwks.example.com",
							RequestTimeoutMs:    567,
							CacheDuration:       metav1.Duration{Duration: 890},
							FetchAsynchronously: true,
							RetryPolicy: &JWKSRetryPolicy{
								NumRetries: 1,
								RetryPolicyBackOff: &RetryPolicyBackOff{
									BaseInterval: metav1.Duration{Duration: 23},
									MaxInterval:  metav1.Duration{Duration: 456},
								},
							},
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									CaCertificateProviderInstance: &JWKSTLSCertProviderInstance{
										InstanceName:    "InstanceName",
										CertificateName: "ROOTCA",
									},
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename:            "cert.crt",
										EnvironmentVariable: "env-variable",
										InlineString:        "inline-string",
										InlineBytes:         []byte("inline-bytes"),
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
					Issuer:    "test-issuer",
					Audiences: []string{"aud1", "aud2"},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name:        "jwt-header",
								ValuePrefix: "my-bearer",
								Forward:     true,
							},
						},
						{
							QueryParam: &JWTLocationQueryParam{
								Name: "jwt-query-param",
							},
						},
						{
							Cookie: &JWTLocationCookie{
								Name: "jwt-cookie",
							},
						},
					},
					Forwarding: &JWTForwardingConfig{
						HeaderName:              "jwt-forward-header",
						PadForwardPayloadHeader: true,
					},
					ClockSkewSeconds: 357,
					CacheConfig: &JWTCacheConfig{
						Size: 468,
					},
				},
			},
			Theirs: &capi.JWTProviderConfigEntry{
				Kind:      capi.JWTProvider,
				Name:      "test-okta2",
				Namespace: "default",
				JSONWebKeySet: &capi.JSONWebKeySet{
					Local: &capi.LocalJWKS{
						JWKS:     "jwks-string",
						Filename: "jwks-file",
					},
					Remote: &capi.RemoteJWKS{
						URI:                 "https://jwks.example.com",
						RequestTimeoutMs:    567,
						CacheDuration:       890,
						FetchAsynchronously: true,
						RetryPolicy: &capi.JWKSRetryPolicy{
							NumRetries: 1,
							RetryPolicyBackOff: &capi.RetryPolicyBackOff{
								BaseInterval: 23,
								MaxInterval:  456,
							},
						},
						JWKSCluster: &capi.JWKSCluster{
							DiscoveryType: "STRICT_DNS",
							TLSCertificates: &capi.JWKSTLSCertificate{
								CaCertificateProviderInstance: &capi.JWKSTLSCertProviderInstance{
									InstanceName:    "InstanceName",
									CertificateName: "ROOTCA",
								},
								TrustedCA: &capi.JWKSTLSCertTrustedCA{
									Filename:            "cert.crt",
									EnvironmentVariable: "env-variable",
									InlineString:        "inline-string",
									InlineBytes:         []byte("inline-bytes"),
								},
							},
							ConnectTimeout: 890,
						},
					},
				},
				Issuer:    "test-issuer",
				Audiences: []string{"aud1", "aud2"},
				Locations: []*capi.JWTLocation{
					{
						Header: &capi.JWTLocationHeader{
							Name:        "jwt-header",
							ValuePrefix: "my-bearer",
							Forward:     true,
						},
					},
					{
						QueryParam: &capi.JWTLocationQueryParam{
							Name: "jwt-query-param",
						},
					},
					{
						Cookie: &capi.JWTLocationCookie{
							Name: "jwt-cookie",
						},
					},
				},
				Forwarding: &capi.JWTForwardingConfig{
					HeaderName:              "jwt-forward-header",
					PadForwardPayloadHeader: true,
				},
				ClockSkewSeconds: 357,
				CacheConfig: &capi.JWTCacheConfig{
					Size: 468,
				},
			},
			Matches: true,
		},
		"mismatched types does not match": {
			Ours: JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta3",
				},
				Spec: JWTProviderSpec{},
			},
			Theirs:  &capi.JWTProviderConfigEntry{},
			Matches: false,
		},
	}
	for name, c := range cases {
		c := c
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestJWTProvider_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours JWTProvider
		Exp  *capi.JWTProviderConfigEntry
	}{
		"empty fields": {
			Ours: JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta1",
				},
				Spec: JWTProviderSpec{},
			},
			Exp: &capi.JWTProviderConfigEntry{
				Kind: capi.JWTProvider,
				Name: "test-okta1",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta2",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Local: &LocalJWKS{
							JWKS:     "jwks-string",
							Filename: "jwks-file",
						},
						Remote: &RemoteJWKS{
							URI:                 "https://jwks.example.com",
							RequestTimeoutMs:    567,
							CacheDuration:       metav1.Duration{Duration: 890},
							FetchAsynchronously: true,
							RetryPolicy: &JWKSRetryPolicy{
								NumRetries: 1,
								RetryPolicyBackOff: &RetryPolicyBackOff{
									BaseInterval: metav1.Duration{Duration: 23},
									MaxInterval:  metav1.Duration{Duration: 456},
								},
							},
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									CaCertificateProviderInstance: &JWKSTLSCertProviderInstance{
										InstanceName:    "InstanceName",
										CertificateName: "ROOTCA",
									},
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename:            "cert.crt",
										EnvironmentVariable: "env-variable",
										InlineString:        "inline-string",
										InlineBytes:         []byte("inline-bytes"),
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
					Issuer:    "test-issuer",
					Audiences: []string{"aud1", "aud2"},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name:        "jwt-header",
								ValuePrefix: "my-bearer",
								Forward:     true,
							},
						},
						{
							QueryParam: &JWTLocationQueryParam{
								Name: "jwt-query-param",
							},
						},
						{
							Cookie: &JWTLocationCookie{
								Name: "jwt-cookie",
							},
						},
					},
					Forwarding: &JWTForwardingConfig{
						HeaderName:              "jwt-forward-header",
						PadForwardPayloadHeader: true,
					},
					ClockSkewSeconds: 357,
					CacheConfig: &JWTCacheConfig{
						Size: 468,
					},
				},
			},
			Exp: &capi.JWTProviderConfigEntry{
				Kind: capi.JWTProvider,
				Name: "test-okta2",
				JSONWebKeySet: &capi.JSONWebKeySet{
					Local: &capi.LocalJWKS{
						JWKS:     "jwks-string",
						Filename: "jwks-file",
					},
					Remote: &capi.RemoteJWKS{
						URI:                 "https://jwks.example.com",
						RequestTimeoutMs:    567,
						CacheDuration:       890,
						FetchAsynchronously: true,
						RetryPolicy: &capi.JWKSRetryPolicy{
							NumRetries: 1,
							RetryPolicyBackOff: &capi.RetryPolicyBackOff{
								BaseInterval: 23,
								MaxInterval:  456,
							},
						},
						JWKSCluster: &capi.JWKSCluster{
							DiscoveryType: "STRICT_DNS",
							TLSCertificates: &capi.JWKSTLSCertificate{
								CaCertificateProviderInstance: &capi.JWKSTLSCertProviderInstance{
									InstanceName:    "InstanceName",
									CertificateName: "ROOTCA",
								},
								TrustedCA: &capi.JWKSTLSCertTrustedCA{
									Filename:            "cert.crt",
									EnvironmentVariable: "env-variable",
									InlineString:        "inline-string",
									InlineBytes:         []byte("inline-bytes"),
								},
							},
							ConnectTimeout: 890,
						},
					},
				},
				Issuer:    "test-issuer",
				Audiences: []string{"aud1", "aud2"},
				Locations: []*capi.JWTLocation{
					{
						Header: &capi.JWTLocationHeader{
							Name:        "jwt-header",
							ValuePrefix: "my-bearer",
							Forward:     true,
						},
					},
					{
						QueryParam: &capi.JWTLocationQueryParam{
							Name: "jwt-query-param",
						},
					},
					{
						Cookie: &capi.JWTLocationCookie{
							Name: "jwt-cookie",
						},
					},
				},
				Forwarding: &capi.JWTForwardingConfig{
					HeaderName:              "jwt-forward-header",
					PadForwardPayloadHeader: true,
				},
				ClockSkewSeconds: 357,
				CacheConfig: &capi.JWTCacheConfig{
					Size: 468,
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul("datacenter")
			mesh, ok := act.(*capi.JWTProviderConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, mesh)
		})
	}
}

func TestJWTProvider_Validate(t *testing.T) {
	cases := map[string]struct {
		input           *JWTProvider
		expectedErrMsgs []string
		consulMeta      common.ConsulMeta
	}{
		"valid - local jwks": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-okta1",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Local: &LocalJWKS{
							Filename: "jwks.txt",
						},
					},
				},
				Status: Status{},
			},
			expectedErrMsgs: nil,
		},

		"valid - remote jwks": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwt-provider",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI:                 "https://jwks.example.com",
							FetchAsynchronously: true,
						},
					},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name: "Authorization",
							},
						},
					},
					Forwarding: &JWTForwardingConfig{
						HeaderName: "jwt-forward-header",
					},
				},
			},
			expectedErrMsgs: nil,
		},

		"valid - remote jwks with all fields with trustedCa": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwt-provider",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI:                 "https://jwks.example.com",
							RequestTimeoutMs:    5000,
							CacheDuration:       metav1.Duration{Duration: 10 * time.Second},
							FetchAsynchronously: true,
							RetryPolicy: &JWKSRetryPolicy{
								NumRetries: 3,
								RetryPolicyBackOff: &RetryPolicyBackOff{
									BaseInterval: metav1.Duration{Duration: 5 * time.Second},
									MaxInterval:  metav1.Duration{Duration: 20 * time.Second},
								},
							},
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename: "cert.crt",
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
					Issuer:    "test-issuer",
					Audiences: []string{"aud1", "aud2"},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name:        "Authorization",
								ValuePrefix: "Bearer",
								Forward:     true,
							},
						},
						{
							QueryParam: &JWTLocationQueryParam{
								Name: "access-token",
							},
						},
						{
							Cookie: &JWTLocationCookie{
								Name: "session-id",
							},
						},
					},
					Forwarding: &JWTForwardingConfig{
						HeaderName:              "jwt-forward-header",
						PadForwardPayloadHeader: true,
					},
					ClockSkewSeconds: 20,
					CacheConfig: &JWTCacheConfig{
						Size: 30,
					},
				},
			},
			expectedErrMsgs: nil,
		},

		"valid - remote jwks with all fields with CaCertificateProviderInstance": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwt-provider",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI:                 "https://jwks.example.com",
							RequestTimeoutMs:    5000,
							CacheDuration:       metav1.Duration{Duration: 10 * time.Second},
							FetchAsynchronously: true,
							RetryPolicy: &JWKSRetryPolicy{
								NumRetries: 3,
								RetryPolicyBackOff: &RetryPolicyBackOff{
									BaseInterval: metav1.Duration{Duration: 5 * time.Second},
									MaxInterval:  metav1.Duration{Duration: 20 * time.Second},
								},
							},
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									CaCertificateProviderInstance: &JWKSTLSCertProviderInstance{
										InstanceName:    "InstanceName",
										CertificateName: "ROOTCA",
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
					Issuer:    "test-issuer",
					Audiences: []string{"aud1", "aud2"},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name:        "Authorization",
								ValuePrefix: "Bearer",
								Forward:     true,
							},
						},
						{
							QueryParam: &JWTLocationQueryParam{
								Name: "access-token",
							},
						},
						{
							Cookie: &JWTLocationCookie{
								Name: "session-id",
							},
						},
					},
					Forwarding: &JWTForwardingConfig{
						HeaderName:              "jwt-forward-header",
						PadForwardPayloadHeader: true,
					},
					ClockSkewSeconds: 20,
					CacheConfig: &JWTCacheConfig{
						Size: 30,
					},
				},
			},
			expectedErrMsgs: nil,
		},

		"invalid - nil jwks": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-no-jwks",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: nil,
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-no-jwks" is invalid: spec.jsonWebKeySet: Invalid value: "null": jsonWebKeySet is required`,
			},
		},

		"invalid - empty jwks": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-no-jwks",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-no-jwks" is invalid: spec.jsonWebKeySet: Invalid value: "{}": exactly one of 'local' or 'remote' is required`,
			},
		},

		"invalid - local jwks with non-base64 string": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-base64",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Local: &LocalJWKS{
							JWKS: "not base64 encoded",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-base64" is invalid: spec.jsonWebKeySet.local.jwks: Invalid value: "not base64 encoded": JWKS must be a valid base64-encoded string`,
			},
		},

		"invalid - both local and remote jwks set": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-local-and-remote",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Local: &LocalJWKS{Filename: "jwks.txt"},
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-local-and-remote" is invalid: spec.jsonWebKeySet: Invalid value: "{\"local\":{\"filename\":\"jwks.txt\"},\"remote\":{\"uri\":\"https://jwks.example.com\",\"cacheDuration\":\"0s\"}}": exactly one of 'local' or 'remote' is required`,
			},
		},

		"invalid - remote jwks missing uri": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-missing-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							FetchAsynchronously: true,
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-missing-uri" is invalid: spec.jsonWebKeySet.remote.uri: Invalid value: "": remote JWKS URI is required`,
			},
		},

		"invalid - remote jwks invalid uri": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-invalid-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "invalid-uri",
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-invalid-uri" is invalid: spec.jsonWebKeySet.remote.uri: Invalid value: "invalid-uri": remote JWKS URI is invalid`,
			},
		},

		"invalid - remote jwks invalid jwkcluster - all TLSCertificates fields set": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-invalid-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									CaCertificateProviderInstance: &JWKSTLSCertProviderInstance{
										InstanceName: "InstanceName",
									},
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename: "cert.crt",
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-invalid-uri" is invalid: spec.jsonWebKeySet.remote.jwksCluster.tlsCertificates: Invalid value:`,
				`exactly one of 'trustedCa' or 'caCertificateProviderInstance' is required`,
			},
		},

		"invalid - remote jwks invalid jwkcluster - invalid discovery type": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-invalid-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
							JWKSCluster: &JWKSCluster{
								DiscoveryType:  "FAKE_DNS",
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-invalid-uri" is invalid: spec.jsonWebKeySet.remote.jwksCluster.discoveryType: Invalid value: "FAKE_DNS": unsupported jwks cluster discovery type.`,
			},
		},

		"invalid - remote jwks invalid jwkcluster - all trustedCa fields set": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-invalid-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename:            "cert.crt",
										EnvironmentVariable: "env-variable",
										InlineString:        "inline-string",
										InlineBytes:         []byte("inline-bytes"),
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-invalid-uri" is invalid: spec.jsonWebKeySet.remote.jwksCluster.tlsCertificates.trustedCa: Invalid value:`,
				`exactly one of 'filename', 'environmentVariable', 'inlineString' or 'inlineBytes' is required`,
			},
		},

		"invalid - remote jwks invalid jwkcluster - set 2 trustedCa fields": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-invalid-uri",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
							JWKSCluster: &JWKSCluster{
								DiscoveryType: "STRICT_DNS",
								TLSCertificates: &JWKSTLSCertificate{
									TrustedCA: &JWKSTLSCertTrustedCA{
										Filename:            "cert.crt",
										EnvironmentVariable: "env-variable",
									},
								},
								ConnectTimeout: metav1.Duration{Duration: 890},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-invalid-uri" is invalid: spec.jsonWebKeySet.remote.jwksCluster.tlsCertificates.trustedCa: Invalid value:`,
				`exactly one of 'filename', 'environmentVariable', 'inlineString' or 'inlineBytes' is required`,
			},
		},

		"invalid - JWT location with all fields": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-all-locations",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
						},
					},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name: "jwt-header",
							},
							QueryParam: &JWTLocationQueryParam{
								Name: "jwt-query-param",
							},
							Cookie: &JWTLocationCookie{
								Name: "jwt-cookie",
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-all-locations" is invalid: spec.locations[0]: Invalid value: "{\"header\":{\"name\":\"jwt-header\"},\"queryParam\":{\"name\":\"jwt-query-param\"},\"cookie\":{\"name\":\"jwt-cookie\"}}": exactly one of 'header', 'queryParam', or 'cookie' is required`,
			},
		},

		"invalid - JWT location with two fields": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-two-locations",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
						},
					},
					Locations: []*JWTLocation{
						{
							Header: &JWTLocationHeader{
								Name: "jwt-header",
							},
							Cookie: &JWTLocationCookie{
								Name: "jwt-cookie",
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-two-locations" is invalid: spec.locations[0]: Invalid value: "{\"header\":{\"name\":\"jwt-header\"},\"cookie\":{\"name\":\"jwt-cookie\"}}": exactly one of 'header', 'queryParam', or 'cookie' is required`,
			},
		},

		"invalid - remote jwks retry policy maxInterval < baseInterval": {
			input: &JWTProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-jwks-retry-intervals",
				},
				Spec: JWTProviderSpec{
					JSONWebKeySet: &JSONWebKeySet{
						Remote: &RemoteJWKS{
							URI: "https://jwks.example.com",
							RetryPolicy: &JWKSRetryPolicy{
								NumRetries: 0,
								RetryPolicyBackOff: &RetryPolicyBackOff{
									BaseInterval: metav1.Duration{Duration: 100 * time.Second},
									MaxInterval:  metav1.Duration{Duration: 10 * time.Second},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`jwtprovider.consul.hashicorp.com "test-jwks-retry-intervals" is invalid: spec.jsonWebKeySet.remote.retryPolicy.retryPolicyBackOff: Invalid value: "{\"baseInterval\":\"1m40s\",\"maxInterval\":\"10s\"}": maxInterval should be greater or equal to baseInterval`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(testCase.consulMeta)
			if len(testCase.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range testCase.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}

}

func TestJWTProvider_AddFinalizer(t *testing.T) {
	jwt := &JWTProvider{}
	jwt.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, jwt.ObjectMeta.Finalizers)
}

func TestJWTProvider_RemoveFinalizer(t *testing.T) {
	jwt := &JWTProvider{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	jwt.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, jwt.ObjectMeta.Finalizers)
}

func TestJWTProvider_SetSyncedCondition(t *testing.T) {
	jwt := &JWTProvider{}
	jwt.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, jwt.Status.Conditions[0].Status)
	require.Equal(t, "reason", jwt.Status.Conditions[0].Reason)
	require.Equal(t, "message", jwt.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, jwt.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestJWTProvider_SetLastSyncedTime(t *testing.T) {
	jwt := &JWTProvider{}
	syncedTime := metav1.NewTime(time.Now())
	jwt.SetLastSyncedTime(&syncedTime)
	require.Equal(t, &syncedTime, jwt.Status.LastSyncedTime)
}

func TestJWTProvider_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			jwt := &JWTProvider{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, jwt.SyncedConditionStatus())
		})
	}
}

func TestJWTProvider_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&JWTProvider{}).GetCondition(ConditionSynced))
}

func TestJWTProvider_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&JWTProvider{}).SyncedConditionStatus())
}

func TestJWTProvider_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&JWTProvider{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestJWTProvider_ConsulKind(t *testing.T) {
	require.Equal(t, capi.JWTProvider, (&JWTProvider{}).ConsulKind())
}

func TestJWTProvider_KubeKind(t *testing.T) {
	require.Equal(t, "jwtprovider", (&JWTProvider{}).KubeKind())
}

func TestJWTProvider_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&JWTProvider{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestJWTProvider_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&JWTProvider{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestJWTProvider_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&JWTProvider{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestJWTProvider_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&JWTProvider{}).ConsulGlobalResource())
}

func TestJWTProvider_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	jwt := &JWTProvider{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, jwt.GetObjectMeta())
}
