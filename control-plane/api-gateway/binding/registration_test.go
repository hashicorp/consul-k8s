// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package binding

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestRegistrationsForPods_Health(t *testing.T) {
	t.Parallel()

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
				{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
				{Status: corev1.PodStatus{Phase: corev1.PodPending}},
				{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
				{Status: corev1.PodStatus{Phase: corev1.PodUnknown}},
				// Running statuses that don't show readiness
				{Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
				}}},
				{Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.PodInitialized, Status: corev1.ConditionTrue},
				}}},
				{Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.DisruptionTarget, Status: corev1.ConditionTrue},
				}}},
				{Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
				}}},
				// And finally, the successful check
				{Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				}}},
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
