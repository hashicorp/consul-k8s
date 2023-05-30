// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	capi "github.com/hashicorp/consul/api"
)

type fakeReferenceValidator struct{}

func (v fakeReferenceValidator) GatewayCanReferenceSecret(gateway gwv1beta1.Gateway, secretRef gwv1beta1.SecretObjectReference) bool {
	return true
}
func (v fakeReferenceValidator) HTTPRouteCanReferenceGateway(httproute gwv1beta1.HTTPRoute, parentRef gwv1beta1.ParentReference) bool {
	return true
}
func (v fakeReferenceValidator) HTTPRouteCanReferenceBackend(httproute gwv1beta1.HTTPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
}
func (v fakeReferenceValidator) TCPRouteCanReferenceGateway(tcpRoute gwv1alpha2.TCPRoute, parentRef gwv1beta1.ParentReference) bool {
	return true
}
func (v fakeReferenceValidator) TCPRouteCanReferenceBackend(tcpRoute gwv1alpha2.TCPRoute, backendRef gwv1beta1.BackendRef) bool {
	return true
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

			expectedConfigEntry := &capi.APIGatewayConfigEntry{
				Kind: capi.APIGateway,
				Name: tc.expectedGWName,
				Meta: map[string]string{
					constants.MetaKeyKubeNS:   k8sNamespace,
					constants.MetaKeyKubeName: k8sObjectName,
				},
				Listeners: []capi.APIGatewayListener{
					{
						Name:     listenerOneName,
						Hostname: listenerOneHostname,
						Port:     listenerOnePort,
						Protocol: listenerOneProtocol,
						TLS: capi.APIGatewayTLSConfiguration{
							Certificates: []capi.ResourceReference{
								{
									Kind:      capi.InlineCertificate,
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
						TLS: capi.APIGatewayTLSConfiguration{
							Certificates: []capi.ResourceReference{
								{
									Kind:      capi.InlineCertificate,
									Name:      listenerTwoCertName,
									Namespace: listenerTwoCertConsulNamespace,
								},
							},
						},
					},
				},
				Status:    capi.ConfigEntryStatus{},
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

// func TestTranslator_HTTPRouteToHTTPRoute(t *testing.T) {
// 	t.Parallel()
// 	type args struct {
// 		k8sHTTPRoute gwv1beta1.HTTPRoute
// 		parentRefs   map[types.NamespacedName]api.ResourceReference
// 		services     map[types.NamespacedName]api.CatalogService
// 		meshServices map[types.NamespacedName]v1alpha1.MeshService
// 	}
// 	tests := map[string]struct {
// 		args args
// 		want capi.HTTPRouteConfigEntry
// 	}{
// 		"base test": {
// 			args: args{
// 				k8sHTTPRoute: gwv1beta1.HTTPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:        "k8s-http-route",
// 						Namespace:   "k8s-ns",
// 						Annotations: map[string]string{},
// 					},
// 					Spec: gwv1beta1.HTTPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:        gwv1beta1.ObjectName("api-gw"),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
// 								},
// 							},
// 						},
// 						Hostnames: []gwv1beta1.Hostname{
// 							"host-name.example.com",
// 							"consul.io",
// 						},
// 						Rules: []gwv1beta1.HTTPRouteRule{
// 							{
// 								Matches: []gwv1beta1.HTTPRouteMatch{
// 									{
// 										Path: &gwv1beta1.HTTPPathMatch{
// 											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
// 											Value: PointerTo("/v1"),
// 										},
// 										Headers: []gwv1beta1.HTTPHeaderMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
// 												Name:  "my header match",
// 												Value: "the value",
// 											},
// 										},
// 										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
// 												Name:  "search",
// 												Value: "term",
// 											},
// 										},
// 										Method: PointerTo(gwv1beta1.HTTPMethodGet),
// 									},
// 								},
// 								Filters: []gwv1beta1.HTTPRouteFilter{
// 									{
// 										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 											Set: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "Magic",
// 													Value: "v2",
// 												},
// 												{
// 													Name:  "Another One",
// 													Value: "dj khaled",
// 												},
// 											},
// 											Add: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "add it on",
// 													Value: "the value",
// 												},
// 											},
// 											Remove: []string{"time to go"},
// 										},
// 										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 											Path: &gwv1beta1.HTTPPathModifier{
// 												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 												ReplacePrefixMatch: PointerTo("v1"),
// 											},
// 										},
// 									},
// 								},
// 								BackendRefs: []gwv1beta1.HTTPBackendRef{
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service one",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 											Weight: PointerTo(int32(45)),
// 										},
// 										Filters: []gwv1beta1.HTTPRouteFilter{
// 											{
// 												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 													Set: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - Magic",
// 															Value: "svc - v2",
// 														},
// 														{
// 															Name:  "svc - Another One",
// 															Value: "svc - dj khaled",
// 														},
// 													},
// 													Add: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - add it on",
// 															Value: "svc - the value",
// 														},
// 													},
// 													Remove: []string{"svc - time to go"},
// 												},
// 												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 													Path: &gwv1beta1.HTTPPathModifier{
// 														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 														ReplacePrefixMatch: PointerTo("path"),
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{Name: "api-gw", Namespace: "k8s-gw-ns"}: {Name: "api-gw", Partition: "part-1", Namespace: "ns"},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "service one", Namespace: "some ns"}: {ServiceName: "service one", Namespace: "other"},
// 				},
// 			},
// 			want: capi.HTTPRouteConfigEntry{
// 				Kind: capi.HTTPRoute,
// 				Name: "k8s-http-route",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "api-gw",
// 						SectionName: "listener-1",
// 						Partition:   "part-1",
// 						Namespace:   "ns",
// 					},
// 				},
// 				Rules: []capi.HTTPRouteRule{
// 					{
// 						Filters: capi.HTTPFilters{
// 							Headers: []capi.HTTPHeaderFilter{
// 								{
// 									Add: map[string]string{
// 										"add it on": "the value",
// 									},
// 									Remove: []string{"time to go"},
// 									Set: map[string]string{
// 										"Magic":       "v2",
// 										"Another One": "dj khaled",
// 									},
// 								},
// 							},
// 							URLRewrite: &capi.URLRewrite{Path: "v1"},
// 						},
// 						Matches: []capi.HTTPMatch{
// 							{
// 								Headers: []capi.HTTPHeaderMatch{
// 									{
// 										Match: capi.HTTPHeaderMatchExact,
// 										Name:  "my header match",
// 										Value: "the value",
// 									},
// 								},
// 								Method: capi.HTTPMatchMethodGet,
// 								Path: capi.HTTPPathMatch{
// 									Match: capi.HTTPPathMatchPrefix,
// 									Value: "/v1",
// 								},
// 								Query: []capi.HTTPQueryMatch{
// 									{
// 										Match: capi.HTTPQueryMatchExact,
// 										Name:  "search",
// 										Value: "term",
// 									},
// 								},
// 							},
// 						},
// 						Services: []capi.HTTPService{
// 							{
// 								Name:   "service one",
// 								Weight: 45,
// 								Filters: capi.HTTPFilters{
// 									Headers: []capi.HTTPHeaderFilter{
// 										{
// 											Add: map[string]string{
// 												"svc - add it on": "svc - the value",
// 											},
// 											Remove: []string{"svc - time to go"},
// 											Set: map[string]string{
// 												"svc - Magic":       "svc - v2",
// 												"svc - Another One": "svc - dj khaled",
// 											},
// 										},
// 									},
// 									URLRewrite: &capi.URLRewrite{
// 										Path: "path",
// 									},
// 								},
// 								Namespace: "other",
// 							},
// 						},
// 					},
// 				},
// 				Hostnames: []string{
// 					"host-name.example.com",
// 					"consul.io",
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "k8s-http-route",
// 					metaKeyKubeName:        "k8s-http-route",
// 				},
// 				Namespace: "k8s-ns",
// 			},
// 		},
// 		"with httproute name override": {
// 			args: args{
// 				k8sHTTPRoute: gwv1beta1.HTTPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "k8s-http-route",
// 						Namespace: "k8s-ns",
// 						Annotations: map[string]string{
// 							AnnotationHTTPRoute: "overrrrride",
// 						},
// 					},
// 					Spec: gwv1beta1.HTTPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:        gwv1beta1.ObjectName("api-gw"),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
// 								},
// 							},
// 						},
// 						Hostnames: []gwv1beta1.Hostname{
// 							"host-name.example.com",
// 							"consul.io",
// 						},
// 						Rules: []gwv1beta1.HTTPRouteRule{
// 							{
// 								Matches: []gwv1beta1.HTTPRouteMatch{
// 									{
// 										Path: &gwv1beta1.HTTPPathMatch{
// 											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
// 											Value: PointerTo("/v1"),
// 										},
// 										Headers: []gwv1beta1.HTTPHeaderMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
// 												Name:  "my header match",
// 												Value: "the value",
// 											},
// 										},
// 										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
// 												Name:  "search",
// 												Value: "term",
// 											},
// 										},
// 										Method: PointerTo(gwv1beta1.HTTPMethodGet),
// 									},
// 								},
// 								Filters: []gwv1beta1.HTTPRouteFilter{
// 									{
// 										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 											Set: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "Magic",
// 													Value: "v2",
// 												},
// 												{
// 													Name:  "Another One",
// 													Value: "dj khaled",
// 												},
// 											},
// 											Add: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "add it on",
// 													Value: "the value",
// 												},
// 											},
// 											Remove: []string{"time to go"},
// 										},
// 										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 											Path: &gwv1beta1.HTTPPathModifier{
// 												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 												ReplacePrefixMatch: PointerTo("v1"),
// 											},
// 										},
// 									},
// 								},
// 								BackendRefs: []gwv1beta1.HTTPBackendRef{
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service one",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 											Weight: PointerTo(int32(45)),
// 										},
// 										Filters: []gwv1beta1.HTTPRouteFilter{
// 											{
// 												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 													Set: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - Magic",
// 															Value: "svc - v2",
// 														},
// 														{
// 															Name:  "svc - Another One",
// 															Value: "svc - dj khaled",
// 														},
// 													},
// 													Add: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - add it on",
// 															Value: "svc - the value",
// 														},
// 													},
// 													Remove: []string{"svc - time to go"},
// 												},
// 												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 													Path: &gwv1beta1.HTTPPathModifier{
// 														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 														ReplacePrefixMatch: PointerTo("path"),
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{Name: "api-gw", Namespace: "k8s-gw-ns"}: {Name: "api-gw", Partition: "part-1", Namespace: "ns"},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "service one", Namespace: "some ns"}: {ServiceName: "service one", Namespace: "some ns"},
// 				},
// 			},
// 			want: capi.HTTPRouteConfigEntry{
// 				Kind: capi.HTTPRoute,
// 				Name: "overrrrride",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "api-gw",
// 						SectionName: "listener-1",
// 						Partition:   "part-1",
// 						Namespace:   "ns",
// 					},
// 				},
// 				Rules: []capi.HTTPRouteRule{
// 					{
// 						Filters: capi.HTTPFilters{
// 							Headers: []capi.HTTPHeaderFilter{
// 								{
// 									Add: map[string]string{
// 										"add it on": "the value",
// 									},
// 									Remove: []string{"time to go"},
// 									Set: map[string]string{
// 										"Magic":       "v2",
// 										"Another One": "dj khaled",
// 									},
// 								},
// 							},
// 							URLRewrite: &capi.URLRewrite{Path: "v1"},
// 						},
// 						Matches: []capi.HTTPMatch{
// 							{
// 								Headers: []capi.HTTPHeaderMatch{
// 									{
// 										Match: capi.HTTPHeaderMatchExact,
// 										Name:  "my header match",
// 										Value: "the value",
// 									},
// 								},
// 								Method: capi.HTTPMatchMethodGet,
// 								Path: capi.HTTPPathMatch{
// 									Match: capi.HTTPPathMatchPrefix,
// 									Value: "/v1",
// 								},
// 								Query: []capi.HTTPQueryMatch{
// 									{
// 										Match: capi.HTTPQueryMatchExact,
// 										Name:  "search",
// 										Value: "term",
// 									},
// 								},
// 							},
// 						},
// 						Services: []capi.HTTPService{
// 							{
// 								Name:   "service one",
// 								Weight: 45,
// 								Filters: capi.HTTPFilters{
// 									Headers: []capi.HTTPHeaderFilter{
// 										{
// 											Add: map[string]string{
// 												"svc - add it on": "svc - the value",
// 											},
// 											Remove: []string{"svc - time to go"},
// 											Set: map[string]string{
// 												"svc - Magic":       "svc - v2",
// 												"svc - Another One": "svc - dj khaled",
// 											},
// 										},
// 									},
// 									URLRewrite: &capi.URLRewrite{
// 										Path: "path",
// 									},
// 								},
// 								Namespace: "some ns",
// 							},
// 						},
// 					},
// 				},
// 				Hostnames: []string{
// 					"host-name.example.com",
// 					"consul.io",
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "k8s-http-route",
// 					metaKeyKubeName:        "k8s-http-route",
// 				},
// 				Namespace: "k8s-ns",
// 			},
// 		},
// 		"dropping path rewrites that are not prefix match": {
// 			args: args{
// 				k8sHTTPRoute: gwv1beta1.HTTPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "k8s-http-route",
// 						Namespace: "k8s-ns",
// 						Annotations: map[string]string{
// 							AnnotationHTTPRoute: "overrrrride",
// 						},
// 					},
// 					Spec: gwv1beta1.HTTPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:        gwv1beta1.ObjectName("api-gw"),
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 								},
// 							},
// 						},
// 						Hostnames: []gwv1beta1.Hostname{
// 							"host-name.example.com",
// 							"consul.io",
// 						},
// 						Rules: []gwv1beta1.HTTPRouteRule{
// 							{
// 								Matches: []gwv1beta1.HTTPRouteMatch{
// 									{
// 										Path: &gwv1beta1.HTTPPathMatch{
// 											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
// 											Value: PointerTo("/v1"),
// 										},
// 										Headers: []gwv1beta1.HTTPHeaderMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
// 												Name:  "my header match",
// 												Value: "the value",
// 											},
// 										},
// 										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
// 												Name:  "search",
// 												Value: "term",
// 											},
// 										},
// 										Method: PointerTo(gwv1beta1.HTTPMethodGet),
// 									},
// 								},
// 								Filters: []gwv1beta1.HTTPRouteFilter{
// 									{
// 										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 											Set: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "Magic",
// 													Value: "v2",
// 												},
// 												{
// 													Name:  "Another One",
// 													Value: "dj khaled",
// 												},
// 											},
// 											Add: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "add it on",
// 													Value: "the value",
// 												},
// 											},
// 											Remove: []string{"time to go"},
// 										},
// 										// THIS IS THE CHANGE
// 										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 											Path: &gwv1beta1.HTTPPathModifier{
// 												Type:            gwv1beta1.FullPathHTTPPathModifier,
// 												ReplaceFullPath: PointerTo("v1"),
// 											},
// 										},
// 									},
// 								},
// 								BackendRefs: []gwv1beta1.HTTPBackendRef{
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service one",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 											Weight: PointerTo(int32(45)),
// 										},
// 										Filters: []gwv1beta1.HTTPRouteFilter{
// 											{
// 												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 													Set: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - Magic",
// 															Value: "svc - v2",
// 														},
// 														{
// 															Name:  "svc - Another One",
// 															Value: "svc - dj khaled",
// 														},
// 													},
// 													Add: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - add it on",
// 															Value: "svc - the value",
// 														},
// 													},
// 													Remove: []string{"svc - time to go"},
// 												},
// 												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 													Path: &gwv1beta1.HTTPPathModifier{
// 														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 														ReplacePrefixMatch: PointerTo("path"),
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{Name: "api-gw", Namespace: "k8s-gw-ns"}: {Name: "api-gw", Partition: "part-1", Namespace: "ns"},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "service one", Namespace: "some ns"}: {ServiceName: "service one", Namespace: "some ns"},
// 				},
// 			},
// 			want: capi.HTTPRouteConfigEntry{
// 				Kind: capi.HTTPRoute,
// 				Name: "overrrrride",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "api-gw",
// 						SectionName: "listener-1",
// 						Partition:   "part-1",
// 						Namespace:   "ns",
// 					},
// 				},
// 				Rules: []capi.HTTPRouteRule{
// 					{
// 						Filters: capi.HTTPFilters{
// 							Headers: []capi.HTTPHeaderFilter{
// 								{
// 									Add: map[string]string{
// 										"add it on": "the value",
// 									},
// 									Remove: []string{"time to go"},
// 									Set: map[string]string{
// 										"Magic":       "v2",
// 										"Another One": "dj khaled",
// 									},
// 								},
// 							},
// 						},
// 						Matches: []capi.HTTPMatch{
// 							{
// 								Headers: []capi.HTTPHeaderMatch{
// 									{
// 										Match: capi.HTTPHeaderMatchExact,
// 										Name:  "my header match",
// 										Value: "the value",
// 									},
// 								},
// 								Method: capi.HTTPMatchMethodGet,
// 								Path: capi.HTTPPathMatch{
// 									Match: capi.HTTPPathMatchPrefix,
// 									Value: "/v1",
// 								},
// 								Query: []capi.HTTPQueryMatch{
// 									{
// 										Match: capi.HTTPQueryMatchExact,
// 										Name:  "search",
// 										Value: "term",
// 									},
// 								},
// 							},
// 						},
// 						Services: []capi.HTTPService{
// 							{
// 								Name:   "service one",
// 								Weight: 45,
// 								Filters: capi.HTTPFilters{
// 									Headers: []capi.HTTPHeaderFilter{
// 										{
// 											Add: map[string]string{
// 												"svc - add it on": "svc - the value",
// 											},
// 											Remove: []string{"svc - time to go"},
// 											Set: map[string]string{
// 												"svc - Magic":       "svc - v2",
// 												"svc - Another One": "svc - dj khaled",
// 											},
// 										},
// 									},
// 									URLRewrite: &capi.URLRewrite{
// 										Path: "path",
// 									},
// 								},
// 								Namespace: "some ns",
// 							},
// 						},
// 					},
// 				},
// 				Hostnames: []string{
// 					"host-name.example.com",
// 					"consul.io",
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "k8s-http-route",
// 					metaKeyKubeName:        "k8s-http-route",
// 				},
// 				Namespace: "k8s-ns",
// 			},
// 		},

// 		"parent ref that is not registered with consul is dropped": {
// 			args: args{
// 				k8sHTTPRoute: gwv1beta1.HTTPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:        "k8s-http-route",
// 						Namespace:   "k8s-ns",
// 						Annotations: map[string]string{},
// 					},
// 					Spec: gwv1beta1.HTTPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:        gwv1beta1.ObjectName("api-gw"),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
// 								},

// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:        gwv1beta1.ObjectName("consul don't know about me"),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-1")),
// 								},
// 							},
// 						},
// 						Hostnames: []gwv1beta1.Hostname{
// 							"host-name.example.com",
// 							"consul.io",
// 						},
// 						Rules: []gwv1beta1.HTTPRouteRule{
// 							{
// 								Matches: []gwv1beta1.HTTPRouteMatch{
// 									{
// 										Path: &gwv1beta1.HTTPPathMatch{
// 											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
// 											Value: PointerTo("/v1"),
// 										},
// 										Headers: []gwv1beta1.HTTPHeaderMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
// 												Name:  "my header match",
// 												Value: "the value",
// 											},
// 										},
// 										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
// 												Name:  "search",
// 												Value: "term",
// 											},
// 										},
// 										Method: PointerTo(gwv1beta1.HTTPMethodGet),
// 									},
// 								},
// 								Filters: []gwv1beta1.HTTPRouteFilter{
// 									{
// 										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 											Set: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "Magic",
// 													Value: "v2",
// 												},
// 												{
// 													Name:  "Another One",
// 													Value: "dj khaled",
// 												},
// 											},
// 											Add: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "add it on",
// 													Value: "the value",
// 												},
// 											},
// 											Remove: []string{"time to go"},
// 										},
// 										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 											Path: &gwv1beta1.HTTPPathModifier{
// 												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 												ReplacePrefixMatch: PointerTo("v1"),
// 											},
// 										},
// 									},
// 								},
// 								BackendRefs: []gwv1beta1.HTTPBackendRef{
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service one",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 											Weight: PointerTo(int32(45)),
// 										},
// 										Filters: []gwv1beta1.HTTPRouteFilter{
// 											{
// 												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 													Set: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - Magic",
// 															Value: "svc - v2",
// 														},
// 														{
// 															Name:  "svc - Another One",
// 															Value: "svc - dj khaled",
// 														},
// 													},
// 													Add: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - add it on",
// 															Value: "svc - the value",
// 														},
// 													},
// 													Remove: []string{"svc - time to go"},
// 												},
// 												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 													Path: &gwv1beta1.HTTPPathModifier{
// 														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 														ReplacePrefixMatch: PointerTo("path"),
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{Name: "api-gw", Namespace: "k8s-gw-ns"}: {Name: "api-gw", Partition: "part-1", Namespace: "ns"},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "service one", Namespace: "some ns"}: {ServiceName: "service one", Namespace: "some ns"},
// 				},
// 			},
// 			want: capi.HTTPRouteConfigEntry{
// 				Kind: capi.HTTPRoute,
// 				Name: "k8s-http-route",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "api-gw",
// 						SectionName: "listener-1",
// 						Partition:   "part-1",
// 						Namespace:   "ns",
// 					},
// 				},
// 				Rules: []capi.HTTPRouteRule{
// 					{
// 						Filters: capi.HTTPFilters{
// 							Headers: []capi.HTTPHeaderFilter{
// 								{
// 									Add: map[string]string{
// 										"add it on": "the value",
// 									},
// 									Remove: []string{"time to go"},
// 									Set: map[string]string{
// 										"Magic":       "v2",
// 										"Another One": "dj khaled",
// 									},
// 								},
// 							},
// 							URLRewrite: &capi.URLRewrite{Path: "v1"},
// 						},
// 						Matches: []capi.HTTPMatch{
// 							{
// 								Headers: []capi.HTTPHeaderMatch{
// 									{
// 										Match: capi.HTTPHeaderMatchExact,
// 										Name:  "my header match",
// 										Value: "the value",
// 									},
// 								},
// 								Method: capi.HTTPMatchMethodGet,
// 								Path: capi.HTTPPathMatch{
// 									Match: capi.HTTPPathMatchPrefix,
// 									Value: "/v1",
// 								},
// 								Query: []capi.HTTPQueryMatch{
// 									{
// 										Match: capi.HTTPQueryMatchExact,
// 										Name:  "search",
// 										Value: "term",
// 									},
// 								},
// 							},
// 						},
// 						Services: []capi.HTTPService{
// 							{
// 								Name:   "service one",
// 								Weight: 45,
// 								Filters: capi.HTTPFilters{
// 									Headers: []capi.HTTPHeaderFilter{
// 										{
// 											Add: map[string]string{
// 												"svc - add it on": "svc - the value",
// 											},
// 											Remove: []string{"svc - time to go"},
// 											Set: map[string]string{
// 												"svc - Magic":       "svc - v2",
// 												"svc - Another One": "svc - dj khaled",
// 											},
// 										},
// 									},
// 									URLRewrite: &capi.URLRewrite{
// 										Path: "path",
// 									},
// 								},
// 								Namespace: "some ns",
// 							},
// 						},
// 					},
// 				},
// 				Hostnames: []string{
// 					"host-name.example.com",
// 					"consul.io",
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "k8s-http-route",
// 					metaKeyKubeName:        "k8s-http-route",
// 				},
// 				Namespace: "k8s-ns",
// 			},
// 		},
// 		"when section name on apigw is not supplied": {
// 			args: args{
// 				k8sHTTPRoute: gwv1beta1.HTTPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:        "k8s-http-route",
// 						Namespace:   "k8s-ns",
// 						Annotations: map[string]string{},
// 					},
// 					Spec: gwv1beta1.HTTPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace: PointerTo(gwv1beta1.Namespace("k8s-gw-ns")),
// 									Name:      gwv1beta1.ObjectName("api-gw"),
// 									Kind:      PointerTo(gwv1beta1.Kind("Gateway")),
// 								},
// 							},
// 						},
// 						Hostnames: []gwv1beta1.Hostname{
// 							"host-name.example.com",
// 							"consul.io",
// 						},
// 						Rules: []gwv1beta1.HTTPRouteRule{
// 							{
// 								Matches: []gwv1beta1.HTTPRouteMatch{
// 									{
// 										Path: &gwv1beta1.HTTPPathMatch{
// 											Type:  PointerTo(gwv1beta1.PathMatchPathPrefix),
// 											Value: PointerTo("/v1"),
// 										},
// 										Headers: []gwv1beta1.HTTPHeaderMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.HeaderMatchExact),
// 												Name:  "my header match",
// 												Value: "the value",
// 											},
// 										},
// 										QueryParams: []gwv1beta1.HTTPQueryParamMatch{
// 											{
// 												Type:  PointerTo(gwv1beta1.QueryParamMatchExact),
// 												Name:  "search",
// 												Value: "term",
// 											},
// 										},
// 										Method: PointerTo(gwv1beta1.HTTPMethodGet),
// 									},
// 								},
// 								Filters: []gwv1beta1.HTTPRouteFilter{
// 									{
// 										RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 											Set: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "Magic",
// 													Value: "v2",
// 												},
// 												{
// 													Name:  "Another One",
// 													Value: "dj khaled",
// 												},
// 											},
// 											Add: []gwv1beta1.HTTPHeader{
// 												{
// 													Name:  "add it on",
// 													Value: "the value",
// 												},
// 											},
// 											Remove: []string{"time to go"},
// 										},
// 										URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 											Path: &gwv1beta1.HTTPPathModifier{
// 												Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 												ReplacePrefixMatch: PointerTo("v1"),
// 											},
// 										},
// 									},
// 								},
// 								BackendRefs: []gwv1beta1.HTTPBackendRef{
// 									{
// 										// this ref should get dropped
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service two",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 										},
// 									},
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "some-service-part-three",
// 												Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 												Group:     PointerTo(gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup)),
// 												Kind:      PointerTo(gwv1beta1.Kind(v1alpha1.MeshServiceKind)),
// 											},
// 										},
// 									},
// 									{
// 										BackendRef: gwv1beta1.BackendRef{
// 											BackendObjectReference: gwv1beta1.BackendObjectReference{
// 												Name:      "service one",
// 												Namespace: PointerTo(gwv1beta1.Namespace("some ns")),
// 											},
// 											Weight: PointerTo(int32(45)),
// 										},
// 										Filters: []gwv1beta1.HTTPRouteFilter{
// 											{
// 												RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
// 													Set: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - Magic",
// 															Value: "svc - v2",
// 														},
// 														{
// 															Name:  "svc - Another One",
// 															Value: "svc - dj khaled",
// 														},
// 													},
// 													Add: []gwv1beta1.HTTPHeader{
// 														{
// 															Name:  "svc - add it on",
// 															Value: "svc - the value",
// 														},
// 													},
// 													Remove: []string{"svc - time to go"},
// 												},
// 												URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
// 													Path: &gwv1beta1.HTTPPathModifier{
// 														Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
// 														ReplacePrefixMatch: PointerTo("path"),
// 													},
// 												},
// 											},
// 										},
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{Name: "api-gw", Namespace: "k8s-gw-ns"}: {Name: "api-gw", Partition: "part-1", Namespace: "ns"},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "service one", Namespace: "some ns"}: {ServiceName: "service one", Namespace: "some ns"},
// 				},
// 				meshServices: map[types.NamespacedName]v1alpha1.MeshService{
// 					{Name: "some-service-part-three", Namespace: "svc-ns"}: {Spec: v1alpha1.MeshServiceSpec{Name: "some-service-part-three"}},
// 				},
// 			},
// 			want: capi.HTTPRouteConfigEntry{
// 				Kind: capi.HTTPRoute,
// 				Name: "k8s-http-route",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "api-gw",
// 						SectionName: "",
// 						Partition:   "part-1",
// 						Namespace:   "ns",
// 					},
// 				},
// 				Rules: []capi.HTTPRouteRule{
// 					{
// 						Filters: capi.HTTPFilters{
// 							Headers: []capi.HTTPHeaderFilter{
// 								{
// 									Add: map[string]string{
// 										"add it on": "the value",
// 									},
// 									Remove: []string{"time to go"},
// 									Set: map[string]string{
// 										"Magic":       "v2",
// 										"Another One": "dj khaled",
// 									},
// 								},
// 							},
// 							URLRewrite: &capi.URLRewrite{Path: "v1"},
// 						},
// 						Matches: []capi.HTTPMatch{
// 							{
// 								Headers: []capi.HTTPHeaderMatch{
// 									{
// 										Match: capi.HTTPHeaderMatchExact,
// 										Name:  "my header match",
// 										Value: "the value",
// 									},
// 								},
// 								Method: capi.HTTPMatchMethodGet,
// 								Path: capi.HTTPPathMatch{
// 									Match: capi.HTTPPathMatchPrefix,
// 									Value: "/v1",
// 								},
// 								Query: []capi.HTTPQueryMatch{
// 									{
// 										Match: capi.HTTPQueryMatchExact,
// 										Name:  "search",
// 										Value: "term",
// 									},
// 								},
// 							},
// 						},
// 						Services: []capi.HTTPService{
// 							{Name: "some-service-part-three", Filters: capi.HTTPFilters{Headers: []capi.HTTPHeaderFilter{{Add: make(map[string]string), Remove: make([]string, 0), Set: make(map[string]string)}}}},
// 							{
// 								Name:   "service one",
// 								Weight: 45,
// 								Filters: capi.HTTPFilters{
// 									Headers: []capi.HTTPHeaderFilter{
// 										{
// 											Add: map[string]string{
// 												"svc - add it on": "svc - the value",
// 											},
// 											Remove: []string{"svc - time to go"},
// 											Set: map[string]string{
// 												"svc - Magic":       "svc - v2",
// 												"svc - Another One": "svc - dj khaled",
// 											},
// 										},
// 									},
// 									URLRewrite: &capi.URLRewrite{
// 										Path: "path",
// 									},
// 								},
// 								Namespace: "some ns",
// 							},
// 						},
// 					},
// 				},
// 				Hostnames: []string{
// 					"host-name.example.com",
// 					"consul.io",
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "k8s-http-route",
// 					metaKeyKubeName:        "k8s-http-route",
// 				},
// 				Namespace: "k8s-ns",
// 			},
// 		},
// 	}
// 	for name, tc := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			tr := K8sToConsulTranslator{
// 				EnableConsulNamespaces: true,
// 				EnableK8sMirroring:     true,
// 			}
// 			got := tr.HTTPRouteToHTTPRoute(&tc.args.k8sHTTPRoute, tc.args.parentRefs, tc.args.services, tc.args.meshServices)
// 			if diff := cmp.Diff(&tc.want, got); diff != "" {
// 				t.Errorf("Translator.HTTPRouteToHTTPRoute() mismatch (-want +got):\n%s", diff)
// 			}
// 		})
// 	}
// }

