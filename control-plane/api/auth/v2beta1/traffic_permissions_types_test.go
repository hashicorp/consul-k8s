// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestTrafficPermissions_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *TrafficPermissions

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbauth.TrafficPermissions
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbauth.TrafficPermissions{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData: &pbauth.TrafficPermissions{
				Destination: nil,
				Action:      pbauth.Action_ACTION_UNSPECIFIED,
				Permissions: nil,
			},
			Matches: true,
		},
		"source namespaces and partitions are compared": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									IdentityName: "source-identity",
									Namespace:    "the space namespace space",
								},
							},
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_ALLOW,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							{
								IdentityName: "source-identity",
								Namespace:    "not space namespace",
							},
						},
					},
				},
			},
			Matches: false,
		},
		"destination namespaces and partitions are compared": {
			OurConsulNamespace: "not-consul-ns",
			OurConsulPartition: "not-consul-partition",
			OurData: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_DENY,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									IdentityName: "source-identity",
								},
							},
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_ALLOW,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							{
								IdentityName: "source-identity",
							},
						},
					},
				},
			},
			Matches: false,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace:     "the space namespace space",
									Partition:     "space-partition",
									Peer:          "space-peer",
									SamenessGroup: "space-group",
									Exclude: []*pbauth.ExcludeSource{
										{
											IdentityName:  "not-source-identity",
											Namespace:     "the space namespace space",
											Partition:     "space-partition",
											Peer:          "space-peer",
											SamenessGroup: "space-group",
										},
									},
								},
								{
									IdentityName: "source-identity",
								},
							},
							DestinationRules: []*pbauth.DestinationRule{
								{
									PathExact:  "/hello",
									PathPrefix: "/world",
									PathRegex:  "/.*/foo",
									Headers: []*pbauth.DestinationRuleHeader{
										{
											Name:    "x-consul-test",
											Present: true,
											Exact:   "true",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "reg.*ex",
											Invert:  true,
										},
									},
									Methods: []string{"GET", "POST"},
									Exclude: []*pbauth.ExcludePermissionRule{
										{
											PathExact:  "/hello",
											PathPrefix: "/world",
											PathRegex:  "/.*/foo",
											Headers: []*pbauth.DestinationRuleHeader{
												{
													Name:    "x-consul-not-test",
													Present: true,
													Exact:   "false",
													Prefix:  "~prefix",
													Suffix:  "~suffix",
													Regex:   "~reg.*ex",
													Invert:  true,
												},
											},
											Methods:   []string{"DELETE"},
											PortNames: []string{"log"},
										},
									},
									PortNames: []string{"web", "admin"},
								},
							},
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_ALLOW,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							// These are intentionally in a different order to show that it doesn't matter
							{
								IdentityName: "source-identity",
							},
							{
								Namespace:     "the space namespace space",
								Partition:     "space-partition",
								Peer:          "space-peer",
								SamenessGroup: "space-group",
								Exclude: []*pbauth.ExcludeSource{
									{
										IdentityName:  "not-source-identity",
										Namespace:     "the space namespace space",
										Partition:     "space-partition",
										Peer:          "space-peer",
										SamenessGroup: "space-group",
									},
								},
							},
						},
						DestinationRules: []*pbauth.DestinationRule{
							{
								PathExact:  "/hello",
								PathPrefix: "/world",
								PathRegex:  "/.*/foo",
								Headers: []*pbauth.DestinationRuleHeader{
									{
										Name:    "x-consul-test",
										Present: true,
										Exact:   "true",
										Prefix:  "prefix",
										Suffix:  "suffix",
										Regex:   "reg.*ex",
										Invert:  true,
									},
								},
								Methods: []string{"GET", "POST"},
								Exclude: []*pbauth.ExcludePermissionRule{
									{
										PathExact:  "/hello",
										PathPrefix: "/world",
										PathRegex:  "/.*/foo",
										Headers: []*pbauth.DestinationRuleHeader{
											{
												Name:    "x-consul-not-test",
												Present: true,
												Exact:   "false",
												Prefix:  "~prefix",
												Suffix:  "~suffix",
												Regex:   "~reg.*ex",
												Invert:  true,
											},
										},
										Methods:   []string{"DELETE"},
										PortNames: []string{"log"},
									},
								},
								PortNames: []string{"web", "admin"},
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
			OurData: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbauth.TrafficPermissions{},
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
				consulResource = constructTrafficPermissionResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestTrafficPermissions_Resource also includes test to verify ResourceID().
func TestTrafficPermissions_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *TrafficPermissions
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbauth.TrafficPermissions
	}{
		"empty fields": {
			Ours: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbauth.TrafficPermissions{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbauth.TrafficPermissions{},
		},
		"every field set": {
			Ours: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace:     "the space namespace space",
									Partition:     "space-partition",
									Peer:          "space-peer",
									SamenessGroup: "space-group",
									Exclude: []*pbauth.ExcludeSource{
										{
											IdentityName:  "not-source-identity",
											Namespace:     "the space namespace space",
											Partition:     "space-partition",
											Peer:          "space-peer",
											SamenessGroup: "space-group",
										},
									},
								},
								{
									IdentityName: "source-identity",
								},
							},
							DestinationRules: []*pbauth.DestinationRule{
								{
									PathExact:  "/hello",
									PathPrefix: "/world",
									PathRegex:  "/.*/foo",
									Headers: []*pbauth.DestinationRuleHeader{{
										Name:    "x-consul-test",
										Present: true,
										Exact:   "true",
										Prefix:  "prefix",
										Suffix:  "suffix",
										Regex:   "reg.*ex",
										Invert:  true,
									}},
									Methods: []string{"GET", "POST"},
									Exclude: []*pbauth.ExcludePermissionRule{
										{
											PathExact:  "/hello",
											PathPrefix: "/world",
											PathRegex:  "/.*/foo",
											Headers: []*pbauth.DestinationRuleHeader{{
												Name:    "x-consul-not-test",
												Present: true,
												Exact:   "false",
												Prefix:  "~prefix",
												Suffix:  "~suffix",
												Regex:   "~reg.*ex",
												Invert:  true,
											}},
											Methods:   []string{"DELETE"},
											PortNames: []string{"log"},
										},
									},
									PortNames: []string{"web", "admin"},
								},
							},
						},
					},
				},
			},
			ConsulNamespace: "not-default-namespace",
			ConsulPartition: "not-default-partition",
			ExpectedName:    "foo",
			ExpectedData: &pbauth.TrafficPermissions{
				Destination: &pbauth.Destination{
					IdentityName: "destination-identity",
				},
				Action: pbauth.Action_ACTION_ALLOW,
				Permissions: []*pbauth.Permission{
					{
						Sources: []*pbauth.Source{
							// These are intentionally in a different order to show that it doesn't matter
							{
								IdentityName: "source-identity",
							},
							{
								Namespace:     "the space namespace space",
								Partition:     "space-partition",
								Peer:          "space-peer",
								SamenessGroup: "space-group",
								Exclude: []*pbauth.ExcludeSource{
									{
										IdentityName:  "not-source-identity",
										Namespace:     "the space namespace space",
										Partition:     "space-partition",
										Peer:          "space-peer",
										SamenessGroup: "space-group",
									},
								},
							},
						},
						DestinationRules: []*pbauth.DestinationRule{
							{
								PathExact:  "/hello",
								PathPrefix: "/world",
								PathRegex:  "/.*/foo",
								Headers: []*pbauth.DestinationRuleHeader{{
									Name:    "x-consul-test",
									Present: true,
									Exact:   "true",
									Prefix:  "prefix",
									Suffix:  "suffix",
									Regex:   "reg.*ex",
									Invert:  true,
								}},
								Methods: []string{"GET", "POST"},
								Exclude: []*pbauth.ExcludePermissionRule{
									{
										PathExact:  "/hello",
										PathPrefix: "/world",
										PathRegex:  "/.*/foo",
										Headers: []*pbauth.DestinationRuleHeader{{
											Name:    "x-consul-not-test",
											Present: true,
											Exact:   "false",
											Prefix:  "~prefix",
											Suffix:  "~suffix",
											Regex:   "~reg.*ex",
											Invert:  true,
										}},
										Methods:   []string{"DELETE"},
										PortNames: []string{"log"},
									},
								},
								PortNames: []string{"web", "admin"},
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
			expected := constructTrafficPermissionResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "TrafficPermissions do not match")
		})
	}
}

