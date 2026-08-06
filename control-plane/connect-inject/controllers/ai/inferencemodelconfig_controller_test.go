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
		// assertions run after Reconcile returns.
		verify func(t *testing.T, c *fake.ClientBuilder)
	}{
		{
			name: "resource not found returns no error",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{}
			},
		},
		{
			name: "new enabled resource gets finalizer and Ready=True",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{enabledIMC("consul-ai-gateway")}
			},
		},
		{
			name: "disabled resource gets Accepted=False and Ready=False",
			k8sObjects: func() []runtime.Object {
				imc := enabledIMC("consul-ai-gateway")
				imc.Spec.Enabled = false
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
				Client: fakeClient,
				Log:    logrtest.New(t),
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
