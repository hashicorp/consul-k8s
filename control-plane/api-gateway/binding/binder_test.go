package binding

import (
	"testing"

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
			t.Parallel()

			tt.config.Setter = statuses.NewSetter(controllerName)

			binder := NewBinder(tt.config)
			actual := binder.Snapshot()
			require.Equal(t, tt.expected, actual)
		})
	}
}
