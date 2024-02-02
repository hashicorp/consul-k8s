// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestHTTPRoute_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *HTTPRoute

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbmesh.HTTPRoute
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.HTTPRoute{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData:            &pbmesh.HTTPRoute{},
			Matches:              true,
		},
		"hostnames are compared": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					Hostnames: []string{
						"a-hostname", "another-hostname",
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.HTTPRoute{
				Hostnames: []string{
					"not-a-hostname", "another-hostname",
				},
			},
			Matches: false,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{
						"a-hostname", "another-hostname",
					},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Matches: []*pbmesh.HTTPRouteMatch{
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
										Value: "exact-value",
									},
									Headers: []*pbmesh.HTTPHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
									QueryParams: []*pbmesh.HTTPQueryParamMatch{
										{
											Type:  pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT,
											Name:  "query-param-name",
											Value: "query-value",
										},
									},
									Method: "GET",
								},
							},
							Filters: []*pbmesh.HTTPRouteFilter{
								{
									RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
										Set: []*pbmesh.HTTPHeader{
											{
												Name:  "set-header",
												Value: "a-header-value",
											},
										},
										Add: []*pbmesh.HTTPHeader{
											{
												Name:  "added-header",
												Value: "another-header-value",
											},
										},
										Remove: []string{
											"remove-header",
										},
									},
									ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
										Set: []*pbmesh.HTTPHeader{
											{
												Name:  "set-header",
												Value: "a-header-value",
											},
										},
										Add: []*pbmesh.HTTPHeader{
											{
												Name:  "added-header",
												Value: "another-header-value",
											},
										},
										Remove: []string{
											"remove-header",
										},
									},
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "a-path-prefix",
									},
								},
							},
							Timeouts: &pbmesh.HTTPRouteTimeouts{
								Request: &durationpb.Duration{
									Seconds: 10,
									Nanos:   5,
								},
								Idle: &durationpb.Duration{
									Seconds: 5,
									Nanos:   10,
								},
							},
							Retries: &pbmesh.HTTPRouteRetries{
								Number: &wrapperspb.UInt32Value{
									Value: 1,
								},
								OnConnectFailure: false,
								OnConditions: []string{
									"condition-one", "condition-two",
								},
								OnStatusCodes: []uint32{
									200, 201, 202,
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "backend-name",
											Section: "backend-section",
										},
										Port:       "20211",
										Datacenter: "another-datacenter",
									},
									Weight: 12,
									Filters: []*pbmesh.HTTPRouteFilter{
										{
											RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
												Set: []*pbmesh.HTTPHeader{
													{
														Name:  "set-header",
														Value: "setting",
													},
												},
												Add: []*pbmesh.HTTPHeader{
													{
														Name:  "added-header",
														Value: "adding",
													},
												},
												Remove: []string{"removing"},
											},
											ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
												Set: []*pbmesh.HTTPHeader{
													{
														Name:  "another-set-header",
														Value: "setting",
													},
												},
												Add: []*pbmesh.HTTPHeader{
													{
														Name:  "another-added-header",
														Value: "adding",
													},
												},
												Remove: []string{"also-removing"},
											},
											UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
												PathPrefix: "/prefixing-it",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.HTTPRoute{
				ParentRefs: []*pbmesh.ParentReference{
					{
						Ref: &pbresource.Reference{
							Type: pbmesh.ComputedRoutesType,
							Tenancy: &pbresource.Tenancy{
								Partition: "a-partition",
								Namespace: "a-namespace",
							},
							Name:    "reference-name",
							Section: "section-name",
						},
						Port: "20201",
					},
				},
				Hostnames: []string{
					"a-hostname", "another-hostname",
				},
				Rules: []*pbmesh.HTTPRouteRule{
					{
						Matches: []*pbmesh.HTTPRouteMatch{
							{
								Path: &pbmesh.HTTPPathMatch{
									Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
									Value: "exact-value",
								},
								Headers: []*pbmesh.HTTPHeaderMatch{
									{
										Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
										Name:  "test-header",
										Value: "header-value",
									},
								},
								QueryParams: []*pbmesh.HTTPQueryParamMatch{
									{
										Type:  pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT,
										Name:  "query-param-name",
										Value: "query-value",
									},
								},
								Method: "GET",
							},
						},
						Filters: []*pbmesh.HTTPRouteFilter{
							{
								RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
									Set: []*pbmesh.HTTPHeader{
										{
											Name:  "set-header",
											Value: "a-header-value",
										},
									},
									Add: []*pbmesh.HTTPHeader{
										{
											Name:  "added-header",
											Value: "another-header-value",
										},
									},
									Remove: []string{
										"remove-header",
									},
								},
								ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
									Set: []*pbmesh.HTTPHeader{
										{
											Name:  "set-header",
											Value: "a-header-value",
										},
									},
									Add: []*pbmesh.HTTPHeader{
										{
											Name:  "added-header",
											Value: "another-header-value",
										},
									},
									Remove: []string{
										"remove-header",
									},
								},
								UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
									PathPrefix: "a-path-prefix",
								},
							},
						},
						Timeouts: &pbmesh.HTTPRouteTimeouts{
							Request: &durationpb.Duration{
								Seconds: 10,
								Nanos:   5,
							},
							Idle: &durationpb.Duration{
								Seconds: 5,
								Nanos:   10,
							},
						},
						Retries: &pbmesh.HTTPRouteRetries{
							Number: &wrapperspb.UInt32Value{
								Value: 1,
							},
							OnConnectFailure: false,
							OnConditions: []string{
								"condition-one", "condition-two",
							},
							OnStatusCodes: []uint32{
								200, 201, 202,
							},
						},
						BackendRefs: []*pbmesh.HTTPBackendRef{
							{
								BackendRef: &pbmesh.BackendReference{
									Ref: &pbresource.Reference{
										Type: pbmesh.ComputedRoutesType,
										Tenancy: &pbresource.Tenancy{
											Partition: "some-partition",
											Namespace: "some-namespace",
										},
										Name:    "backend-name",
										Section: "backend-section",
									},
									Port:       "20211",
									Datacenter: "another-datacenter",
								},
								Weight: 12,
								Filters: []*pbmesh.HTTPRouteFilter{
									{
										RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
											Set: []*pbmesh.HTTPHeader{
												{
													Name:  "set-header",
													Value: "setting",
												},
											},
											Add: []*pbmesh.HTTPHeader{
												{
													Name:  "added-header",
													Value: "adding",
												},
											},
											Remove: []string{"removing"},
										},
										ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
											Set: []*pbmesh.HTTPHeader{
												{
													Name:  "another-set-header",
													Value: "setting",
												},
											},
											Add: []*pbmesh.HTTPHeader{
												{
													Name:  "another-added-header",
													Value: "adding",
												},
											},
											Remove: []string{"also-removing"},
										},
										UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
											PathPrefix: "/prefixing-it",
										},
									},
								},
							},
						},
					},
				},
			},
			Matches: true,
		},
		"different types does not match": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.HTTPRoute{},
			},
			ResourceOverride: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "name",
					Type: pbmesh.ProxyConfigurationType,
					Tenancy: &pbresource.Tenancy{
						Partition: constants.DefaultConsulNS,
						Namespace: constants.DefaultConsulPartition,
					},
				},
				Data:     inject.ToProtoAny(&pbmesh.ProxyConfiguration{}),
				Metadata: meshConfigMeta(),
			},
			Matches: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			consulResource := c.ResourceOverride
			if c.TheirName != "" {
				consulResource = constructHTTPRouteResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestHTTPRoute_Resource also includes test to verify ResourceID().
func TestHTTPRoute_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *HTTPRoute
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbmesh.HTTPRoute
	}{
		"empty fields": {
			Ours: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbmesh.HTTPRoute{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbmesh.HTTPRoute{},
		},
		"every field set": {
			Ours: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{
						"a-hostname", "another-hostname",
					},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Matches: []*pbmesh.HTTPRouteMatch{
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
										Value: "exact-value",
									},
									Headers: []*pbmesh.HTTPHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
									QueryParams: []*pbmesh.HTTPQueryParamMatch{
										{
											Type:  pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT,
											Name:  "query-param-name",
											Value: "query-value",
										},
									},
									Method: "GET",
								},
							},
							Filters: []*pbmesh.HTTPRouteFilter{
								{
									RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
										Set: []*pbmesh.HTTPHeader{
											{
												Name:  "set-header",
												Value: "a-header-value",
											},
										},
										Add: []*pbmesh.HTTPHeader{
											{
												Name:  "added-header",
												Value: "another-header-value",
											},
										},
										Remove: []string{
											"remove-header",
										},
									},
									ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
										Set: []*pbmesh.HTTPHeader{
											{
												Name:  "set-header",
												Value: "a-header-value",
											},
										},
										Add: []*pbmesh.HTTPHeader{
											{
												Name:  "added-header",
												Value: "another-header-value",
											},
										},
										Remove: []string{
											"remove-header",
										},
									},
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "a-path-prefix",
									},
								},
							},
							Timeouts: &pbmesh.HTTPRouteTimeouts{
								Request: &durationpb.Duration{
									Seconds: 10,
									Nanos:   5,
								},
								Idle: &durationpb.Duration{
									Seconds: 5,
									Nanos:   10,
								},
							},
							Retries: &pbmesh.HTTPRouteRetries{
								Number: &wrapperspb.UInt32Value{
									Value: 1,
								},
								OnConnectFailure: false,
								OnConditions: []string{
									"condition-one", "condition-two",
								},
								OnStatusCodes: []uint32{
									200, 201, 202,
								},
							},
						},
					},
				},
			},
			ConsulNamespace: "not-default-namespace",
			ConsulPartition: "not-default-partition",
			ExpectedName:    "foo",
			ExpectedData: &pbmesh.HTTPRoute{
				ParentRefs: []*pbmesh.ParentReference{
					{
						Ref: &pbresource.Reference{
							Type: pbmesh.ComputedRoutesType,
							Tenancy: &pbresource.Tenancy{
								Partition: "a-partition",
								Namespace: "a-namespace",
							},
							Name:    "reference-name",
							Section: "section-name",
						},
						Port: "20201",
					},
				},
				Hostnames: []string{
					"a-hostname", "another-hostname",
				},
				Rules: []*pbmesh.HTTPRouteRule{
					{
						Matches: []*pbmesh.HTTPRouteMatch{
							{
								Path: &pbmesh.HTTPPathMatch{
									Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
									Value: "exact-value",
								},
								Headers: []*pbmesh.HTTPHeaderMatch{
									{
										Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
										Name:  "test-header",
										Value: "header-value",
									},
								},
								QueryParams: []*pbmesh.HTTPQueryParamMatch{
									{
										Type:  pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT,
										Name:  "query-param-name",
										Value: "query-value",
									},
								},
								Method: "GET",
							},
						},
						Filters: []*pbmesh.HTTPRouteFilter{
							{
								RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{
									Set: []*pbmesh.HTTPHeader{
										{
											Name:  "set-header",
											Value: "a-header-value",
										},
									},
									Add: []*pbmesh.HTTPHeader{
										{
											Name:  "added-header",
											Value: "another-header-value",
										},
									},
									Remove: []string{
										"remove-header",
									},
								},
								ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{
									Set: []*pbmesh.HTTPHeader{
										{
											Name:  "set-header",
											Value: "a-header-value",
										},
									},
									Add: []*pbmesh.HTTPHeader{
										{
											Name:  "added-header",
											Value: "another-header-value",
										},
									},
									Remove: []string{
										"remove-header",
									},
								},
								UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
									PathPrefix: "a-path-prefix",
								},
							},
						},
						Timeouts: &pbmesh.HTTPRouteTimeouts{
							Request: &durationpb.Duration{
								Seconds: 10,
								Nanos:   5,
							},
							Idle: &durationpb.Duration{
								Seconds: 5,
								Nanos:   10,
							},
						},
						Retries: &pbmesh.HTTPRouteRetries{
							Number: &wrapperspb.UInt32Value{
								Value: 1,
							},
							OnConnectFailure: false,
							OnConditions: []string{
								"condition-one", "condition-two",
							},
							OnStatusCodes: []uint32{
								200, 201, 202,
							},
						},
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			actual := c.Ours.Resource(c.ConsulNamespace, c.ConsulPartition)
			expected := constructHTTPRouteResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "HTTPRoute do not match")
		})
	}
}

