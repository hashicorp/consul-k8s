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
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
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
	grants           []gwv1beta1.ReferenceGrant
	secrets          []corev1.Secret
	gateways         []gwv1beta1.Gateway
	httpRoutes       []gwv1beta1.HTTPRoute
	tcpRoutes        []gwv1alpha2.TCPRoute
	meshServices     []v1alpha1.MeshService
	services         []types.NamespacedName
	consulHTTPRoutes []api.HTTPRouteConfigEntry
	consulTCPRoutes  []api.TCPRouteConfigEntry
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
	return resourceMap
}

func TestBinder_Lifecycle(t *testing.T) {
	t.Parallel()

	certificateOne, secretOne := generateTestCertificate(t, "", "secret-one")
	certificateTwo, secretTwo := generateTestCertificate(t, "", "secret-two")

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
									Type:    "Conflicted",
									Status:  metav1.ConditionFalse,
									Reason:  "NoConflicts",
									Message: "listener has no conflicts",
								}, {
									Type:    "ResolvedRefs",
									Status:  metav1.ConditionTrue,
									Reason:  "ResolvedRefs",
									Message: "resolved certificate references",
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
						"k8s-namespace": "",
					},
					Listeners: []api.APIGatewayListener{{
						Protocol: "http",
						TLS: api.APIGatewayTLSConfiguration{
							Certificates: []api.ResourceReference{{
								Kind: api.InlineCertificate,
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
						"k8s-namespace": "",
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
				ConsulHTTPRoutes: []api.HTTPRouteConfigEntry{{
					Kind: api.HTTPRoute,
					Name: "route",
					Parents: []api.ResourceReference{
						{Kind: api.APIGateway, Name: "gateway"},
					},
				}},
			}),
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
						"k8s-namespace": "",
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
						"k8s-namespace": "",
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
				ConsulTCPRoutes: []api.TCPRouteConfigEntry{{
					Kind: api.TCPRoute,
					Name: "route",
					Parents: []api.ResourceReference{
						{Kind: api.APIGateway, Name: "gateway"},
					},
				}},
			}),
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
						"k8s-namespace": "",
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
				ConsulHTTPRoutes: []api.HTTPRouteConfigEntry{
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
				ConsulTCPRoutes: []api.TCPRouteConfigEntry{
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
				ConsulInlineCertificates: []api.InlineCertificateConfigEntry{
					*certificateOne,
					*certificateTwo,
				},
			}),
			resources: resourceMapResources{
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
					Status: api.ConfigEntryStatus{Conditions: []api.Condition{}},
				},
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
					Status: api.ConfigEntryStatus{Conditions: []api.Condition{}},
				},
			},
			expectedConsulDeletions: []api.ResourceReference{
				{Kind: api.HTTPRoute, Name: "http-route-one"},
				{Kind: api.TCPRoute, Name: "tcp-route-one"},
				{Kind: api.InlineCertificate, Name: "secret-two"},
				{Kind: api.APIGateway, Name: "gateway-deleted"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt.resources.gateways = append(tt.resources.gateways, tt.config.Gateway)
			tt.resources.httpRoutes = append(tt.resources.httpRoutes, tt.config.HTTPRoutes...)
			tt.resources.tcpRoutes = append(tt.resources.tcpRoutes, tt.config.TCPRoutes...)
			tt.resources.consulHTTPRoutes = append(tt.resources.consulHTTPRoutes, tt.config.ConsulHTTPRoutes...)
			tt.resources.consulTCPRoutes = append(tt.resources.consulTCPRoutes, tt.config.ConsulTCPRoutes...)

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
			tt.resources.consulHTTPRoutes = append(tt.resources.consulHTTPRoutes, tt.config.ConsulHTTPRoutes...)
			tt.resources.consulTCPRoutes = append(tt.resources.consulTCPRoutes, tt.config.ConsulTCPRoutes...)

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

// func TestBinder_BindingRulesKitchenSink(t *testing.T) {
// 	t.Parallel()

// 	className := "gateway-class"
// 	gatewayClassName := gwv1beta1.ObjectName(className)
// 	controllerName := "test-controller"
// 	gatewayClass := &gwv1beta1.GatewayClass{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name: className,
// 		},
// 		Spec: gwv1beta1.GatewayClassSpec{
// 			ControllerName: gwv1beta1.GatewayController(controllerName),
// 		},
// 	}

// 	gateway := gwv1beta1.Gateway{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:       "gateway",
// 			Finalizers: []string{common.GatewayFinalizer},
// 		},
// 		Spec: gwv1beta1.GatewaySpec{
// 			GatewayClassName: gatewayClassName,
// 			Listeners: []gwv1beta1.Listener{{
// 				Name:     "http-listener-default-same",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 			}, {
// 				Name:     "http-listener-hostname",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 				Hostname: common.PointerTo[gwv1beta1.Hostname]("host.name"),
// 			}, {
// 				Name:     "http-listener-mismatched-kind-allowed",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Kinds: []gwv1beta1.RouteGroupKind{{
// 						Kind: "Foo",
// 					}},
// 				},
// 			}, {
// 				Name:     "http-listener-explicit-all-allowed",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromAll),
// 					},
// 				},
// 			}, {
// 				Name:     "http-listener-explicit-allowed-same",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromSame),
// 					},
// 				},
// 			}, {
// 				Name:     "http-listener-allowed-selector",
// 				Protocol: gwv1beta1.HTTPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromSelector),
// 						Selector: &metav1.LabelSelector{
// 							MatchLabels: map[string]string{
// 								"test": "foo",
// 							},
// 						},
// 					},
// 				},
// 			}, {
// 				Name:     "http-listener-tls",
// 				Protocol: gwv1beta1.HTTPSProtocolType,
// 				TLS: &gwv1beta1.GatewayTLSConfig{
// 					CertificateRefs: []gwv1beta1.SecretObjectReference{{
// 						Name: "secret-one",
// 					}},
// 				},
// 			}, {
// 				Name:     "tcp-listener-default-same",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 			}, {
// 				Name:     "tcp-listener-mismatched-kind-allowed",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Kinds: []gwv1beta1.RouteGroupKind{{
// 						Kind: "Foo",
// 					}},
// 				},
// 			}, {
// 				Name:     "tcp-listener-explicit-all-allowed",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromAll),
// 					},
// 				},
// 			}, {
// 				Name:     "tcp-listener-explicit-allowed-same",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromSame),
// 					},
// 				},
// 			}, {
// 				Name:     "tcp-listener-allowed-selector",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 				AllowedRoutes: &gwv1beta1.AllowedRoutes{
// 					Namespaces: &gwv1beta1.RouteNamespaces{
// 						From: common.PointerTo(gwv1beta1.NamespacesFromSelector),
// 						Selector: &metav1.LabelSelector{
// 							MatchLabels: map[string]string{
// 								"test": "foo",
// 							},
// 						},
// 					},
// 				},
// 			}, {
// 				Name:     "tcp-listener-tls",
// 				Protocol: gwv1beta1.TCPProtocolType,
// 				TLS: &gwv1beta1.GatewayTLSConfig{
// 					CertificateRefs: []gwv1beta1.SecretObjectReference{{
// 						Name: "secret-one",
// 					}},
// 				},
// 			}},
// 		},
// 	}

// 	namespaces := map[string]corev1.Namespace{
// 		"": {
// 			ObjectMeta: metav1.ObjectMeta{
// 				Name: "",
// 			},
// 		},
// 		"test": {
// 			ObjectMeta: metav1.ObjectMeta{
// 				Name: "test",
// 				Labels: map[string]string{
// 					"test": "foo",
// 				},
// 			},
// 		},
// 	}

// 	defaultNamespacePointer := common.PointerTo[gwv1beta1.Namespace]("")

// 	httpTypeMeta := metav1.TypeMeta{}
// 	httpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("HTTPRoute"))
// 	tcpTypeMeta := metav1.TypeMeta{}
// 	tcpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("TCPRoute"))

// 	for name, tt := range map[string]struct {
// 		httpRoute                     *gwv1beta1.HTTPRoute
// 		expectedHTTPRouteUpdate       *gwv1beta1.HTTPRoute
// 		expectedHTTPRouteUpdateStatus *gwv1beta1.HTTPRoute
// 		expectedHTTPConsulRouteUpdate *api.HTTPRouteConfigEntry
// 		expectedHTTPConsulRouteDelete *api.ResourceReference

// 		tcpRoute                     *gwv1alpha2.TCPRoute
// 		expectedTCPRouteUpdate       *gwv1alpha2.TCPRoute
// 		expectedTCPRouteUpdateStatus *gwv1alpha2.TCPRoute
// 		expectedTCPConsulRouteUpdate *api.TCPRouteConfigEntry
// 		expectedTCPConsulRouteDelete *api.ResourceReference
// 	}{
// 		"untargeted http route same namespace": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted http route same namespace missing backend": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1beta1.HTTPRouteRule{{
// 						BackendRefs: []gwv1beta1.HTTPBackendRef{{
// 							BackendRef: gwv1beta1.BackendRef{
// 								BackendObjectReference: gwv1beta1.BackendObjectReference{
// 									Name: gwv1beta1.ObjectName("backend"),
// 								},
// 							},
// 						}},
// 					}},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1beta1.HTTPRouteRule{{
// 						BackendRefs: []gwv1beta1.HTTPBackendRef{{
// 							BackendRef: gwv1beta1.BackendRef{
// 								BackendObjectReference: gwv1beta1.BackendObjectReference{
// 									Name: gwv1beta1.ObjectName("backend"),
// 								},
// 							},
// 						}},
// 					}},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "BackendNotFound",
// 								Message: "/backend: backend not found",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted http route same namespace invalid backend type": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1beta1.HTTPRouteRule{{
// 						BackendRefs: []gwv1beta1.HTTPBackendRef{{
// 							BackendRef: gwv1beta1.BackendRef{
// 								BackendObjectReference: gwv1beta1.BackendObjectReference{
// 									Name:  gwv1beta1.ObjectName("backend"),
// 									Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
// 								},
// 							},
// 						}},
// 					}},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1beta1.HTTPRouteRule{{
// 						BackendRefs: []gwv1beta1.HTTPBackendRef{{
// 							BackendRef: gwv1beta1.BackendRef{
// 								BackendObjectReference: gwv1beta1.BackendObjectReference{
// 									Name:  gwv1beta1.ObjectName("backend"),
// 									Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
// 								},
// 							},
// 						}},
// 					}},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "InvalidKind",
// 								Message: "/backend [Service.invalid.foo.com]: invalid backend kind",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted http route different namespace": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:      "gateway",
// 							Namespace: defaultNamespacePointer,
// 						}},
// 					},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:      "gateway",
// 							Namespace: defaultNamespacePointer,
// 						}},
// 					},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:      "gateway",
// 								Namespace: defaultNamespacePointer,
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"targeted http route same namespace": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-allowed-selector: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"targeted http route different namespace": {
// 			httpRoute: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 			},
// 			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
// 				TypeMeta: httpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1beta1.HTTPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 				Status: gwv1beta1.HTTPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-default-same: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-hostname: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-explicit-allowed-same: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-tls"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-tls: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted tcp route same namespace": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted tcp route same namespace missing backend": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1alpha2.TCPRouteRule{{
// 						BackendRefs: []gwv1beta1.BackendRef{{
// 							BackendObjectReference: gwv1beta1.BackendObjectReference{
// 								Name: gwv1beta1.ObjectName("backend"),
// 							},
// 						}},
// 					}},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1alpha2.TCPRouteRule{{
// 						BackendRefs: []gwv1beta1.BackendRef{{
// 							BackendObjectReference: gwv1beta1.BackendObjectReference{
// 								Name: gwv1beta1.ObjectName("backend"),
// 							},
// 						}},
// 					}},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "BackendNotFound",
// 								Message: "/backend: backend not found",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted tcp route same namespace invalid backend type": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1alpha2.TCPRouteRule{{
// 						BackendRefs: []gwv1beta1.BackendRef{{
// 							BackendObjectReference: gwv1beta1.BackendObjectReference{
// 								Name:  gwv1beta1.ObjectName("backend"),
// 								Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
// 							},
// 						}},
// 					}},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name: "gateway",
// 						}},
// 					},
// 					Rules: []gwv1alpha2.TCPRouteRule{{
// 						BackendRefs: []gwv1beta1.BackendRef{{
// 							BackendObjectReference: gwv1beta1.BackendObjectReference{
// 								Name:  gwv1beta1.ObjectName("backend"),
// 								Group: common.PointerTo[gwv1beta1.Group]("invalid.foo.com"),
// 							},
// 						}},
// 					}},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name: "gateway",
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "InvalidKind",
// 								Message: "/backend [Service.invalid.foo.com]: invalid backend kind",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"untargeted tcp route different namespace": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:      "gateway",
// 							Namespace: defaultNamespacePointer,
// 						}},
// 					},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:      "gateway",
// 							Namespace: defaultNamespacePointer,
// 						}},
// 					},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:      "gateway",
// 								Namespace: defaultNamespacePointer,
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"targeted tcp route same namespace": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-mismatched-kind-allowed: listener does not support route protocol",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-allowed-selector: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 		"targeted tcp route different namespace": {
// 			tcpRoute: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 			},
// 			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
// 				TypeMeta: tcpTypeMeta,
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:       "route",
// 					Namespace:  "test",
// 					Finalizers: []string{common.GatewayFinalizer},
// 				},
// 				Spec: gwv1alpha2.TCPRouteSpec{
// 					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
// 						ParentRefs: []gwv1beta1.ParentReference{{
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 						}, {
// 							Name:        "gateway",
// 							Namespace:   defaultNamespacePointer,
// 							SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 						}},
// 					},
// 				},
// 				Status: gwv1alpha2.TCPRouteStatus{
// 					RouteStatus: gwv1beta1.RouteStatus{
// 						Parents: []gwv1beta1.RouteParentStatus{{
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-default-same: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-mismatched-kind-allowed: listener does not support route protocol",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-explicit-allowed-same: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "Accepted",
// 								Message: "route accepted",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "tcp-listener-tls: listener does not allow binding routes from the given namespace",
// 							}},
// 						}, {
// 							ControllerName: gatewayClass.Spec.ControllerName,
// 							ParentRef: gwv1beta1.ParentReference{
// 								Name:        "gateway",
// 								Namespace:   defaultNamespacePointer,
// 								SectionName: common.PointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
// 							},
// 							Conditions: []metav1.Condition{{
// 								Type:    "ResolvedRefs",
// 								Status:  metav1.ConditionTrue,
// 								Reason:  "ResolvedRefs",
// 								Message: "resolved backend references",
// 							}, {
// 								Type:    "Accepted",
// 								Status:  metav1.ConditionFalse,
// 								Reason:  "NotAllowedByListeners",
// 								Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
// 							}},
// 						}},
// 					},
// 				},
// 			},
// 		},
// 	} {
// 		t.Run(name, func(t *testing.T) {
// 			config := BinderConfig{
// 				ControllerName:     controllerName,
// 				GatewayClassConfig: &v1alpha1.GatewayClassConfig{},
// 				GatewayClass:       gatewayClass,
// 				Gateway:            gateway,
// 				Namespaces:         namespaces,
// 				ControlledGateways: map[types.NamespacedName]gwv1beta1.Gateway{
// 					{Name: "gateway"}: gateway,
// 				},
// 				Secrets: []corev1.Secret{{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name: "secret-one",
// 					},
// 				}},
// 			}
// 			serializeGatewayClassConfig(&config.Gateway, config.GatewayClassConfig)

// 			if tt.httpRoute != nil {
// 				config.HTTPRoutes = append(config.HTTPRoutes, *tt.httpRoute)
// 			}
// 			if tt.tcpRoute != nil {
// 				config.TCPRoutes = append(config.TCPRoutes, *tt.tcpRoute)
// 			}

// 			binder := NewBinder(config)
// 			actual := binder.Snapshot()

// 			compareUpdates(t, tt.expectedHTTPRouteUpdate, actual.Kubernetes.Updates)
// 			compareUpdates(t, tt.expectedTCPRouteUpdate, actual.Kubernetes.Updates)
// 			compareUpdates(t, tt.expectedHTTPRouteUpdateStatus, actual.Kubernetes.StatusUpdates)
// 			compareUpdates(t, tt.expectedTCPRouteUpdateStatus, actual.Kubernetes.StatusUpdates)
// 		})
// 	}
// }

// func compareUpdates[T client.Object](t *testing.T, expected T, updates []client.Object) {
// 	t.Helper()

// 	if isNil(expected) {
// 		for _, update := range updates {
// 			if u, ok := update.(T); ok {
// 				t.Error("found unexpected update", u)
// 			}
// 		}
// 	} else {
// 		found := false
// 		for _, update := range updates {
// 			if u, ok := update.(T); ok {
// 				diff := cmp.Diff(expected, u, cmp.FilterPath(func(p cmp.Path) bool {
// 					return p.String() == "Status.RouteStatus.Parents.Conditions.LastTransitionTime"
// 				}, cmp.Ignore()))
// 				if diff != "" {
// 					t.Error("diff between actual and expected", diff)
// 				}
// 				found = true
// 			}
// 		}
// 		if !found {
// 			t.Error("expected route update not found in", updates)
// 		}
// 	}
// }

func addClassConfig(g gwv1beta1.Gateway) *gwv1beta1.Gateway {
	serializeGatewayClassConfig(&g, &v1alpha1.GatewayClassConfig{})
	return &g
}

func consulCertificateNamespaceName(namespace, name string) *api.InlineCertificateConfigEntry {
	return &api.InlineCertificateConfigEntry{
		Kind: api.InlineCertificate,
		Name: name,
		Meta: map[string]string{
			"k8s-name":      name,
			"k8s-namespace": namespace,
		},
	}
}

func gatewayWithFinalizer(spec gwv1beta1.GatewaySpec) gwv1beta1.Gateway {
	spec.GatewayClassName = testGatewayClassObjectName

	return gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gateway",
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

	return gwv1beta1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: rules,
		},
	}
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

	return gwv1alpha2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{common.GatewayFinalizer}},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
			Rules: rules,
		},
	}
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

func generateTestCertificate(t *testing.T, namespace, name string) (*api.InlineCertificateConfigEntry, corev1.Secret) {
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

	certificate, err := (common.ResourceTranslator{}).ToInlineCertificate(secret)
	require.NoError(t, err)

	return certificate, secret
}
