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
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestTCPRoute_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *TCPRoute

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbmesh.TCPRoute
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.TCPRoute{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData:            &pbmesh.TCPRoute{},
			Matches:              true,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "parent-name",
								Section: "parent-section",
							},
							Port: "20122",
						},
					},
					Rules: []*pbmesh.TCPRouteRule{
						{
							BackendRefs: []*pbmesh.TCPBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Namespace: "another-namespace",
											},
											Name:    "backend-name",
											Section: "backend-section",
										},
										Port:       "20111",
										Datacenter: "different-datacenter",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.TCPRoute{
				ParentRefs: []*pbmesh.ParentReference{
					{
						Ref: &pbresource.Reference{
							Type: pbmesh.ComputedRoutesType,
							Tenancy: &pbresource.Tenancy{
								Partition: "some-partition",
								Namespace: "some-namespace",
							},
							Name:    "parent-name",
							Section: "parent-section",
						},
						Port: "20122",
					},
				},
				Rules: []*pbmesh.TCPRouteRule{
					{
						BackendRefs: []*pbmesh.TCPBackendRef{
							{
								BackendRef: &pbmesh.BackendReference{
									Ref: &pbresource.Reference{
										Type: pbmesh.ComputedRoutesType,
										Tenancy: &pbresource.Tenancy{
											Namespace: "another-namespace",
										},
										Name:    "backend-name",
										Section: "backend-section",
									},
									Port:       "20111",
									Datacenter: "different-datacenter",
								},
								Weight: 50,
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
			OurData: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.TCPRoute{},
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
				consulResource = constructTCPRouteResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestTCPRoute_Resource also includes test to verify ResourceID().
func TestTCPRoute_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *TCPRoute
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbmesh.TCPRoute
	}{
		"empty fields": {
			Ours: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbmesh.TCPRoute{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbmesh.TCPRoute{},
		},
		"every field set": {
			Ours: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "parent-name",
								Section: "parent-section",
							},
							Port: "20122",
						},
					},
					Rules: []*pbmesh.TCPRouteRule{
						{
							BackendRefs: []*pbmesh.TCPBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Namespace: "another-namespace",
											},
											Name:    "backend-name",
											Section: "backend-section",
										},
										Port:       "20111",
										Datacenter: "different-datacenter",
									},
									Weight: 50,
								},
							},
						},
					},
				},
			},
			ConsulNamespace: "not-default-namespace",
			ConsulPartition: "not-default-partition",
			ExpectedName:    "foo",
			ExpectedData: &pbmesh.TCPRoute{
				ParentRefs: []*pbmesh.ParentReference{
					{
						Ref: &pbresource.Reference{
							Type: pbmesh.ComputedRoutesType,
							Tenancy: &pbresource.Tenancy{
								Partition: "some-partition",
								Namespace: "some-namespace",
							},
							Name:    "parent-name",
							Section: "parent-section",
						},
						Port: "20122",
					},
				},
				Rules: []*pbmesh.TCPRouteRule{
					{
						BackendRefs: []*pbmesh.TCPBackendRef{
							{
								BackendRef: &pbmesh.BackendReference{
									Ref: &pbresource.Reference{
										Type: pbmesh.ComputedRoutesType,
										Tenancy: &pbresource.Tenancy{
											Namespace: "another-namespace",
										},
										Name:    "backend-name",
										Section: "backend-section",
									},
									Port:       "20111",
									Datacenter: "different-datacenter",
								},
								Weight: 50,
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
			expected := constructTCPRouteResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "TCPRoute do not match")
		})
	}
}

func TestTCPRoute_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &TCPRoute{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestTCPRoute_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &TCPRoute{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestTCPRoute_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &TCPRoute{
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

func TestTCPRoute_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&TCPRoute{}).GetCondition(ConditionSynced))
}

func TestTCPRoute_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&TCPRoute{}).SyncedConditionStatus())
}

func TestTCPRoute_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&TCPRoute{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestTCPRoute_KubeKind(t *testing.T) {
	require.Equal(t, "tcproute", (&TCPRoute{}).KubeKind())
}

func TestTCPRoute_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&TCPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbmesh.TCPRoute{},
	}).KubernetesName())
}

func TestTCPRoute_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &TCPRoute{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
// TODO: add when implemented
//func TestTCPRoute_DefaultNamespaceFields(t *testing.T)

func TestTCPRoute_Validate(t *testing.T) {
	cases := []struct {
		name            string
		input           *TCPRoute
		expectedErrMsgs []string
	}{
		{
			name: "kitchen sink OK",
			input: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{
						{
							Ref: &pbresource.Reference{
								Type: pbmesh.ComputedRoutesType,
								Tenancy: &pbresource.Tenancy{
									Partition: "some-partition",
									Namespace: "some-namespace",
								},
								Name:    "parent-name",
								Section: "parent-section",
							},
							Port: "20122",
						},
					},
					Rules: []*pbmesh.TCPRouteRule{
						{
							BackendRefs: []*pbmesh.TCPBackendRef{
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
											Tenancy: &pbresource.Tenancy{
												Namespace: "another-namespace",
											},
											Name:    "backend-name",
											Section: "backend-section",
										},
										Port: "20111",
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
			name: "no parentRefs",
			input: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{},
				},
			},
			expectedErrMsgs: []string{
				`spec.parentRefs: Required value: cannot be empty`,
			},
		},
		{
			name: "multiple rules",
			input: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{{}},
					Rules: []*pbmesh.TCPRouteRule{
						{BackendRefs: []*pbmesh.TCPBackendRef{{BackendRef: &pbmesh.BackendReference{}}}},
						{BackendRefs: []*pbmesh.TCPBackendRef{{BackendRef: &pbmesh.BackendReference{}}}},
					},
				},
			},
			expectedErrMsgs: []string{
				`must only specify a single rule for now`,
			},
		},
		{
			name: "rules.backendRefs",
			input: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{{}},
					Rules: []*pbmesh.TCPRouteRule{
						{BackendRefs: []*pbmesh.TCPBackendRef{}},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].backendRefs: Required value: cannot be empty`,
			},
		},
		{
			name: "rules.backendRefs.backendRef",
			input: &TCPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.TCPRoute{
					ParentRefs: []*pbmesh.ParentReference{{}},
					Rules: []*pbmesh.TCPRouteRule{
						{
							BackendRefs: []*pbmesh.TCPBackendRef{
								{},
								{
									BackendRef: &pbmesh.BackendReference{
										Ref: &pbresource.Reference{
											Type: pbmesh.ComputedRoutesType,
										},
										Datacenter: "backend-datacenter",
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.rules[0].backendRefs[0].backendRef: Required value: missing required field`,
				`spec.rules[0].backendRefs[1].backendRef.datacenter: Invalid value: "backend-datacenter": datacenter is not yet supported on backend refs`,
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

func constructTCPRouteResource(tp *pbmesh.TCPRoute, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbmesh.TCPRouteType,
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