// func TestTranslator_TCPRouteToTCPRoute(t *testing.T) {
// 	t.Parallel()
// 	type args struct {
// 		k8sRoute     gwv1alpha2.TCPRoute
// 		parentRefs   map[types.NamespacedName]api.ResourceReference
// 		services     map[types.NamespacedName]api.CatalogService
// 		meshServices map[types.NamespacedName]v1alpha1.MeshService
// 	}
// 	tests := map[string]struct {
// 		args args
// 		want capi.TCPRouteConfigEntry
// 	}{
// 		"base test": {
// 			args: args{
// 				k8sRoute: gwv1alpha2.TCPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "tcp-route",
// 						Namespace: "k8s-ns",
// 					},
// 					Spec: gwv1alpha2.TCPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("another-ns")),
// 									Name:        "mygw",
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-one")),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 								},
// 							},
// 						},
// 						Rules: []gwv1alpha2.TCPRouteRule{
// 							{
// 								BackendRefs: []gwv1beta1.BackendRef{
// 									{
// 										BackendObjectReference: gwv1beta1.BackendObjectReference{
// 											Name:      "some-service",
// 											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 										},
// 										Weight: new(int32),
// 									},
// 								},
// 							},
// 							{
// 								BackendRefs: []gwv1beta1.BackendRef{
// 									{
// 										BackendObjectReference: gwv1beta1.BackendObjectReference{
// 											Name:      "some-service-part-two",
// 											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 										},
// 										Weight: new(int32),
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{
// 						Namespace: "another-ns",
// 						Name:      "mygw",
// 					}: {
// 						Name:      "mygw",
// 						Namespace: "another-ns",
// 						Partition: "",
// 					},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "some-service", Namespace: "svc-ns"}:          {ServiceName: "some-service", Namespace: "svc-ns"},
// 					{Name: "some-service-part-two", Namespace: "svc-ns"}: {ServiceName: "some-service-part-two", Namespace: "svc-ns"},
// 				},
// 				meshServices: map[types.NamespacedName]v1alpha1.MeshService{
// 					{Name: "some-service-part-three", Namespace: "svc-ns"}: {Spec: v1alpha1.MeshServiceSpec{Name: "some-service-part-three"}},
// 				},
// 			},
// 			want: capi.TCPRouteConfigEntry{
// 				Kind:      capi.TCPRoute,
// 				Name:      "tcp-route",
// 				Namespace: "k8s-ns",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "mygw",
// 						SectionName: "listener-one",
// 						Partition:   "",
// 						Namespace:   "another-ns",
// 					},
// 				},
// 				Services: []capi.TCPService{
// 					{
// 						Name:      "some-service",
// 						Partition: "",
// 						Namespace: "svc-ns",
// 					},
// 					{
// 						Name:      "some-service-part-two",
// 						Partition: "",
// 						Namespace: "svc-ns",
// 					},
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "tcp-route",
// 					metaKeyKubeName:        "tcp-route",
// 				},
// 			},
// 		},

// 		"overwrite the route name via annotaions": {
// 			args: args{
// 				k8sRoute: gwv1alpha2.TCPRoute{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      "tcp-route",
// 						Namespace: "k8s-ns",
// 						Annotations: map[string]string{
// 							AnnotationTCPRoute: "replaced-name",
// 						},
// 					},
// 					Spec: gwv1alpha2.TCPRouteSpec{
// 						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 							ParentRefs: []gwv1beta1.ParentReference{
// 								{
// 									Namespace:   PointerTo(gwv1beta1.Namespace("another-ns")),
// 									Name:        "mygw",
// 									SectionName: PointerTo(gwv1beta1.SectionName("listener-one")),
// 									Kind:        PointerTo(gwv1beta1.Kind("Gateway")),
// 								},
// 							},
// 						},
// 						Rules: []gwv1alpha2.TCPRouteRule{
// 							{
// 								BackendRefs: []gwv1beta1.BackendRef{
// 									{
// 										BackendObjectReference: gwv1beta1.BackendObjectReference{
// 											Name:      "some-service",
// 											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 										},
// 										Weight: new(int32),
// 									},
// 								},
// 							},
// 							{
// 								BackendRefs: []gwv1beta1.BackendRef{
// 									{
// 										BackendObjectReference: gwv1beta1.BackendObjectReference{
// 											Name:      "some-service-part-two",
// 											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 										},
// 										Weight: new(int32),
// 									},
// 								},
// 							},
// 							{
// 								BackendRefs: []gwv1beta1.BackendRef{
// 									{
// 										BackendObjectReference: gwv1beta1.BackendObjectReference{
// 											Name:      "some-service-part-three",
// 											Namespace: PointerTo(gwv1beta1.Namespace("svc-ns")),
// 											Group:     PointerTo(gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup)),
// 											Kind:      PointerTo(gwv1beta1.Kind(v1alpha1.MeshServiceKind)),
// 										},
// 										Weight: new(int32),
// 									},
// 								},
// 							},
// 						},
// 					},
// 				},
// 				parentRefs: map[types.NamespacedName]api.ResourceReference{
// 					{
// 						Namespace: "another-ns",
// 						Name:      "mygw",
// 					}: {
// 						Name:      "mygw",
// 						Namespace: "another-ns",
// 						Partition: "",
// 					},
// 				},
// 				services: map[types.NamespacedName]api.CatalogService{
// 					{Name: "some-service", Namespace: "svc-ns"}: {ServiceName: "some-service", Namespace: "other"},
// 				},
// 				meshServices: map[types.NamespacedName]v1alpha1.MeshService{
// 					{Name: "some-service-part-three", Namespace: "svc-ns"}: {Spec: v1alpha1.MeshServiceSpec{Name: "some-service-part-three"}},
// 				},
// 			},
// 			want: capi.TCPRouteConfigEntry{
// 				Kind:      capi.TCPRoute,
// 				Name:      "replaced-name",
// 				Namespace: "k8s-ns",
// 				Parents: []capi.ResourceReference{
// 					{
// 						Kind:        capi.APIGateway,
// 						Name:        "mygw",
// 						SectionName: "listener-one",
// 						Partition:   "",
// 						Namespace:   "another-ns",
// 					},
// 				},
// 				Services: []capi.TCPService{
// 					{
// 						Name:      "some-service",
// 						Partition: "",
// 						Namespace: "other",
// 					},
// 					{Name: "some-service-part-three"},
// 				},
// 				Meta: map[string]string{
// 					metaKeyManagedBy:       metaValueManagedBy,
// 					metaKeyKubeNS:          "k8s-ns",
// 					metaKeyKubeServiceName: "tcp-route",
// 					metaKeyKubeName:        "tcp-route",
// 				},
// 			},
// 		},
// 	}
// 	for name, tt := range tests {
// 		t.Run(name, func(t *testing.T) {
// 			tr := K8sToConsulTranslator{
// 				EnableConsulNamespaces: true,
// 				EnableK8sMirroring:     true,
// 			}

// 			got := tr.TCPRouteToTCPRoute(&tt.args.k8sRoute, tt.args.parentRefs, tt.args.services, tt.args.meshServices)
// 			if diff := cmp.Diff(&tt.want, got); diff != "" {
// 				t.Errorf("Translator.TCPRouteToTCPRoute() mismatch (-want +got):\n%s", diff)
// 			}
// 		})
// 	}
// }

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
