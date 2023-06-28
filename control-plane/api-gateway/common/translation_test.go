// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	logrtest "github.com/go-logr/logr/testing"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul/api"
)

type fakeReferenceValidator struct{}

func (v fakeReferenceValidator) GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) bool {
	return true
}

func (v fakeReferenceValidator) HTTPRouteCanReferenceBackend(httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
}

func (v fakeReferenceValidator) TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
}

func TestTranslator_Namespace(t *testing.T) {
	testCases := []struct {
		EnableConsulNamespaces bool
		ConsulDestNamespace    string
		EnableK8sMirroring     bool
		MirroringPrefix        string
		Input, ExpectedOutput  string
	}{
		{
			EnableConsulNamespaces: false,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     false,
			MirroringPrefix:        "",
			Input:                  "namespace-1",
			ExpectedOutput:         "",
		},
		{
			EnableConsulNamespaces: false,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     true,
			MirroringPrefix:        "",
			Input:                  "namespace-1",
			ExpectedOutput:         "",
		},
		{
			EnableConsulNamespaces: false,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     true,
			MirroringPrefix:        "pre-",
			Input:                  "namespace-1",
			ExpectedOutput:         "",
		},
		{
			EnableConsulNamespaces: true,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     false,
			MirroringPrefix:        "",
			Input:                  "namespace-1",
			ExpectedOutput:         "default",
		},
		{
			EnableConsulNamespaces: true,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     true,
			MirroringPrefix:        "",
			Input:                  "namespace-1",
			ExpectedOutput:         "namespace-1",
		},
		{
			EnableConsulNamespaces: true,
			ConsulDestNamespace:    "default",
			EnableK8sMirroring:     true,
			MirroringPrefix:        "pre-",
			Input:                  "namespace-1",
			ExpectedOutput:         "pre-namespace-1",
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%s_%d", t.Name(), i), func(t *testing.T) {
			translator := ResourceTranslator{
				EnableConsulNamespaces: tc.EnableConsulNamespaces,
				ConsulDestNamespace:    tc.ConsulDestNamespace,
				EnableK8sMirroring:     tc.EnableK8sMirroring,
				MirroringPrefix:        tc.MirroringPrefix,
			}
			assert.Equal(t, tc.ExpectedOutput, translator.Namespace(tc.Input))
		})
	}
}

