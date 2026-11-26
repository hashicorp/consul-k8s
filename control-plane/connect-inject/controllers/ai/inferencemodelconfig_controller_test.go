// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

func TestInferenceModelConfigStatusWrites(t *testing.T) {
	testConfigStatusWrites(t, func() configStatusFixture {
		imc := enabledIMC("model")
		return configStatusFixture{
			object: imc, conditions: &imc.Status.Conditions, timestamp: &imc.Status.LastSyncedTime, enabled: &imc.Spec.Enabled,
			sync: func(ctx context.Context, c client.Client) error {
				controller := &InferenceModelConfigController{Client: c, Log: logrtest.New(t)}
				return controller.syncStatus(ctx, imc)
			},
		}
	})
}

type configStatusFixture struct {
	object     client.Object
	conditions *[]metav1.Condition
	timestamp  **metav1.Time
	enabled    *bool
	sync       func(context.Context, client.Client) error
}

// All three configuration controllers share the same Accepted/Ready status contract.
func testConfigStatusWrites(t *testing.T, newFixture func() configStatusFixture) {
	t.Helper()
	ctx := context.Background()
	oldTime := metav1.NewTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name          string
		startDisabled bool
		mutate        func(configStatusFixture)
		wantPatch     bool
		wantReady     bool
		failPatch     bool
	}{
		{name: "unchanged enabled", wantReady: true},
		{name: "unchanged disabled", startDisabled: true},
		{name: "disabled", wantPatch: true, mutate: func(f configStatusFixture) { *f.enabled = false }},
		{name: "re-enabled", startDisabled: true, wantPatch: true, wantReady: true,
			mutate: func(f configStatusFixture) { *f.enabled = true }},
		{name: "generation changed", wantPatch: true, wantReady: true,
			mutate: func(f configStatusFixture) { f.object.SetGeneration(f.object.GetGeneration() + 1) }},
		{name: "message drifted", wantPatch: true, wantReady: true,
			mutate: func(f configStatusFixture) { (*f.conditions)[0].Message = "stale" }},
		{name: "reason drifted", wantPatch: true, wantReady: true,
			mutate: func(f configStatusFixture) { (*f.conditions)[0].Reason = "Stale" }},
		{name: "missing timestamp", wantPatch: true, wantReady: true,
			mutate: func(f configStatusFixture) { *f.timestamp = nil }},
		{name: "patch failure and retry", wantPatch: true, wantReady: true, failPatch: true,
			mutate: func(f configStatusFixture) { f.object.SetGeneration(f.object.GetGeneration() + 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture()
			f.object.SetGeneration(1)
			*f.enabled = !tc.startDisabled
			s := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(s))
			patches := 0
			failPatch := false
			patchErr := errors.New("status patch failed")
			c := fake.NewClientBuilder().WithScheme(s).WithObjects(f.object).
				WithStatusSubresource(f.object).WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(ctx context.Context, c client.Client, subresource string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					require.Equal(t, "status", subresource)
					patches++
					if failPatch {
						return patchErr
					}
					return c.SubResource(subresource).Patch(ctx, obj, patch, opts...)
				},
			}).Build()
			key := client.ObjectKeyFromObject(f.object)
			require.NoError(t, f.sync(ctx, c))
			require.Equal(t, 1, patches, "first sync must initialize status")
			require.NoError(t, c.Get(ctx, key, f.object))
			require.NotNil(t, *f.timestamp)
			require.Len(t, *f.conditions, 2)
			acceptedMessage := (*f.conditions)[0].Message
			for i := range *f.conditions {
				(*f.conditions)[i].LastTransitionTime = oldTime
			}
			*f.conditions = append(*f.conditions, metav1.Condition{
				Type: "External", Status: metav1.ConditionTrue, Reason: "External",
				Message: "preserve", LastTransitionTime: oldTime,
			})
			*f.timestamp = &oldTime
			if tc.mutate != nil {
				tc.mutate(f)
			}
			conditions, timestamp := *f.conditions, *f.timestamp
			require.NoError(t, c.Update(ctx, f.object))
			*f.conditions, *f.timestamp = conditions, timestamp
			require.NoError(t, c.Status().Update(ctx, f.object))
			require.NoError(t, c.Get(ctx, key, f.object))
			before := f.object.DeepCopyObject()
			beforeConditions := append([]metav1.Condition(nil), (*f.conditions)...)
			patches = 0
			failPatch = tc.failPatch
			if tc.failPatch {
				require.ErrorIs(t, f.sync(ctx, c), patchErr)
				require.Equal(t, 1, patches)
				require.NoError(t, c.Get(ctx, key, f.object))
				require.Equal(t, before, f.object, "failed patch must not change persisted status")
				failPatch = false
				patches = 0
			}

			require.NoError(t, f.sync(ctx, c))
			require.NoError(t, c.Get(ctx, key, f.object))
			if tc.wantPatch {
				require.Equal(t, 1, patches)
				require.NotNil(t, *f.timestamp)
				require.True(t, (*f.timestamp).After(oldTime.Time))
			} else {
				require.Zero(t, patches)
				require.Equal(t, before, f.object, "status and resourceVersion must not change")
			}
			require.Len(t, *f.conditions, 3)
			require.Equal(t, beforeConditions[2], (*f.conditions)[2], "unrelated condition and ordering must be preserved")
			for _, condition := range (*f.conditions)[:2] {
				require.Equal(t, f.object.GetGeneration(), condition.ObservedGeneration)
				require.Equal(t, reasonReconciled, condition.Reason)
				previous := findCondition(beforeConditions, condition.Type)
				require.NotNil(t, previous)
				if condition.Status == previous.Status {
					require.True(t, oldTime.Equal(&condition.LastTransitionTime))
				} else {
					require.True(t, condition.LastTransitionTime.After(oldTime.Time))
				}
			}
			accepted := findCondition(*f.conditions, conditionTypeAccepted)
			require.NotNil(t, accepted)
			require.Equal(t, metav1.ConditionTrue, accepted.Status)
			require.Equal(t, acceptedMessage, accepted.Message)
			ready := findCondition(*f.conditions, conditionTypeReady)
			require.NotNil(t, ready)
			require.Equal(t, tc.wantReady, ready.Status == metav1.ConditionTrue)

			before = f.object.DeepCopyObject()
			patches = 0
			require.NoError(t, f.sync(ctx, c))
			require.Zero(t, patches, "status-update event must settle without another patch")
			require.NoError(t, c.Get(ctx, key, f.object))
			require.Equal(t, before, f.object)
		})
	}
}

