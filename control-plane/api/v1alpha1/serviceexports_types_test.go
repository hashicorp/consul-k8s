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
func TestServiceExports_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ServiceExports
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{},
			},
			Theirs: &capi.ServiceExportsConfigEntry{
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
			Ours: ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{
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
			Theirs: &capi.ServiceExportsConfigEntry{
				Partition: "default",
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
			Ours: ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: common.Exports,
				Kind: capi.ServiceExports,
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

func TestServiceExports_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ServiceExports
		Exp  *capi.ServiceExportsConfigEntry
	}{
		"empty fields": {
			Ours: ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{},
			},
			Exp: &capi.ServiceExportsConfigEntry{
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ServiceExports{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Exports,
				},
				Spec: ServiceExportsSpec{
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
			Exp: &capi.ServiceExportsConfigEntry{
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
			serviceExports, ok := act.(*capi.ServiceExportsConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, serviceExports)
		})
	}
}

func TestServiceExports_AddFinalizer(t *testing.T) {
	serviceExports := &ServiceExports{}
	serviceExports.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceExports.ObjectMeta.Finalizers)
}

func TestServiceExports_RemoveFinalizer(t *testing.T) {
	serviceExports := &ServiceExports{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceExports.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceExports.ObjectMeta.Finalizers)
}

func TestServiceExports_SetSyncedCondition(t *testing.T) {
	serviceExports := &ServiceExports{}
	serviceExports.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceExports.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceExports.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceExports.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceExports.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceExports_SetLastSyncedTime(t *testing.T) {
	serviceExports := &ServiceExports{}
	syncedTime := metav1.NewTime(time.Now())
	serviceExports.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, serviceExports.Status.LastSyncedTime)
}

func TestServiceExports_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceExports := &ServiceExports{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceExports.SyncedConditionStatus())
		})
	}
}

func TestServiceExports_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceExports{}).GetCondition(ConditionSynced))
}

func TestServiceExports_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceExports{}).SyncedConditionStatus())
}

func TestServiceExports_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceExports{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceExports_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceExports, (&ServiceExports{}).ConsulKind())
}

func TestServiceExports_KubeKind(t *testing.T) {
	require.Equal(t, "serviceexports", (&ServiceExports{}).KubeKind())
}

func TestServiceExports_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceExports{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceExports_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceExports{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestServiceExports_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&ServiceExports{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceExports_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&ServiceExports{}).ConsulGlobalResource())
}

func TestServiceExports_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	ServiceExports := &ServiceExports{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, ServiceExports.GetObjectMeta())
}
