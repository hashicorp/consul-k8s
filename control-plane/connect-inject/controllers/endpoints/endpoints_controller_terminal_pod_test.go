// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsPodTerminal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		phase    corev1.PodPhase
		expected bool
	}{
		{name: "failed", phase: corev1.PodFailed, expected: true},
		{name: "succeeded", phase: corev1.PodSucceeded, expected: true},
		{name: "running", phase: corev1.PodRunning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, isPodTerminal(corev1.Pod{
				Status: corev1.PodStatus{Phase: tc.phase},
			}))
		})
	}
}

func TestRequestsForTerminalPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "terminal-pod", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	otherPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-pod"},
	}
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "service", Namespace: "default"},
		Subsets: []corev1.EndpointSubset{{
			Addresses: []corev1.EndpointAddress{{
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: pod.Name},
			}},
			NotReadyAddresses: []corev1.EndpointAddress{{
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: pod.Name},
			}, {
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: otherPod.Name},
			}},
		}},
	}
	controller := Controller{
		Client: fake.NewClientBuilder().WithRuntimeObjects(endpoints).Build(),
	}

	requests := controller.requestsForTerminalPod(context.Background(), pod)
	require.Len(t, requests, 1)
	require.Equal(t, "default", requests[0].Namespace)
	require.Equal(t, "service", requests[0].Name)

	pod.Status.Phase = corev1.PodRunning
	require.Empty(t, controller.requestsForTerminalPod(context.Background(), pod))
}
