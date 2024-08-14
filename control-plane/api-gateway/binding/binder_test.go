// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func init() {
	timeFunc = func() metav1.Time {
		return metav1.Time{}
	}
}

const (
	testGatewayClassName = "gateway-class"
	testControllerName   = "test-controller"
)

var (
	testGatewayClassObjectName = gwv1beta1.ObjectName(testGatewayClassName)
	deletionTimestamp          = common.PointerTo(metav1.Now())

	testGatewayClass = &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: testGatewayClassName,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gwv1beta1.GatewayController(testControllerName),
		},
	}
)

type resourceMapResources struct {
	grants                       []gwv1beta1.ReferenceGrant
	secrets                      []corev1.Secret
	gateways                     []gwv1beta1.Gateway
	httpRoutes                   []gwv1beta1.HTTPRoute
	tcpRoutes                    []gwv1alpha2.TCPRoute
	meshServices                 []v1alpha1.MeshService
	services                     []types.NamespacedName
	jwtProviders                 []*v1alpha1.JWTProvider
	gatewayPolicies              []*v1alpha1.GatewayPolicy
	externalAuthFilters          []*v1alpha1.RouteAuthFilter
	consulFileSystemCertificates []api.FileSystemCertificateConfigEntry
	consulHTTPRoutes             []api.HTTPRouteConfigEntry
	consulTCPRoutes              []api.TCPRouteConfigEntry
}

func newTestResourceMap(t *testing.T, resources resourceMapResources) *common.ResourceMap {
	resourceMap := common.NewResourceMap(common.ResourceTranslator{}, NewReferenceValidator(resources.grants), logrtest.NewTestLogger(t))

	for _, s := range resources.services {
		resourceMap.AddService(s, s.Name)
	}
	for _, s := range resources.meshServices {
		resourceMap.AddMeshService(s)
	}
	for _, s := range resources.secrets {
		resourceMap.ReferenceCountCertificate(s)
	}
	for _, g := range resources.gateways {
		resourceMap.ReferenceCountGateway(g)
	}
	for _, r := range resources.httpRoutes {
		resourceMap.ReferenceCountHTTPRoute(r)
	}
	for _, r := range resources.tcpRoutes {
		resourceMap.ReferenceCountTCPRoute(r)
	}
	for _, r := range resources.consulHTTPRoutes {
		resourceMap.ReferenceCountConsulHTTPRoute(r)
	}
	for _, r := range resources.consulTCPRoutes {
		resourceMap.ReferenceCountConsulTCPRoute(r)
	}
	for _, r := range resources.gatewayPolicies {
		resourceMap.AddGatewayPolicy(r)
	}
	for _, r := range resources.jwtProviders {
		resourceMap.AddJWTProvider(r)
	}

	for _, r := range resources.externalAuthFilters {
		resourceMap.AddExternalFilter(r)
	}

	return resourceMap
}

