package v1alpha1

import (
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Test MatchesConsul for cases that should return true.
func TestPartitionExports_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    PartitionExports
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: PartitionExportsSpec{},
			},
			Theirs: &capi.PartitionExportsConfigEntry{
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
			Ours: PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: PartitionExportsSpec{
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
							},
						},
					},
				},
			},
			Theirs: &capi.PartitionExportsConfigEntry{
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
			Ours: PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: PartitionExportsSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: common.DefaultConsulPartition,
				Kind: capi.PartitionExports,
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

func TestPartitionExports_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours PartitionExports
		Exp  *capi.PartitionExportsConfigEntry
	}{
		"empty fields": {
			Ours: PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: PartitionExportsSpec{},
			},
			Exp: &capi.PartitionExportsConfigEntry{
				Name: common.DefaultConsulPartition,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: PartitionExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.DefaultConsulPartition,
				},
				Spec: PartitionExportsSpec{
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
							},
						},
					},
				},
			},
			Exp: &capi.PartitionExportsConfigEntry{
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
			partitionExports, ok := act.(*capi.PartitionExportsConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, partitionExports)
		})
	}
}

func TestPartitionExports_AddFinalizer(t *testing.T) {
	partitionExports := &PartitionExports{}
	partitionExports.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, partitionExports.ObjectMeta.Finalizers)
}

func TestPartitionExports_RemoveFinalizer(t *testing.T) {
	partitionExports := &PartitionExports{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	partitionExports.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, partitionExports.ObjectMeta.Finalizers)
}

func TestPartitionExports_SetSyncedCondition(t *testing.T) {
	partitionExports := &PartitionExports{}
	partitionExports.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, partitionExports.Status.Conditions[0].Status)
	require.Equal(t, "reason", partitionExports.Status.Conditions[0].Reason)
	require.Equal(t, "message", partitionExports.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, partitionExports.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestPartitionExports_SetLastSyncedTime(t *testing.T) {
	partitionExports := &PartitionExports{}
	syncedTime := metav1.NewTime(time.Now())
	partitionExports.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, partitionExports.Status.LastSyncedTime)
}

func TestPartitionExports_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			partitionExports := &PartitionExports{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, partitionExports.SyncedConditionStatus())
		})
	}
}

func TestPartitionExports_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&PartitionExports{}).GetCondition(ConditionSynced))
}

func TestPartitionExports_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&PartitionExports{}).SyncedConditionStatus())
}

func TestPartitionExports_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&PartitionExports{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestPartitionExports_ConsulKind(t *testing.T) {
	require.Equal(t, capi.PartitionExports, (&PartitionExports{}).ConsulKind())
}

func TestPartitionExports_KubeKind(t *testing.T) {
	require.Equal(t, "partitionexports", (&PartitionExports{}).KubeKind())
}

func TestPartitionExports_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&PartitionExports{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestPartitionExports_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&PartitionExports{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestPartitionExports_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&PartitionExports{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestPartitionExports_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&PartitionExports{}).ConsulGlobalResource())
}

func TestPartitionExports_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	partitionExports := &PartitionExports{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, partitionExports.GetObjectMeta())
}
