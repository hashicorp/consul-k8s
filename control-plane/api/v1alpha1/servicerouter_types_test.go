// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// Test MatchesConsul.
func TestServiceRouter_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ServiceRouter
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{},
			},
			Theirs: &capi.ServiceRouterConfigEntry{
				Kind:        capi.ServiceRouter,
				Name:        "name",
				Namespace:   "namespace",
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
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									CaseInsensitive: true,
									PathExact:       "pathExact",
									PathPrefix:      "pathPrefix",
									PathRegex:       "pathRegex",
									Header: []ServiceRouteHTTPMatchHeader{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "regex",
											Invert:  true,
										},
									},
									QueryParam: []ServiceRouteHTTPMatchQueryParam{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Regex:   "regex",
										},
									},
									Methods: []string{"method1", "method2"},
								},
							},
							Destination: &ServiceRouteDestination{
								Service:               "service",
								ServiceSubset:         "serviceSubset",
								Namespace:             "namespace",
								PrefixRewrite:         "prefixRewrite",
								IdleTimeout:           metav1.Duration{Duration: 1 * time.Second},
								RequestTimeout:        metav1.Duration{Duration: 1 * time.Second},
								NumRetries:            1,
								RetryOnConnectFailure: true,
								RetryOn:               []string{"gateway-error"},
								RetryOnStatusCodes:    []uint32{500, 400},
								RequestHeaders: &HTTPHeaderModifiers{
									Add: map[string]string{
										"foo":    "bar",
										"source": "dest",
									},
									Set: map[string]string{
										"bar": "baz",
										"key": "car",
									},
									Remove: []string{
										"foo",
										"bar",
										"baz",
									},
								},
								ResponseHeaders: &HTTPHeaderModifiers{
									Add: map[string]string{
										"doo":    "var",
										"aource": "sest",
									},
									Set: map[string]string{
										"var": "vaz",
										"jey": "xar",
									},
									Remove: []string{
										"doo",
										"var",
										"vaz",
									},
								},
							},
						},
					},
				},
			},
			Theirs: &capi.ServiceRouterConfigEntry{
				Name: "name",
				Kind: capi.ServiceRouter,
				Routes: []capi.ServiceRoute{
					{
						Match: &capi.ServiceRouteMatch{
							HTTP: &capi.ServiceRouteHTTPMatch{
								CaseInsensitive: true,
								PathExact:       "pathExact",
								PathPrefix:      "pathPrefix",
								PathRegex:       "pathRegex",
								Header: []capi.ServiceRouteHTTPMatchHeader{
									{
										Name:    "name",
										Present: true,
										Exact:   "exact",
										Prefix:  "prefix",
										Suffix:  "suffix",
										Regex:   "regex",
										Invert:  true,
									},
								},
								QueryParam: []capi.ServiceRouteHTTPMatchQueryParam{
									{
										Name:    "name",
										Present: true,
										Exact:   "exact",
										Regex:   "regex",
									},
								},
								Methods: []string{"method1", "method2"},
							},
						},
						Destination: &capi.ServiceRouteDestination{
							Service:               "service",
							ServiceSubset:         "serviceSubset",
							Namespace:             "namespace",
							Partition:             "default",
							PrefixRewrite:         "prefixRewrite",
							IdleTimeout:           1 * time.Second,
							RequestTimeout:        1 * time.Second,
							NumRetries:            1,
							RetryOnConnectFailure: true,
							RetryOn:               []string{"gateway-error"},
							RetryOnStatusCodes:    []uint32{500, 400},
							RequestHeaders: &capi.HTTPHeaderModifiers{
								Add: map[string]string{
									"foo":    "bar",
									"source": "dest",
								},
								Set: map[string]string{
									"bar": "baz",
									"key": "car",
								},
								Remove: []string{
									"foo",
									"bar",
									"baz",
								},
							},
							ResponseHeaders: &capi.HTTPHeaderModifiers{
								Add: map[string]string{
									"doo":    "var",
									"aource": "sest",
								},
								Set: map[string]string{
									"var": "vaz",
									"jey": "xar",
								},
								Remove: []string{
									"doo",
									"var",
									"vaz",
								},
							},
						},
					},
				},
			},
			Matches: true,
		},
		"mismatched type does not match": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Kind:        capi.ServiceRouter,
				Name:        "name",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestServiceRouter_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ServiceRouter
		Exp  *capi.ServiceRouterConfigEntry
	}{
		"empty fields": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{},
			},
			Exp: &capi.ServiceRouterConfigEntry{
				Name: "name",
				Kind: capi.ServiceRouter,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									CaseInsensitive: true,
									PathExact:       "pathExact",
									PathPrefix:      "pathPrefix",
									PathRegex:       "pathRegex",
									Header: []ServiceRouteHTTPMatchHeader{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "regex",
											Invert:  true,
										},
									},
									QueryParam: []ServiceRouteHTTPMatchQueryParam{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Regex:   "regex",
										},
									},
									Methods: []string{"method1", "method2"},
								},
							},
							Destination: &ServiceRouteDestination{
								Service:               "service",
								ServiceSubset:         "serviceSubset",
								Namespace:             "namespace",
								PrefixRewrite:         "prefixRewrite",
								IdleTimeout:           metav1.Duration{Duration: 1 * time.Second},
								RequestTimeout:        metav1.Duration{Duration: 1 * time.Second},
								NumRetries:            1,
								RetryOnConnectFailure: true,
								RetryOn:               []string{"gateway-error"},
								RetryOnStatusCodes:    []uint32{500, 400},
								RequestHeaders: &HTTPHeaderModifiers{
									Add: map[string]string{
										"foo":    "bar",
										"source": "dest",
									},
									Set: map[string]string{
										"bar": "baz",
										"key": "car",
									},
									Remove: []string{
										"foo",
										"bar",
										"baz",
									},
								},
								ResponseHeaders: &HTTPHeaderModifiers{
									Add: map[string]string{
										"doo":    "var",
										"aource": "sest",
									},
									Set: map[string]string{
										"var": "vaz",
										"jey": "xar",
									},
									Remove: []string{
										"doo",
										"var",
										"vaz",
									},
								},
							},
						},
					},
				},
			},
			Exp: &capi.ServiceRouterConfigEntry{
				Name: "name",
				Kind: capi.ServiceRouter,
				Routes: []capi.ServiceRoute{
					{
						Match: &capi.ServiceRouteMatch{
							HTTP: &capi.ServiceRouteHTTPMatch{
								CaseInsensitive: true,
								PathExact:       "pathExact",
								PathPrefix:      "pathPrefix",
								PathRegex:       "pathRegex",
								Header: []capi.ServiceRouteHTTPMatchHeader{
									{
										Name:    "name",
										Present: true,
										Exact:   "exact",
										Prefix:  "prefix",
										Suffix:  "suffix",
										Regex:   "regex",
										Invert:  true,
									},
								},
								QueryParam: []capi.ServiceRouteHTTPMatchQueryParam{
									{
										Name:    "name",
										Present: true,
										Exact:   "exact",
										Regex:   "regex",
									},
								},
								Methods: []string{"method1", "method2"},
							},
						},
						Destination: &capi.ServiceRouteDestination{
							Service:               "service",
							ServiceSubset:         "serviceSubset",
							Namespace:             "namespace",
							PrefixRewrite:         "prefixRewrite",
							IdleTimeout:           1 * time.Second,
							RequestTimeout:        1 * time.Second,
							NumRetries:            1,
							RetryOnConnectFailure: true,
							RetryOn:               []string{"gateway-error"},
							RetryOnStatusCodes:    []uint32{500, 400},
							RequestHeaders: &capi.HTTPHeaderModifiers{
								Add: map[string]string{
									"foo":    "bar",
									"source": "dest",
								},
								Set: map[string]string{
									"bar": "baz",
									"key": "car",
								},
								Remove: []string{
									"foo",
									"bar",
									"baz",
								},
							},
							ResponseHeaders: &capi.HTTPHeaderModifiers{
								Add: map[string]string{
									"doo":    "var",
									"aource": "sest",
								},
								Set: map[string]string{
									"var": "vaz",
									"jey": "xar",
								},
								Remove: []string{
									"doo",
									"var",
									"vaz",
								},
							},
						},
					},
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
			ServiceRouter, ok := act.(*capi.ServiceRouterConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, ServiceRouter)
		})
	}
}

