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

func TestAgentConfigReconcile(t *testing.T) {
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
			name: "new enabled resource gets finalizer and Ready=True",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{enabledAgentConfig("consul-ai-agent")}
			},
			requeue: true,
		},
		{
			name: "disabled resource gets Ready=False",
			k8sObjects: func() []runtime.Object {
				ac := enabledAgentConfig("consul-ai-agent")
				ac.Spec.Enabled = false
				ac.Finalizers = []string{agentConfigFinalizer}
				return []runtime.Object{ac}
			},
		},
		{
			name: "resource marked for deletion — finalizer is removed",
			k8sObjects: func() []runtime.Object {
				ac := enabledAgentConfig("consul-ai-agent")
				ac.ObjectMeta.DeletionTimestamp = &deletionTimestamp
				ac.ObjectMeta.Finalizers = []string{agentConfigFinalizer}
				return []runtime.Object{ac}
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
				WithStatusSubresource(&v1alpha1.AgentConfig{}).
				Build()

			controller := &AgentConfigController{
				Client:   fakeClient,
				Log:      logrtest.New(t),
				Recorder: record.NewFakeRecorder(10),
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: "consul-ai-agent"},
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

// TestAgentConfigReconcile_Finalizer verifies the finalizer lifecycle.
func TestAgentConfigReconcile_Finalizer(t *testing.T) {
	t.Parallel()

	t.Run("finalizer is added on first reconcile", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		ac := enabledAgentConfig("consul-ai-agent")
		require.Empty(t, ac.Finalizers)

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(ac).
			WithStatusSubresource(&v1alpha1.AgentConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &AgentConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		resp, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-agent"},
		})
		require.NoError(t, err)
		require.True(t, resp.Requeue, "should requeue after adding finalizer")

		got := &v1alpha1.AgentConfig{}
		require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul-ai-agent"}, got))
		require.Contains(t, got.Finalizers, agentConfigFinalizer)

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerAdded)
	})

	t.Run("finalizer is idempotent on subsequent reconciles", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		ac := enabledAgentConfig("consul-ai-agent")
		ac.Finalizers = []string{agentConfigFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(ac).
			WithStatusSubresource(&v1alpha1.AgentConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &AgentConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-agent"},
		})
		require.NoError(t, err)

		got := &v1alpha1.AgentConfig{}
		require.NoError(t, fakeClient.Get(context.Background(), types.NamespacedName{Name: "consul-ai-agent"}, got))
		count := 0
		for _, f := range got.Finalizers {
			if f == agentConfigFinalizer {
				count++
			}
		}
		require.Equal(t, 1, count, "finalizer must appear exactly once")

		// Synced event emitted, no FinalizerAdded event.
		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonSynced)
	})

	t.Run("finalizer is removed on deletion", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		ts := metav1.Now()
		ac := enabledAgentConfig("consul-ai-agent")
		ac.Finalizers = []string{agentConfigFinalizer}
		ac.DeletionTimestamp = &ts

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(ac).
			WithStatusSubresource(&v1alpha1.AgentConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &AgentConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-agent"},
		})
		require.NoError(t, err)

		require.Len(t, recorder.Events, 1)
		require.Contains(t, <-recorder.Events, eventReasonFinalizerRemoved)
	})
}

// TestAgentConfigReconcile_Events verifies the Synced and SyncFailed events.
func TestAgentConfigReconcile_Events(t *testing.T) {
	t.Parallel()

	t.Run("successful reconcile emits Synced event", func(t *testing.T) {
		s := runtime.NewScheme()
		require.NoError(t, clientgoscheme.AddToScheme(s))
		require.NoError(t, v1alpha1.AddToScheme(s))

		ac := enabledAgentConfig("consul-ai-agent")
		ac.Finalizers = []string{agentConfigFinalizer}

		fakeClient := fake.NewClientBuilder().
			WithScheme(s).WithRuntimeObjects(ac).
			WithStatusSubresource(&v1alpha1.AgentConfig{}).Build()

		recorder := record.NewFakeRecorder(10)
		controller := &AgentConfigController{Client: fakeClient, Log: logrtest.New(t), Recorder: recorder}

		_, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "consul-ai-agent"},
		})
		require.NoError(t, err)

		require.Len(t, recorder.Events, 1)
		event := <-recorder.Events
		require.Contains(t, event, string(corev1.EventTypeNormal))
		require.Contains(t, event, eventReasonSynced)
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func enabledAgentConfig(name string) *v1alpha1.AgentConfig {
	return &v1alpha1.AgentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.AgentConfigSpec{
			Enabled: true,
			Defaults: v1alpha1.AgentDefaults{
				InterceptorPort: 21101,
				McpPort:         15101,
				HITL: v1alpha1.AgentHITL{
					Port:            16101,
					ApprovalTimeout: "60s",
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("128Mi"),
						corev1.ResourceCPU:    resource.MustParse("250m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
						corev1.ResourceCPU:    resource.MustParse("500m"),
					},
				},
			},
		},
	}
}