func TestBinder_Lifecycle(t *testing.T) {
	t.Parallel()

	certificateOne, secretOne := generateTestCertificate(t, "default", "secret-one")
	certificateTwo, secretTwo := generateTestCertificate(t, "default", "secret-two")

	for name, tt := range map[string]struct {
		resources               resourceMapResources
		config                  BinderConfig
		expectedStatusUpdates   []client.Object
		expectedUpdates         []client.Object
		expectedConsulDeletions []api.ResourceReference
		expectedConsulUpdates   []api.ConfigEntry
	}{
		"no gateway class and empty routes": {
			config: BinderConfig{
				Gateway: gwv1beta1.Gateway{},
			},
			expectedConsulDeletions: []api.ResourceReference{{
				Kind: api.APIGateway,
			}},
		},
		"no gateway class and empty routes remove finalizer": {
			config: BinderConfig{
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{common.GatewayFinalizer},
					},
				},
			},
			expectedUpdates: []client.Object{
				addClassConfig(gwv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{}}}),
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.APIGateway},
			},
		},
		"deleting gateway empty routes": {
			config: BinderConfig{
				ControllerName: testControllerName,
				GatewayClass:   testGatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{common.GatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassObjectName,
					},
				},
			},
			expectedUpdates: []client.Object{
				addClassConfig(gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: deletionTimestamp, Finalizers: []string{}},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassObjectName,
					},
				}),
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.APIGateway},
			},
		},
		"basic gateway no finalizer": {
			config: BinderConfig{
				ControllerName: testControllerName,
				GatewayClass:   testGatewayClass,
				Gateway: gwv1beta1.Gateway{
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassObjectName,
					},
				},
			},
			expectedUpdates: []client.Object{
				addClassConfig(gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{Finalizers: []string{common.GatewayFinalizer}},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassObjectName,
					},
				}),
			},
		},
		"basic gateway": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{{
						Protocol: gwv1beta1.HTTPSProtocolType,
						TLS: &gwv1beta1.GatewayTLSConfig{
							CertificateRefs: []gwv1beta1.SecretObjectReference{
								{Name: "secret-one"},
							},
							Mode: common.PointerTo(gwv1beta1.TLSModeTerminate),
						},
					}},
				}),
			}),
			resources: resourceMapResources{
				secrets: []corev1.Secret{
					secretOne,
				},
			},
			expectedStatusUpdates: []client.Object{
				addClassConfig(gatewayWithFinalizerStatus(
					gwv1beta1.GatewaySpec{
						Listeners: []gwv1beta1.Listener{{
							Protocol: gwv1beta1.HTTPSProtocolType,
							TLS: &gwv1beta1.GatewayTLSConfig{
								Mode: common.PointerTo(gwv1beta1.TLSModeTerminate),
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{Name: "secret-one"},
								},
							},
						}},
					},
					gwv1beta1.GatewayStatus{
						Addresses: []gwv1beta1.GatewayAddress{},
						Conditions: []metav1.Condition{
							{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "gateway accepted",
							}, {
								Type:    "Programmed",
								Status:  metav1.ConditionFalse,
								Reason:  "Pending",
								Message: "gateway pods are still being scheduled",
							},
						},
						Listeners: []gwv1beta1.ListenerStatus{{
							SupportedKinds: supportedKindsForProtocol[gwv1beta1.HTTPSProtocolType],
							Conditions: []metav1.Condition{
								{
									Type:    "Accepted",
									Status:  metav1.ConditionTrue,
									Reason:  "Accepted",
									Message: "listener accepted",
								}, {
									Type:    "Programmed",
									Status:  metav1.ConditionTrue,
									Reason:  "Programmed",
									Message: "listener programmed",
								}, {
									Type:    "Conflicted",
									Status:  metav1.ConditionFalse,
									Reason:  "NoConflicts",
									Message: "listener has no conflicts",
								}, {
									Type:    "ResolvedRefs",
									Status:  metav1.ConditionTrue,
									Reason:  "ResolvedRefs",
									Message: "resolved references",
								},
							},
						}},
					}),
				),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				certificateOne,
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "gateway",
					Meta: map[string]string{
						"k8s-name":      "gateway",
						"k8s-namespace": "default",
					},
					Listeners: []api.APIGatewayListener{{
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{{
								Kind: api.FileSystemCertificate,
								Name: "secret-one",
							}},
						},
					}},
				},
			},
		},
		"gateway http route no finalizer": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
				HTTPRoutes: []gwv1beta1.HTTPRoute{
					{
						TypeMeta: metav1.TypeMeta{
							Kind:       "HTTPRoute",
							APIVersion: "gateway.networking.k8s.io/v1beta1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "route",
						},
						Spec: gwv1beta1.HTTPRouteSpec{
							CommonRouteSpec: gwv1beta1.CommonRouteSpec{
								ParentRefs: []gwv1beta1.ParentReference{{
									Name: "gateway",
								}},
							},
						},
					},
				},
			}),
			expectedUpdates: []client.Object{
				common.PointerTo(testHTTPRoute("route", []string{"gateway"}, nil)),
			},
			expectedStatusUpdates: []client.Object{
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
				})),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "gateway",
					Meta: map[string]string{
						"k8s-name":      "gateway",
						"k8s-namespace": "default",
					},
					Listeners: []api.APIGatewayListener{},
				},
			},
		},
		"gateway http route deleting": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
				HTTPRoutes: []gwv1beta1.HTTPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "route",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{common.GatewayFinalizer},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			}),
			resources: resourceMapResources{
				consulHTTPRoutes: []api.HTTPRouteConfigEntry{{
					Kind: api.HTTPRoute,
					Name: "route",
					Parents: []api.ResourceReference{
						{Kind: api.APIGateway, Name: "gateway"},
					},
				}},
			},
			expectedUpdates: []client.Object{
				&gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "route",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				},
			},
			expectedStatusUpdates: []client.Object{
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
				})),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "gateway",
					Meta: map[string]string{
						"k8s-name":      "gateway",
						"k8s-namespace": "default",
					},
					Listeners: []api.APIGatewayListener{},
				},
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.HTTPRoute, Name: "route"},
			},
		},
		"gateway tcp route no finalizer": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
				TCPRoutes: []gwv1alpha2.TCPRoute{
					{
						TypeMeta: metav1.TypeMeta{
							Kind:       "TCPRoute",
							APIVersion: "gateway.networking.k8s.io/v1beta1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "route",
						},
						Spec: gwv1alpha2.TCPRouteSpec{
							CommonRouteSpec: gwv1beta1.CommonRouteSpec{
								ParentRefs: []gwv1beta1.ParentReference{{
									Name: "gateway",
								}},
							},
						},
					},
				},
			}),
			expectedUpdates: []client.Object{
				common.PointerTo(testTCPRoute("route", []string{"gateway"}, nil)),
			},
			expectedStatusUpdates: []client.Object{
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
				})),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "gateway",
					Meta: map[string]string{
						"k8s-name":      "gateway",
						"k8s-namespace": "default",
					},
					Listeners: []api.APIGatewayListener{},
				},
			},
		},
		"gateway tcp route deleting": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
				TCPRoutes: []gwv1alpha2.TCPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "route",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{common.GatewayFinalizer},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			}),
			resources: resourceMapResources{
				consulTCPRoutes: []api.TCPRouteConfigEntry{{
					Kind: api.TCPRoute,
					Name: "route",
					Parents: []api.ResourceReference{
						{Kind: api.APIGateway, Name: "gateway"},
					},
				}},
			},
			expectedUpdates: []client.Object{
				&gwv1alpha2.TCPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "route",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				},
			},
			expectedStatusUpdates: []client.Object{
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
				})),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind: api.APIGateway,
					Name: "gateway",
					Meta: map[string]string{
						"k8s-name":      "gateway",
						"k8s-namespace": "default",
					},
					Listeners: []api.APIGatewayListener{},
				},
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.TCPRoute, Name: "route"},
			},
		},
		"gateway deletion routes and secrets": {
			config: controlledBinder(BinderConfig{
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "gateway-deleted",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{common.GatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassName,
						Listeners: []gwv1beta1.Listener{{
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{Name: "secret-one"},
									{Name: "secret-two"},
								},
							},
						}},
					},
				},
				HTTPRoutes: []gwv1beta1.HTTPRoute{
					testHTTPRoute("http-route-one", []string{"gateway-deleted"}, nil),
					testHTTPRouteStatus("http-route-two", nil, []gwv1alpha2.RouteParentStatus{
						{ParentRef: gwv1beta1.ParentReference{Name: "gateway-deleted"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						}},
						{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						}},
					}),
				},
				TCPRoutes: []gwv1alpha2.TCPRoute{
					testTCPRoute("tcp-route-one", []string{"gateway-deleted"}, nil),
					testTCPRouteStatus("tcp-route-two", nil, []gwv1alpha2.RouteParentStatus{
						{ParentRef: gwv1beta1.ParentReference{Name: "gateway-deleted"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						}},
						{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
							{
								Type:   "Accepted",
								Status: metav1.ConditionTrue,
							},
						}},
					}),
				},
			}),
			resources: resourceMapResources{
				consulHTTPRoutes: []api.HTTPRouteConfigEntry{
					{
						Kind: api.HTTPRoute, Name: "http-route-two", Meta: map[string]string{
							"k8s-name":      "http-route-two",
							"k8s-namespace": "",
						},
						Parents: []api.ResourceReference{
							{Kind: api.APIGateway, Name: "gateway-deleted"},
							{Kind: api.APIGateway, Name: "gateway"},
						},
					},
					{
						Kind: api.HTTPRoute, Name: "http-route-one", Meta: map[string]string{
							"k8s-name":      "http-route-one",
							"k8s-namespace": "",
						},
						Parents: []api.ResourceReference{
							{Kind: api.APIGateway, Name: "gateway-deleted"},
						},
					},
				},
				consulTCPRoutes: []api.TCPRouteConfigEntry{
					{
						Kind: api.TCPRoute, Name: "tcp-route-two",
						Meta: map[string]string{
							"k8s-name":      "tcp-route-two",
							"k8s-namespace": "",
						},
						Parents: []api.ResourceReference{
							{Kind: api.APIGateway, Name: "gateway-deleted"},
							{Kind: api.APIGateway, Name: "gateway"},
						},
					},
					{
						Kind: api.TCPRoute, Name: "tcp-route-one",
						Meta: map[string]string{
							"k8s-name":      "tcp-route-one",
							"k8s-namespace": "",
						},
						Parents: []api.ResourceReference{
							{Kind: api.APIGateway, Name: "gateway-deleted"},
						},
					},
				},
				consulFileSystemCertificates: []api.FileSystemCertificateConfigEntry{
					*certificateOne,
					*certificateTwo,
				},
				secrets: []corev1.Secret{
					secretOne,
					secretTwo,
				},
				gateways: []gwv1beta1.Gateway{
					gatewayWithFinalizer(gwv1beta1.GatewaySpec{
						Listeners: []gwv1beta1.Listener{{
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{Name: "secret-one"},
									{Name: "secret-three"},
								},
							},
						}},
					}),
				},
			},
			expectedStatusUpdates: []client.Object{
				common.PointerTo(testHTTPRouteStatus("http-route-two", nil, []gwv1beta1.RouteParentStatus{
					{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
						{
							Type:   "Accepted",
							Status: metav1.ConditionTrue,
						},
					}},
				}, "gateway-deleted")),
				common.PointerTo(testTCPRouteStatus("tcp-route-two", nil, []gwv1beta1.RouteParentStatus{
					{ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, ControllerName: testControllerName, Conditions: []metav1.Condition{
						{
							Type:   "Accepted",
							Status: metav1.ConditionTrue,
						},
					}},
				}, "gateway-deleted")),
			},
			expectedUpdates: []client.Object{
				&gwv1beta1.HTTPRoute{
					TypeMeta: metav1.TypeMeta{
						Kind:       "HTTPRoute",
						APIVersion: "gateway.networking.k8s.io/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "http-route-one",
						// removing a finalizer
						Finalizers: []string{},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{Name: "gateway-deleted"},
							},
						},
					},
					Status: gwv1beta1.HTTPRouteStatus{RouteStatus: gwv1beta1.RouteStatus{Parents: []gwv1alpha2.RouteParentStatus{}}},
				},
				&gwv1alpha2.TCPRoute{
					TypeMeta: metav1.TypeMeta{
						Kind:       "TCPRoute",
						APIVersion: "gateway.networking.k8s.io/v1beta1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:       "tcp-route-one",
						Finalizers: []string{},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{Name: "gateway-deleted"},
							},
						},
					},
					Status: gwv1alpha2.TCPRouteStatus{RouteStatus: gwv1beta1.RouteStatus{Parents: []gwv1alpha2.RouteParentStatus{}}},
				},
				addClassConfig(gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "gateway-deleted",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassName,
						Listeners: []gwv1beta1.Listener{{
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{
									{Name: "secret-one"},
									{Name: "secret-two"},
								},
							},
						}},
					},
				}),
			},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.HTTPRouteConfigEntry{
					Kind: api.HTTPRoute,
					Name: "http-route-two",
					Meta: map[string]string{
						"k8s-name":      "http-route-two",
						"k8s-namespace": "",
					},
					// dropped ref to gateway
					Parents: []api.ResourceReference{{
						Kind: api.APIGateway,
						Name: "gateway",
					}},
				},
				&api.TCPRouteConfigEntry{
					Kind: api.TCPRoute,
					Name: "tcp-route-two",
					Meta: map[string]string{
						"k8s-name":      "tcp-route-two",
						"k8s-namespace": "",
					},
					// dropped ref to gateway
					Parents: []api.ResourceReference{{
						Kind: api.APIGateway,
						Name: "gateway",
					}},
				},
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.HTTPRoute, Name: "http-route-one"},
				{Kind: api.TCPRoute, Name: "tcp-route-one"},
				{Kind: api.FileSystemCertificate, Name: "secret-two"},
				{Kind: api.APIGateway, Name: "gateway-deleted"},
			},
		},
		"gateway deletion policies": {
			config: controlledBinder(BinderConfig{
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "gateway-deleted",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{common.GatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassName,
						Listeners: []gwv1beta1.Listener{
							{
								Name: gwv1beta1.SectionName("l1"),
							},
							{
								Name: gwv1beta1.SectionName("l2"),
							},
						},
					},
				},
				Policies: []v1alpha1.GatewayPolicy{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "p1",
						},
						Spec: v1alpha1.GatewayPolicySpec{
							TargetRef: v1alpha1.PolicyTargetReference{
								Kind:        "Gateway",
								Name:        "gateway-deleted",
								SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
							},
						},
						Status: v1alpha1.GatewayPolicyStatus{
							Conditions: []metav1.Condition{
								{
									Type:               "Accepted",
									Status:             metav1.ConditionTrue,
									Reason:             "Accepted",
									ObservedGeneration: 5,
									Message:            "gateway policy accepted",
								},
								{
									Type:               "ResolvedRefs",
									Status:             metav1.ConditionTrue,
									Reason:             "ResolvedRefs",
									ObservedGeneration: 5,
									Message:            "resolved references",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "p2",
						},
						Spec: v1alpha1.GatewayPolicySpec{
							TargetRef: v1alpha1.PolicyTargetReference{
								Kind:        "Gateway",
								Name:        "gateway-deleted",
								SectionName: common.PointerTo(gwv1beta1.SectionName("l2")),
							},
						},
						Status: v1alpha1.GatewayPolicyStatus{
							Conditions: []metav1.Condition{
								{
									Type:               "Accepted",
									Status:             metav1.ConditionTrue,
									Reason:             "Accepted",
									ObservedGeneration: 5,
									Message:            "gateway policy accepted",
								},
								{
									Type:               "ResolvedRefs",
									Status:             metav1.ConditionTrue,
									Reason:             "ResolvedRefs",
									ObservedGeneration: 5,
									Message:            "resolved references",
								},
							},
						},
					},
				},
			}),
			resources: resourceMapResources{
				gateways: []gwv1beta1.Gateway{
					gatewayWithFinalizer(gwv1beta1.GatewaySpec{
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
							{
								Name: "l2",
							},
						},
					}),
				},
			},
			expectedStatusUpdates: []client.Object{
				&v1alpha1.GatewayPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "p1",
					},
					Spec: v1alpha1.GatewayPolicySpec{
						TargetRef: v1alpha1.PolicyTargetReference{
							Kind:        "Gateway",
							Name:        "gateway-deleted",
							SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
						},
					},
				},
				&v1alpha1.GatewayPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name: "p2",
					},
					Spec: v1alpha1.GatewayPolicySpec{
						TargetRef: v1alpha1.PolicyTargetReference{
							Kind:        "Gateway",
							Name:        "gateway-deleted",
							SectionName: common.PointerTo(gwv1beta1.SectionName("l2")),
						},
					},
				},
			},
			expectedUpdates: []client.Object{
				addClassConfig(gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "gateway-deleted",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: testGatewayClassName,
						Listeners: []gwv1beta1.Listener{
							{
								Name: "l1",
							},
							{
								Name: "l2",
							},
						},
					},
				}),
			},
			expectedConsulUpdates: []api.ConfigEntry{},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.APIGateway, Name: "gateway-deleted"},
			},
		},
		"gateway http route references missing external ref": {
			resources: resourceMapResources{
				gateways: []gwv1beta1.Gateway{gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{{
						Name:     "l1",
						Protocol: "HTTP",
					}},
				})},
				httpRoutes: []gwv1beta1.HTTPRoute{},
				jwtProviders: []*v1alpha1.JWTProvider{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "okta",
						},
					},
				},
				externalAuthFilters: []*v1alpha1.RouteAuthFilter{},
			},
			config: controlledBinder(BinderConfig{
				ConsulGateway: &api.APIGatewayConfigEntry{
					Name: "gateway",
					Kind: "api-gateway",
					Listeners: []api.APIGatewayListener{
						{
							Name:     "l1",
							Protocol: "HTTP",
						},
					},
					Meta: map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
				},
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}),
				HTTPRoutes: []gwv1beta1.HTTPRoute{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "h1",
							Finalizers: []string{common.GatewayFinalizer},
						},
						Spec: gwv1beta1.HTTPRouteSpec{
							CommonRouteSpec: gwv1beta1.CommonRouteSpec{
								ParentRefs: []gwv1beta1.ParentReference{
									{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										Name:        "gateway",
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
								},
							},
							Rules: []gwv1beta1.HTTPRouteRule{
								{
									Filters: []gwv1beta1.HTTPRouteFilter{{
										Type: "ExtensionRef",
										ExtensionRef: &gwv1beta1.LocalObjectReference{
											Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
											Kind:  "RouteAuthFilter",
											Name:  "route-auth",
										},
									}},
								},
							},
						},
					},
					testHTTPRoute("http-route-2", []string{"gateway"}, nil),
				},
			}),
			expectedStatusUpdates: []client.Object{
				&gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "h1",
						Finalizers: []string{common.GatewayFinalizer},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Group:       (*gwv1beta1.Group)(&common.BetaGroup),
									Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
									Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
									Name:        "gateway",
									SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
								},
							},
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Filters: []gwv1beta1.HTTPRouteFilter{{
									Type: "ExtensionRef",
									ExtensionRef: &gwv1beta1.LocalObjectReference{
										Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
										Kind:  "RouteAuthFilter",
										Name:  "route-auth",
									},
								}},
							},
						},
					},
					Status: gwv1beta1.HTTPRouteStatus{
						RouteStatus: gwv1beta1.RouteStatus{
							Parents: []gwv1beta1.RouteParentStatus{
								{
									ParentRef: gwv1beta1.ParentReference{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Name:        "gateway",
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
									ControllerName: testControllerName,
									Conditions: []metav1.Condition{
										{
											Type:    "ResolvedRefs",
											Status:  metav1.ConditionTrue,
											Reason:  "ResolvedRefs",
											Message: "resolved backend references",
										},
										{
											Type:    "Accepted",
											Status:  metav1.ConditionFalse,
											Reason:  "FilterNotFound",
											Message: "ref not found",
										},
									},
								},
							},
						},
					},
				},
				common.PointerTo(testHTTPRoute("http-route-2", []string{"gateway"}, nil)),
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
					Listeners: []gwv1beta1.ListenerStatus{
						{
							Name:           "l1",
							SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: (*gwv1beta1.Group)(&common.BetaGroup), Kind: "HTTPRoute"}},
							Conditions: []metav1.Condition{
								{
									Type:    "Accepted",
									Status:  "True",
									Reason:  "Accepted",
									Message: "listener accepted",
								},
								{
									Type:    "Programmed",
									Status:  "True",
									Reason:  "Programmed",
									Message: "listener programmed",
								},
								{
									Type:    "Conflicted",
									Status:  "False",
									Reason:  "NoConflicts",
									Message: "listener has no conflicts",
								},
								{
									Type:    "ResolvedRefs",
									Status:  "True",
									Reason:  "ResolvedRefs",
									Message: "resolved references",
								},
							},
						},
					},
				})),
			},
			expectedUpdates:         []client.Object{},
			expectedConsulDeletions: []api.ResourceReference{},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind:      "api-gateway",
					Name:      "gateway",
					Meta:      map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
					Listeners: []api.APIGatewayListener{{Name: "l1", Protocol: "http"}},
				},
			},
		},
		"gateway http route route auth filter references missing jwt provider": {
			resources: resourceMapResources{
				gateways: []gwv1beta1.Gateway{gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{{
						Name:     "l1",
						Protocol: "HTTP",
					}},
				})},
				httpRoutes:   []gwv1beta1.HTTPRoute{},
				jwtProviders: []*v1alpha1.JWTProvider{},
				externalAuthFilters: []*v1alpha1.RouteAuthFilter{
					{
						TypeMeta: metav1.TypeMeta{
							Kind: v1alpha1.RouteAuthFilterKind,
						},
						ObjectMeta: metav1.ObjectMeta{
							Name:      "route-auth",
							Namespace: "default",
						},
						Spec: v1alpha1.RouteAuthFilterSpec{
							JWT: &v1alpha1.GatewayJWTRequirement{
								Providers: []*v1alpha1.GatewayJWTProvider{
									{
										Name: "okta",
									},
								},
							},
						},
					},
				},
			},
			config: controlledBinder(BinderConfig{
				ConsulGateway: &api.APIGatewayConfigEntry{
					Name: "gateway",
					Kind: "api-gateway",
					Listeners: []api.APIGatewayListener{
						{
							Name:     "l1",
							Protocol: "HTTP",
						},
					},
					Meta: map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
				},
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}),
				HTTPRoutes: []gwv1beta1.HTTPRoute{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "h1",
							Finalizers: []string{common.GatewayFinalizer},
							Namespace:  "default",
						},
						Spec: gwv1beta1.HTTPRouteSpec{
							CommonRouteSpec: gwv1beta1.CommonRouteSpec{
								ParentRefs: []gwv1beta1.ParentReference{
									{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										Name:        "gateway",
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
								},
							},
							Rules: []gwv1beta1.HTTPRouteRule{
								{
									Filters: []gwv1beta1.HTTPRouteFilter{{
										Type: "ExtensionRef",
										ExtensionRef: &gwv1beta1.LocalObjectReference{
											Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
											Kind:  v1alpha1.RouteAuthFilterKind,
											Name:  "route-auth",
										},
									}},
								},
							},
						},
					},
					testHTTPRoute("http-route-2", []string{"gateway"}, nil),
				},
			}),
			expectedStatusUpdates: []client.Object{
				&gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "h1",
						Finalizers: []string{common.GatewayFinalizer},
						Namespace:  "default",
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Group:       (*gwv1beta1.Group)(&common.BetaGroup),
									Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
									Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
									Name:        "gateway",
									SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
								},
							},
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Filters: []gwv1beta1.HTTPRouteFilter{{
									Type: "ExtensionRef",
									ExtensionRef: &gwv1beta1.LocalObjectReference{
										Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
										Kind:  "RouteAuthFilter",
										Name:  "route-auth",
									},
								}},
							},
						},
					},
					Status: gwv1beta1.HTTPRouteStatus{
						RouteStatus: gwv1beta1.RouteStatus{
							Parents: []gwv1beta1.RouteParentStatus{
								{
									ParentRef: gwv1beta1.ParentReference{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Name:        "gateway",
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
									ControllerName: testControllerName,
									Conditions: []metav1.Condition{
										{
											Type:    "ResolvedRefs",
											Status:  metav1.ConditionTrue,
											Reason:  "ResolvedRefs",
											Message: "resolved backend references",
										},
										{
											Type:    "Accepted",
											Status:  metav1.ConditionFalse,
											Reason:  "JWTProviderNotFound",
											Message: "filter invalid: default/route-auth",
										},
									},
								},
							},
						},
					},
				},
				common.PointerTo(testHTTPRoute("http-route-2", []string{"gateway"}, nil)),
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
					Listeners: []gwv1beta1.ListenerStatus{
						{
							Name:           "l1",
							SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: (*gwv1beta1.Group)(&common.BetaGroup), Kind: "HTTPRoute"}},
							Conditions: []metav1.Condition{
								{
									Type:    "Accepted",
									Status:  "True",
									Reason:  "Accepted",
									Message: "listener accepted",
								},
								{
									Type:    "Programmed",
									Status:  "True",
									Reason:  "Programmed",
									Message: "listener programmed",
								},
								{
									Type:    "Conflicted",
									Status:  "False",
									Reason:  "NoConflicts",
									Message: "listener has no conflicts",
								},
								{
									Type:    "ResolvedRefs",
									Status:  "True",
									Reason:  "ResolvedRefs",
									Message: "resolved references",
								},
							},
						},
					},
				})),
				&v1alpha1.RouteAuthFilter{
					TypeMeta:   metav1.TypeMeta{Kind: "RouteAuthFilter"},
					ObjectMeta: metav1.ObjectMeta{Name: "route-auth", Namespace: "default"},
					Spec: v1alpha1.RouteAuthFilterSpec{
						JWT: &v1alpha1.GatewayJWTRequirement{
							Providers: []*v1alpha1.GatewayJWTProvider{
								{
									Name: "okta",
								},
							},
						},
					},
					Status: v1alpha1.RouteAuthFilterStatus{
						Conditions: []metav1.Condition{
							{
								Type:    "Accepted",
								Status:  "False",
								Reason:  "ReferencesNotValid",
								Message: "route filter is not accepted due to errors with references",
							},
							{
								Type:    "ResolvedRefs",
								Status:  "False",
								Reason:  "MissingJWTProviderReference",
								Message: "route filter references one or more jwt providers that do not exist: missingProviderNames: okta",
							},
						},
					},
				},
			},
			expectedUpdates:         []client.Object{},
			expectedConsulDeletions: []api.ResourceReference{},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind:      "api-gateway",
					Name:      "gateway",
					Meta:      map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
					Listeners: []api.APIGatewayListener{{Name: "l1", Protocol: "http"}},
				},
			},
		},
		"gateway http route route references invalid external ref type": {
			resources: resourceMapResources{
				gateways: []gwv1beta1.Gateway{gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{{
						Name:     "l1",
						Protocol: "HTTP",
					}},
				})},
			},
			config: controlledBinder(BinderConfig{
				ConsulGateway: &api.APIGatewayConfigEntry{
					Name: "gateway",
					Kind: "api-gateway",
					Listeners: []api.APIGatewayListener{
						{
							Name:     "l1",
							Protocol: "HTTP",
						},
					},
					Meta: map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
				},
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}),
				HTTPRoutes: []gwv1beta1.HTTPRoute{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:       "h1",
							Finalizers: []string{common.GatewayFinalizer},
							Namespace:  "default",
						},
						Spec: gwv1beta1.HTTPRouteSpec{
							CommonRouteSpec: gwv1beta1.CommonRouteSpec{
								ParentRefs: []gwv1beta1.ParentReference{
									{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										Name:        "gateway",
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
								},
							},
							Rules: []gwv1beta1.HTTPRouteRule{
								{
									Filters: []gwv1beta1.HTTPRouteFilter{{
										Type: "ExtensionRef",
										ExtensionRef: &gwv1beta1.LocalObjectReference{
											Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
											Kind:  "OhNoThisIsInvalid",
											Name:  "route-auth",
										},
									}},
								},
							},
						},
					},
					testHTTPRoute("http-route-2", []string{"gateway"}, nil),
				},
			}),
			expectedStatusUpdates: []client.Object{
				&gwv1beta1.HTTPRoute{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "h1",
						Finalizers: []string{common.GatewayFinalizer},
						Namespace:  "default",
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{
								{
									Group:       (*gwv1beta1.Group)(&common.BetaGroup),
									Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
									Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
									Name:        "gateway",
									SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
								},
							},
						},
						Rules: []gwv1beta1.HTTPRouteRule{
							{
								Filters: []gwv1beta1.HTTPRouteFilter{{
									Type: "ExtensionRef",
									ExtensionRef: &gwv1beta1.LocalObjectReference{
										Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
										Kind:  "OhNoThisIsInvalid",
										Name:  "route-auth",
									},
								}},
							},
						},
					},
					Status: gwv1beta1.HTTPRouteStatus{
						RouteStatus: gwv1beta1.RouteStatus{
							Parents: []gwv1beta1.RouteParentStatus{
								{
									ParentRef: gwv1beta1.ParentReference{
										Group:       (*gwv1beta1.Group)(&common.BetaGroup),
										Kind:        common.PointerTo(gwv1beta1.Kind("Gateway")),
										Name:        "gateway",
										Namespace:   common.PointerTo(gwv1beta1.Namespace("default")),
										SectionName: common.PointerTo(gwv1beta1.SectionName("l1")),
									},
									ControllerName: testControllerName,
									Conditions: []metav1.Condition{
										{
											Type:    "ResolvedRefs",
											Status:  metav1.ConditionTrue,
											Reason:  "ResolvedRefs",
											Message: "resolved backend references",
										},
										{
											Type:    "Accepted",
											Status:  metav1.ConditionFalse,
											Reason:  "UnsupportedValue",
											Message: "invalid externalref filter kind",
										},
									},
								},
							},
						},
					},
				},
				common.PointerTo(testHTTPRoute("http-route-2", []string{"gateway"}, nil)),
				addClassConfig(gatewayWithFinalizerStatus(gwv1beta1.GatewaySpec{
					Listeners: []gwv1beta1.Listener{
						{
							Name:     "l1",
							Protocol: gwv1beta1.HTTPProtocolType,
						},
					},
				}, gwv1beta1.GatewayStatus{
					Addresses: []gwv1beta1.GatewayAddress{},
					Conditions: []metav1.Condition{{
						Type:    "Accepted",
						Status:  metav1.ConditionTrue,
						Reason:  "Accepted",
						Message: "gateway accepted",
					}, {
						Type:    "Programmed",
						Status:  metav1.ConditionFalse,
						Reason:  "Pending",
						Message: "gateway pods are still being scheduled",
					}},
					Listeners: []gwv1beta1.ListenerStatus{
						{
							Name:           "l1",
							SupportedKinds: []gwv1beta1.RouteGroupKind{{Group: (*gwv1beta1.Group)(&common.BetaGroup), Kind: "HTTPRoute"}},
							Conditions: []metav1.Condition{
								{
									Type:    "Accepted",
									Status:  "True",
									Reason:  "Accepted",
									Message: "listener accepted",
								},
								{
									Type:    "Programmed",
									Status:  "True",
									Reason:  "Programmed",
									Message: "listener programmed",
								},
								{
									Type:    "Conflicted",
									Status:  "False",
									Reason:  "NoConflicts",
									Message: "listener has no conflicts",
								},
								{
									Type:    "ResolvedRefs",
									Status:  "True",
									Reason:  "ResolvedRefs",
									Message: "resolved references",
								},
							},
						},
					},
				})),
			},
			expectedUpdates:         []client.Object{},
			expectedConsulDeletions: []api.ResourceReference{},
			expectedConsulUpdates: []api.ConfigEntry{
				&api.APIGatewayConfigEntry{
					Kind:      "api-gateway",
					Name:      "gateway",
					Meta:      map[string]string{"k8s-name": "gateway", "k8s-namespace": "default"},
					Listeners: []api.APIGatewayListener{{Name: "l1", Protocol: "http"}},
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt.resources.gateways = append(tt.resources.gateways, tt.config.Gateway)
			tt.resources.httpRoutes = append(tt.resources.httpRoutes, tt.config.HTTPRoutes...)
			tt.resources.tcpRoutes = append(tt.resources.tcpRoutes, tt.config.TCPRoutes...)

			tt.config.Resources = newTestResourceMap(t, tt.resources)
			tt.config.ControllerName = testControllerName
			tt.config.Logger = logrtest.NewTestLogger(t)
			tt.config.GatewayClassConfig = &v1alpha1.GatewayClassConfig{}
			serializeGatewayClassConfig(&tt.config.Gateway, tt.config.GatewayClassConfig)

			binder := NewBinder(tt.config)
			actual := binder.Snapshot()

			actualConsulUpdates := common.ConvertSliceFunc(actual.Consul.Updates, func(op *common.ConsulUpdateOperation) api.ConfigEntry {
				return op.Entry
			})

			require.ElementsMatch(t, tt.expectedConsulUpdates, actualConsulUpdates, "consul updates differ", cmp.Diff(tt.expectedConsulUpdates, actualConsulUpdates))
			require.ElementsMatch(t, tt.expectedConsulDeletions, actual.Consul.Deletions, "consul deletions differ")
			require.ElementsMatch(t, tt.expectedStatusUpdates, actual.Kubernetes.StatusUpdates.Operations(), "kubernetes statuses differ", cmp.Diff(tt.expectedStatusUpdates, actual.Kubernetes.StatusUpdates.Operations()))
			require.ElementsMatch(t, tt.expectedUpdates, actual.Kubernetes.Updates.Operations(), "kubernetes updates differ", cmp.Diff(tt.expectedUpdates, actual.Kubernetes.Updates.Operations()))
		})
	}
}

