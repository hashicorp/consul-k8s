// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestFailoverPolicy_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *FailoverPolicy

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbcatalog.FailoverPolicy
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &FailoverPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbcatalog.FailoverPolicy{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData:            &pbcatalog.FailoverPolicy{},
			Matches:              true,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &FailoverPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbcatalog.FailoverPolicy{
					Config: &pbcatalog.FailoverConfig{
						Destinations: []*pbcatalog.FailoverDestination{
							{
								Ref: &pbresource.Reference{
									Type: pbcatalog.ServiceType,
									Tenancy: &pbresource.Tenancy{
										Partition: "test-partition",
										Namespace: "test-namespace",
									},
									Name:    "test-service",
									Section: "section-one",
								},
								Port:       "default-port",
								Datacenter: "test-datacenter",
							},
						},
						Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_SEQUENTIAL,
						Regions:       []string{"this-region", "other-region"},
						SamenessGroup: "this-sameness-group",
					},
					PortConfigs: map[string]*pbcatalog.FailoverConfig{
						"test": {
							Destinations: []*pbcatalog.FailoverDestination{
								{
									Ref: &pbresource.Reference{
										Type: pbcatalog.ServiceType,
										Tenancy: &pbresource.Tenancy{
											PeerName: "other",
										},
										Name:    "destination-name",
										Section: "section-two",
									},
									Port:       "other-port",
									Datacenter: "another-datacenter",
								},
							},
							Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_ORDER_BY_LOCALITY,
							Regions:       []string{"another-region"},
							SamenessGroup: "that-sameness-group",
						},
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbcatalog.FailoverPolicy{
				Config: &pbcatalog.FailoverConfig{
					Destinations: []*pbcatalog.FailoverDestination{
						{
							Ref: &pbresource.Reference{
								Type: pbcatalog.ServiceType,
								Tenancy: &pbresource.Tenancy{
									Partition: "test-partition",
									Namespace: "test-namespace",
								},
								Name:    "test-service",
								Section: "section-one",
							},
							Port:       "default-port",
							Datacenter: "test-datacenter",
						},
					},
					Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_SEQUENTIAL,
					Regions:       []string{"this-region", "other-region"},
					SamenessGroup: "this-sameness-group",
				},
				PortConfigs: map[string]*pbcatalog.FailoverConfig{
					"test": {
						Destinations: []*pbcatalog.FailoverDestination{
							{
								Ref: &pbresource.Reference{
									Type: pbcatalog.ServiceType,
									Tenancy: &pbresource.Tenancy{
										PeerName: "other",
									},
									Name:    "destination-name",
									Section: "section-two",
								},
								Port:       "other-port",
								Datacenter: "another-datacenter",
							},
						},
						Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_ORDER_BY_LOCALITY,
						Regions:       []string{"another-region"},
						SamenessGroup: "that-sameness-group",
					},
				},
			},
			Matches: true,
		},
		"different types does not match": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &FailoverPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbcatalog.FailoverPolicy{},
			},
			ResourceOverride: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "name",
					Type: pbmesh.ProxyConfigurationType,
					Tenancy: &pbresource.Tenancy{
						Partition: constants.DefaultConsulNS,
						Namespace: constants.DefaultConsulPartition,

						// Because we are explicitly defining NS/partition, this will not default and must be explicit.
						// At a future point, this will move out of the Tenancy block.
						PeerName: constants.DefaultConsulPeer,
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
				consulResource = constructFailoverPolicyResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestFailoverPolicy_Resource also includes test to verify ResourceID().
func TestFailoverPolicy_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *FailoverPolicy
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbcatalog.FailoverPolicy
	}{
		"empty fields": {
			Ours: &FailoverPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbcatalog.FailoverPolicy{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbcatalog.FailoverPolicy{},
		},
		"every field set": {
			Ours: &FailoverPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbcatalog.FailoverPolicy{
					Config: &pbcatalog.FailoverConfig{
						Destinations: []*pbcatalog.FailoverDestination{
							{
								Ref: &pbresource.Reference{
									Type: pbcatalog.ServiceType,
									Tenancy: &pbresource.Tenancy{
										Partition: "test-partition",
										Namespace: "test-namespace",
									},
									Name:    "test-service",
									Section: "section-one",
								},
								Port:       "default-port",
								Datacenter: "test-datacenter",
							},
						},
						Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_SEQUENTIAL,
						Regions:       []string{"this-region", "other-region"},
						SamenessGroup: "this-sameness-group",
					},
					PortConfigs: map[string]*pbcatalog.FailoverConfig{
						"test": {
							Destinations: []*pbcatalog.FailoverDestination{
								{
									Ref: &pbresource.Reference{
										Type: pbcatalog.ServiceType,
										Tenancy: &pbresource.Tenancy{
											PeerName: "other",
										},
										Name:    "destination-name",
										Section: "section-two",
									},
									Port:       "other-port",
									Datacenter: "another-datacenter",
								},
							},
							Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_ORDER_BY_LOCALITY,
							Regions:       []string{"another-region"},
							SamenessGroup: "that-sameness-group",
						},
					},
				},
			},
			ConsulNamespace: "not-default-namespace",
			ConsulPartition: "not-default-partition",
			ExpectedName:    "foo",
			ExpectedData: &pbcatalog.FailoverPolicy{
				Config: &pbcatalog.FailoverConfig{
					Destinations: []*pbcatalog.FailoverDestination{
						{
							Ref: &pbresource.Reference{
								Type: pbcatalog.ServiceType,
								Tenancy: &pbresource.Tenancy{
									Partition: "test-partition",
									Namespace: "test-namespace",
								},
								Name:    "test-service",
								Section: "section-one",
							},
							Port:       "default-port",
							Datacenter: "test-datacenter",
						},
					},
					Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_SEQUENTIAL,
					Regions:       []string{"this-region", "other-region"},
					SamenessGroup: "this-sameness-group",
				},
				PortConfigs: map[string]*pbcatalog.FailoverConfig{
					"test": {
						Destinations: []*pbcatalog.FailoverDestination{
							{
								Ref: &pbresource.Reference{
									Type: pbcatalog.ServiceType,
									Tenancy: &pbresource.Tenancy{
										PeerName: "other",
									},
									Name:    "destination-name",
									Section: "section-two",
								},
								Port:       "other-port",
								Datacenter: "another-datacenter",
							},
						},
						Mode:          pbcatalog.FailoverMode_FAILOVER_MODE_ORDER_BY_LOCALITY,
						Regions:       []string{"another-region"},
						SamenessGroup: "that-sameness-group",
					},
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			actual := c.Ours.Resource(c.ConsulNamespace, c.ConsulPartition)
			expected := constructFailoverPolicyResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "FailoverPolicy do not match")
		})
	}
}

func TestFailoverPolicy_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &FailoverPolicy{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestFailoverPolicy_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &FailoverPolicy{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestFailoverPolicy_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &FailoverPolicy{
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

func TestFailoverPolicy_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&FailoverPolicy{}).GetCondition(ConditionSynced))
}

func TestFailoverPolicy_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&FailoverPolicy{}).SyncedConditionStatus())
}

func TestFailoverPolicy_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&FailoverPolicy{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestFailoverPolicy_KubeKind(t *testing.T) {
	require.Equal(t, "failoverpolicy", (&FailoverPolicy{}).KubeKind())
}

func TestFailoverPolicy_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&FailoverPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbcatalog.FailoverPolicy{},
	}).KubernetesName())
}

func TestFailoverPolicy_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &FailoverPolicy{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

func constructFailoverPolicyResource(tp *pbcatalog.FailoverPolicy, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbcatalog.FailoverPolicyType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
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
