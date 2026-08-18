// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consulcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
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
	require.NoError(t, appsv1.AddToScheme(s))
	require.NoError(t, policyv1.AddToScheme(s))
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

func TestReconcile_DoesNotCreateClientOrExposeService(t *testing.T) {
	t.Parallel()

	// The UI and expose Services are Helm-owned (ui-service.yaml,
	// expose-servers-service.yaml) — Helm remains the delivery mechanism for
	// cross-cutting objects, so the operator must not also create them.
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
	err := fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-ui", Namespace: "default"}, svc)
	assert.True(t, k8serrors.IsNotFound(err), "operator must not create the Helm-owned UI service")

	err = fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "consul-expose", Namespace: "default"}, svc)
	assert.True(t, k8serrors.IsNotFound(err), "operator must not create an expose service")
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

func TestReconcile_PausedSkipsStatefulSet(t *testing.T) {
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

	stsList := &appsv1.StatefulSetList{}
	require.NoError(t, fakeClient.List(context.Background(), stsList))
	assert.Equal(t, 0, len(stsList.Items), "paused cluster must not create a StatefulSet")
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

func reconcileN(t *testing.T, r *ConsulClusterReconciler, name, namespace string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := r.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: namespace},
		})
		require.NoError(t, err)
	}
}

func newTestReconciler(t *testing.T, objs ...client.Object) (*ConsulClusterReconciler, client.Client) {
	t.Helper()
	s := testScheme(t)
	statusObjs := make([]client.Object, 0, len(objs))
	for _, o := range objs {
		if _, ok := o.(*v1alpha1.ConsulCluster); ok {
			statusObjs = append(statusObjs, o)
		}
	}
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(statusObjs...).
		Build()
	return &ConsulClusterReconciler{
		Client: fakeClient,
		Log:    ctrl.Log.WithName("test"),
		Scheme: s,
	}, fakeClient
}

func getSTS(t *testing.T, c client.Client, name, namespace string) *appsv1.StatefulSet {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: name, Namespace: namespace}, sts))
	return sts
}

func TestReconcile_CreatesStatefulSet(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	r, c := newTestReconciler(t, cluster)
	reconcileN(t, r, "consul", "default", 3)

	sts := getSTS(t, c, "consul-server", "default")
	require.NotNil(t, sts.Spec.Replicas)
	assert.Equal(t, int32(3), *sts.Spec.Replicas)
	assert.Equal(t, "consul-server", sts.Spec.ServiceName,
		"must match the headless service so pods get stable DNS names")
	assert.Equal(t, appsv1.ParallelPodManagement, sts.Spec.PodManagementPolicy)

	require.Len(t, sts.Spec.VolumeClaimTemplates, 1)
	assert.Equal(t, "data", sts.Spec.VolumeClaimTemplates[0].Name,
		"claim template name determines the PVC name each ordinal reattaches to")
}

func TestReconcile_ScalesStatefulSetInsteadOfCreatingPods(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	r, c := newTestReconciler(t, cluster)
	reconcileN(t, r, "consul", "default", 2)

	// The operator must not create pods itself; that is the StatefulSet
	// controller's job, and it is what keeps volumes attached to identities.
	podList := &corev1.PodList{}
	require.NoError(t, c.List(context.Background(), podList))
	assert.Empty(t, podList.Items)

	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: "consul", Namespace: "default"}, cluster))
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Spec.Size = 5
	require.NoError(t, c.Patch(context.Background(), cluster, patch))

	reconcileN(t, r, "consul", "default", 1)

	sts := getSTS(t, c, "consul-server", "default")
	assert.Equal(t, int32(5), *sts.Spec.Replicas)
}

func TestEnsureStatefulSet_FreezesRolloutOnTemplateChange(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	r, c := newTestReconciler(t, cluster)
	reconcileN(t, r, "consul", "default", 2)

	sts := getSTS(t, c, "consul-server", "default")
	assert.Equal(t, int32(0), currentPartition(sts, 3),
		"nothing to roll on first create")

	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: "consul", Namespace: "default"}, cluster))
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Spec.Version = "1.19.0"
	require.NoError(t, c.Patch(context.Background(), cluster, patch))

	// A single reconcile writes the new template. The partition must be raised
	// in the same step, before the StatefulSet controller can start rolling on
	// pod Readiness alone.
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "consul", Namespace: "default"},
	})
	require.NoError(t, err)

	sts = getSTS(t, c, "consul-server", "default")
	assert.Equal(t, int32(3), currentPartition(sts, 3),
		"rollout must be frozen until the raft health gate allows a step")
	assert.Contains(t, sts.Spec.Template.Spec.Containers[0].Image, "1.19.0")
}