func TestInferenceModelConfigReconcile(t *testing.T) {
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
			name: "new enabled resource gets finalizer and requeues",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{enabledIMC("consul-ai-gateway")}
			},
			requeue: true,
		},
		{
			name: "disabled resource with finalizer gets Ready=False",
			k8sObjects: func() []runtime.Object {
				imc := enabledIMC("consul-ai-gateway")
				imc.Spec.Enabled = false
				imc.Finalizers = []string{inferenceModelConfigFinalizer}
				return []runtime.Object{imc}
			},
		},
		{
			name: "resource marked for deletion — finalizer is removed",
			k8sObjects: func() []runtime.Object {
				imc := enabledIMC("consul-ai-gateway")
				imc.ObjectMeta.DeletionTimestamp = &deletionTimestamp
				imc.ObjectMeta.Finalizers = []string{inferenceModelConfigFinalizer}
				return []runtime.Object{imc}
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, clientgoscheme.AddToScheme(s))
			require.NoError(t, v1alpha1.AddToScheme(s))

			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.k8sObjects()...).
				WithStatusSubresource(&v1alpha1.InferenceModelConfig{}).
				Build()

			controller := &InferenceModelConfigController{
				Client:   fakeClient,
				Log:      logrtest.New(t),
				Recorder: record.NewFakeRecorder(10),
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "consul-ai-gateway"},
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