func TestHTTPRoute_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &HTTPRoute{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestHTTPRoute_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &HTTPRoute{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestHTTPRoute_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &HTTPRoute{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, trafficPermissions.SyncedConditionStatus())
		})
	}
}

func TestHTTPRoute_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&HTTPRoute{}).GetCondition(ConditionSynced))
}

func TestHTTPRoute_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&HTTPRoute{}).SyncedConditionStatus())
}

func TestHTTPRoute_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&HTTPRoute{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestHTTPRoute_KubeKind(t *testing.T) {
	require.Equal(t, "httproute", (&HTTPRoute{}).KubeKind())
}

func TestHTTPRoute_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbmesh.HTTPRoute{},
	}).KubernetesName())
}

func TestHTTPRoute_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &HTTPRoute{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
// TODO: add when implemented
//func TestHTTPRoute_DefaultNamespaceFields(t *testing.T)

func TestHTTPRoute_Validate(t *testing.T) {
	cases := []struct {
		name            string
		input           *HTTPRoute
		expectedErrMsgs []string
	}{
		{
			name: "kitchen sink OK",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Matches: []*pbmesh.HTTPRouteMatch{
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
										Value: "/exactValue",
									},
									Headers: []*pbmesh.HTTPHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
									QueryParams: []*pbmesh.HTTPQueryParamMatch{
										{
											Type:  pbmesh.QueryParamMatchType_QUERY_PARAM_MATCH_TYPE_PRESENT,
											Name:  "query-param-name",
											Value: "query-value",
										},
									},
									Method: "GET",
								},
							},
							Filters: []*pbmesh.HTTPRouteFilter{
								{
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "a-path-prefix",
									},
								},
							},
							Timeouts: &pbmesh.HTTPRouteTimeouts{
								Request: &durationpb.Duration{
									Seconds: 10,
									Nanos:   5,
								},
								Idle: &durationpb.Duration{
									Seconds: 5,
									Nanos:   10,
								},
							},
							Retries: &pbmesh.HTTPRouteRetries{
								Number: &wrapperspb.UInt32Value{
									Value: 1,
								},
								OnConnectFailure: false,
								OnConditions: []string{
									"reset", "cancelled",
								},
								OnStatusCodes: []uint32{
									200, 201, 202,
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "backend",
											Section: "backend-section",
										},
										Port: "20101",
									},
									Weight: 15,
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: nil,
		},
		{
			name: "missing parentRefs",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{},
				},
			},
			expectedErrMsgs: []string{
				`spec.parentRefs: Required value: cannot be empty`,
			},
		},
		{
			name: "hostnames created",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{"a-hostname", "another-hostname"},
				},
			},
			expectedErrMsgs: []string{
				`spec.hostnames: Invalid value: []string{"a-hostname", "another-hostname"}: should not populate hostnames`,
			},
		},
		{
			name: "rules.matches.path",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Matches: []*pbmesh.HTTPRouteMatch{
								{
									Path: &pbmesh.HTTPPathMatch{
										Type: pbmesh.PathMatchType_PATH_MATCH_TYPE_UNSPECIFIED,
									},
								},
								{
									Path: &pbmesh.HTTPPathMatch{},
								},
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_EXACT,
										Value: "does-not-have-/-prefix",
									},
								},
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_PREFIX,
										Value: "does-not-have-/-prefix-either",
									},
								},
								{
									Path: &pbmesh.HTTPPathMatch{
										Type:  pbmesh.PathMatchType_PATH_MATCH_TYPE_REGEX,
										Value: "",
									},
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{{BackendRef: &pbmesh.BackendReference{}}},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].matches[0].path.type: Invalid value: PATH_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[1].path.type: Invalid value: PATH_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[2].path.value: Invalid value: "does-not-have-/-prefix": exact patch value does not start with '/'`,
				`spec.rules[0].matches[3].path.value: Invalid value: "does-not-have-/-prefix-either": prefix patch value does not start with '/'`,
				`spec.rules[0].matches[4].path.value: Required value: missing required field`,
			},
		},
		{
			name: "rules.matches.headers",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Matches: []*pbmesh.HTTPRouteMatch{
								{
									Headers: []*pbmesh.HTTPHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_UNSPECIFIED,
											Name:  "test-header",
											Value: "header-value",
										},
										{
											// Type: "",
											Name:  "test-header",
											Value: "header-value",
										},
										{
											Type: pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_EXACT,
											Name: "",
										},
									},
									Method: "GET",
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{{BackendRef: &pbmesh.BackendReference{}}},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].matches[0].headers[0].type: Invalid value: HEADER_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[0].headers[1].type: Invalid value: HEADER_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[0].headers[2].name: Required value: missing required field`,
			},
		},
		{
			name: "rules.filters",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Filters: []*pbmesh.HTTPRouteFilter{
								{
									RequestHeaderModifier:  &pbmesh.HTTPHeaderFilter{},
									ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{},
								},
								{
									RequestHeaderModifier: &pbmesh.HTTPHeaderFilter{},
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "prefix-1",
									},
								},
								{
									ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{},
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "prefix-2",
									},
								},
								{
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "",
									},
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{{BackendRef: &pbmesh.BackendReference{}}},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.filters[0]: Invalid value`,
				`spec.filters[1]: Invalid value`,
				`spec.filters[2]: Invalid value`,
				`spec.filters[3].urlRewrite.pathPrefix: Invalid value: "": field should not be empty if enclosing section is set`,
				`exactly one of request_header_modifier, response_header_modifier, or url_rewrite is required`,
			},
		},
		{
			name: "rule.backendRefs",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							BackendRefs: []*pbmesh.HTTPBackendRef{},
						},
						{
							BackendRefs: []*pbmesh.HTTPBackendRef{
								{},
								{
									BackendRef: &pbmesh.BackendReference{
										Datacenter: "some-datacenter",
									},
								},
								{
									BackendRef: &pbmesh.BackendReference{},
									Filters: []*pbmesh.HTTPRouteFilter{
										{
											UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
												PathPrefix: "/prefixed",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].backendRefs: Required value: missing required field`,
				`spec.rules[1].backendRefs[0].backendRef: Required value: missing required field`,
				`spec.rules[1].backendRefs[1].backendRef.datacenter: Invalid value: "some-datacenter": datacenter is not yet supported on backend refs`,
				`spec.rules[1].backendRefs[2].filters: Invalid value`,
				`filters are not supported at this level yet`,
			},
		},
		{
			name: "rules.timeouts",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Timeouts: &pbmesh.HTTPRouteTimeouts{
								Request: &durationpb.Duration{
									Seconds: -10,
									Nanos:   -5,
								},
								Idle: &durationpb.Duration{
									Seconds: -5,
									Nanos:   -10,
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{{BackendRef: &pbmesh.BackendReference{}}},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].timeouts.request: Invalid value: -10.000000005s: timeout cannot be negative`,
				`spec.rules[0].timeouts.idle: Invalid value: -5.00000001s: timeout cannot be negative`,
			},
		},
		{
			name: "rules.timeouts",
			input: &HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.HTTPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "a-partition",
									Namespace: "a-namespace",
								},
								Name:    "reference-name",
								Section: "section-name",
							},
							Port: "20201",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.HTTPRouteRule{
						{
							Retries: &pbmesh.HTTPRouteRetries{
								OnConditions: []string{
									"invalid-condition", "another-invalid-condition",
								},
							},
							BackendRefs: []*pbmesh.HTTPBackendRef{{BackendRef: &pbmesh.BackendReference{}}},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].retries.onConditions[0]: Invalid value: "invalid-condition": not a valid retry condition`,
				`spec.rules[0].retries.onConditions[1]: Invalid value: "another-invalid-condition": not a valid retry condition`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate(common.ConsulTenancyConfig{})
			if len(tc.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range tc.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func constructHTTPRouteResource(tp *pbmesh.HTTPRoute, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbmesh.HTTPRouteType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,
		},
		Uid: "ABCD", // We add this to show it does not factor into the comparison
	}

	return &pbresource.Resource{
		Id:       id,
		Data:     data,
		Metadata: meshConfigMeta(),

		// We add the fields below to prove that they are not used in the Match when comparing the CRD to Consul.
		Version:    "123456",
		Generation: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Status: map[string]*pbresource.Status{
			"knock": {
				ObservedGeneration: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
				Conditions:         make([]*pbresource.Condition, 0),
				UpdatedAt:          timestamppb.Now(),
			},
		},
	}
}