func TestBinder_Registrations(t *testing.T) {
	t.Parallel()

	setDeleted := func(gateway gwv1beta1.Gateway) gwv1beta1.Gateway {
		gateway.DeletionTimestamp = deletionTimestamp
		return gateway
	}

	for name, tt := range map[string]struct {
		config                  BinderConfig
		resources               resourceMapResources
		expectedRegistrations   []string
		expectedDeregistrations []api.CatalogDeregistration
	}{
		"deleting gateway with consul services": {
			config: controlledBinder(BinderConfig{
				Gateway: setDeleted(gatewayWithFinalizer(gwv1beta1.GatewaySpec{})),
				ConsulGatewayServices: []api.CatalogService{
					{Node: "test", ServiceID: "pod1", Namespace: "namespace1"},
					{Node: "test", ServiceID: "pod2", Namespace: "namespace1"},
					{Node: "test", ServiceID: "pod3", Namespace: "namespace1"},
				},
				Pods: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
						Status: corev1.PodStatus{
							Phase:      corev1.PodRunning,
							Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod2"},
						Status: corev1.PodStatus{
							Phase:      corev1.PodRunning,
							Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod3"},
						Status: corev1.PodStatus{
							Phase:      corev1.PodRunning,
							Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						},
					},
				},
			}),
			expectedDeregistrations: []api.CatalogDeregistration{
				{Node: "test", ServiceID: "pod1", Namespace: "namespace1"},
				{Node: "test", ServiceID: "pod2", Namespace: "namespace1"},
				{Node: "test", ServiceID: "pod3", Namespace: "namespace1"},
			},
		},
		"gateway with consul services and mixed pods": {
			config: controlledBinder(BinderConfig{
				Gateway: gatewayWithFinalizer(gwv1beta1.GatewaySpec{}),
				Pods: []corev1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod1", Namespace: "namespace1"},
						Status: corev1.PodStatus{
							Phase:      corev1.PodRunning,
							Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod3", Namespace: "namespace1"},
						Status: corev1.PodStatus{
							Phase: corev1.PodFailed,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "pod4", Namespace: "namespace1"},
						Status: corev1.PodStatus{
							Phase:      corev1.PodRunning,
							Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
						},
					},
				},
				ConsulGatewayServices: []api.CatalogService{
					{Node: "test", ServiceID: "pod1", Namespace: "namespace1"},
					{Node: "test", ServiceID: "pod2", Namespace: "namespace1"},
					{Node: "test", ServiceID: "pod3", Namespace: "namespace1"},
				},
			}),
			expectedRegistrations: []string{"pod1", "pod3", "pod4"},
			expectedDeregistrations: []api.CatalogDeregistration{
				{Node: "test", ServiceID: "pod2", Namespace: "namespace1"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt.resources.gateways = append(tt.resources.gateways, tt.config.Gateway)
			tt.resources.httpRoutes = append(tt.resources.httpRoutes, tt.config.HTTPRoutes...)
			tt.resources.tcpRoutes = append(tt.resources.tcpRoutes, tt.config.TCPRoutes...)

			tt.config.Resources = newTestResourceMap(t, tt.resources)
			tt.config.ControllerName = testControllerName
			tt.config.Logger = logrtest.NewTestLogger(t)
			tt.config.GatewayClassConfig = &v1alpha1.GatewayClassConfig{}
			serializeGatewayClassConfig(&tt.config.Gateway, tt.config.GatewayClassConfig)

			binder := NewBinder(tt.config)
			actual := binder.Snapshot()

			require.Len(t, actual.Consul.Registrations, len(tt.expectedRegistrations))
			for i := range actual.Consul.Registrations {
				registration := actual.Consul.Registrations[i]
				expected := tt.expectedRegistrations[i]

				require.EqualValues(t, expected, registration.Service.ID)
				require.EqualValues(t, "gateway", registration.Service.Service)
			}

			require.EqualValues(t, tt.expectedDeregistrations, actual.Consul.Deregistrations)
		})
	}
}