func TestServiceRouter_AddFinalizer(t *testing.T) {
	serviceRouter := &ServiceRouter{}
	serviceRouter.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceRouter.ObjectMeta.Finalizers)
}

func TestServiceRouter_RemoveFinalizer(t *testing.T) {
	serviceRouter := &ServiceRouter{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceRouter.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceRouter.ObjectMeta.Finalizers)
}

func TestServiceRouter_SetSyncedCondition(t *testing.T) {
	serviceRouter := &ServiceRouter{}
	serviceRouter.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceRouter.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceRouter.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceRouter.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceRouter.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceRouter_SetLastSyncedTime(t *testing.T) {
	serviceRouter := &ServiceRouter{}
	syncedTime := metav1.NewTime(time.Now())
	serviceRouter.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, serviceRouter.Status.LastSyncedTime)
}

func TestServiceRouter_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceRouter := &ServiceRouter{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceRouter.SyncedConditionStatus())
		})
	}
}

func TestServiceRouter_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceRouter{}).GetCondition(ConditionSynced))
}

func TestServiceRouter_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceRouter{}).SyncedConditionStatus())
}

func TestServiceRouter_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceRouter{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceRouter_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceRouter, (&ServiceRouter{}).ConsulKind())
}

