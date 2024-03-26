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

func TestGRPCRoute_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *GRPCRoute

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbmesh.GRPCRoute
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.GRPCRoute{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData:            &pbmesh.GRPCRoute{},
			Matches:              true,
		},
		"hostnames are compared": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					Hostnames: []string{
						"a-hostname", "another-hostname",
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.GRPCRoute{
				Hostnames: []string{
					"not-a-hostname", "another-hostname",
				},
			},
			Matches: false,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Matches: []*pbmesh.GRPCRouteMatch{
								{
									Method: &pbmesh.GRPCMethodMatch{
										Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
										Service: "test-service",
										Method:  "GET",
									},
									Headers: []*pbmesh.GRPCHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
								},
							},
							Filters: []*pbmesh.GRPCRouteFilter{
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
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.GRPCRoute{
				Rules: []*pbmesh.GRPCRouteRule{
					{
						Matches: []*pbmesh.GRPCRouteMatch{
							{
								Method: &pbmesh.GRPCMethodMatch{
									Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
									Service: "test-service",
									Method:  "GET",
								},
								Headers: []*pbmesh.GRPCHeaderMatch{
									{
										Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
										Name:  "test-header",
										Value: "header-value",
									},
								},
							},
						},
						Filters: []*pbmesh.GRPCRouteFilter{
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
			Matches: true,
		},
		"different types does not match": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.GRPCRoute{},
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
				consulResource = constructGRPCRouteResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestGRPCRoute_Resource also includes test to verify ResourceID().
func TestGRPCRoute_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *GRPCRoute
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbmesh.GRPCRoute
	}{
		"empty fields": {
			Ours: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbmesh.GRPCRoute{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbmesh.GRPCRoute{},
		},
		"every field set": {
			Ours: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Matches: []*pbmesh.GRPCRouteMatch{
								{
									Method: &pbmesh.GRPCMethodMatch{
										Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
										Service: "test-service",
										Method:  "GET",
									},
									Headers: []*pbmesh.GRPCHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
								},
							},
							Filters: []*pbmesh.GRPCRouteFilter{
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
			ExpectedData: &pbmesh.GRPCRoute{
				Rules: []*pbmesh.GRPCRouteRule{
					{
						Matches: []*pbmesh.GRPCRouteMatch{
							{
								Method: &pbmesh.GRPCMethodMatch{
									Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
									Service: "test-service",
									Method:  "GET",
								},
								Headers: []*pbmesh.GRPCHeaderMatch{
									{
										Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
										Name:  "test-header",
										Value: "header-value",
									},
								},
							},
						},
						Filters: []*pbmesh.GRPCRouteFilter{
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
			expected := constructGRPCRouteResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "GRPCRoute do not match")
		})
	}
}

func TestGRPCRoute_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &GRPCRoute{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestGRPCRoute_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &GRPCRoute{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestGRPCRoute_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &GRPCRoute{
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

func TestGRPCRoute_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&GRPCRoute{}).GetCondition(ConditionSynced))
}

func TestGRPCRoute_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&GRPCRoute{}).SyncedConditionStatus())
}

func TestGRPCRoute_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&GRPCRoute{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestGRPCRoute_KubeKind(t *testing.T) {
	require.Equal(t, "grpcroute", (&GRPCRoute{}).KubeKind())
}

func TestGRPCRoute_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbmesh.GRPCRoute{},
	}).KubernetesName())
}

func TestGRPCRoute_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &GRPCRoute{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
// TODO: add when implemented
//func TestGRPCRoute_DefaultNamespaceFields(t *testing.T)

func TestGRPCRoute_Validate(t *testing.T) {
	cases := []struct {
		name            string
		input           *GRPCRoute
		expectedErrMsgs []string
	}{
		{
			name: "kitchen sink OK",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Matches: []*pbmesh.GRPCRouteMatch{
								{
									Method: &pbmesh.GRPCMethodMatch{
										Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
										Service: "test-service",
										Method:  "GET",
									},
									Headers: []*pbmesh.GRPCHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Name:  "test-header",
											Value: "header-value",
										},
									},
								},
							},
							Filters: []*pbmesh.GRPCRouteFilter{
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
									"5xx", "resource-exhausted",
								},
								OnStatusCodes: []uint32{
									200, 201, 202,
								},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: nil,
		},
		{
			name: "empty parentRefs",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{},
				},
			},
			expectedErrMsgs: []string{
				`spec.parentRefs: Required value: cannot be empty`,
			},
		},
		{
			name: "populated hostnames",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{"a-hostname"},
				},
			},
			expectedErrMsgs: []string{
				`spec.hostnames: Invalid value: []string{"a-hostname"}: should not populate hostnames`,
			},
		},
		{
			name: "rules.matches.method",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Matches: []*pbmesh.GRPCRouteMatch{
								{
									Method: &pbmesh.GRPCMethodMatch{
										Type:    pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_UNSPECIFIED,
										Service: "test-service",
										Method:  "GET",
									},
								}, {
									Method: &pbmesh.GRPCMethodMatch{
										Service: "test-service",
										Method:  "GET",
									},
								}, {
									Method: &pbmesh.GRPCMethodMatch{
										Type: pbmesh.GRPCMethodMatchType_GRPC_METHOD_MATCH_TYPE_EXACT,
									},
								},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].matches[0].method.type: Invalid value: GRPC_METHOD_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[1].method.type: Invalid value: GRPC_METHOD_MATCH_TYPE_UNSPECIFIED: missing required field`,
				`spec.rules[0].matches[2].method.service: Invalid value: "": at least one of "service" or "method" must be set`,
			},
		},
		{
			name: "rules.matches.headers",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Matches: []*pbmesh.GRPCRouteMatch{
								{
									Headers: []*pbmesh.GRPCHeaderMatch{
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_UNSPECIFIED,
											Name:  "test-header",
											Value: "header-value",
										},
										{
											Name:  "test-header",
											Value: "header-value",
										},
										{
											Type:  pbmesh.HeaderMatchType_HEADER_MATCH_TYPE_PREFIX,
											Value: "header-value",
										},
									},
								},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
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
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Filters: []*pbmesh.GRPCRouteFilter{
								{
									RequestHeaderModifier:  &pbmesh.HTTPHeaderFilter{},
									ResponseHeaderModifier: &pbmesh.HTTPHeaderFilter{},
									UrlRewrite: &pbmesh.HTTPURLRewriteFilter{
										PathPrefix: "",
									},
								},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].filters[0].urlRewrite.pathPrefix: Required value: field should not be empty if enclosing section is set`,
				`exactly one of request_header_modifier, response_header_modifier, or url_rewrite is required`,
			},
		},
		{
			name: "missing backendRefs",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							BackendRefs: []*pbmesh.GRPCBackendRef{},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].backendRefs: Required value: missing required field`,
			},
		},
		{
			name: "rules.backendRefs",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									Weight: 50,
								},
								{
									BackendRef: &pbmesh.BackendReference{
										Datacenter: "wrong-datacenter",
										Port:       "21000",
									},
									Weight: 50,
								},
								{
									BackendRef: &pbmesh.BackendReference{
										Port: "21000",
									},
									Filters: []*pbmesh.GRPCRouteFilter{{}},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].backendRefs[0].backendRef: Required value: missing required field`,
				`spec.rules[0].backendRefs[1].backendRef.datacenter: Invalid value: "wrong-datacenter": datacenter is not yet supported on backend refs`,
				`filters are not supported at this level yet`,
			},
		},
		{
			name: "rules.timeout",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Timeouts: &pbmesh.HTTPRouteTimeouts{
								Request: &durationpb.Duration{
									Seconds: -9,
									Nanos:   -10,
								},
								Idle: &durationpb.Duration{
									Seconds: -2,
									Nanos:   -3,
								},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].timeouts.request: Invalid value: -9.00000001s: timeout cannot be negative`,
				`spec.rules[0].timeouts.idle: Invalid value: -2.000000003s: timeout cannot be negative`,
			},
		},
		{
			name: "rules.retries",
			input: &GRPCRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.GRPCRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "reference",
								Section: "some-section",
							},
							Port: "20020",
						},
					},
					Hostnames: []string{},
					Rules: []*pbmesh.GRPCRouteRule{
						{
							Retries: &pbmesh.HTTPRouteRetries{
								OnConditions: []string{"invalid-condition", "another-invalid-condition", "internal"},
							},
							BackendRefs: []*pbmesh.GRPCBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Partition: "some-partition",
												Namespace: "some-namespace",
											},
											Name:    "reference",
											Section: "some-section",
										},
										Port: "21000",
									},
									Weight: 50,
								},
							},
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

func constructGRPCRouteResource(tp *pbmesh.GRPCRoute, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbmesh.GRPCRouteType,
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