func TestBinder_BindingRulesKitchenSink(t *testing.T) {
	t.Parallel()

	gateway := gatewayWithFinalizer(gwv1beta1.GatewaySpec{
		Listeners: []gwv1beta1.Listener{{
			Name:     "http-listener-default-same",
			Protocol: gwv1beta1.HTTPProtocolType,
		}, {
			Name:     "http-listener-hostname",
			Protocol: gwv1beta1.HTTPProtocolType,
			Hostname: common.PointerTo[gwv1beta1.Hostname]("host.name"),
		}, {
			Name:     "http-listener-mismatched-kind-allowed",
			Protocol: gwv1beta1.HTTPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Kinds: []gwv1beta1.RouteGroupKind{{
					Kind: "Foo",
				}},
			},
		}, {
			Name:     "http-listener-explicit-all-allowed",
			Protocol: gwv1beta1.HTTPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromAll),
				},
			},
		}, {
			Name:     "http-listener-explicit-allowed-same",
			Protocol: gwv1beta1.HTTPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromSame),
				},
			},
		}, {
			Name:     "http-listener-allowed-selector",
			Protocol: gwv1beta1.HTTPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromSelector),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "foo",
						},
					},
				},
			},
		}, {
			Name:     "http-listener-tls",
			Protocol: gwv1beta1.HTTPSProtocolType,
			TLS: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{{
					Name: "secret-one",
				}},
			},
		}, {
			Name:     "tcp-listener-default-same",
			Protocol: gwv1beta1.TCPProtocolType,
		}, {
			Name:     "tcp-listener-mismatched-kind-allowed",
			Protocol: gwv1beta1.TCPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Kinds: []gwv1beta1.RouteGroupKind{{
					Kind: "Foo",
				}},
			},
		}, {
			Name:     "tcp-listener-explicit-all-allowed",
			Protocol: gwv1beta1.TCPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromAll),
				},
			},
		}, {
			Name:     "tcp-listener-explicit-allowed-same",
			Protocol: gwv1beta1.TCPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromSame),
				},
			},
		}, {
			Name:     "tcp-listener-allowed-selector",
			Protocol: gwv1beta1.TCPProtocolType,
			AllowedRoutes: &gwv1beta1.AllowedRoutes{
				Namespaces: &gwv1beta1.RouteNamespaces{
					From: common.PointerTo(gwv1beta1.NamespacesFromSelector),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "foo",
						},
					},
				},
			},
		}, {
			Name:     "tcp-listener-tls",
			Protocol: gwv1beta1.TCPProtocolType,
			TLS: &gwv1beta1.GatewayTLSConfig{
				CertificateRefs: []gwv1beta1.SecretObjectReference{{
					Name: "secret-one",
				}},
			},
		}},
	})

	namespaces := map[string]corev1.Namespace{
		"default": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		},
		"test": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Labels: map[string]string{
					"test": "foo",
				},
			},
		},
	}

	_, secretOne := generateTestCertificate(t, "", "secret-one")

	gateway.Namespace = "default"
	defaultNamespacePointer := common.PointerTo[gwv1beta1.Namespace]("default")

	for name, tt := range map[string]struct {
		httpRoute             *gwv1beta1.HTTPRoute
		tcpRoute              *gwv1alpha2.TCPRoute
		referenceGrants       []gwv1beta1.ReferenceGrant
		expectedStatusUpdates []client.Object
	}{
		"untargeted http route same namespace": {
			httpRoute: testHTTPRouteBackends("route", "default", nil, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "default", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted http route same namespace missing backend": {
			httpRoute: testHTTPRouteBackends("route", "default", []gwv1beta1.BackendObjectReference{
				{Name: gwv1beta1.ObjectName("backend")},
			}, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "default", []gwv1beta1.BackendObjectReference{
					{Name: gwv1beta1.ObjectName("backend")},
				}, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionFalse,
							Reason:  "BackendNotFound",
							Message: "default/backend: backend not found",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted http route same namespace invalid backend type": {
			httpRoute: testHTTPRouteBackends("route", "default", []gwv1beta1.BackendObjectReference{
				{
					Name:  gwv1beta1.ObjectName("backend"),
					Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
				},
			}, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "default", []gwv1beta1.BackendObjectReference{
					{
						Name:  gwv1beta1.ObjectName("backend"),
						Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
					},
				}, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionFalse,
							Reason:  "InvalidKind",
							Message: "default/backend [Service.invalid.foo.com]: invalid backend kind",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted http route different namespace": {
			httpRoute: testHTTPRouteBackends("route", "other", nil, []gwv1beta1.ParentReference{
				{
					Name:      "gateway",
					Namespace: defaultNamespacePointer,
				},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "other", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{
						Name:      "gateway",
						Namespace: defaultNamespacePointer,
					}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted http route different namespace and reference grants": {
			httpRoute: testHTTPRouteBackends("route", "other", nil, []gwv1beta1.ParentReference{
				{
					Name:      "gateway",
					Namespace: defaultNamespacePointer,
				},
			}),
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "HTTPRoute", Namespace: gwv1beta1.Namespace("other")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Group: gwv1beta1.GroupName, Kind: "Gateway"},
					},
				}},
			},
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "other", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{
						Name:      "gateway",
						Namespace: defaultNamespacePointer,
					}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"targeted http route same namespace": {
			httpRoute: testHTTPRouteBackends("route", "default", nil, []gwv1beta1.ParentReference{
				{
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
				},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "default", nil, []gwv1beta1.RouteParentStatus{
					{
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-allowed-selector: listener does not allow binding routes from the given namespace",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
						}},
					},
				}),
			},
		},
		"targeted http route different namespace": {
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "HTTPRoute", Namespace: gwv1beta1.Namespace("test")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Group: gwv1beta1.GroupName, Kind: "Gateway"},
					},
				}},
			},
			httpRoute: testHTTPRouteBackends("route", "test", nil, []gwv1beta1.ParentReference{
				{
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
				},
			}),
			expectedStatusUpdates: []client.Object{
				testHTTPRouteStatusBackends("route", "test", nil, []gwv1beta1.RouteParentStatus{
					{
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-default-same: listener does not allow binding routes from the given namespace",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-hostname: listener does not allow binding routes from the given namespace",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-explicit-allowed-same: listener does not allow binding routes from the given namespace",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-tls: listener does not allow binding routes from the given namespace",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
						}},
					},
				}),
			},
		},
		"untargeted tcp route same namespace": {
			tcpRoute: testTCPRouteBackends("route", "default", nil, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "default", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted tcp route same namespace missing backend": {
			tcpRoute: testTCPRouteBackends("route", "default", []gwv1beta1.BackendObjectReference{
				{Name: gwv1beta1.ObjectName("backend")},
			}, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "default", []gwv1beta1.BackendObjectReference{
					{Name: gwv1beta1.ObjectName("backend")},
				}, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionFalse,
							Reason:  "BackendNotFound",
							Message: "default/backend: backend not found",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted tcp route same namespace invalid backend type": {
			tcpRoute: testTCPRouteBackends("route", "default", []gwv1beta1.BackendObjectReference{
				{
					Name:  gwv1beta1.ObjectName("backend"),
					Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
				},
			}, []gwv1beta1.ParentReference{
				{Name: "gateway"},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "default", []gwv1beta1.BackendObjectReference{
					{
						Name:  gwv1beta1.ObjectName("backend"),
						Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
					},
				}, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{Name: "gateway"}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionFalse,
							Reason:  "InvalidKind",
							Message: "default/backend [Service.invalid.foo.com]: invalid backend kind",
						},
						{
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted tcp route different namespace": {
			tcpRoute: testTCPRouteBackends("route", "other", nil, []gwv1beta1.ParentReference{
				{
					Name:      "gateway",
					Namespace: defaultNamespacePointer,
				},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "other", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{
						Name:      "gateway",
						Namespace: defaultNamespacePointer,
					}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"untargeted tcp route different namespace and reference grants": {
			tcpRoute: testTCPRouteBackends("route", "other", nil, []gwv1beta1.ParentReference{
				{
					Name:      "gateway",
					Namespace: defaultNamespacePointer,
				},
			}),
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "TCPRoute", Namespace: gwv1beta1.Namespace("other")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Group: gwv1beta1.GroupName, Kind: "Gateway"},
					},
				}},
			},
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "other", nil, []gwv1beta1.RouteParentStatus{
					{ControllerName: testControllerName, ParentRef: gwv1beta1.ParentReference{
						Name:      "gateway",
						Namespace: defaultNamespacePointer,
					}, Conditions: []metav1.Condition{
						{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						},
					}},
				}),
			},
		},
		"targeted tcp route same namespace": {
			tcpRoute: testTCPRouteBackends("route", "default", nil, []gwv1beta1.ParentReference{
				{
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
				}, {
					Name:        "gateway",
					SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
				},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "default", nil, []gwv1beta1.RouteParentStatus{
					{
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-default-same: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-hostname: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-explicit-allowed-same: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-allowed-selector: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-tls: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					},
				}),
			},
		},
		"targeted tcp route different namespace": {
			referenceGrants: []gwv1beta1.ReferenceGrant{
				{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "grant"}, Spec: gwv1beta1.ReferenceGrantSpec{
					From: []gwv1beta1.ReferenceGrantFrom{
						{Group: gwv1beta1.GroupName, Kind: "TCPRoute", Namespace: gwv1beta1.Namespace("test")},
					},
					To: []gwv1beta1.ReferenceGrantTo{
						{Group: gwv1beta1.GroupName, Kind: "Gateway"},
					},
				}},
			},
			tcpRoute: testTCPRouteBackends("route", "test", nil, []gwv1beta1.ParentReference{
				{
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
				}, {
					Name:        "gateway",
					Namespace:   defaultNamespacePointer,
					SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
				},
			}),
			expectedStatusUpdates: []client.Object{
				testTCPRouteStatusBackends("route", "test", nil, []gwv1beta1.RouteParentStatus{
					{
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-default-same: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-hostname: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-explicit-allowed-same: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-allowed-selector: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionFalse,
							Reason:  "NotAllowedByListeners",
							Message: "http-listener-tls: listener does not support route protocol",
						}},
					}, {
						ControllerName: testControllerName,
						ParentRef: gwv1beta1.ParentReference{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						},
						Conditions: []metav1.Condition{{
							Type:    "ResolvedRefs",
							Status:  metav1.ConditionTrue,
							Reason:  "ResolvedRefs",
							Message: "resolved backend references",
						}, {
							Type:    "Accepted",
							Status:  metav1.ConditionTrue,
							Reason:  "Accepted",
							Message: "route accepted",
						}},
					},
				}),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := *addClassConfig(gateway)

			resources := resourceMapResources{
				gateways: []gwv1beta1.Gateway{g},
				secrets: []corev1.Secret{
					secretOne,
				},
				grants: tt.referenceGrants,
			}

			if tt.httpRoute != nil {
				resources.httpRoutes = append(resources.httpRoutes, *tt.httpRoute)
			}
			if tt.tcpRoute != nil {
				resources.tcpRoutes = append(resources.tcpRoutes, *tt.tcpRoute)
			}

			config := controlledBinder(BinderConfig{
				Gateway:            g,
				GatewayClassConfig: &v1alpha1.GatewayClassConfig{},
				Namespaces:         namespaces,
				Resources:          newTestResourceMap(t, resources),
				HTTPRoutes:         resources.httpRoutes,
				TCPRoutes:          resources.tcpRoutes,
			})

			binder := NewBinder(config)
			actual := binder.Snapshot()

			compareUpdates(t, tt.expectedStatusUpdates, actual.Kubernetes.StatusUpdates.Operations())
		})
	}
}

