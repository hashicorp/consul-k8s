// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package ai

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
)

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
