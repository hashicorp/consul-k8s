// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

// Test MatchesConsul for cases that should return true.
func TestExportedServices_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ExportedServices
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{},
			},
			Theirs: &capi.ExportedServicesConfigEntry{
				Name:        common.DefaultConsulPartition,
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
			Ours: ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "second",
								},
								{
									Partition: "third",
								},
								{
									Peer: "second-peer",
								},
								{
									SamenessGroup: "sg1",
								},
							},
						},
						{
							Name:      "service-backend",
							Namespace: "backend",
							Consumers: []ServiceConsumer{
								{
									Partition: "fourth",
								},
								{
									Partition: "fifth",
								},
								{
									Peer: "third-peer",
								},
								{
									SamenessGroup: "sg2",
								},
							},
						},
					},
				},
			},
			Theirs: &capi.ExportedServicesConfigEntry{
				Name: common.DefaultConsulPartition,
				Services: []capi.ExportedService{
					{
						Name:      "service-frontend",
						Namespace: "frontend",
						Consumers: []capi.ServiceConsumer{
							{
								Partition: "second",
							},
							{
								Partition: "third",
							},
							{
								Peer: "second-peer",
							},
							{
								SamenessGroup: "sg1",
								Partition:     "default",
							},
						},
					},
					{
						Name:      "service-backend",
						Namespace: "backend",
						Consumers: []capi.ServiceConsumer{
							{
								Partition: "fourth",
							},
							{
								Partition: "fifth",
							},
							{
								Peer: "third-peer",
							},
							{
								SamenessGroup: "sg2",
							},
						},
					},
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"mismatched types does not match": {
			Ours: ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: common.DefaultConsulPartition,
				Kind: capi.ExportedServices,
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

func TestExportedServices_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ExportedServices
		Exp  *capi.ExportedServicesConfigEntry
	}{
		"empty fields": {
			Ours: ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{},
			},
			Exp: &capi.ExportedServicesConfigEntry{
				Name: common.DefaultConsulPartition,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "second",
								},
								{
									Partition: "third",
								},
								{
									Peer: "second-peer",
								},
								{
									SamenessGroup: "sg2",
								},
							},
						},
						{
							Name:      "service-backend",
							Namespace: "backend",
							Consumers: []ServiceConsumer{
								{
									Partition: "fourth",
								},
								{
									Partition: "fifth",
								},
								{
									Peer: "third-peer",
								},
								{
									SamenessGroup: "sg3",
								},
							},
						},
					},
				},
			},
			Exp: &capi.ExportedServicesConfigEntry{
				Name: common.DefaultConsulPartition,
				Services: []capi.ExportedService{
					{
						Name:      "service-frontend",
						Namespace: "frontend",
						Consumers: []capi.ServiceConsumer{
							{
								Partition: "second",
							},
							{
								Partition: "third",
							},
							{
								Peer: "second-peer",
							},
							{
								SamenessGroup: "sg2",
							},
						},
					},
					{
						Name:      "service-backend",
						Namespace: "backend",
						Consumers: []capi.ServiceConsumer{
							{
								Partition: "fourth",
							},
							{
								Partition: "fifth",
							},
							{
								Peer: "third-peer",
							},
							{
								SamenessGroup: "sg3",
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
			exportedServices, ok := act.(*capi.ExportedServicesConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, exportedServices)
		})
	}
}

func TestExportedServices_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *ExportedServices
		namespaceEnabled  bool
		partitionsEnabled bool
		expectedErrMsgs   []string
	}{
		"valid": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "second",
								},
								{
									Peer: "second-peer",
								},
								{
									SamenessGroup: "sg2",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs:   []string{},
		},
		"no consumers specified": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`spec.services[0]: Invalid value: []v1alpha1.ServiceConsumer{}: service must have at least 1 consumer.`,
			},
		},
		"both partition and peer name specified": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "second",
									Peer:      "second-peer",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`service consumer must define at most one of Peer, Partition, or SamenessGroup`,
			},
		},
		"none of peer, partition, or sameness group defined": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`service consumer must define at least one of Peer, Partition, or SamenessGroup`,
			},
		},
		"partition provided when partitions are disabled": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "test-partition",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				`spec.services[0].consumers[0].partition: Invalid value: "test-partition": Consul Admin Partitions need to be enabled to specify partition.`,
			},
		},
		"namespace provided when namespaces are disabled": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Peer: "test-peer",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  false,
			partitionsEnabled: false,
			expectedErrMsgs: []string{
				`spec.services[0]: Invalid value: "frontend": Consul Namespaces must be enabled to specify service namespace.`,
			},
		},
		"exporting to all partitions is not supported": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "*",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`exporting to all partitions (wildcard) is not supported`,
			},
		},
		"exporting to all peers (wildcard) is not supported": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Peer: "*",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`exporting to all peers (wildcard) is not supported`,
			},
		},
		"exporting to all sameness groups (wildcard) is not supported": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									SamenessGroup: "*",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`exporting to all sameness groups (wildcard) is not supported`,
			},
		},
		"multiple errors": {
			input: &ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: ExportedServicesSpec{
					Services: []ExportedService{
						{
							Name:      "service-frontend",
							Namespace: "frontend",
							Consumers: []ServiceConsumer{
								{
									Partition: "second",
									Peer:      "second-peer",
								},
								{},
								{
									SamenessGroup: "sg2",
									Partition:     "partition2",
								},
							},
						},
					},
				},
			},
			namespaceEnabled:  true,
			partitionsEnabled: true,
			expectedErrMsgs: []string{
				`spec.services[0].consumers[0]: Invalid value: v1alpha1.ServiceConsumer{Partition:"second", Peer:"second-peer", SamenessGroup:""}: service consumer must define at most one of Peer, Partition, or SamenessGroup`,
				`spec.services[0].consumers[1]: Invalid value: v1alpha1.ServiceConsumer{Partition:"", Peer:"", SamenessGroup:""}: service consumer must define at least one of Peer, Partition, or SamenessGroup`,
				`spec.services[0].consumers[2]: Invalid value: v1alpha1.ServiceConsumer{Partition:"partition2", Peer:"", SamenessGroup:"sg2"}: service consumer must define at most one of Peer, Partition, or SamenessGroup`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{NamespacesEnabled: testCase.namespaceEnabled, PartitionsEnabled: testCase.partitionsEnabled, Partition: common.DefaultConsulPartition})
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

