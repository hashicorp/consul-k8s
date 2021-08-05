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
func TestMesh_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    Mesh
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
				Spec: MeshSpec{},
			},
			Theirs: &capi.MeshConfigEntry{
				Namespace:   "default",
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
			Ours: Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
				Spec: MeshSpec{
					TransparentProxy: TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			Theirs: &capi.MeshConfigEntry{
				TransparentProxy: capi.TransparentProxyMeshConfig{
					MeshDestinationsOnly: true,
				},
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			Matches: true,
		},
		"mismatched types does not match": {
			Ours: Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Mesh,
				},
				Spec: MeshSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: common.Mesh,
				Kind: capi.MeshConfig,
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

func TestMesh_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours Mesh
		Exp  *capi.MeshConfigEntry
	}{
		"empty fields": {
			Ours: Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: MeshSpec{},
			},
			Exp: &capi.MeshConfigEntry{
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: MeshSpec{
					TransparentProxy: TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			Exp: &capi.MeshConfigEntry{
				TransparentProxy: capi.TransparentProxyMeshConfig{
					MeshDestinationsOnly: true,
				},
				Namespace: "",
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
			mesh, ok := act.(*capi.MeshConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, mesh)
		})
	}
}

func TestMesh_AddFinalizer(t *testing.T) {
	mesh := &Mesh{}
	mesh.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, mesh.ObjectMeta.Finalizers)
}

func TestMesh_RemoveFinalizer(t *testing.T) {
	mesh := &Mesh{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	mesh.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, mesh.ObjectMeta.Finalizers)
}

func TestMesh_SetSyncedCondition(t *testing.T) {
	mesh := &Mesh{}
	mesh.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, mesh.Status.Conditions[0].Status)
	require.Equal(t, "reason", mesh.Status.Conditions[0].Reason)
	require.Equal(t, "message", mesh.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, mesh.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestMesh_SetLastSyncedTime(t *testing.T) {
	mesh := &Mesh{}
	syncedTime := metav1.NewTime(time.Now())
	mesh.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, mesh.Status.LastSyncedTime)
}

func TestMesh_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			mesh := &Mesh{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, mesh.SyncedConditionStatus())
		})
	}
}

func TestMesh_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&Mesh{}).GetCondition(ConditionSynced))
}

func TestMesh_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&Mesh{}).SyncedConditionStatus())
}

func TestMesh_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&Mesh{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestMesh_ConsulKind(t *testing.T) {
	require.Equal(t, capi.MeshConfig, (&Mesh{}).ConsulKind())
}

func TestMesh_KubeKind(t *testing.T) {
	require.Equal(t, "mesh", (&Mesh{}).KubeKind())
}

func TestMesh_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&Mesh{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestMesh_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&Mesh{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestMesh_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&Mesh{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestMesh_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&Mesh{}).ConsulGlobalResource())
}

func TestMesh_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	mesh := &Mesh{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, mesh.GetObjectMeta())
}
