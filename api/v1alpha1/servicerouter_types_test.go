package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test MatchesConsul for cases that should return true.
func TestServiceRouter_MatchesConsulTrue(t *testing.T) {
	cases := map[string]struct {
		Ours   ServiceRouter
		Theirs *capi.ServiceRouterConfigEntry
	}{
		"empty fields": {
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
			},
		},
		"all fields set": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{
					Routes: []ServiceRoute{
						{
							Match: &ServiceRouteMatch{
								HTTP: &ServiceRouteHTTPMatch{
									PathExact:  "pathExact",
									PathPrefix: "pathPrefix",
									PathRegex:  "pathRegex",
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
								RequestTimeout:        1 * time.Second,
								NumRetries:            1,
								RetryOnConnectFailure: true,
								RetryOnStatusCodes:    []uint32{500, 400},
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
								PathExact:  "pathExact",
								PathPrefix: "pathPrefix",
								PathRegex:  "pathRegex",
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
							RequestTimeout:        1 * time.Second,
							NumRetries:            1,
							RetryOnConnectFailure: true,
							RetryOnStatusCodes:    []uint32{500, 400},
						},
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.True(t, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

// Test MatchesConsul for cases that should return false.
func TestServiceRouter_MatchesConsulFalse(t *testing.T) {
	cases := map[string]struct {
		Ours   ServiceRouter
		Theirs capi.ConfigEntry
	}{
		"different type": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: "name",
				Kind: capi.ServiceRouter,
			},
		},
		"different name": {
			Ours: ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ServiceRouterSpec{},
			},
			Theirs: &capi.ServiceRouterConfigEntry{
				Name: "other_name",
				Kind: capi.ServiceRouter,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.False(t, c.Ours.MatchesConsul(c.Theirs))
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
									PathExact:  "pathExact",
									PathPrefix: "pathPrefix",
									PathRegex:  "pathRegex",
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
								RequestTimeout:        1 * time.Second,
								NumRetries:            1,
								RetryOnConnectFailure: true,
								RetryOnStatusCodes:    []uint32{500, 400},
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
								PathExact:  "pathExact",
								PathPrefix: "pathPrefix",
								PathRegex:  "pathRegex",
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
							RequestTimeout:        1 * time.Second,
							NumRetries:            1,
							RetryOnConnectFailure: true,
							RetryOnStatusCodes:    []uint32{500, 400},
						},
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul()
			ServiceRouter, ok := act.(*capi.ServiceRouterConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, ServiceRouter)
		})
	}
}

func TestServiceRouter_AddFinalizer(t *testing.T) {
	ServiceRouter := &ServiceRouter{}
	ServiceRouter.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, ServiceRouter.ObjectMeta.Finalizers)
}

func TestServiceRouter_RemoveFinalizer(t *testing.T) {
	ServiceRouter := &ServiceRouter{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	ServiceRouter.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, ServiceRouter.ObjectMeta.Finalizers)
}

func TestServiceRouter_SetSyncedCondition(t *testing.T) {
	ServiceRouter := &ServiceRouter{}
	ServiceRouter.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, ServiceRouter.Status.Conditions[0].Status)
	require.Equal(t, "reason", ServiceRouter.Status.Conditions[0].Reason)
	require.Equal(t, "message", ServiceRouter.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, ServiceRouter.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceRouter_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			ServiceRouter := &ServiceRouter{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, ServiceRouter.SyncedConditionStatus())
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

func TestServiceRouter(t *testing.T) {
	require.Equal(t, "servicerouter", (&ServiceRouter{}).KubeKind())
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
		input          *ServiceRouter
		expectedErrMsg string
	}{
		"valid": {
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
						},
					},
				},
			},
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
			expectedErrMsg: `servicerouter.consul.hashicorp.com "foo" is invalid: [spec.routes[0].match.http: Invalid value: "{\"pathExact\":\"exact\",\"pathPrefix\":\"prefix\",\"pathRegex\":\"regex\",\"queryParam\":[{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"regex\":\"regex\"}]}": at most only one of pathExact, pathPrefix, or pathRegex may be configured, spec.routes[0].match.http.pathExact: Invalid value: "exact": must begin with a '/', spec.routes[0].match.http.pathPrefix: Invalid value: "prefix": must begin with a '/', spec.routes[0].match.http.queryParam[0]: Invalid value: "{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"regex\":\"regex\"}": at most only one of exact, regex, or present may be configured]`,
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
			expectedErrMsg: `servicerouter.consul.hashicorp.com "foo" is invalid: [spec.routes[0].match.http: Invalid value: "{\"pathExact\":\"exact\",\"pathPrefix\":\"prefix\",\"pathRegex\":\"regex\",\"header\":[{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"prefix\":\"prefix\",\"suffix\":\"suffix\",\"regex\":\"regex\"}]}": at most only one of pathExact, pathPrefix, or pathRegex may be configured, spec.routes[0].match.http.pathExact: Invalid value: "exact": must begin with a '/', spec.routes[0].match.http.pathPrefix: Invalid value: "prefix": must begin with a '/', spec.routes[0].match.http.header[0]: Invalid value: "{\"name\":\"name\",\"present\":true,\"exact\":\"exact\",\"prefix\":\"prefix\",\"suffix\":\"suffix\",\"regex\":\"regex\"}": at most only one of exact, prefix, suffix, regex, or present may be configured]`,
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
			expectedErrMsg: `servicerouter.consul.hashicorp.com "foo" is invalid: spec.routes[0]: Invalid value: "{\"match\":{\"http\":{}},\"destination\":{\"prefixRewrite\":\"prefixRewrite\"}}": destination.prefixRewrite requires that either match.http.pathPrefix or match.http.pathExact be configured on this route`,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate()
			if testCase.expectedErrMsg != "" {
				require.EqualError(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