func TestExportedServices_AddFinalizer(t *testing.T) {
	exportedServices := &ExportedServices{}
	exportedServices.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, exportedServices.ObjectMeta.Finalizers)
}

func TestExportedServices_RemoveFinalizer(t *testing.T) {
	exportedServices := &ExportedServices{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	exportedServices.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, exportedServices.ObjectMeta.Finalizers)
}

func TestExportedServices_SetSyncedCondition(t *testing.T) {
	exportedServices := &ExportedServices{}
	exportedServices.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, exportedServices.Status.Conditions[0].Status)
	require.Equal(t, "reason", exportedServices.Status.Conditions[0].Reason)
	require.Equal(t, "message", exportedServices.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, exportedServices.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestExportedServices_SetLastSyncedTime(t *testing.T) {
	exportedServices := &ExportedServices{}
	syncedTime := metav1.NewTime(time.Now())
	exportedServices.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, exportedServices.Status.LastSyncedTime)
}

func TestExportedServices_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			exportedServices := &ExportedServices{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, exportedServices.SyncedConditionStatus())
		})
	}
}

func TestExportedServices_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ExportedServices{}).GetCondition(ConditionSynced))
}

func TestExportedServices_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ExportedServices{}).SyncedConditionStatus())
}

func TestExportedServices_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ExportedServices{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestExportedServices_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ExportedServices, (&ExportedServices{}).ConsulKind())
}

func TestExportedServices_KubeKind(t *testing.T) {
	require.Equal(t, "exportedservices", (&ExportedServices{}).KubeKind())
}

func TestExportedServices_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ExportedServices{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestExportedServices_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ExportedServices{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestExportedServices_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&ExportedServices{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestExportedServices_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&ExportedServices{}).ConsulGlobalResource())
}

func TestExportedServices_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	exportedServices := &ExportedServices{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, exportedServices.GetObjectMeta())
}
