package binding

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/statuses"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestBinder_Lifecycle(t *testing.T) {
	t.Parallel()

	className := "gateway-class"
	gatewayClassName := gwv1beta1.ObjectName(className)
	controllerName := "test-controller"
	deletionTimestamp := pointerTo(metav1.Now())
	gatewayClass := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: className,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gwv1beta1.GatewayController(controllerName),
		},
	}

	for name, tt := range map[string]struct {
		config   BinderConfig
		expected Snapshot
	}{
		"no gateway class and empty routes": {
			config: BinderConfig{
				Gateway: gwv1beta1.Gateway{},
			},
			expected: Snapshot{
				Consul: ConsulSnapshot{
					Deletions: []api.ResourceReference{{
						Kind: api.APIGateway,
					}},
				},
			},
		},
		"no gateway class and empty routes remove finalizer": {
			config: BinderConfig{
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{gatewayFinalizer},
					},
				},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1beta1.Gateway{
							ObjectMeta: metav1.ObjectMeta{
								Finalizers: []string{},
							},
						},
					},
				},
				Consul: ConsulSnapshot{
					Deletions: []api.ResourceReference{{
						Kind: api.APIGateway,
					}},
				},
			},
		},
		"deleting gateway empty routes": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1beta1.Gateway{
							ObjectMeta: metav1.ObjectMeta{
								DeletionTimestamp: deletionTimestamp,
								Finalizers:        []string{},
							},
							Spec: gwv1beta1.GatewaySpec{
								GatewayClassName: gatewayClassName,
							},
						},
					},
				},
				Consul: ConsulSnapshot{
					Deletions: []api.ResourceReference{{
						Kind: api.APIGateway,
					}},
				},
			},
		},
		"basic gateway no finalizer": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1beta1.Gateway{
							ObjectMeta: metav1.ObjectMeta{
								Finalizers: []string{gatewayFinalizer},
							},
							Spec: gwv1beta1.GatewaySpec{
								GatewayClassName: gatewayClassName,
							},
						},
					},
				},
				Consul: ConsulSnapshot{},
			},
		},
		"basic gateway": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwv1beta1.Listener{{
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{{
									Name: "secret-one",
								}},
							},
						}},
					},
				},
				Secrets: []corev1.Secret{{
					ObjectMeta: metav1.ObjectMeta{
						Name: "secret-one",
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.InlineCertificateConfigEntry{
							Kind: api.InlineCertificate,
							Name: "secret-one",
							Meta: map[string]string{
								"k8s-name":         "secret-one",
								"k8s-namespace":    "",
								"k8s-service-name": "secret-one",
								"managed-by":       "consul-k8s-gateway-controller",
							},
						},
						&api.APIGatewayConfigEntry{
							Kind: api.APIGateway,
							Meta: map[string]string{
								"k8s-name":         "",
								"k8s-namespace":    "",
								"k8s-service-name": "",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							Listeners: []api.APIGatewayListener{{
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
			},
		},
		"gateway http route no finalizer": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "gateway",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
				HTTPRoutes: []gwv1beta1.HTTPRoute{{
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1beta1.HTTPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Finalizers: []string{gatewayFinalizer},
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
				},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.APIGatewayConfigEntry{
							Kind: api.APIGateway,
							Name: "gateway",
							Meta: map[string]string{
								"k8s-name":         "gateway",
								"k8s-namespace":    "",
								"k8s-service-name": "gateway",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							Listeners: []api.APIGatewayListener{},
						},
					},
				},
			},
		},
		"gateway http route deleting": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "gateway",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
				HTTPRoutes: []gwv1beta1.HTTPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1beta1.HTTPRoute{
							ObjectMeta: metav1.ObjectMeta{
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
				},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.APIGatewayConfigEntry{
							Kind: api.APIGateway,
							Name: "gateway",
							Meta: map[string]string{
								"k8s-name":         "gateway",
								"k8s-namespace":    "",
								"k8s-service-name": "gateway",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							Listeners: []api.APIGatewayListener{},
						},
					},
					Deletions: []api.ResourceReference{{
						Kind: api.HTTPRoute,
					}},
				},
			},
		},
		"gateway tcp route no finalizer": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "gateway",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
				TCPRoutes: []gwv1alpha2.TCPRoute{{
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1alpha2.TCPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Finalizers: []string{gatewayFinalizer},
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
				},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.APIGatewayConfigEntry{
							Kind: api.APIGateway,
							Name: "gateway",
							Meta: map[string]string{
								"k8s-name":         "gateway",
								"k8s-namespace":    "",
								"k8s-service-name": "gateway",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							Listeners: []api.APIGatewayListener{},
						},
					},
				},
			},
		},
		"gateway tcp route deleting": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "gateway",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
					},
				},
				TCPRoutes: []gwv1alpha2.TCPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{gatewayFinalizer},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					Updates: []client.Object{
						&gwv1alpha2.TCPRoute{
							ObjectMeta: metav1.ObjectMeta{
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
				},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.APIGatewayConfigEntry{
							Kind: api.APIGateway,
							Name: "gateway",
							Meta: map[string]string{
								"k8s-name":         "gateway",
								"k8s-namespace":    "",
								"k8s-service-name": "gateway",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							Listeners: []api.APIGatewayListener{},
						},
					},
					Deletions: []api.ResourceReference{{
						Kind: api.TCPRoute,
					}},
				},
			},
		},
		"gateway deletion routes and secrets": {
			config: BinderConfig{
				ControllerName: controllerName,
				GatewayClass:   gatewayClass,
				Gateway: gwv1beta1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "gateway",
						DeletionTimestamp: deletionTimestamp,
						Finalizers:        []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.GatewaySpec{
						GatewayClassName: gatewayClassName,
						Listeners: []gwv1beta1.Listener{{
							TLS: &gwv1beta1.GatewayTLSConfig{
								CertificateRefs: []gwv1beta1.SecretObjectReference{{
									Name: "secret-one",
								}, {
									Name: "secret-two",
								}},
							},
						}},
					},
				},
				ControlledGateways: map[types.NamespacedName]gwv1beta1.Gateway{
					{Name: "gateway"}: {
						ObjectMeta: metav1.ObjectMeta{
							Name:              "gateway",
							DeletionTimestamp: deletionTimestamp,
							Finalizers:        []string{gatewayFinalizer},
						},
						Spec: gwv1beta1.GatewaySpec{
							GatewayClassName: gatewayClassName,
							Listeners: []gwv1beta1.Listener{{
								TLS: &gwv1beta1.GatewayTLSConfig{
									CertificateRefs: []gwv1beta1.SecretObjectReference{{
										Name: "secret-one",
									}, {
										Name: "secret-two",
									}},
								},
							}},
						},
					},
					{Name: "gateway-two"}: {
						ObjectMeta: metav1.ObjectMeta{
							Name:       "gateway-two",
							Finalizers: []string{gatewayFinalizer},
						},
						Spec: gwv1beta1.GatewaySpec{
							GatewayClassName: gatewayClassName,
							Listeners: []gwv1beta1.Listener{{
								TLS: &gwv1beta1.GatewayTLSConfig{
									CertificateRefs: []gwv1beta1.SecretObjectReference{{
										Name: "secret-one",
									}, {
										Name: "secret-three",
									}},
								},
							}},
						},
					},
				},
				Secrets: []corev1.Secret{{
					ObjectMeta: metav1.ObjectMeta{
						Name: "secret-one",
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name: "secret-two",
					},
				}},
				HTTPRoutes: []gwv1beta1.HTTPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "http-route-one",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "http-route-two",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1beta1.HTTPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}, {
								Name: "gateway-two",
							}},
						},
					},
					Status: gwv1beta1.HTTPRouteStatus{
						RouteStatus: gwv1beta1.RouteStatus{
							Parents: []gwv1beta1.RouteParentStatus{{
								ControllerName: gwv1beta1.GatewayController(controllerName),
								ParentRef:      gwv1beta1.ParentReference{Name: "gateway"},
								Conditions: []metav1.Condition{{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
								}},
							}, {
								ControllerName: gwv1beta1.GatewayController(controllerName),
								ParentRef:      gwv1beta1.ParentReference{Name: "gateway-two"},
								Conditions: []metav1.Condition{{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
								}},
							}},
						},
					},
				}},
				TCPRoutes: []gwv1alpha2.TCPRoute{{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "tcp-route-one",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}},
						},
					},
				}, {
					ObjectMeta: metav1.ObjectMeta{
						Name:       "tcp-route-two",
						Finalizers: []string{gatewayFinalizer},
					},
					Spec: gwv1alpha2.TCPRouteSpec{
						CommonRouteSpec: gwv1beta1.CommonRouteSpec{
							ParentRefs: []gwv1beta1.ParentReference{{
								Name: "gateway",
							}, {
								Name: "gateway-two",
							}},
						},
					},
					Status: gwv1alpha2.TCPRouteStatus{
						RouteStatus: gwv1beta1.RouteStatus{
							Parents: []gwv1beta1.RouteParentStatus{{
								ControllerName: gwv1beta1.GatewayController(controllerName),
								ParentRef:      gwv1beta1.ParentReference{Name: "gateway"},
								Conditions: []metav1.Condition{{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
								}},
							}, {
								ControllerName: gwv1beta1.GatewayController(controllerName),
								ParentRef:      gwv1beta1.ParentReference{Name: "gateway-two"},
								Conditions: []metav1.Condition{{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
								}},
							}},
						},
					},
				}},
				ConsulHTTPRoutes: []api.HTTPRouteConfigEntry{{
					Kind: api.HTTPRoute,
					Name: "http-route-two",
					Meta: map[string]string{
						"k8s-name":         "http-route-two",
						"k8s-namespace":    "",
						"k8s-service-name": "http-route-two",
						"managed-by":       "consul-k8s-gateway-controller",
					},
					Parents: []api.ResourceReference{{
						Kind: api.APIGateway,
						Name: "gateway",
					}, {
						Kind: api.APIGateway,
						Name: "gateway-two",
					}},
				}},
				ConsulTCPRoutes: []api.TCPRouteConfigEntry{{
					Kind: api.TCPRoute,
					Name: "tcp-route-two",
					Meta: map[string]string{
						"k8s-name":         "tcp-route-two",
						"k8s-namespace":    "",
						"k8s-service-name": "tcp-route-two",
						"managed-by":       "consul-k8s-gateway-controller",
					},
					Parents: []api.ResourceReference{{
						Kind: api.APIGateway,
						Name: "gateway",
					}, {
						Kind: api.APIGateway,
						Name: "gateway-two",
					}},
				}},
				ConsulInlineCertificates: []api.InlineCertificateConfigEntry{{
					Kind: api.InlineCertificate,
					Name: "secret-one",
					Meta: map[string]string{
						"k8s-name":         "secret-one",
						"k8s-namespace":    "",
						"k8s-service-name": "secret-one",
						"managed-by":       "consul-k8s-gateway-controller",
					},
				}, {
					Kind: api.InlineCertificate,
					Name: "secret-two",
					Meta: map[string]string{
						"k8s-name":         "secret-two",
						"k8s-namespace":    "",
						"k8s-service-name": "secret-two",
						"managed-by":       "consul-k8s-gateway-controller",
					},
				}},
			},
			expected: Snapshot{
				Kubernetes: KubernetesSnapshot{
					StatusUpdates: []client.Object{
						&gwv1beta1.HTTPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "http-route-two",
								Finalizers: []string{gatewayFinalizer},
							},
							Spec: gwv1beta1.HTTPRouteSpec{
								CommonRouteSpec: gwv1beta1.CommonRouteSpec{
									ParentRefs: []gwv1beta1.ParentReference{{
										Name: "gateway",
									}, {
										Name: "gateway-two",
									}},
								},
							},
							Status: gwv1beta1.HTTPRouteStatus{
								RouteStatus: gwv1beta1.RouteStatus{
									// removed gateway status
									Parents: []gwv1beta1.RouteParentStatus{{
										ControllerName: gwv1beta1.GatewayController(controllerName),
										ParentRef:      gwv1beta1.ParentReference{Name: "gateway-two"},
										Conditions: []metav1.Condition{{
											Type:   "Accepted",
											Status: metav1.ConditionTrue,
										}},
									}},
								},
							},
						},
						&gwv1alpha2.TCPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "tcp-route-two",
								Finalizers: []string{gatewayFinalizer},
							},
							Spec: gwv1alpha2.TCPRouteSpec{
								CommonRouteSpec: gwv1beta1.CommonRouteSpec{
									ParentRefs: []gwv1beta1.ParentReference{{
										Name: "gateway",
									}, {
										Name: "gateway-two",
									}},
								},
							},
							// removed gateway status
							Status: gwv1alpha2.TCPRouteStatus{
								RouteStatus: gwv1beta1.RouteStatus{
									Parents: []gwv1beta1.RouteParentStatus{{
										ControllerName: gwv1beta1.GatewayController(controllerName),
										ParentRef:      gwv1beta1.ParentReference{Name: "gateway-two"},
										Conditions: []metav1.Condition{{
											Type:   "Accepted",
											Status: metav1.ConditionTrue,
										}},
									}},
								},
							},
						},
					},
					Updates: []client.Object{
						&gwv1beta1.Gateway{
							ObjectMeta: metav1.ObjectMeta{
								Name:              "gateway",
								DeletionTimestamp: deletionTimestamp,
								Finalizers:        []string{},
							},
							Spec: gwv1beta1.GatewaySpec{
								GatewayClassName: gatewayClassName,
								Listeners: []gwv1beta1.Listener{{
									TLS: &gwv1beta1.GatewayTLSConfig{
										CertificateRefs: []gwv1beta1.SecretObjectReference{{
											Name: "secret-one",
										}, {
											Name: "secret-two",
										}},
									},
								}},
							},
						},
						&gwv1beta1.HTTPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "http-route-one",
								Finalizers: []string{},
							},
							Spec: gwv1beta1.HTTPRouteSpec{
								CommonRouteSpec: gwv1beta1.CommonRouteSpec{
									ParentRefs: []gwv1beta1.ParentReference{{
										Name: "gateway",
									}},
								},
							},
						},
						&gwv1alpha2.TCPRoute{
							ObjectMeta: metav1.ObjectMeta{
								Name:       "tcp-route-one",
								Finalizers: []string{},
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
				},
				Consul: ConsulSnapshot{
					Updates: []api.ConfigEntry{
						&api.HTTPRouteConfigEntry{
							Kind: api.HTTPRoute,
							Name: "http-route-two",
							Meta: map[string]string{
								"k8s-name":         "http-route-two",
								"k8s-namespace":    "",
								"k8s-service-name": "http-route-two",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							// dropped ref to gateway
							Parents: []api.ResourceReference{{
								Kind: api.APIGateway,
								Name: "gateway-two",
							}},
						},
						&api.TCPRouteConfigEntry{
							Kind: api.TCPRoute,
							Name: "tcp-route-two",
							Meta: map[string]string{
								"k8s-name":         "tcp-route-two",
								"k8s-namespace":    "",
								"k8s-service-name": "tcp-route-two",
								"managed-by":       "consul-k8s-gateway-controller",
							},
							// dropped ref to gateway
							Parents: []api.ResourceReference{{
								Kind: api.APIGateway,
								Name: "gateway-two",
							}},
						},
					},
					Deletions: []api.ResourceReference{{
						Kind: api.APIGateway,
						Name: "gateway",
					}, {
						Kind: api.HTTPRoute,
						Name: "http-route-one",
					}, {
						Kind: api.TCPRoute,
						Name: "tcp-route-one",
					}, {
						Kind: api.InlineCertificate,
						Name: "secret-two",
					}},
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tt.config.Setter = statuses.NewSetter(controllerName)

			binder := NewBinder(tt.config)
			actual := binder.Snapshot()
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestBinder_BindingRulesKitchenSink(t *testing.T) {
	t.Parallel()

	className := "gateway-class"
	gatewayClassName := gwv1beta1.ObjectName(className)
	controllerName := "test-controller"
	gatewayClass := &gwv1beta1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: className,
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: gwv1beta1.GatewayController(controllerName),
		},
	}

	gateway := gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "gateway",
			Finalizers: []string{gatewayFinalizer},
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gatewayClassName,
			Listeners: []gwv1beta1.Listener{{
				Name:     "http-listener-default-same",
				Protocol: gwv1beta1.HTTPProtocolType,
			}, {
				Name:     "http-listener-hostname",
				Protocol: gwv1beta1.HTTPProtocolType,
				Hostname: pointerTo[gwv1beta1.Hostname]("host.name"),
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
						From: pointerTo(gwv1beta1.NamespacesFromAll),
					},
				},
			}, {
				Name:     "http-listener-explicit-allowed-same",
				Protocol: gwv1beta1.HTTPProtocolType,
				AllowedRoutes: &gwv1beta1.AllowedRoutes{
					Namespaces: &gwv1beta1.RouteNamespaces{
						From: pointerTo(gwv1beta1.NamespacesFromSame),
					},
				},
			}, {
				Name:     "http-listener-allowed-selector",
				Protocol: gwv1beta1.HTTPProtocolType,
				AllowedRoutes: &gwv1beta1.AllowedRoutes{
					Namespaces: &gwv1beta1.RouteNamespaces{
						From: pointerTo(gwv1beta1.NamespacesFromSelector),
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
						From: pointerTo(gwv1beta1.NamespacesFromAll),
					},
				},
			}, {
				Name:     "tcp-listener-explicit-allowed-same",
				Protocol: gwv1beta1.TCPProtocolType,
				AllowedRoutes: &gwv1beta1.AllowedRoutes{
					Namespaces: &gwv1beta1.RouteNamespaces{
						From: pointerTo(gwv1beta1.NamespacesFromSame),
					},
				},
			}, {
				Name:     "tcp-listener-allowed-selector",
				Protocol: gwv1beta1.TCPProtocolType,
				AllowedRoutes: &gwv1beta1.AllowedRoutes{
					Namespaces: &gwv1beta1.RouteNamespaces{
						From: pointerTo(gwv1beta1.NamespacesFromSelector),
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
		},
	}

	namespaces := map[string]corev1.Namespace{
		"": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "",
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

	defaultNamespacePointer := pointerTo[gwv1beta1.Namespace]("")

	httpTypeMeta := metav1.TypeMeta{}
	httpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("HTTPRoute"))
	tcpTypeMeta := metav1.TypeMeta{}
	tcpTypeMeta.SetGroupVersionKind(gwv1beta1.SchemeGroupVersion.WithKind("TCPRoute"))

	for name, tt := range map[string]struct {
		httpRoute                     *gwv1beta1.HTTPRoute
		expectedHTTPRouteUpdate       *gwv1beta1.HTTPRoute
		expectedHTTPRouteUpdateStatus *gwv1beta1.HTTPRoute
		expectedHTTPConsulRouteUpdate *api.HTTPRouteConfigEntry
		expectedHTTPConsulRouteDelete *api.ResourceReference

		tcpRoute                     *gwv1alpha2.TCPRoute
		expectedTCPRouteUpdate       *gwv1alpha2.TCPRoute
		expectedTCPRouteUpdateStatus *gwv1alpha2.TCPRoute
		expectedTCPConsulRouteUpdate *api.TCPRouteConfigEntry
		expectedTCPConsulRouteDelete *api.ResourceReference
	}{
		"untargeted http route same namespace": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted http route same namespace missing backend": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1beta1.HTTPRouteRule{{
						BackendRefs: []gwv1beta1.HTTPBackendRef{{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: gwv1beta1.ObjectName("backend"),
								},
							},
						}},
					}},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1beta1.HTTPRouteRule{{
						BackendRefs: []gwv1beta1.HTTPBackendRef{{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: gwv1beta1.ObjectName("backend"),
								},
							},
						}},
					}},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionFalse,
								Reason:  "BackendNotFound",
								Message: "/backend: backend not found",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted http route same namespace invalid backend type": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1beta1.HTTPRouteRule{{
						BackendRefs: []gwv1beta1.HTTPBackendRef{{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name:  gwv1beta1.ObjectName("backend"),
									Group: pointerTo[gwv1beta1.Group]("invalid.foo.com"),
								},
							},
						}},
					}},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1beta1.HTTPRouteRule{{
						BackendRefs: []gwv1beta1.HTTPBackendRef{{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name:  gwv1beta1.ObjectName("backend"),
									Group: pointerTo[gwv1beta1.Group]("invalid.foo.com"),
								},
							},
						}},
					}},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionFalse,
								Reason:  "InvalidKind",
								Message: "/backend [Service.invalid.foo.com]: invalid backend kind",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted http route different namespace": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:      "gateway",
							Namespace: defaultNamespacePointer,
						}},
					},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:      "gateway",
							Namespace: defaultNamespacePointer,
						}},
					},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:      "gateway",
								Namespace: defaultNamespacePointer,
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"targeted http route same namespace": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}},
					},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}},
					},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-allowed-selector: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
							}},
						}},
					},
				},
			},
		},
		"targeted http route different namespace": {
			httpRoute: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}},
					},
				},
			},
			expectedHTTPRouteUpdateStatus: &gwv1beta1.HTTPRoute{
				TypeMeta: httpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1beta1.HTTPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}},
					},
				},
				Status: gwv1beta1.HTTPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-default-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-default-same: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-hostname"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-hostname: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-mismatched-kind-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-mismatched-kind-allowed: listener does not support route protocol",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-allowed-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-explicit-allowed-same: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-allowed-selector"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-tls"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-tls: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-explicit-all-allowed: listener does not support route protocol",
							}},
						}},
					},
				},
			},
		},
		"untargeted tcp route same namespace": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted tcp route same namespace missing backend": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1alpha2.TCPRouteRule{{
						BackendRefs: []gwv1beta1.BackendRef{{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name: gwv1beta1.ObjectName("backend"),
							},
						}},
					}},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1alpha2.TCPRouteRule{{
						BackendRefs: []gwv1beta1.BackendRef{{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name: gwv1beta1.ObjectName("backend"),
							},
						}},
					}},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionFalse,
								Reason:  "BackendNotFound",
								Message: "/backend: backend not found",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted tcp route same namespace invalid backend type": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1alpha2.TCPRouteRule{{
						BackendRefs: []gwv1beta1.BackendRef{{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name:  gwv1beta1.ObjectName("backend"),
								Group: pointerTo[gwv1beta1.Group]("invalid.foo.com"),
							},
						}},
					}},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name: "gateway",
						}},
					},
					Rules: []gwv1alpha2.TCPRouteRule{{
						BackendRefs: []gwv1beta1.BackendRef{{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name:  gwv1beta1.ObjectName("backend"),
								Group: pointerTo[gwv1beta1.Group]("invalid.foo.com"),
							},
						}},
					}},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionFalse,
								Reason:  "InvalidKind",
								Message: "/backend [Service.invalid.foo.com]: invalid backend kind",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name: "gateway",
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"untargeted tcp route different namespace": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:      "gateway",
							Namespace: defaultNamespacePointer,
						}},
					},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:      "gateway",
							Namespace: defaultNamespacePointer,
						}},
					},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:      "gateway",
								Namespace: defaultNamespacePointer,
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}},
					},
				},
			},
		},
		"targeted tcp route same namespace": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}},
					},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
						}, {
							Name:        "gateway",
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}},
					},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-mismatched-kind-allowed: listener does not support route protocol",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-allowed-selector: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
							}},
						}},
					},
				},
			},
		},
		"targeted tcp route different namespace": {
			tcpRoute: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}},
					},
				},
			},
			expectedTCPRouteUpdateStatus: &gwv1alpha2.TCPRoute{
				TypeMeta: tcpTypeMeta,
				ObjectMeta: metav1.ObjectMeta{
					Name:       "route",
					Namespace:  "test",
					Finalizers: []string{gatewayFinalizer},
				},
				Spec: gwv1alpha2.TCPRouteSpec{
					CommonRouteSpec: gwv1beta1.CommonRouteSpec{
						ParentRefs: []gwv1beta1.ParentReference{{
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
						}, {
							Name:        "gateway",
							Namespace:   defaultNamespacePointer,
							SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
						}},
					},
				},
				Status: gwv1alpha2.TCPRouteStatus{
					RouteStatus: gwv1beta1.RouteStatus{
						Parents: []gwv1beta1.RouteParentStatus{{
							ControllerName: gatewayClass.Spec.ControllerName,
							Conditions: []metav1.Condition{{
								Type:    "ResolvedRefs",
								Status:  metav1.ConditionTrue,
								Reason:  "ResolvedRefs",
								Message: "resolved backend references",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-default-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-default-same: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-mismatched-kind-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-mismatched-kind-allowed: listener does not support route protocol",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-explicit-allowed-same"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-explicit-allowed-same: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-allowed-selector"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionTrue,
								Reason:  "Accepted",
								Message: "route accepted",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("tcp-listener-tls"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "tcp-listener-tls: listener does not allow binding routes from the given namespace",
							}},
						}, {
							ControllerName: gatewayClass.Spec.ControllerName,
							ParentRef: gwv1beta1.ParentReference{
								Name:        "gateway",
								Namespace:   defaultNamespacePointer,
								SectionName: pointerTo[gwv1beta1.SectionName]("http-listener-explicit-all-allowed"),
							},
							Conditions: []metav1.Condition{{
								Type:    "Accepted",
								Status:  metav1.ConditionFalse,
								Reason:  "NotAllowedByListeners",
								Message: "http-listener-explicit-all-allowed: listener does not support route protocol",
							}},
						}},
					},
				},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			config := BinderConfig{
				ControllerName: controllerName,
				Setter:         statuses.NewSetter(controllerName),
				GatewayClass:   gatewayClass,
				Gateway:        gateway,
				Namespaces:     namespaces,
				ControlledGateways: map[types.NamespacedName]gwv1beta1.Gateway{
					{Name: "gateway"}: gateway,
				},
				Secrets: []corev1.Secret{{
					ObjectMeta: metav1.ObjectMeta{
						Name: "secret-one",
					},
				}},
			}

			if tt.httpRoute != nil {
				config.HTTPRoutes = append(config.HTTPRoutes, *tt.httpRoute)
			}
			if tt.tcpRoute != nil {
				config.TCPRoutes = append(config.TCPRoutes, *tt.tcpRoute)
			}

			binder := NewBinder(config)
			actual := binder.Snapshot()

			compareUpdates(t, tt.expectedHTTPRouteUpdate, actual.Kubernetes.Updates)
			compareUpdates(t, tt.expectedTCPRouteUpdate, actual.Kubernetes.Updates)
			compareUpdates(t, tt.expectedHTTPRouteUpdateStatus, actual.Kubernetes.StatusUpdates)
			compareUpdates(t, tt.expectedTCPRouteUpdateStatus, actual.Kubernetes.StatusUpdates)
		})
	}
}

func compareUpdates[T client.Object](t *testing.T, expected T, updates []client.Object) {
	t.Helper()

	if isNil(expected) {
		for _, update := range updates {
			if u, ok := update.(T); ok {
				t.Error("found unexpected update", u)
			}
		}
	} else {
		found := false
		for _, update := range updates {
			if u, ok := update.(T); ok {
				diff := cmp.Diff(expected, u, cmp.FilterPath(func(p cmp.Path) bool {
					return p.String() == "Status.RouteStatus.Parents.Conditions.LastTransitionTime"
				}, cmp.Ignore()))
				if diff != "" {
					t.Error("diff between actual and expected", diff)
				}
				found = true
			}
		}
		if !found {
			t.Error("expected route update not found in", updates)
		}
	}
}