func TestTranslator_ToAPIGateway(t *testing.T) {
	t.Parallel()
	k8sObjectName := "my-k8s-gw"
	k8sNamespace := "my-k8s-namespace"

	// gw status
	gwLastTransmissionTime := time.Now()

	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "http"

	// listener one tls config
	listenerOneCertName := "one-cert"
	listenerOneCertK8sNamespace := "one-cert-ns"
	listenerOneCertConsulNamespace := "one-cert-ns"
	listenerOneCert := generateTestCertificate(t, "one-cert-ns", "one-cert")

	// listener one status
	listenerOneLastTransmissionTime := time.Now()

	// listener two configuration
	listenerTwoName := "listener-two"
	listenerTwoHostname := "*.consul.io"
	listenerTwoPort := 5432
	listenerTwoProtocol := "http"

	// listener one tls config
	listenerTwoCertName := "two-cert"
	listenerTwoCertK8sNamespace := "two-cert-ns"
	listenerTwoCertConsulNamespace := "two-cert-ns"
	listenerTwoCert := generateTestCertificate(t, "two-cert-ns", "two-cert")

	// listener two status
	listenerTwoLastTransmissionTime := time.Now()

	testCases := map[string]struct {
		annotations            map[string]string
		expectedGWName         string
		listenerOneK8sCertRefs []gwv1beta1.SecretObjectReference
	}{
		"gw name": {
			annotations:    make(map[string]string),
			expectedGWName: k8sObjectName,
			listenerOneK8sCertRefs: []gwv1beta1.SecretObjectReference{
				{
					Name:      gwv1beta1.ObjectName(listenerOneCertName),
					Namespace: PointerTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
			},
		},
		"when k8s has certs that are not referenced in consul": {
			annotations:    make(map[string]string),
			expectedGWName: k8sObjectName,
			listenerOneK8sCertRefs: []gwv1beta1.SecretObjectReference{
				{
					Name:      gwv1beta1.ObjectName(listenerOneCertName),
					Namespace: PointerTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
				{
					Name:      gwv1beta1.ObjectName("cert that won't exist in the translated type"),
					Namespace: PointerTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
				},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			input := gwv1beta1.Gateway{
				TypeMeta: metav1.TypeMeta{
					Kind: "Gateway",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:        k8sObjectName,
					Namespace:   k8sNamespace,
					Annotations: tc.annotations,
				},
				Spec: gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     gwv1beta1.SectionName(listenerOneName),
							Hostname: PointerTo(gwv1beta1.Hostname(listenerOneHostname)),
							Port:     gwv1beta1.PortNumber(listenerOnePort),
							Protocol: gwv1beta1.ProtocolType(listenerOneProtocol),
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: tc.listenerOneK8sCertRefs,
							},
						},
						{
							Name:     gwv1beta1.SectionName(listenerTwoName),
							Hostname: PointerTo(gwv1beta1.Hostname(listenerTwoHostname)),
							Port:     gwv1beta1.PortNumber(listenerTwoPort),
							Protocol: gwv1beta1.ProtocolType(listenerTwoProtocol),
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{
										Name:      gwv1beta1.ObjectName(listenerTwoCertName),
										Namespace: PointerTo(gwv1beta1.Namespace(listenerTwoCertK8sNamespace)),
									},
								},
							},
						},
					},
				},
				Status: gwv1beta1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gwv1beta1.GatewayConditionAccepted),
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: gwLastTransmissionTime},
							Reason:             string(gwv1beta1.GatewayReasonAccepted),
							Message:            "I'm accepted",
						},
					},
					Listeners: []gwv1beta1.ListenerStatus{
						{
							Name:           gwv1beta1.SectionName(listenerOneName),
							AttachedRoutes: 5,
							Conditions: []metav1.Condition{
								{
									Type:               string(gwv1beta1.GatewayConditionReady),
									Status:             metav1.ConditionTrue,
									LastTransitionTime: metav1.Time{Time: listenerOneLastTransmissionTime},
									Reason:             string(gwv1beta1.GatewayConditionReady),
									Message:            "I'm ready",
								},
							},
						},

						{
							Name:           gwv1beta1.SectionName(listenerTwoName),
							AttachedRoutes: 3,
							Conditions: []metav1.Condition{
								{
									Type:               string(gwv1beta1.GatewayConditionReady),
									Status:             metav1.ConditionTrue,
									LastTransitionTime: metav1.Time{Time: listenerTwoLastTransmissionTime},
									Reason:             string(gwv1beta1.GatewayConditionReady),
									Message:            "I'm also ready",
								},
							},
						},
					},
				},
			}

			expectedConfigEntry := &api.APIGatewayConfigEntry{
				Kind: api.APIGateway,
				Name: tc.expectedGWName,
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   k8sNamespace,
					constants.MetaKeyKubeName: k8sObjectName,
				},
				Listeners: []api.APIGatewayListener{
					{
						Name:     listenerOneName,
						Hostname: listenerOneHostname,
						Port:     listenerOnePort,
						Protocol: listenerOneProtocol,
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:      api.InlineCertificate,
									Name:      listenerOneCertName,
									Namespace: listenerOneCertConsulNamespace,
								},
							},
						},
					},
					{
						Name:     listenerTwoName,
						Hostname: listenerTwoHostname,
						Port:     listenerTwoPort,
						Protocol: listenerTwoProtocol,
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{
								{
									Kind:      api.InlineCertificate,
									Name:      listenerTwoCertName,
									Namespace: listenerTwoCertConsulNamespace,
								},
							},
						},
					},
				},
				Status:    api.ConfigEntryStatus{},
				Namespace: k8sNamespace,
			}
			translator := ResourceTranslator{
				EnableConsulNamespaces: true,
				ConsulDestNamespace:    "",
				EnableK8sMirroring:     true,
				MirroringPrefix:        "",
			}

			resources := NewResourceMap(translator, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
			resources.ReferenceCountCertificate(listenerOneCert)
			resources.ReferenceCountCertificate(listenerTwoCert)

			actualConfigEntry := translator.ToAPIGateway(input, resources)

			if diff := cmp.Diff(expectedConfigEntry, actualConfigEntry); diff != "" {
				t.Errorf("Translator.GatewayToAPIGateway() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTranslator_ToHTTPRoute(t *testing.T) {
	t.Parallel()
	type args struct {
		k8sHTTPRoute gwv1beta1.HTTPRoute
		services     []types.NamespacedName
		meshServices []v1alpha1.MeshService
	}
	tests := map[string]struct {
		args args
		want api.HTTPRouteConfigEntry
	}{
		"base test": {
			args: args{
				k8sHTTPRoute: gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "k8s-http-route",
						Namespace:   "k8s-ns",
						Annotations: map[string]string{},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
									Name:        gwv1beta1.ObjectName("api-gw"),
									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
								},
							},
						},
						Hostnames: []gwv1beta1.Hostname{
							"host-name.example.com",
							"consul.io",
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Matches: []gwv1beta1.HTTPRouteMatch{
									{
										Path: &gwv1beta1.HTTPPathMatch{
											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
											Value: PointerTo("/v1"),
										},
										Headers: []gwv1beta1.HTTPHeaderMatch{
											{
												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
												Name:  "my header match",
												Value: "the value",
											},
										},
										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
											{
												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
												Name:  "search",
												Value: "term",
											},
										},
										Method: PointerTo(gwv1beta1.HTTPMethodGet),
									},
								},
								Filters: []gwv1beta1.HTTPRouteFilter{
									{
										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
											Set: []gwv1beta1.HTTPHeader{
												{
													Name:  "Magic",
													Value: "v2",
												},
												{
													Name:  "Another One",
													Value: "dj khaled",
												},
											},
											Add: []gwv1beta1.HTTPHeader{
												{
													Name:  "add it on",
													Value: "the value",
												},
											},
											Remove: []string{"time to go"},
										},
										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
											Path: &gwv1beta1.HTTPPathModifier{
												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
												ReplacePrefixMatch: PointerTo("v1"),
											},
										},
									},
								},
								BackendRefs: []gwv1beta1.HTTPBackendRef{
									{
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "service one",
												Namespace: PointerTo(gwv1beta1.Namespace("other")),
											},
											Weight: PointerTo(int32(45)),
										},
										Filters: []gwv1beta1.HTTPRouteFilter{
											{
												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
													Set: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - Magic",
															Value: "svc - v2",
														},
														{
															Name:  "svc - Another One",
															Value: "svc - dj khaled",
														},
													},
													Add: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - add it on",
															Value: "svc - the value",
														},
													},
													Remove: []string{"svc - time to go"},
												},
												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
													Path: &gwv1beta1.HTTPPathModifier{
														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
														ReplacePrefixMatch: PointerTo("path"),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				services: []types.NamespacedName{
					{Name: "service one", Namespace: "other"},
				},
			},
			want: api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "k8s-http-route",
				Rules: []api.HTTPRouteRule{
					{
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"add it on": "the value",
									},
									Remove: []string{"time to go"},
									Set: map[string]string{
										"Magic":       "v2",
										"Another One": "dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{Path: "v1"},
						},
						Matches: []api.HTTPMatch{
							{
								Headers: []api.HTTPHeaderMatch{
									{
										Match: api.HTTPHeaderMatchExact,
										Name:  "my header match",
										Value: "the value",
									},
								},
								Method: api.HTTPMatchMethodGet,
								Path: api.HTTPPathMatch{
									Match: api.HTTPPathMatchPrefix,
									Value: "/v1",
								},
								Query: []api.HTTPQueryMatch{
									{
										Match: api.HTTPQueryMatchExact,
										Name:  "search",
										Value: "term",
									},
								},
							},
						},
						Services: []api.HTTPService{
							{
								Name:   "service one",
								Weight: 45,
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"svc - add it on": "svc - the value",
											},
											Remove: []string{"svc - time to go"},
											Set: map[string]string{
												"svc - Magic":       "svc - v2",
												"svc - Another One": "svc - dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{
										Path: "path",
									},
								},
								Namespace: "other",
							},
						},
					},
				},
				Hostnames: []string{
					"host-name.example.com",
					"consul.io",
				},
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   "k8s-ns",
					constants.MetaKeyKubeName: "k8s-http-route",
				},
				Namespace: "k8s-ns",
			},
		},
		"dropping path rewrites that are not prefix match": {
			args: args{
				k8sHTTPRoute: gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "k8s-http-route",
						Namespace: "k8s-ns",
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
									Name:        gwv1beta1.ObjectName("api-gw"),
									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
								},
							},
						},
						Hostnames: []gwv1beta1.Hostname{
							"host-name.example.com",
							"consul.io",
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Matches: []gwv1beta1.HTTPRouteMatch{
									{
										Path: &gwv1beta1.HTTPPathMatch{
											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
											Value: PointerTo("/v1"),
										},
										Headers: []gwv1beta1.HTTPHeaderMatch{
											{
												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
												Name:  "my header match",
												Value: "the value",
											},
										},
										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
											{
												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
												Name:  "search",
												Value: "term",
											},
										},
										Method: PointerTo(gwv1beta1.HTTPMethodGet),
									},
								},
								Filters: []gwv1beta1.HTTPRouteFilter{
									{
										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
											Set: []gwv1beta1.HTTPHeader{
												{
													Name:  "Magic",
													Value: "v2",
												},
												{
													Name:  "Another One",
													Value: "dj khaled",
												},
											},
											Add: []gwv1beta1.HTTPHeader{
												{
													Name:  "add it on",
													Value: "the value",
												},
											},
											Remove: []string{"time to go"},
										},
										// THIS IS THE CHANGE
										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
											Path: &gwv1beta1.HTTPPathModifier{
												Type:            gwv1beta1.FullPathHTTPPathModifier,
												ReplaceFullPath: PointerTo("v1"),
											},
										},
									},
								},
								BackendRefs: []gwv1beta1.HTTPBackendRef{
									{
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "service one",
												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
											},
											Weight: PointerTo(int32(45)),
										},
										Filters: []gwv1beta1.HTTPRouteFilter{
											{
												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
													Set: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - Magic",
															Value: "svc - v2",
														},
														{
															Name:  "svc - Another One",
															Value: "svc - dj khaled",
														},
													},
													Add: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - add it on",
															Value: "svc - the value",
														},
													},
													Remove: []string{"svc - time to go"},
												},
												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
													Path: &gwv1beta1.HTTPPathModifier{
														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
														ReplacePrefixMatch: PointerTo("path"),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				services: []types.NamespacedName{
					{Name: "service one", Namespace: "some ns"},
				},
			},
			want: api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "k8s-http-route",
				Rules: []api.HTTPRouteRule{
					{
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"add it on": "the value",
									},
									Remove: []string{"time to go"},
									Set: map[string]string{
										"Magic":       "v2",
										"Another One": "dj khaled",
									},
								},
							},
						},
						Matches: []api.HTTPMatch{
							{
								Headers: []api.HTTPHeaderMatch{
									{
										Match: api.HTTPHeaderMatchExact,
										Name:  "my header match",
										Value: "the value",
									},
								},
								Method: api.HTTPMatchMethodGet,
								Path: api.HTTPPathMatch{
									Match: api.HTTPPathMatchPrefix,
									Value: "/v1",
								},
								Query: []api.HTTPQueryMatch{
									{
										Match: api.HTTPQueryMatchExact,
										Name:  "search",
										Value: "term",
									},
								},
							},
						},
						Services: []api.HTTPService{
							{
								Name:   "service one",
								Weight: 45,
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"svc - add it on": "svc - the value",
											},
											Remove: []string{"svc - time to go"},
											Set: map[string]string{
												"svc - Magic":       "svc - v2",
												"svc - Another One": "svc - dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{
										Path: "path",
									},
								},
								Namespace: "some ns",
							},
						},
					},
				},
				Hostnames: []string{
					"host-name.example.com",
					"consul.io",
				},
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   "k8s-ns",
					constants.MetaKeyKubeName: "k8s-http-route",
				},
				Namespace: "k8s-ns",
			},
		},
		"parent ref that is not registered with consul is dropped": {
			args: args{
				k8sHTTPRoute: gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "k8s-http-route",
						Namespace:   "k8s-ns",
						Annotations: map[string]string{},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
									Name:        gwv1beta1.ObjectName("api-gw"),
									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
								},

								{
									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
									Name:        gwv1beta1.ObjectName("consul don't know about me"),
									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
								},
							},
						},
						Hostnames: []gwv1beta1.Hostname{
							"host-name.example.com",
							"consul.io",
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Matches: []gwv1beta1.HTTPRouteMatch{
									{
										Path: &gwv1beta1.HTTPPathMatch{
											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
											Value: PointerTo("/v1"),
										},
										Headers: []gwv1beta1.HTTPHeaderMatch{
											{
												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
												Name:  "my header match",
												Value: "the value",
											},
										},
										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
											{
												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
												Name:  "search",
												Value: "term",
											},
										},
										Method: PointerTo(gwv1beta1.HTTPMethodGet),
									},
								},
								Filters: []gwv1beta1.HTTPRouteFilter{
									{
										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
											Set: []gwv1beta1.HTTPHeader{
												{
													Name:  "Magic",
													Value: "v2",
												},
												{
													Name:  "Another One",
													Value: "dj khaled",
												},
											},
											Add: []gwv1beta1.HTTPHeader{
												{
													Name:  "add it on",
													Value: "the value",
												},
											},
											Remove: []string{"time to go"},
										},
										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
											Path: &gwv1beta1.HTTPPathModifier{
												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
												ReplacePrefixMatch: PointerTo("v1"),
											},
										},
									},
								},
								BackendRefs: []gwv1beta1.HTTPBackendRef{
									{
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "service one",
												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
											},
											Weight: PointerTo(int32(45)),
										},
										Filters: []gwv1beta1.HTTPRouteFilter{
											{
												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
													Set: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - Magic",
															Value: "svc - v2",
														},
														{
															Name:  "svc - Another One",
															Value: "svc - dj khaled",
														},
													},
													Add: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - add it on",
															Value: "svc - the value",
														},
													},
													Remove: []string{"svc - time to go"},
												},
												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
													Path: &gwv1beta1.HTTPPathModifier{
														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
														ReplacePrefixMatch: PointerTo("path"),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				services: []types.NamespacedName{
					{Name: "service one", Namespace: "some ns"},
				},
			},
			want: api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "k8s-http-route",
				Rules: []api.HTTPRouteRule{
					{
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"add it on": "the value",
									},
									Remove: []string{"time to go"},
									Set: map[string]string{
										"Magic":       "v2",
										"Another One": "dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{Path: "v1"},
						},
						Matches: []api.HTTPMatch{
							{
								Headers: []api.HTTPHeaderMatch{
									{
										Match: api.HTTPHeaderMatchExact,
										Name:  "my header match",
										Value: "the value",
									},
								},
								Method: api.HTTPMatchMethodGet,
								Path: api.HTTPPathMatch{
									Match: api.HTTPPathMatchPrefix,
									Value: "/v1",
								},
								Query: []api.HTTPQueryMatch{
									{
										Match: api.HTTPQueryMatchExact,
										Name:  "search",
										Value: "term",
									},
								},
							},
						},
						Services: []api.HTTPService{
							{
								Name:   "service one",
								Weight: 45,
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"svc - add it on": "svc - the value",
											},
											Remove: []string{"svc - time to go"},
											Set: map[string]string{
												"svc - Magic":       "svc - v2",
												"svc - Another One": "svc - dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{
										Path: "path",
									},
								},
								Namespace: "some ns",
							},
						},
					},
				},
				Hostnames: []string{
					"host-name.example.com",
					"consul.io",
				},
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   "k8s-ns",
					constants.MetaKeyKubeName: "k8s-http-route",
				},
				Namespace: "k8s-ns",
			},
		},
		"when section name on apigw is not supplied": {
			args: args{
				k8sHTTPRoute: gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "k8s-http-route",
						Namespace:   "k8s-ns",
						Annotations: map[string]string{},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Namespace: PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
									Name:      gwv1beta1.ObjectName("api-gw"),
									Kind:      PointerTo(gwv1beta1.Kind("Gateway")),
								},
							},
						},
						Hostnames: []gwv1beta1.Hostname{
							"host-name.example.com",
							"consul.io",
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Matches: []gwv1beta1.HTTPRouteMatch{
									{
										Path: &gwv1beta1.HTTPPathMatch{
											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
											Value: PointerTo("/v1"),
										},
										Headers: []gwv1beta1.HTTPHeaderMatch{
											{
												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
												Name:  "my header match",
												Value: "the value",
											},
										},
										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
											{
												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
												Name:  "search",
												Value: "term",
											},
										},
										Method: PointerTo(gwv1beta1.HTTPMethodGet),
									},
								},
								Filters: []gwv1beta1.HTTPRouteFilter{
									{
										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
											Set: []gwv1beta1.HTTPHeader{
												{
													Name:  "Magic",
													Value: "v2",
												},
												{
													Name:  "Another One",
													Value: "dj khaled",
												},
											},
											Add: []gwv1beta1.HTTPHeader{
												{
													Name:  "add it on",
													Value: "the value",
												},
											},
											Remove: []string{"time to go"},
										},
										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
											Path: &gwv1beta1.HTTPPathModifier{
												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
												ReplacePrefixMatch: PointerTo("v1"),
											},
										},
									},
								},
								BackendRefs: []gwv1beta1.HTTPBackendRef{
									{
										// this ref should get dropped
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "service two",
												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
											},
										},
									},
									{
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "some-service-part-three",
												Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
												Group:     PointerTo(gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup)),
												Kind:      PointerTo(gwv1beta1.Kind(v1alpha1.MeshServiceKind)),
											},
										},
									},
									{
										BackendRef: gwv1beta1.BackendRef{
											BackendObjectReference: gwv1beta1.BackendObjectReference{
												Name:      "service one",
												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
											},
											Weight: PointerTo(int32(45)),
										},
										Filters: []gwv1beta1.HTTPRouteFilter{
											{
												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
													Set: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - Magic",
															Value: "svc - v2",
														},
														{
															Name:  "svc - Another One",
															Value: "svc - dj khaled",
														},
													},
													Add: []gwv1beta1.HTTPHeader{
														{
															Name:  "svc - add it on",
															Value: "svc - the value",
														},
													},
													Remove: []string{"svc - time to go"},
												},
												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
													Path: &gwv1beta1.HTTPPathModifier{
														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
														ReplacePrefixMatch: PointerTo("path"),
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
				services: []types.NamespacedName{
					{Name: "service one", Namespace: "some ns"},
				},
				meshServices: []v1alpha1.MeshService{
					{ObjectMeta: metav1.ObjectMeta{Name: "some-service-part-three", Namespace: "svc-ns"}, Spec: v1alpha1.MeshServiceSpec{Name: "some-override"}},
				},
			},
			want: api.HTTPRouteConfigEntry{
				Kind: api.HTTPRoute,
				Name: "k8s-http-route",
				Rules: []api.HTTPRouteRule{
					{
						Filters: api.HTTPFilters{
							Headers: []api.HTTPHeaderFilter{
								{
									Add: map[string]string{
										"add it on": "the value",
									},
									Remove: []string{"time to go"},
									Set: map[string]string{
										"Magic":       "v2",
										"Another One": "dj khaled",
									},
								},
							},
							URLRewrite: &api.URLRewrite{Path: "v1"},
						},
						Matches: []api.HTTPMatch{
							{
								Headers: []api.HTTPHeaderMatch{
									{
										Match: api.HTTPHeaderMatchExact,
										Name:  "my header match",
										Value: "the value",
									},
								},
								Method: api.HTTPMatchMethodGet,
								Path: api.HTTPPathMatch{
									Match: api.HTTPPathMatchPrefix,
									Value: "/v1",
								},
								Query: []api.HTTPQueryMatch{
									{
										Match: api.HTTPQueryMatchExact,
										Name:  "search",
										Value: "term",
									},
								},
							},
						},
						Services: []api.HTTPService{
							{Name: "some-override", Namespace: "svc-ns", Weight: 1, Filters: api.HTTPFilters{Headers: []api.HTTPHeaderFilter{{Add: make(map[string]string), Set: make(map[string]string)}}}},
							{
								Name:   "service one",
								Weight: 45,
								Filters: api.HTTPFilters{
									Headers: []api.HTTPHeaderFilter{
										{
											Add: map[string]string{
												"svc - add it on": "svc - the value",
											},
											Remove: []string{"svc - time to go"},
											Set: map[string]string{
												"svc - Magic":       "svc - v2",
												"svc - Another One": "svc - dj khaled",
											},
										},
									},
									URLRewrite: &api.URLRewrite{
										Path: "path",
									},
								},
								Namespace: "some ns",
							},
						},
					},
				},
				Hostnames: []string{
					"host-name.example.com",
					"consul.io",
				},
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   "k8s-ns",
					constants.MetaKeyKubeName: "k8s-http-route",
				},
				Namespace: "k8s-ns",
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tr := ResourceTranslator{
				EnableConsulNamespaces: true,
				EnableK8sMirroring:     true,
			}

			resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
			for _, service := range tc.args.services {
				resources.AddService(service, service.Name)
			}
			for _, service := range tc.args.meshServices {
				resources.AddMeshService(service)
			}

			got := tr.ToHTTPRoute(tc.args.k8sHTTPRoute, resources)
			if diff := cmp.Diff(&tc.want, got); diff != "" {
				t.Errorf("Translator.ToHTTPRoute() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTranslator_ToTCPRoute(t *testing.T) {
	t.Parallel()
	type args struct {
		k8sRoute     gwv1alpha2.TCPRoute
		services     []types.NamespacedName
		meshServices []v1alpha1.MeshService
	}
	tests := map[string]struct {
		args args
		want api.TCPRouteConfigEntry
	}{
		"base test": {
			args: args{
				k8sRoute: gwv1alpha2.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tcp-route",
						Namespace: "k8s-ns",
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						Rules: []gwv1alpha2.TCPRouteRule{
							{
								BackendRefs: []gwv1beta1.BackendRef{
									{
										BackendObjectReference: gwv1beta1.BackendObjectReference{
											Name:      "some-service",
											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
										},
										Weight: new(int32),
									},
								},
							},
							{
								BackendRefs: []gwv1beta1.BackendRef{
									{
										BackendObjectReference: gwv1beta1.BackendObjectReference{
											Name:      "some-service-part-two",
											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
										},
										Weight: new(int32),
									},
									{
										BackendObjectReference: gwv1beta1.BackendObjectReference{
											Group:     PointerTo(gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup)),
											Kind:      PointerTo(gwv1beta1.Kind(v1alpha1.MeshServiceKind)),
											Name:      "some-service-part-three",
											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
										},
										Weight: new(int32),
									},
								},
							},
						},
					},
				},
				services: []types.NamespacedName{
					{Name: "some-service", Namespace: "svc-ns"},
					{Name: "some-service-part-two", Namespace: "svc-ns"},
				},
				meshServices: []v1alpha1.MeshService{
					{ObjectMeta: metav1.ObjectMeta{Name: "some-service-part-three", Namespace: "svc-ns"}, Spec: v1alpha1.MeshServiceSpec{Name: "some-override"}},
				},
			},
			want: api.TCPRouteConfigEntry{
				Kind:      api.TCPRoute,
				Name:      "tcp-route",
				Namespace: "k8s-ns",
				Services: []api.TCPService{
					{
						Name:      "some-service",
						Partition: "",
						Namespace: "svc-ns",
					},
					{
						Name:      "some-service-part-two",
						Partition: "",
						Namespace: "svc-ns",
					},
					{
						Name:      "some-override",
						Partition: "",
						Namespace: "svc-ns",
					},
				},
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   "k8s-ns",
					constants.MetaKeyKubeName: "tcp-route",
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			tr := ResourceTranslator{
				EnableConsulNamespaces: true,
				EnableK8sMirroring:     true,
			}

			resources := NewResourceMap(tr, fakeReferenceValidator{}, logrtest.NewTestLogger(t))
			for _, service := range tt.args.services {
				resources.AddService(service, service.Name)
			}
			for _, service := range tt.args.meshServices {
				resources.AddMeshService(service)
			}

			got := tr.ToTCPRoute(tt.args.k8sRoute, resources)
			if diff := cmp.Diff(&tt.want, got); diff != "" {
				t.Errorf("Translator.TCPRouteToTCPRoute() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func generateTestCertificate(t *testing.T, namespace, name string) corev1.Secret {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)

	usage := x509.KeyUsageCertSign
	expiration := time.Now().AddDate(10, 0, 0)

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "consul.test",
		},
		IsCA:                  true,
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              expiration,
		SubjectKeyId:          []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              usage,
		BasicConstraintsValid: true,
	}
	caCert := cert
	caPrivateKey := privateKey

	data, err := x509.CreateCertificate(rand.Reader, cert, caCert, &privateKey.PublicKey, caPrivateKey)
	require.NoError(t, err)

	certBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: data,
	})

	privateKeyBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       certBytes,
			corev1.TLSPrivateKeyKey: privateKeyBytes,
		},
	}
}