func TestEnsureStatefulSet_ConfigChangeRollsServers(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	r, c := newTestReconciler(t, cluster)
	reconcileN(t, r, "consul", "default", 2)

	before := getSTS(t, c, "consul-server", "default")
	beforeHash := before.Annotations[templateHashAnnotation]

	require.NoError(t, c.Get(context.Background(),
		types.NamespacedName{Name: "consul", Namespace: "default"}, cluster))
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Spec.LogLevel = "debug"
	require.NoError(t, c.Patch(context.Background(), cluster, patch))

	reconcileN(t, r, "consul", "default", 1)

	after := getSTS(t, c, "consul-server", "default")
	assert.NotEqual(t, beforeHash, after.Annotations[templateHashAnnotation],
		"a config-only change must still produce a new revision, because Consul "+
			"cannot reload every setting at runtime")
	assert.NotEmpty(t, after.Spec.Template.Annotations[configChecksumAnnotation])
}

func TestReconcile_IsIdempotent(t *testing.T) {
	t.Parallel()

	cluster := minimalCluster("consul", "default", 3)
	r, c := newTestReconciler(t, cluster)
	reconcileN(t, r, "consul", "default", 2)

	first := getSTS(t, c, "consul-server", "default")
	firstVersion := first.ResourceVersion

	reconcileN(t, r, "consul", "default", 3)

	second := getSTS(t, c, "consul-server", "default")
	assert.Equal(t, firstVersion, second.ResourceVersion,
		"a converged cluster must not be written on every reconcile")
}

func TestCurrentPartition(t *testing.T) {
	t.Parallel()

	partitionOf := func(p *int32) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
					RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{Partition: p},
				},
			},
		}
	}
	two, six := int32(2), int32(6)

	cases := map[string]struct {
		sts      *appsv1.StatefulSet
		replicas int32
		expected int32
	}{
		"unset partition means roll everything": {
			sts:      &appsv1.StatefulSet{},
			replicas: 3,
			expected: 0,
		},
		"explicit partition is honoured": {
			sts:      partitionOf(&two),
			replicas: 3,
			expected: 2,
		},
		"partition above replicas is clamped after a scale-down": {
			sts:      partitionOf(&six),
			replicas: 3,
			expected: 3,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, currentPartition(tc.sts, tc.replicas))
		})
	}
}

func TestRaftCanTolerateFailure_SingleServer(t *testing.T) {
	t.Parallel()

	// A one-server cluster has no failure tolerance by definition, so gating on
	// it would deadlock every rollout.
	cluster := minimalCluster("consul", "default", 1)
	r, _ := newTestReconciler(t, cluster)

	tolerant, err := r.raftCanTolerateFailure(context.Background(), cluster)
	require.NoError(t, err)
	assert.True(t, tolerant)
}

func TestServerMaxUnavailable(t *testing.T) {
	t.Parallel()

	three := 3
	cases := map[string]struct {
		size     int
		override *int
		expected int
	}{
		"single server may never be voluntarily evicted": {size: 1, expected: 0},
		"multi-server defaults to one":                   {size: 3, expected: 1},
		"explicit override wins":                         {size: 5, override: &three, expected: 3},
		"override is ignored for a single server":        {size: 1, override: &three, expected: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cluster := minimalCluster("consul", "default", tc.size)
			cluster.Spec.PodDisruptionBudget = &v1alpha1.ConsulPodDisruptionBudgetSpec{
				Enabled:        true,
				MaxUnavailable: tc.override,
			}
			assert.Equal(t, tc.expected, serverMaxUnavailable(cluster))
		})
	}
}

func TestBuildServerPodTemplate_TerminationGracePeriod(t *testing.T) {
	t.Parallel()

	template := buildServerPodTemplate(minimalCluster("consul", "default", 3), "abc")
	require.NotNil(t, template.Spec.TerminationGracePeriodSeconds)
	assert.Equal(t, int64(60), *template.Spec.TerminationGracePeriodSeconds)
}