func TestServiceRouter_KubeKind(t *testing.T) {
	require.Equal(t, "servicerouter", (&ServiceRouter{}).KubeKind())
}

func TestServiceRouter_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceRouter{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceRouter_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceRouter{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestServiceRouter_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&ServiceRouter{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceRouter_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceRouter{}).ConsulGlobalResource())
}

func TestServiceRouter_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	serviceRouter := &ServiceRouter{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceRouter.GetObjectMeta())
}

func TestServiceRouter_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *ServiceRouter
		namespacesEnabled bool
		partitionsEnabled bool
		expectedErrMsgs   []string
	}{
		"namespaces enabled: valid": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service:   "destA",
								Namespace: "namespace-a",
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			expectedErrMsgs:   nil,
		},
		"namespaces disabled: valid": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service: "destA",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs:   nil,
		},
		"partitions enabled: valid": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service:   "destA",
								Namespace: "namespace-a",
								Partition: "other",
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: true,
			expectedErrMsgs:   nil,
		},
		"partitions disabled: valid": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service: "destA",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			partitionsEnabled: false,
			expectedErrMsgs:   nil,
		},
		"http match queryParam": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathExact:  "exact",
									PathPrefix: "prefix",
									PathRegex:  "regex",
									QueryParam: []ServiceRouteHTTPMatchQueryParam{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Regex:   "regex",
										},
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`servicerouter.consul.hashicorp.com "foo" is invalid: [spec.routes[0].match.http: Invalid value: "{\"pathExact\":\"exact\",\"pathPrefix\":\"prefix\",\"pathRegex\":\"regex\",\"queryParam\":[{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"regex\":\"regex\"}]}": at most only one of pathExact, pathPrefix, or pathRegex may be configured, spec.routes[0].match.http.pathExact: Invalid value: "exact": must begin with a '/', spec.routes[0].match.http.pathPrefix: Invalid value: "prefix": must begin with a '/', spec.routes[0].match.http.queryParam[0]: Invalid value: "{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"regex\":\"regex\"}": at most only one of exact, regex, or present may be configured]`,
			},
		},
		"http match header": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathExact:  "exact",
									PathPrefix: "prefix",
									PathRegex:  "regex",
									Header: []ServiceRouteHTTPMatchHeader{
										{
											Name:    "name",
											Present: true,
											Exact:   "exact",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "regex",
										},
									},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`servicerouter.consul.hashicorp.com "foo" is invalid: [spec.routes[0].match.http: Invalid value: "{\"pathExact\":\"exact\",\"pathPrefix\":\"prefix\",\"pathRegex\":\"regex\",\"header\":[{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"prefix\":\"prefix\",\"suffix\":\"suffix\",\"regex\":\"regex\"}]}": at most only one of pathExact, pathPrefix, or pathRegex may be configured, spec.routes[0].match.http.pathExact: Invalid value: "exact": must begin with a '/', spec.routes[0].match.http.pathPrefix: Invalid value: "prefix": must begin with a '/', spec.routes[0].match.http.header[0]: Invalid value: "{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"prefix\":\"prefix\",\"suffix\":\"suffix\",\"regex\":\"regex\"}": at most only one of exact, prefix, suffix, regex, or present may be configured]`,
			},
		},
		"destination and prefixRewrite": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Destination: &ServiceRouteDestination{
								PrefixRewrite: "prefixRewrite",
							},
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathExact:  "",
									PathPrefix: "",
									PathRegex:  "",
									Header:     nil,
									QueryParam: nil,
									Methods:    nil,
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`servicerouter.consul.hashicorp.com "foo" is invalid: spec.routes[0]: Invalid value: "{\"match\":{\"http\":{}},\"destination\":{\"prefixRewrite\":\"prefixRewrite\",\"idleTimeout\":\"0s\",\"requestTimeout\":\"0s\"}}": destination.prefixRewrite requires that either match.http.pathPrefix or match.http.pathExact be configured on this route`,
			},
		},
		"namespaces disabled: single destination namespace specified": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-a",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				"servicerouter.consul.hashicorp.com \"foo\" is invalid: spec.routes[0].destination.namespace: Invalid value: \"namespace-a\": Consul Enterprise namespaces must be enabled to set destination.namespace",
			},
		},
		"namespaces disabled: multiple destination namespaces specified": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-a",
							},
						},
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-b",
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				"spec.routes[0].destination.namespace: Invalid value: \"namespace-a\": Consul Enterprise namespaces must be enabled to set destination.namespace",
				"spec.routes[1].destination.namespace: Invalid value: \"namespace-b\": Consul Enterprise namespaces must be enabled to set destination.namespace",
			},
		},
		"partitions disabled: single destination partition specified": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-a",
								Partition: "partition-a",
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				"servicerouter.consul.hashicorp.com \"foo\" is invalid: spec.routes[0].destination.partition: Invalid value: \"partition-a\": Consul Enterprise partitions must be enabled to set destination.partition",
			},
		},
		"partitions disabled: multiple destination partitions specified": {
			input: &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-a",
								Partition: "partition-a",
							},
						},
						{
							Destination: &ServiceRouteDestination{
								Namespace: "namespace-b",
								Partition: "partition-b",
							},
						},
					},
				},
			},
			namespacesEnabled: true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				"spec.routes[0].destination.partition: Invalid value: \"partition-a\": Consul Enterprise partitions must be enabled to set destination.partition",
				"spec.routes[1].destination.partition: Invalid value: \"partition-b\": Consul Enterprise partitions must be enabled to set destination.partition",
			},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{NamespacesEnabled: testCase.namespacesEnabled, PartitionsEnabled: testCase.partitionsEnabled})
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

