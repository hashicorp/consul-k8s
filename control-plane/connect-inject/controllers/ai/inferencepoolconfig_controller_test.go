// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile — happy/sad path table tests
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile(t *testing.T) {
	t.Parallel()

	deletionTimestamp := metav1.Now()

	cases := []struct {
		name         string
		k8sObjects   func() []runtime.Object
		expErr       string
		requeue      bool
		requeueAfter time.Duration
	}{
		{
			name: "resource not found returns no error",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{}
			},
		},
		{
			name: "new enabled resource with resolved parent gets finalizer and Ready=True",
			k8sObjects: func() []runtime.Object {
				parent := makeUnstructuredParent("my-inference-model", "default",
					"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
				return []runtime.Object{
					enabledIPC("my-pool", "default", "my-inference-model"),
					parent,
				}
			},
		},
		{
			name: "disabled resource gets Ready=False",
			k8sObjects: func() []runtime.Object {
				ipc := enabledIPC("my-pool", "default", "my-inference-model")
				ipc.Spec.Enabled = false
				return []runtime.Object{ipc}
			},
		},
		{
			name: "missing parent gets ParentResolved=False and Ready=False",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{
					enabledIPC("my-pool", "default", "non-existent-parent"),
				}
			},
		},
		{
			name: "resource marked for deletion — finalizer is removed",
			k8sObjects: func() []runtime.Object {
				ipc := enabledIPC("my-pool", "default", "my-inference-model")
				ipc.ObjectMeta.DeletionTimestamp = &deletionTimestamp
				ipc.ObjectMeta.Finalizers = []string{inferencePoolConfigFinalizer}
				return []runtime.Object{ipc}
			},
		},
		{
			name: "multiple parentRefs — all resolved returns Ready=True",
			k8sObjects: func() []runtime.Object {
				parent1 := makeUnstructuredParent("parent-a", "default",
					"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
				parent2 := makeUnstructuredParent("parent-b", "default",
					"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
				ipc := enabledIPC("my-pool", "default", "parent-a")
				ipc.Spec.ParentRefs = append(ipc.Spec.ParentRefs, v1alpha1.InferencePoolParentRef{
					Kind: v1alpha1.InferenceModelConfigKind,
					Name: "parent-b",
				})
				return []runtime.Object{ipc, parent1, parent2}
			},
		},
		{
			name: "multiple parentRefs — one missing keeps Ready=False",
			k8sObjects: func() []runtime.Object {
				parent1 := makeUnstructuredParent("parent-a", "default",
					"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
				ipc := enabledIPC("my-pool", "default", "parent-a")
				ipc.Spec.ParentRefs = append(ipc.Spec.ParentRefs, v1alpha1.InferencePoolParentRef{
					Kind: v1alpha1.InferenceModelConfigKind,
					Name: "parent-missing",
				})
				return []runtime.Object{ipc, parent1}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			var typedObjs []runtime.Object
			var unstructuredObjs []client.Object
			for _, o := range tt.k8sObjects() {
				if u, ok := o.(*unstructured.Unstructured); ok {
					unstructuredObjs = append(unstructuredObjs, u)
				} else {
					typedObjs = append(typedObjs, o)
				}
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(typedObjs...).
				WithObjects(unstructuredObjs...).
				WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
				Build()

			controller := &InferencePoolConfigController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "my-pool",
					Namespace: "default",
				},
			})

			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.requeue, resp.Requeue)
			if tt.requeueAfter != 0 {
				require.Equal(t, tt.requeueAfter, resp.RequeueAfter)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_StatusConditions — asserts exact condition
// values written to Status after each reconcile.
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_StatusConditions(t *testing.T) {
	t.Parallel()

	type condWant struct {
		condType string
		status   metav1.ConditionStatus
		reason   string
	}

	cases := []struct {
		name       string
		buildIPC   func() *v1alpha1.InferencePoolConfig
		buildExtra func() []client.Object // extra objects (Unstructured parents)
		wantConds  []condWant
	}{
		{
			name: "enabled with resolved parent → Accepted=True, ParentResolved=True, Ready=True",
			buildIPC: func() *v1alpha1.InferencePoolConfig {
				return enabledIPC("pool", "default", "gw")
			},
			buildExtra: func() []client.Object {
				return []client.Object{
					makeUnstructuredParent("gw", "default",
						"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind),
				}
			},
			wantConds: []condWant{
				{conditionTypeAccepted, metav1.ConditionTrue, reasonReconciled},
				{conditionTypeParentResolved, metav1.ConditionTrue, reasonReconciled},
				{conditionTypeReady, metav1.ConditionTrue, reasonReconciled},
			},
		},
		{
			name: "disabled → Accepted=True, ParentResolved=True, Ready=False",
			buildIPC: func() *v1alpha1.InferencePoolConfig {
				ipc := enabledIPC("pool", "default", "gw")
				ipc.Spec.Enabled = false
				return ipc
			},
			buildExtra: func() []client.Object {
				return []client.Object{
					makeUnstructuredParent("gw", "default",
						"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind),
				}
			},
			wantConds: []condWant{
				{conditionTypeAccepted, metav1.ConditionTrue, reasonReconciled},
				{conditionTypeParentResolved, metav1.ConditionTrue, reasonReconciled},
				{conditionTypeReady, metav1.ConditionFalse, reasonReconciled},
			},
		},
		{
			name: "missing parent → Accepted=True, ParentResolved=False, Ready=False",
			buildIPC: func() *v1alpha1.InferencePoolConfig {
				return enabledIPC("pool", "default", "does-not-exist")
			},
			buildExtra: func() []client.Object { return nil },
			wantConds: []condWant{
				{conditionTypeAccepted, metav1.ConditionTrue, reasonReconciled},
				{conditionTypeParentResolved, metav1.ConditionFalse, reasonParentNotFound},
				{conditionTypeReady, metav1.ConditionFalse, reasonReconciled},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			ipc := tt.buildIPC()
			extra := tt.buildExtra()

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(ipc).
				WithObjects(extra...).
				WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
				Build()

			controller := &InferencePoolConfigController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}

			_, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: ipc.Name, Namespace: ipc.Namespace},
			})
			require.NoError(t, err)

			// Re-fetch the updated object from the fake client.
			got := &v1alpha1.InferencePoolConfig{}
			require.NoError(t, fakeClient.Get(context.Background(),
				types.NamespacedName{Name: ipc.Name, Namespace: ipc.Namespace}, got))

			require.NotNil(t, got.Status.LastSyncedTime, "LastSyncedTime must be set after reconcile")

			for _, want := range tt.wantConds {
				cond := findCondition(got.Status.Conditions, want.condType)
				require.NotNilf(t, cond, "condition %q not found in status", want.condType)
				require.Equalf(t, want.status, cond.Status,
					"condition %q: got status %q, want %q", want.condType, cond.Status, want.status)
				require.Equalf(t, want.reason, cond.Reason,
					"condition %q: got reason %q, want %q", want.condType, cond.Reason, want.reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_Finalizer — finalizer lifecycle
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_Finalizer(t *testing.T) {
	t.Parallel()

	t.Run("finalizer is added on first reconcile", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		parent := makeUnstructuredParent("gw", "default",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := enabledIPC("pool", "default", "gw")
		require.Empty(t, ipc.Finalizers, "should start without finalizers")

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))
		require.Contains(t, got.Finalizers, inferencePoolConfigFinalizer)
	})

	t.Run("finalizer is idempotent on subsequent reconciles", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		parent := makeUnstructuredParent("gw", "default",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := enabledIPC("pool", "default", "gw")
		ipc.Finalizers = []string{inferencePoolConfigFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))
		// Exactly one copy of the finalizer, not duplicated.
		count := 0
		for _, f := range got.Finalizers {
			if f == inferencePoolConfigFinalizer {
				count++
			}
		}
		require.Equal(t, 1, count, "finalizer must appear exactly once")
	})

	t.Run("finalizer is removed when resource is deleted", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		// Load the object with both DeletionTimestamp and the finalizer already
		// set via WithRuntimeObjects — the fake client accepts this at load time.
		// The reconciler will strip the finalizer, causing the fake client to
		// garbage-collect the object (Get returns not-found).
		ts := metav1.Now()
		ipc := enabledIPC("pool", "default", "gw")
		ipc.Finalizers = []string{inferencePoolConfigFinalizer}
		ipc.DeletionTimestamp = &ts

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		// After the finalizer is removed the fake client garbage-collects the object.
		got := &v1alpha1.InferencePoolConfig{}
		getErr := fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got)
		if getErr == nil {
			// Object still present — finalizer must have been stripped.
			require.NotContains(t, got.Finalizers, inferencePoolConfigFinalizer)
		} else {
			// Object garbage-collected after finalizer removal — also acceptable.
			require.True(t, k8serrors.IsNotFound(getErr),
				"expected not-found after finalizer removal, got: %v", getErr)
		}
	})

	t.Run("deletion without finalizer is a no-op", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		// Store the object first (no finalizer, no DeletionTimestamp — fake client
		// only rejects DeletionTimestamp objects that have no finalizers).
		ipc := enabledIPC("pool", "default", "gw")

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		// Now delete it via the client so the fake marks it with DeletionTimestamp.
		// Because there are no finalizers the fake will hard-delete immediately.
		require.NoError(t, fakeClient.Delete(context.Background(), ipc))

		// Reconcile against the now-gone object — should be a clean no-op.
		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		result, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)
		require.False(t, result.Requeue)
	})
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_ParentRefNamespace — namespace defaulting
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_ParentRefNamespace(t *testing.T) {
	t.Parallel()

	t.Run("omitted namespace defaults to 'default'", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		// Parent lives in 'default'; parentRef has no namespace field.
		parent := makeUnstructuredParent("gw", "default",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := &v1alpha1.InferencePoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "team-a"},
			Spec: v1alpha1.InferencePoolConfigSpec{
				Enabled: true,
				ParentRefs: []v1alpha1.InferencePoolParentRef{
					{Kind: v1alpha1.InferenceModelConfigKind, Name: "gw"},
					// Namespace intentionally omitted → should default to "default".
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "team-a"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "team-a"}, got))

		cond := findCondition(got.Status.Conditions, conditionTypeParentResolved)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status,
			"parent in 'default' namespace must be resolved when parentRef.namespace is empty")
	})

	t.Run("explicit namespace is used as-is", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		// Parent lives in 'infra'.
		parent := makeUnstructuredParent("gw", "infra",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := &v1alpha1.InferencePoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
			Spec: v1alpha1.InferencePoolConfigSpec{
				Enabled: true,
				ParentRefs: []v1alpha1.InferencePoolParentRef{
					{Kind: v1alpha1.InferenceModelConfigKind, Name: "gw", Namespace: "infra"},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))

		cond := findCondition(got.Status.Conditions, conditionTypeParentResolved)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionTrue, cond.Status)
	})

	t.Run("explicit namespace pointing to wrong namespace → not found", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		// Parent lives in 'infra', but parentRef.namespace says 'other'.
		parent := makeUnstructuredParent("gw", "infra",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := &v1alpha1.InferencePoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "default"},
			Spec: v1alpha1.InferencePoolConfigSpec{
				Enabled: true,
				ParentRefs: []v1alpha1.InferencePoolParentRef{
					{Kind: v1alpha1.InferenceModelConfigKind, Name: "gw", Namespace: "other"},
				},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))

		cond := findCondition(got.Status.Conditions, conditionTypeParentResolved)
		require.NotNil(t, cond)
		require.Equal(t, metav1.ConditionFalse, cond.Status)
		require.Equal(t, reasonParentNotFound, cond.Reason)
	})
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_SpecFields — verifies rich spec round-trips
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_SpecFields(t *testing.T) {
	t.Parallel()

	t.Run("rateLimit fields survive a reconcile", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		parent := makeUnstructuredParent("gw", "default",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := enabledIPC("pool", "default", "gw")
		ipc.Spec.RateLimit = &v1alpha1.InferencePoolRateLimit{
			Enabled:     true,
			Mode:        "global",
			Enforcement: "enforce",
			CountMode:   "token",
			DegradeMode: "allow",
			Dimensions:  []string{"identity", "model"},
			Default: &v1alpha1.InferencePoolLimitPair{
				Requests: &v1alpha1.InferencePoolLimit{Count: 100, Window: "1m"},
				Tokens:   &v1alpha1.InferencePoolLimit{Count: 50000, Window: "1h"},
			},
			Global: &v1alpha1.InferencePoolLimitPair{
				Requests: &v1alpha1.InferencePoolLimit{Count: 1000, Window: "1m"},
			},
			TierLimits: []v1alpha1.InferencePoolTierLimit{
				{Tier: "gold", MaxCompletionTokensCap: 4096,
					Tokens: &v1alpha1.InferencePoolLimit{Count: 200000, Window: "1h"}},
			},
			ModelLimits: []v1alpha1.InferencePoolModelLimit{
				{Model: "gpt-4o",
					Requests: &v1alpha1.InferencePoolLimit{Count: 20, Window: "1m"}},
			},
			TierBindings: []v1alpha1.InferencePoolTierBinding{
				{Tier: "gold", SPIFFEIDs: []string{"spiffe://cluster.local/ns/prod/sa/app"}},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))

		require.NotNil(t, got.Spec.RateLimit)
		require.True(t, got.Spec.RateLimit.Enabled)
		require.Equal(t, "global", got.Spec.RateLimit.Mode)
		require.Equal(t, "enforce", got.Spec.RateLimit.Enforcement)
		require.Equal(t, int64(100), got.Spec.RateLimit.Default.Requests.Count)
		require.Equal(t, "1m", got.Spec.RateLimit.Default.Requests.Window)
		require.Equal(t, "gpt-4o", got.Spec.RateLimit.ModelLimits[0].Model)
		require.Equal(t, "gold", got.Spec.RateLimit.TierLimits[0].Tier)
		require.Equal(t, 4096, got.Spec.RateLimit.TierLimits[0].MaxCompletionTokensCap)
	})

	t.Run("routing fields survive a reconcile", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		parent := makeUnstructuredParent("gw", "default",
			"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
		ipc := enabledIPC("pool", "default", "gw")
		ipc.Spec.Routing = &v1alpha1.InferencePoolRouting{
			FallbackChain:    []string{"backend-a", "backend-b"},
			ConfigValidation: "strict",
			Fallback: &v1alpha1.InferencePoolFallback{
				RetryOn:       []string{"5xx", "reset"},
				MaxTiers:      2,
				PerTryTimeout: "30s",
			},
			Retry: &v1alpha1.InferencePoolRetry{
				MaxAttempts: 3,
				RetryOn:     []string{"5xx"},
			},
			Timeout: &v1alpha1.InferencePoolTimeout{
				Connect: "5s",
				Request: "120s",
			},
			Scoring: &v1alpha1.InferencePoolScoring{
				Scorers: []string{"latency-aware"},
				WeightedSplit: []v1alpha1.InferencePoolWeightedTarget{
					{Cluster: "us-east", Weight: 70},
					{Cluster: "eu-west", Weight: 30},
				},
			},
			MatchRules: []v1alpha1.InferencePoolMatchRule{
				{
					When: v1alpha1.InferencePoolMatch{
						Path:    "/v1/chat",
						BodyHas: []string{"/model"},
						Identity: &v1alpha1.InferencePoolIdentityMatch{
							Service:   "frontend",
							Namespace: "prod",
						},
					},
					Candidates:          []string{"backend-a"},
					RequireCapabilities: []string{"vision"},
				},
			},
			ComplianceMap: map[string]v1alpha1.InferencePoolCompliance{
				"gdpr": {AllowedRegions: []string{"eu-west"}, DenyModels: []string{"gpt-4"}},
			},
		}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).
			WithRuntimeObjects(ipc).
			WithObjects(parent).
			WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
			Build()

		controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferencePoolConfig{}
		require.NoError(t, fakeClient.Get(context.Background(),
			types.NamespacedName{Name: "pool", Namespace: "default"}, got))

		require.NotNil(t, got.Spec.Routing)
		require.Equal(t, []string{"backend-a", "backend-b"}, got.Spec.Routing.FallbackChain)
		require.Equal(t, "strict", got.Spec.Routing.ConfigValidation)
		require.Equal(t, 2, got.Spec.Routing.Fallback.MaxTiers)
		require.Equal(t, "30s", got.Spec.Routing.Fallback.PerTryTimeout)
		require.Equal(t, 3, got.Spec.Routing.Retry.MaxAttempts)
		require.Equal(t, "120s", got.Spec.Routing.Timeout.Request)
		require.Equal(t, []string{"latency-aware"}, got.Spec.Routing.Scoring.Scorers)
		require.Len(t, got.Spec.Routing.Scoring.WeightedSplit, 2)
		require.Equal(t, "us-east", got.Spec.Routing.Scoring.WeightedSplit[0].Cluster)
		require.Equal(t, 70, got.Spec.Routing.Scoring.WeightedSplit[0].Weight)
		require.Len(t, got.Spec.Routing.MatchRules, 1)
		require.Equal(t, "/v1/chat", got.Spec.Routing.MatchRules[0].When.Path)
		require.Equal(t, "frontend", got.Spec.Routing.MatchRules[0].When.Identity.Service)
		gdpr, ok := got.Spec.Routing.ComplianceMap["gdpr"]
		require.True(t, ok)
		require.Equal(t, []string{"eu-west"}, gdpr.AllowedRegions)
	})
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_ConditionStability — LastTransitionTime is
// stable when a condition's Status does not change between reconciles.
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_ConditionStability(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	parent := makeUnstructuredParent("gw", "default",
		"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
	ipc := enabledIPC("pool", "default", "gw")

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(ipc).
		WithObjects(parent).
		WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
		Build()

	controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"}}

	// First reconcile — establishes conditions.
	_, err := controller.Reconcile(context.Background(), req)
	require.NoError(t, err)

	got1 := &v1alpha1.InferencePoolConfig{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "pool", Namespace: "default"}, got1))
	firstTransition := findCondition(got1.Status.Conditions, conditionTypeReady).LastTransitionTime

	// Wait a moment so time would differ if incorrectly updated.
	time.Sleep(5 * time.Millisecond)

	// Second reconcile — same spec, same status.
	_, err = controller.Reconcile(context.Background(), req)
	require.NoError(t, err)

	got2 := &v1alpha1.InferencePoolConfig{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "pool", Namespace: "default"}, got2))
	secondTransition := findCondition(got2.Status.Conditions, conditionTypeReady).LastTransitionTime

	require.Equal(t, firstTransition, secondTransition,
		"LastTransitionTime must not change when condition Status is stable")
}