func compareUpdates(t *testing.T, expected []client.Object, actual []client.Object) {
	t.Helper()

	filtered := common.Filter(actual, func(o client.Object) bool {
		if _, ok := o.(*gwv1beta1.HTTPRoute); ok {
			return false
		}
		if _, ok := o.(*gwv1alpha2.TCPRoute); ok {
			return false
		}
		return true
	})

	require.ElementsMatch(t, expected, filtered, "statuses don't match", cmp.Diff(expected, filtered))
}

func addClassConfig(g gwv1beta1.Gateway) *gwv1beta1.Gateway {
	serializeGatewayClassConfig(&g, &v1alpha1.GatewayClassConfig{})
	return &g
}

func gatewayWithFinalizer(spec gwv1beta1.GatewaySpec) gwv1beta1.Gateway {
	spec.GatewayClassName = testGatewayClassObjectName

	typeMeta := metav1.TypeMeta{}
	typeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("Gateway"))

	return gwv1beta1.Gateway{
		TypeMeta: typeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gateway",
			Namespace:  "default",
			Finalizers: []string{common.GatewayFinalizer},
		},
		Spec: spec,
	}
}

func gatewayWithFinalizerStatus(spec gwv1beta1.GatewaySpec, status gwv1beta1.GatewayStatus) gwv1beta1.Gateway {
	g := gatewayWithFinalizer(spec)
	g.Status = status
	return g
}