func TestBuildServerPodTemplate_PreStopLeaveHook(t *testing.T) {
	t.Parallel()

	template := buildServerPodTemplate(minimalCluster("consul", "default", 3), "abc")
	require.Len(t, template.Spec.Containers, 1)
	lc := template.Spec.Containers[0].Lifecycle
	require.NotNil(t, lc)
	require.NotNil(t, lc.PreStop)
	require.NotNil(t, lc.PreStop.Exec)
	assert.Contains(t, lc.PreStop.Exec.Command, "consul leave")
}

func TestBuildServerPodTemplate_WritableTmpUnderReadOnlyRoot(t *testing.T) {
	t.Parallel()

	template := buildServerPodTemplate(minimalCluster("consul", "default", 3), "abc")
	container := template.Spec.Containers[0]

	require.NotNil(t, container.SecurityContext.ReadOnlyRootFilesystem)
	require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)

	var mounted bool
	for _, m := range container.VolumeMounts {
		if m.MountPath == "/tmp" {
			mounted = true
		}
	}
	assert.True(t, mounted, "consul needs a writable /tmp when the root filesystem is read-only")
}

func TestBuildServerPodTemplate_AdvertiseAddressFollowsExposedPorts(t *testing.T) {
	t.Parallel()

	advertiseField := func(template corev1.PodTemplateSpec) string {
		for _, e := range template.Spec.Containers[0].Env {
			if e.Name == "ADVERTISE_IP" {
				return e.ValueFrom.FieldRef.FieldPath
			}
		}
		return ""
	}

	cluster := minimalCluster("consul", "default", 3)
	assert.Equal(t, "status.podIP", advertiseField(buildServerPodTemplate(cluster, "abc")))

	// With gossip and RPC published as hostPorts the servers are reachable at
	// the node address, so advertising the pod IP would make them unreachable
	// to the external client agents the flag exists to serve.
	cluster.Spec.ExposeGossipAndRPCPorts = true
	assert.Equal(t, "status.hostIP", advertiseField(buildServerPodTemplate(cluster, "abc")))
}

func TestBuildReadinessProbe_RequiresALeader(t *testing.T) {
	t.Parallel()

	// /v1/status/leader answers 200 with an empty body during an election, so an
	// HTTP probe would report a leaderless server as Ready.
	probe := buildReadinessProbe(minimalCluster("consul", "default", 3))
	require.Nil(t, probe.HTTPGet)
	require.NotNil(t, probe.Exec)
	assert.Contains(t, probe.Exec.Command[2], `grep -E '".+"'`)
}

func TestConsulDomain_DefaultsToConsulNotClusterLocal(t *testing.T) {
	t.Parallel()

	// Every *.service.consul lookup depends on this default.
	assert.Equal(t, "consul", consulDomain(minimalCluster("consul", "default", 3)))

	cluster := minimalCluster("consul", "default", 3)
	cluster.Spec.Domain = "mesh"
	assert.Equal(t, "mesh", consulDomain(cluster))
}

func TestBuildServerConfigJSON_Domain(t *testing.T) {
	t.Parallel()

	data, err := buildServerConfigJSON(minimalCluster("consul", "default", 3))
	require.NoError(t, err)
	assert.Contains(t, data, `"domain": "consul"`)
	assert.Contains(t, data, `"auto_reload_config": true`)
}

func TestApiServicePorts_HTTPSOnlyDropsPlaintext(t *testing.T) {
	t.Parallel()

	names := func(cluster *v1alpha1.ConsulCluster) []string {
		var out []string
		for _, p := range apiServicePorts(cluster) {
			out = append(out, p.Name)
		}
		return out
	}

	cluster := minimalCluster("consul", "default", 3)
	assert.Equal(t, []string{"http"}, names(cluster))

	cluster.Spec.TLS = &v1alpha1.ConsulTLSSpec{Enabled: true}
	assert.Equal(t, []string{"http", "https"}, names(cluster))

	// Publishing a port the agent has disabled leaves an endpoint that refuses
	// every connection.
	cluster.Spec.TLS.HTTPSOnly = true
	assert.Equal(t, []string{"https"}, names(cluster))
}
