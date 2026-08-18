// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))
	return s
}

func minimalCluster(name, namespace string, size int) *v1alpha1.ConsulCluster {
	qty := resource.MustParse("1Gi")
	return &v1alpha1.ConsulCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.ConsulClusterSpec{
			Size:    size,
			Version: "1.18.0",
			Storage: qty,
		},
	}
}

func TestReconcile_AddsFinalizer(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
	})
	require.NoError(t, err)

	updated := &v1alpha1.ConsulCluster{}
	require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul", Namespace: "default"}, updated))
	assert.Contains(t, updated.Finalizers, finalizerName)
}

func TestReconcile_CreatesHeadlessService(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 3; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	svc := &corev1.Service{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-server", Namespace: "default"}, svc))
	assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
	assert.True(t, svc.Spec.PublishNotReadyAddresses)
}

func TestReconcile_CreatesClientService(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 1)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 4; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	svc := &corev1.Service{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-ui", Namespace: "default"}, svc))
	assert.NotEmpty(t, svc.Spec.Ports)
}

func TestReconcile_CreatesConfigMap(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 4; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	cm := &corev1.ConfigMap{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-server-config", Namespace: "default"}, cm))
	assert.Contains(t, cm.Data["server.json"], `"bootstrap_expect": 3`)
	assert.Contains(t, cm.Data["server.json"], `"datacenter": "dc1"`)
	assert.Contains(t, cm.Data["server.json"], `consul-server.default.svc`)
}

func TestReconcile_ScalesUpPods(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 4; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)

		podList := &corev1.PodList{}
		require.NoError(t, fakeClient.List(context.Background(), podList))
		for j := range podList.Items {
			podList.Items[j].Status.Phase = corev1.PodRunning
			require.NoError(t, fakeClient.Status().Update(context.Background(), &podList.Items[j]))
		}
	}

	podList := &corev1.PodList{}
	require.NoError(t, fakeClient.List(context.Background(), podList,
		client.MatchingLabels(map[string]string{
			labelApp:       labelAppValue,
			labelComponent: labelComponentValue,
			labelCluster:   "consul",
		}),
	))
	assert.Equal(t, 3, len(podList.Items))
}

func TestReconcile_ScalesDownPods(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 1)
	s := testScheme(t)

	existingPods := []*corev1.Pod{
		makeRunningPod("consul-aaaaaa", "consul", "default"),
		makeRunningPod("consul-bbbbbb", "consul", "default"),
	}

	objs := []runtime.Object{cluster}
	for _, p := range existingPods {
		objs = append(objs, p)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(cluster).
		Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 10; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	podList := &corev1.PodList{}
	require.NoError(t, fakeClient.List(context.Background(), podList,
		client.MatchingLabels(map[string]string{
			labelApp:       labelAppValue,
			labelComponent: labelComponentValue,
			labelCluster:   "consul",
		}),
	))
	assert.Equal(t, 1, len(podList.Items))
}

func TestReconcile_PausedSkipsScaling(t *testing.T) {
	t.Parallel()

	qty := resource.MustParse("1Gi")
	cluster := &v1alpha1.ConsulCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "consul", Namespace: "default"},
		Spec: v1alpha1.ConsulClusterSpec{
			Size:    3,
			Version: "1.18.0",
			Storage: qty,
			Paused:  true,
		},
	}
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 5; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	podList := &corev1.PodList{}
	require.NoError(t, fakeClient.List(context.Background(), podList))
	assert.Equal(t, 0, len(podList.Items), "paused cluster must not create pods")
}

func TestReconcile_HandlesDeletion(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	qty := resource.MustParse("1Gi")
	cluster := &v1alpha1.ConsulCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "consul",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{finalizerName},
		},
		Spec: v1alpha1.ConsulClusterSpec{
			Size:    3,
			Version: "1.18.0",
			Storage: qty,
			PersistentVolumeClaimRetentionPolicy: &v1alpha1.ConsulClusterPVCRetentionPolicy{
				WhenDeleted: v1alpha1.PVCRetentionPolicyDelete,
			},
		},
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-consul-aaaaaa",
			Namespace: "default",
			Labels: map[string]string{
				labelApp:       labelAppValue,
				labelComponent: labelComponentValue,
				labelCluster:   "consul",
			},
		},
	}

	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster, pvc).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
	})
	require.NoError(t, err)

	updated := &v1alpha1.ConsulCluster{}
	err = fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul", Namespace: "default"}, updated)
	assert.True(t, k8serrors.IsNotFound(err) || !containsFinalizer(updated.Finalizers, finalizerName),
		"finalizer should be removed or object deleted")

	pvcList := &corev1.PersistentVolumeClaimList{}
	require.NoError(t, fakeClient.List(context.Background(), pvcList))
	assert.Equal(t, 0, len(pvcList.Items), "PVCs should be deleted when WhenDeleted=Delete")
}