// Test defaulting behavior when namespaces are enabled as well as disabled.
func TestServiceRouter_DefaultNamespaceFields(t *testing.T) {
	namespaceConfig := map[string]struct {
		consulMeta          common.ConsulMeta
		expectedDestination string
	}{
		"disabled": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    false,
				DestinationNamespace: "",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "",
		},
		"destinationNS": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "foo",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "foo",
		},
		"mirroringEnabledWithoutPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "",
			},
			expectedDestination: "bar",
		},
		"mirroringWithPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "ns-",
			},
			expectedDestination: "ns-bar",
		},
	}

	for name, s := range namespaceConfig {
		t.Run(name, func(t *testing.T) {
			input := &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service: "destA",
							},
						},
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/other",
								},
							},
							Destination: &ServiceRouteDestination{
								Service:   "destB",
								Namespace: "other",
							},
						},
					},
				},
			}
			output := &ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
							Destination: &ServiceRouteDestination{
								Service:   "destA",
								Namespace: s.expectedDestination,
							},
						},
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathPrefix: "/other",
								},
							},
							Destination: &ServiceRouteDestination{
								Service:   "destB",
								Namespace: "other",
							},
						},
					},
				},
			}
			input.DefaultNamespaceFields(s.consulMeta)
			require.True(t, cmp.Equal(input, output))
		})
	}
}