// TestInferenceModelConfigReconcile_Finalizer verifies the finalizer lifecycle.
func TestInferenceModelConfigReconcile_Finalizer(t *testing.T) {
	t.Parallel()

	t.Run("finalizer is added on first reconcile", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		imc := enabledIMC("consul-ai-gateway")
		require.Empty(t, imc.Finalizers)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(imc).
			WithStatusSubresource(&v1alpha1.InferenceModelConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &InferenceModelConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		resp, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-gateway"},
		})
		require.NoError(t, err)
		require.True(t, resp.Requeue, "should requeue after adding finalizer")

		got := &v1alpha1.InferenceModelConfig{}
		require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, got))
		require.Contains(t, got.Finalizers, inferenceModelConfigFinalizer)

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerAdded)
	})

	t.Run("finalizer is idempotent on subsequent reconciles", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		imc := enabledIMC("consul-ai-gateway")
		imc.Finalizers = []string{inferenceModelConfigFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(imc).
			WithStatusSubresource(&v1alpha1.InferenceModelConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &InferenceModelConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-gateway"},
		})
		require.NoError(t, err)

		got := &v1alpha1.InferenceModelConfig{}
		require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul-ai-gateway"}, got))
		count := 0
		for _, f := range got.Finalizers {
			if f == inferenceModelConfigFinalizer {
				count++
			}
		}
		require.Equal(t, 1, count, "finalizer must appear exactly once")

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonSynced)
	})

	t.Run("finalizer is removed on deletion", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		ts := metav1.Now()
		imc := enabledIMC("consul-ai-gateway")
		imc.Finalizers = []string{inferenceModelConfigFinalizer}
		imc.DeletionTimestamp = &ts

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(imc).
			WithStatusSubresource(&v1alpha1.InferenceModelConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &InferenceModelConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-gateway"},
		})
		require.NoError(t, err)

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerRemoved)
	})
}

// TestInferenceModelConfigReconcile_Events verifies Synced and Normal event type.
func TestInferenceModelConfigReconcile_Events(t *testing.T) {
	t.Parallel()

	t.Run("successful reconcile emits Synced event", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		imc := enabledIMC("consul-ai-gateway")
		imc.Finalizers = []string{inferenceModelConfigFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(imc).
			WithStatusSubresource(&v1alpha1.InferenceModelConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &InferenceModelConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-gateway"},
		})
		require.NoError(t, err)

		require.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		require.Contains(t, event, string(corev1.EventTypeNormal))
		require.Contains(t, event, eventReasonSynced)
	})
}

// TestMergeConditions verifies the condition merge helper preserves
// LastTransitionTime when Status is unchanged, and updates it when Status
// changes.
func TestMergeConditions(t *testing.T) {
	t.Parallel()

	originalTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))
	existingConditions := []metav1.Condition{
		{
			Type:               conditionTypeAccepted,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: originalTime,
			Reason:             reasonReconciled,
			Message:            "old",
		},
	}

	t.Run("same status preserves LastTransitionTime", func(t *testing.T) {
		newTime := metav1.Now()
		incoming := []metav1.Condition{
			{
				Type:               conditionTypeAccepted,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: newTime,
				Reason:             reasonReconciled,
				Message:            "updated message",
			},
		}
		result := mergeConditions(existingConditions, incoming)
		require.Len(t, result, 1)
		require.Equal(t, originalTime, result[0].LastTransitionTime, "should preserve original time when Status unchanged")
	})

	t.Run("status change updates LastTransitionTime", func(t *testing.T) {
		newTime := metav1.Now()
		incoming := []metav1.Condition{
			{
				Type:               conditionTypeAccepted,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: newTime,
				Reason:             reasonReconciled,
				Message:            "status flipped to False",
			},
		}
		result := mergeConditions(existingConditions, incoming)
		require.Len(t, result, 1)
		require.Equal(t, newTime, result[0].LastTransitionTime, "should use new time when Status changes")
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func enabledIMC(name string) *v1alpha1.InferenceModelConfig {
	return &v1alpha1.InferenceModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.InferenceModelConfigSpec{
			Enabled: true,
			Defaults: v1alpha1.InferenceModelDefaults{
				InterceptorPort:   21101,
				InferencePath:     "/v1",
				InferenceProtocol: "openai",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("512Mi"),
						corev1.ResourceCPU:    resource.MustParse("1000m"),
					},
				},
			},
		},
	}
}