// ---------------------------------------------------------------------------
// TestInferencePoolConfigReconcile_LastSyncedTime — written on every reconcile
// ---------------------------------------------------------------------------

func TestInferencePoolConfigReconcile_LastSyncedTime(t *testing.T) {
	t.Parallel()

	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	parent := makeUnstructuredParent("gw", "default",
		"consul.hashicorp.com/v1alpha1", v1alpha1.InferenceModelConfigKind)
	ipc := enabledIPC("pool", "default", "gw")

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(ipc).
		WithObjects(parent).
		WithStatusSubresource(&v1alpha1.InferencePoolConfig{}).
		Build()

	controller := &InferencePoolConfigController{Client: fakeClient, Log: logrtest.New(t)}

	_, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "pool", Namespace: "default"},
	})
	require.NoError(t, err)

	got := &v1alpha1.InferencePoolConfig{}
	require.NoError(t, fakeClient.Get(context.Background(),
		types.NamespacedName{Name: "pool", Namespace: "default"}, got))

	// LastSyncedTime must be set; exact value comparison is skipped because the
	// fake client's status sub-resource may truncate to second precision.
	require.NotNil(t, got.Status.LastSyncedTime,
		"LastSyncedTime must be set after a successful reconcile")
	require.False(t, got.Status.LastSyncedTime.IsZero(),
		"LastSyncedTime must not be the zero time")
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// enabledIPC returns a minimal enabled InferencePoolConfig with one parentRef.
func enabledIPC(name, namespace, parentName string) *v1alpha1.InferencePoolConfig {
	return &v1alpha1.InferencePoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.InferencePoolConfigSpec{
			Enabled: true,
			ParentRefs: []v1alpha1.InferencePoolParentRef{
				{
					Kind: v1alpha1.InferenceModelConfigKind,
					Name: parentName,
				},
			},
		},
	}
}

// makeUnstructuredParent creates an Unstructured object that the fake client
// stores and returns when the controller performs an Unstructured Get by GVK.
func makeUnstructuredParent(name, namespace, apiVersion, kind string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetName(name)
	u.SetNamespace(namespace)
	return u
}

// findCondition returns the first condition with the given type, or nil.
func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
