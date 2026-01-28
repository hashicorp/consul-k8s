// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestRegistrationsForPods_Health(t *testing.T) {
	t.Parallel()

	const (
		testNodeName = "node1"
		testPodIP    = "10.0.0.1"
		testHostIP   = "192.168.1.1"
	)

	for name, tt := range map[string]struct {
		consulNamespace string
		gateway         gwv1beta1.Gateway
		pods            []corev1.Pod
		expected        []string
	}{
		"empty": {
			consulNamespace: "",
			gateway:         gwv1beta1.Gateway{},
			pods:            []corev1.Pod{},
			expected:        []string{},
		},
		"mix": {
			consulNamespace: "",
			gateway:         gwv1beta1.Gateway{},
			pods: []corev1.Pod{
				// Pods without a running status
				{
					Spec:   corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{Phase: corev1.PodFailed, PodIP: testPodIP, HostIP: testHostIP},
				},
				{
					Spec:   corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{Phase: corev1.PodPending, PodIP: testPodIP, HostIP: testHostIP},
				},
				{
					Spec:   corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{Phase: corev1.PodSucceeded, PodIP: testPodIP, HostIP: testHostIP},
				},
				{
					Spec:   corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{Phase: corev1.PodUnknown, PodIP: testPodIP, HostIP: testHostIP},
				},
				// Running statuses that don't show readiness
				{
					Spec: corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						PodIP:  testPodIP,
						HostIP: testHostIP,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					Spec: corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						PodIP:  testPodIP,
						HostIP: testHostIP,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					Spec: corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						PodIP:  testPodIP,
						HostIP: testHostIP,
						Conditions: []corev1.PodCondition{
							{Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					Spec: corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						PodIP:  testPodIP,
						HostIP: testHostIP,
						Conditions: []corev1.PodCondition{
							{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
						},
					},
				},
				// And finally, the successful check
				{
					Spec: corev1.PodSpec{NodeName: testNodeName},
					Status: corev1.PodStatus{
						Phase:  corev1.PodRunning,
						PodIP:  testPodIP,
						HostIP: testHostIP,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expected: []string{
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthCritical,
				api.HealthPassing,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			registrations := registrationsForPods(common.MetricsConfig{}, tt.consulNamespace, tt.gateway, tt.pods)
			require.Len(t, registrations, len(tt.expected))

			for i := range registrations {
				registration := registrations[i]
				expected := tt.expected[i]

				require.EqualValues(t, "Kubernetes Readiness Check", registration.Check.Name)
				require.EqualValues(t, expected, registration.Check.Status)
			}
		})
	}
}

func TestRegistrationsForPods_SkipIncompleteNodeInfo(t *testing.T) {
	t.Parallel()

	gateway := gwv1beta1.Gateway{}

	testCases := []struct {
		name        string
		pods        []corev1.Pod
		expectedLen int
		description string
	}{
		{
			name: "skip pod without NodeName",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
					Spec: corev1.PodSpec{
						NodeName: "", // Missing NodeName
					},
					Status: corev1.PodStatus{
						PodIP:  "10.0.0.1",
						HostIP: "192.168.1.1",
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedLen: 0,
			description: "Pod without NodeName should be skipped",
		},
		{
			name: "skip pod without PodIP",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod2"},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						PodIP:  "", // Missing PodIP
						HostIP: "192.168.1.1",
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedLen: 0,
			description: "Pod without PodIP should be skipped",
		},
		{
			name: "skip pod without HostIP",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod3"},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						PodIP:  "10.0.0.1",
						HostIP: "", // Missing HostIP
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedLen: 0,
			description: "Pod without HostIP should be skipped",
		},
		{
			name: "include pod with complete node information",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "pod4"},
					Spec: corev1.PodSpec{
						NodeName: "node1",
					},
					Status: corev1.PodStatus{
						PodIP:  "10.0.0.1",
						HostIP: "192.168.1.1",
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedLen: 1,
			description: "Pod with complete node information should be included",
		},
		{
			name: "mixed pod states",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "incomplete1"},
					Spec:       corev1.PodSpec{NodeName: ""},
					Status:     corev1.PodStatus{PodIP: "10.0.0.1", HostIP: "192.168.1.1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "incomplete2"},
					Spec:       corev1.PodSpec{NodeName: "node1"},
					Status:     corev1.PodStatus{PodIP: "", HostIP: "192.168.1.1"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "complete1"},
					Spec:       corev1.PodSpec{NodeName: "node1"},
					Status: corev1.PodStatus{
						PodIP:  "10.0.0.1",
						HostIP: "192.168.1.1",
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "complete2"},
					Spec:       corev1.PodSpec{NodeName: "node2"},
					Status: corev1.PodStatus{
						PodIP:  "10.0.0.2",
						HostIP: "192.168.1.2",
						Phase:  corev1.PodRunning,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedLen: 2,
			description: "Only pods with complete node information should be included",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registrations := registrationsForPods(common.MetricsConfig{}, "", gateway, tc.pods)
			require.Len(t, registrations, tc.expectedLen, tc.description)
		})
	}
}