func TestResourceTranslator_translateHTTPFilters(t1 *testing.T) {
	type fields struct {
		EnableConsulNamespaces bool
		ConsulDestNamespace    string
		EnableK8sMirroring     bool
		MirroringPrefix        string
		ConsulPartition        string
		Datacenter             string
	}
	type args struct {
		filters []gwv1beta1.HTTPRouteFilter
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   api.HTTPFilters
	}{
		{
			name:   "no httproutemodifier set",
			fields: fields{},
			args: args{
				filters: []gwv1beta1.HTTPRouteFilter{
					{
						URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{},
					},
				},
			},
			want: api.HTTPFilters{
				Headers: []api.HTTPHeaderFilter{
					{
						Add: map[string]string{},
						Set: map[string]string{},
					},
				},
				URLRewrite: nil,
			},
		},
	}
	for _, tt := range tests {
		t1.Run(tt.name, func(t1 *testing.T) {
			t := ResourceTranslator{
				EnableConsulNamespaces: tt.fields.EnableConsulNamespaces,
				ConsulDestNamespace:    tt.fields.ConsulDestNamespace,
				EnableK8sMirroring:     tt.fields.EnableK8sMirroring,
				MirroringPrefix:        tt.fields.MirroringPrefix,
				ConsulPartition:        tt.fields.ConsulPartition,
				Datacenter:             tt.fields.Datacenter,
			}
			assert.Equalf(t1, tt.want, t.translateHTTPFilters(tt.args.filters), "translateHTTPFilters(%v)", tt.args.filters)
		})
	}
}