func testHTTPRoute(name string, parents []string, services []string) gwv1beta1.HTTPRoute {
	var parentRefs []gwv1beta1.ParentReference
	var rules []gwv1beta1.HTTPRouteRule

	for _, parent := range parents {
		parentRefs = append(parentRefs, gwv1beta1.ParentReference{Name: gwv1beta1.ObjectName(parent)})
	}

	for _, service := range services {
		rules = append(rules, gwv1beta1.HTTPRouteRule{
			BackendRefs: []gwv1beta1.HTTPBackendRef{
				{
					BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: gwv1beta1.BackendObjectReference{
							Name: gwv1beta1.ObjectName(service),
						},
					},
				},
			},
		})
	}

	httpTypeMeta := metav1.TypeMeta{}
	httpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("HTTPRoute"))

	return gwv1beta1.HTTPRoute{
		TypeMeta:   httpTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: rules,
		},
	}
}

func testHTTPRouteBackends(name, namespace string, services []gwv1beta1.BackendObjectReference, parents []gwv1beta1.ParentReference) *gwv1beta1.HTTPRoute {
	var rules []gwv1beta1.HTTPRouteRule
	for _, service := range services {
		rules = append(rules, gwv1beta1.HTTPRouteRule{
			BackendRefs: []gwv1beta1.HTTPBackendRef{
				{
					BackendRef: gwv1beta1.BackendRef{
						BackendObjectReference: service,
					},
				},
			},
		})
	}

	httpTypeMeta := metav1.TypeMeta{}
	httpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("HTTPRoute"))

	return &gwv1beta1.HTTPRoute{
		TypeMeta:   httpTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parents,
			},
			Rules: rules,
		},
	}
}