func TestBuildServerConfigJSON_DatacenterAndRetryJoin(t *testing.T) {
	t.Parallel()

	qty := resource.MustParse("1Gi")
	cluster := &v1alpha1.ConsulCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "mydc", Namespace: "myns"},
		Spec: v1alpha1.ConsulClusterSpec{
			Size:           5,
			Version:        "1.18.0",
			Storage:        qty,
			DatacenterName: "dc2",
		},
	}

	data, err := buildServerConfigJSON(cluster)
	require.NoError(t, err)
	assert.Contains(t, data, `"datacenter": "dc2"`)
	assert.Contains(t, data, `"bootstrap_expect": 5`)
	assert.Contains(t, data, `mydc-server.myns.svc`)
}

func TestBuildServerConfigJSON_TLSDisablesHTTP(t *testing.T) {
	t.Parallel()

	qty := resource.MustParse("1Gi")
	cluster := &v1alpha1.ConsulCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "consul", Namespace: "default"},
		Spec: v1alpha1.ConsulClusterSpec{
			Size:    3,
			Version: "1.18.0",
			Storage: qty,
			TLS: &v1alpha1.ConsulTLSSpec{
				Enabled:              true,
				CASecretName:         "consul-ca",
				ServerCertSecretName: "consul-server-cert",
				HTTPSOnly:            true,
			},
		},
	}

	data, err := buildServerConfigJSON(cluster)
	require.NoError(t, err)
	assert.Contains(t, data, `"http": -1`)
}

func TestGeneratePodName(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		name, err := generatePodName("consul")
		require.NoError(t, err)
		assert.True(t, len(name) > len("consul-server-"), "name should be longer than prefix")
		assert.False(t, seen[name], "pod names must be unique")
		seen[name] = true
	}
}

func TestPVCRetentionPolicy_Defaults(t *testing.T) {
	t.Parallel()

	cluster := &v1alpha1.ConsulCluster{}
	policy := pvcRetentionPolicy(cluster)
	assert.Equal(t, v1alpha1.PVCRetentionPolicyDelete, policy.WhenScaled)
	assert.Equal(t, v1alpha1.PVCRetentionPolicyDelete, policy.WhenDeleted)
}

func makeRunningPod(name, clusterName, namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labelApp:       labelAppValue,
				labelComponent: labelComponentValue,
				labelCluster:   clusterName,
				labelVersion:   "1.18.0",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
}

func containsFinalizer(finalizers []string, name string) bool {
	for _, f := range finalizers {
		if f == name {
			return true
		}
	}
	return false
}

func TestReconcile_ConfigMapUpdatedOnResize(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	s := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(cluster).WithStatusSubresource(cluster).Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 5; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	cm := &corev1.ConfigMap{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-server-config", Namespace: "default"}, cm))
	assert.Contains(t, cm.Data["server.json"], `"bootstrap_expect": 3`)

	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Spec.Size = 5
	require.NoError(t, fakeClient.Patch(context.Background(), cluster, patch))

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
	})
	require.NoError(t, err)

	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-server-config", Namespace: "default"}, cm))
	assert.Contains(t, cm.Data["server.json"], `"bootstrap_expect": 5`)
}

func TestBuildServerPod_TerminationGracePeriod(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	pod := buildServerPod(cluster, "consul-abc123", "data-consul-abc123")
	require.NotNil(t, pod.Spec.TerminationGracePeriodSeconds)
	assert.Equal(t, int64(60), *pod.Spec.TerminationGracePeriodSeconds)
}

func TestBuildServerPod_PreStopLeaveHook(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	pod := buildServerPod(cluster, "consul-abc123", "data-consul-abc123")
	require.Len(t, pod.Spec.Containers, 1)
	lc := pod.Spec.Containers[0].Lifecycle
	require.NotNil(t, lc)
	require.NotNil(t, lc.PreStop)
	require.NotNil(t, lc.PreStop.Exec)
	assert.Contains(t, lc.PreStop.Exec.Command, "consul leave")
}

func TestReconcile_ScaleDown_PVCDeletedAfterPod(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 1)
	s := testScheme(t)

	pod := makeRunningPod("consul-aaaaaa", "consul", "default")
	pod2 := makeRunningPod("consul-bbbbbb", "consul", "default")
	pvc2 := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "data-consul-bbbbbb",
			Namespace: "default",
			Labels:    serverPodLabels(cluster),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster, pod, pod2, pvc2).
		WithStatusSubresource(cluster).
		Build()

	r := &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}

	for i := 0; i < 5; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
		})
		require.NoError(t, err)
	}

	podList := &corev1.PodList{}
	require.NoError(t, fakeClient.List(context.Background(), podList,
		client.MatchingLabels(serverPodLabels(cluster))))
	assert.Equal(t, 1, len(podList.Items))

	pvcList := &corev1.PersistentVolumeClaimList{}
	require.NoError(t, fakeClient.List(context.Background(), pvcList))
	assert.Equal(t, 0, len(pvcList.Items), "PVC should be deleted after pod on scale-down")
}