func TestTrafficPermissions_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &TrafficPermissions{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestTrafficPermissions_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &TrafficPermissions{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestTrafficPermissions_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &TrafficPermissions{
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

func TestTrafficPermissions_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&TrafficPermissions{}).GetCondition(ConditionSynced))
}

func TestTrafficPermissions_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&TrafficPermissions{}).SyncedConditionStatus())
}

func TestTrafficPermissions_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&TrafficPermissions{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestTrafficPermissions_KubeKind(t *testing.T) {
	require.Equal(t, "trafficpermissions", (&TrafficPermissions{}).KubeKind())
}

func TestTrafficPermissions_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&TrafficPermissions{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbauth.TrafficPermissions{
			Destination: &pbauth.Destination{
				IdentityName: "foo",
			},
		},
	}).KubernetesName())
}

func TestTrafficPermissions_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &TrafficPermissions{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
// TODO: add when implemented
//func TestTrafficPermissions_DefaultNamespaceFields(t *testing.T)

func TestTrafficPermissions_Validate(t *testing.T) {
	cases := []struct {
		name            string
		input           *TrafficPermissions
		expectedErrMsgs []string
	}{
		{
			name: "kitchen sink OK",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace: "the space namespace space",
									Partition: "space-partition",
									Exclude: []*pbauth.ExcludeSource{
										{
											IdentityName:  "not-source-identity",
											Namespace:     "the space namespace space",
											SamenessGroup: "space-group",
										},
									},
								},
								{
									IdentityName: "source-identity",
									Namespace:    "another-namespace",
								},
							},
							DestinationRules: []*pbauth.DestinationRule{
								{
									PathExact: "/hello",
									Headers: []*pbauth.DestinationRuleHeader{
										{
											Name:    "x-consul-test",
											Present: true,
											Exact:   "true",
											Prefix:  "prefix",
											Suffix:  "suffix",
											Regex:   "reg.*ex",
											Invert:  true,
										},
									},
									Methods: []string{"GET", "POST"},
									Exclude: []*pbauth.ExcludePermissionRule{
										{
											PathPrefix: "/world",
											Headers: []*pbauth.DestinationRuleHeader{
												{
													Name:    "x-consul-not-test",
													Present: true,
													Exact:   "false",
													Prefix:  "~prefix",
													Suffix:  "~suffix",
													Regex:   "~reg.*ex",
													Invert:  true,
												},
											},
											Methods:   []string{"DELETE"},
											PortNames: []string{"log"},
										},
									},
									PortNames: []string{"web", "admin"},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: nil,
		},
		{
			name: "must have an action",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "dest-service",
					},
				},
			},
			expectedErrMsgs: []string{
				`trafficpermissions.auth.consul.hashicorp.com "does-not-matter" is invalid: spec.action: Invalid value: ACTION_UNSPECIFIED: action must be either allow or deny`,
			},
		},
		{
			name: "destination is required",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Action: pbauth.Action_ACTION_ALLOW,
				},
			},
			expectedErrMsgs: []string{
				`trafficpermissions.auth.consul.hashicorp.com "does-not-matter" is invalid: spec.destination: Invalid value: "null": cannot be empty`,
			},
		},
		{
			name: "destination.identityName is required",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Action:      pbauth.Action_ACTION_ALLOW,
					Destination: &pbauth.Destination{},
				},
			},
			expectedErrMsgs: []string{
				`trafficpermissions.auth.consul.hashicorp.com "does-not-matter" is invalid: spec.destination: Invalid value: authv2beta1.Destination{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:""}: cannot be empty`,
			},
		},
		{
			name: "permission.sources: partitions, peers, and sameness_groups",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace: "the space namespace space",
									Partition: "space-partition",
									Peer:      "space-peer",
								},
								{
									Namespace:     "the space namespace space",
									Partition:     "space-partition",
									SamenessGroup: "space-sameness",
								},
								{
									Namespace:     "the space namespace space",
									Peer:          "space-peer",
									SamenessGroup: "space-sameness",
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].sources[0]: Invalid value: authv2beta1.Source{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"space-partition", Peer:"space-peer", SamenessGroup:"", Exclude:[]*authv2beta1.ExcludeSource(nil)}: permission sources may not specify partitions, peers, and sameness_groups together`,
				`spec.permissions[0].sources[1]: Invalid value: authv2beta1.Source{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"space-partition", Peer:"", SamenessGroup:"space-sameness", Exclude:[]*authv2beta1.ExcludeSource(nil)}: permission sources may not specify partitions, peers, and sameness_groups together`,
				`spec.permissions[0].sources[2]: Invalid value: authv2beta1.Source{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"", Peer:"space-peer", SamenessGroup:"space-sameness", Exclude:[]*authv2beta1.ExcludeSource(nil)}: permission sources may not specify partitions, peers, and sameness_groups together`,
			},
		},
		{
			name: "permission.sources: identity name without namespace",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									IdentityName: "false-identity",
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].sources[0]: Invalid value: authv2beta1.Source{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"false-identity", Namespace:"", Partition:"", Peer:"", SamenessGroup:"", Exclude:[]*authv2beta1.ExcludeSource(nil)}: permission sources may not have wildcard namespaces and explicit names`,
			},
		},
		{
			name: "permission.sources: identity name with excludes",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace:    "default-namespace",
									IdentityName: "false-identity",
									Exclude: []*pbauth.ExcludeSource{
										{
											IdentityName: "not-source-identity",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`must be defined on wildcard sources`,
			},
		},
		{
			name: "permission.sources.exclude: incompatible tenancies",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace: "default-namespace",
									Exclude: []*pbauth.ExcludeSource{
										{
											Namespace: "the space namespace space",
											Partition: "space-partition",
											Peer:      "space-peer",
										},
										{
											Namespace:     "the space namespace space",
											Partition:     "space-partition",
											SamenessGroup: "space-sameness",
										},
										{
											Namespace:     "the space namespace space",
											Peer:          "space-peer",
											SamenessGroup: "space-sameness",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].sources[0].exclude[0]: Invalid value: authv2beta1.ExcludeSource{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"space-partition", Peer:"space-peer", SamenessGroup:""}: permissions sources may not specify partitions, peers, and sameness_groups together`,
				`spec.permissions[0].sources[0].exclude[1]: Invalid value: authv2beta1.ExcludeSource{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"space-partition", Peer:"", SamenessGroup:"space-sameness"}: permissions sources may not specify partitions, peers, and sameness_groups together`,
				`spec.permissions[0].sources[0].exclude[2]: Invalid value: authv2beta1.ExcludeSource{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"", Namespace:"the space namespace space", Partition:"", Peer:"space-peer", SamenessGroup:"space-sameness"}: permissions sources may not specify partitions, peers, and sameness_groups together`,
			},
		},
		{
			name: "permission.sources.exclude: identity name without namespace",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							Sources: []*pbauth.Source{
								{
									Namespace: "default-namespace",
									Exclude: []*pbauth.ExcludeSource{
										{
											IdentityName: "false-identity",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].sources[0].exclude[0]: Invalid value: authv2beta1.ExcludeSource{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), IdentityName:"false-identity", Namespace:"", Partition:"", Peer:"", SamenessGroup:""}: permission sources may not have wildcard namespaces and explicit names`,
			},
		},
		{
			name: "permission.destinationRules: incompatible destination rules",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							DestinationRules: []*pbauth.DestinationRule{
								{
									PathExact:  "/hello",
									PathPrefix: "foobar",
								},
								{
									PathExact: "/hello",
									PathRegex: "path-regex",
								},
								{
									PathPrefix: "foobar",
									PathRegex:  "path-regex",
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].destinationRules[0]: Invalid value: authv2beta1.DestinationRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"/hello", PathPrefix:"foobar", PathRegex:"", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil), Exclude:[]*authv2beta1.ExcludePermissionRule(nil)}: prefix values, regex values, and explicit names must not combined`,
				`spec.permissions[0].destinationRules[1]: Invalid value: authv2beta1.DestinationRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"/hello", PathPrefix:"", PathRegex:"path-regex", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil), Exclude:[]*authv2beta1.ExcludePermissionRule(nil)}: prefix values, regex values, and explicit names must not combined`,
				`spec.permissions[0].destinationRules[2]: Invalid value: authv2beta1.DestinationRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"", PathPrefix:"foobar", PathRegex:"path-regex", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil), Exclude:[]*authv2beta1.ExcludePermissionRule(nil)}: prefix values, regex values, and explicit names must not combined`,
			},
		},
		{
			name: "permission.destinationRules.exclude: incompatible destination rules",
			input: &TrafficPermissions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "does-not-matter",
					Namespace: "not-default-ns",
				},
				Spec: pbauth.TrafficPermissions{
					Destination: &pbauth.Destination{
						IdentityName: "destination-identity",
					},
					Action: pbauth.Action_ACTION_ALLOW,
					Permissions: []*pbauth.Permission{
						{
							DestinationRules: []*pbauth.DestinationRule{
								{
									Exclude: []*pbauth.ExcludePermissionRule{
										{
											PathExact:  "/hello",
											PathPrefix: "foobar",
										},
										{
											PathExact: "/hello",
											PathRegex: "path-regex",
										},
										{
											PathPrefix: "foobar",
											PathRegex:  "path-regex",
										},
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.permissions[0].destinationRules[0].exclude[0]: Invalid value: authv2beta1.ExcludePermissionRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"/hello", PathPrefix:"foobar", PathRegex:"", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil)}: prefix values, regex values, and explicit names must not combined`,
				`spec.permissions[0].destinationRules[0].exclude[1]: Invalid value: authv2beta1.ExcludePermissionRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"/hello", PathPrefix:"", PathRegex:"path-regex", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil)}: prefix values, regex values, and explicit names must not combined`,
				`spec.permissions[0].destinationRules[0].exclude[2]: Invalid value: authv2beta1.ExcludePermissionRule{state:impl.MessageState{NoUnkeyedLiterals:pragma.NoUnkeyedLiterals{}, DoNotCompare:pragma.DoNotCompare{}, DoNotCopy:pragma.DoNotCopy{}, atomicMessageInfo:(*impl.MessageInfo)(nil)}, sizeCache:0, unknownFields:[]uint8(nil), PathExact:"", PathPrefix:"foobar", PathRegex:"path-regex", Methods:[]string(nil), Headers:[]*authv2beta1.DestinationRuleHeader(nil), PortNames:[]string(nil)}: prefix values, regex values, and explicit names must not combined`,
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

func constructTrafficPermissionResource(tp *pbauth.TrafficPermissions, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbauth.TrafficPermissionsType,
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