func testHTTPRouteStatusBackends(name, namespace string, services []gwv1beta1.BackendObjectReference, parentStatuses []gwv1beta1.RouteParentStatus) *gwv1beta1.HTTPRoute {
	var parentRefs []gwv1beta1.ParentReference

	for _, parent := range parentStatuses {
		parentRefs = append(parentRefs, parent.ParentRef)
	}

	route := testHTTPRouteBackends(name, namespace, services, parentRefs)
	route.Status.RouteStatus.Parents = parentStatuses
	return route
}

func testHTTPRouteStatus(name string, services []string, parentStatuses []gwv1beta1.RouteParentStatus, extraParents ...string) gwv1beta1.HTTPRoute {
	parentRefs := extraParents

	for _, parent := range parentStatuses {
		parentRefs = append(parentRefs, string(parent.ParentRef.Name))
	}

	route := testHTTPRoute(name, parentRefs, services)
	route.Status.RouteStatus.Parents = parentStatuses

	return route
}

func testTCPRoute(name string, parents []string, services []string) gwv1alpha2.TCPRoute {
	var parentRefs []gwv1beta1.ParentReference
	var rules []gwv1alpha2.TCPRouteRule

	for _, parent := range parents {
		parentRefs = append(parentRefs, gwv1beta1.ParentReference{Name: gwv1beta1.ObjectName(parent)})
	}

	for _, service := range services {
		rules = append(rules, gwv1alpha2.TCPRouteRule{
			BackendRefs: []gwv1beta1.BackendRef{
				{
					BackendObjectReference: gwv1beta1.BackendObjectReference{
						Name: gwv1beta1.ObjectName(service),
					},
				},
			},
		})
	}

	tcpTypeMeta := metav1.TypeMeta{}
	tcpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("TCPRoute"))

	return gwv1alpha2.TCPRoute{
		TypeMeta:   tcpTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: rules,
		},
	}
}

func testTCPRouteBackends(name, namespace string, services []gwv1beta1.BackendObjectReference, parents []gwv1beta1.ParentReference) *gwv1alpha2.TCPRoute {
	var rules []gwv1alpha2.TCPRouteRule
	for _, service := range services {
		rules = append(rules, gwv1alpha2.TCPRouteRule{
			BackendRefs: []gwv1beta1.BackendRef{
				{BackendObjectReference: service},
			},
		})
	}

	tcpTypeMeta := metav1.TypeMeta{}
	tcpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("TCPRoute"))

	return &gwv1alpha2.TCPRoute{
		TypeMeta:   tcpTypeMeta,
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parents,
			},
			Rules: rules,
		},
	}
}

func testTCPRouteStatusBackends(name, namespace string, services []gwv1beta1.BackendObjectReference, parentStatuses []gwv1beta1.RouteParentStatus) *gwv1alpha2.TCPRoute {
	var parentRefs []gwv1beta1.ParentReference

	for _, parent := range parentStatuses {
		parentRefs = append(parentRefs, parent.ParentRef)
	}

	route := testTCPRouteBackends(name, namespace, services, parentRefs)
	route.Status.RouteStatus.Parents = parentStatuses
	return route
}

func testTCPRouteStatus(name string, services []string, parentStatuses []gwv1beta1.RouteParentStatus, extraParents ...string) gwv1alpha2.TCPRoute {
	parentRefs := extraParents

	for _, parent := range parentStatuses {
		parentRefs = append(parentRefs, string(parent.ParentRef.Name))
	}

	route := testTCPRoute(name, parentRefs, services)
	route.Status.RouteStatus.Parents = parentStatuses

	return route
}

func controlledBinder(config BinderConfig) BinderConfig {
	config.ControllerName = testControllerName
	config.GatewayClass = testGatewayClass
	return config
}

func generateTestCertificate(t *testing.T, namespace, name string) (*api.FileSystemCertificateConfigEntry, corev1.Secret) {
	privateKey, err := rsa.GenerateKey(rand.Reader, common.MinKeyLength)
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

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       certBytes,
			corev1.TLSPrivateKeyKey: privateKeyBytes,
		},
	}

	certificate := (common.ResourceTranslator{}).ToFileSystemCertificate(secret)

	return certificate, secret
}
