// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestService_ExternalTrafficPolicy(t *testing.T) {
	gateway := gwv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "gw", Namespace: "default"}}
	serviceType := corev1.ServiceTypeLoadBalancer

	t.Run("unset by default", func(t *testing.T) {
		gcc := v1alpha1.GatewayClassConfig{Spec: v1alpha1.GatewayClassConfigSpec{ServiceType: &serviceType}}
		svc := (&Gatekeeper{}).service(gateway, gcc)
		require.Empty(t, svc.Spec.ExternalTrafficPolicy)
	})

	t.Run("set from GatewayClassConfig", func(t *testing.T) {
		policy := corev1.ServiceExternalTrafficPolicyLocal
		gcc := v1alpha1.GatewayClassConfig{Spec: v1alpha1.GatewayClassConfigSpec{ServiceType: &serviceType, ExternalTrafficPolicy: &policy}}
		svc := (&Gatekeeper{}).service(gateway, gcc)
		require.Equal(t, corev1.ServiceExternalTrafficPolicyLocal, svc.Spec.ExternalTrafficPolicy)
	})

	// Kubernetes rejects ExternalTrafficPolicy on a ClusterIP service, so it
	// must never be set even if the GatewayClassConfig requests it.
	t.Run("omitted for ClusterIP services", func(t *testing.T) {
		clusterIP := corev1.ServiceTypeClusterIP
		policy := corev1.ServiceExternalTrafficPolicyLocal
		gcc := v1alpha1.GatewayClassConfig{Spec: v1alpha1.GatewayClassConfigSpec{ServiceType: &clusterIP, ExternalTrafficPolicy: &policy}}
		svc := (&Gatekeeper{}).service(gateway, gcc)
		require.Empty(t, svc.Spec.ExternalTrafficPolicy)
	})
}

// TestMergeServiceInto_HealthCheckNodePort guards against a real Kubernetes API
// server rejection: HealthCheckNodePort is auto-assigned by Kubernetes when
// ExternalTrafficPolicy is Local, and it cannot be changed once set. Since
// mergeServiceInto resets existing.Spec to our generated (zero-valued) desired
// spec on every reconcile, it must preserve the previously-assigned value for as
// long as the policy stays Local, or every subsequent reconcile would send an
// update the API server rejects.
func TestMergeServiceInto_HealthCheckNodePort(t *testing.T) {
	t.Run("preserved while ExternalTrafficPolicy stays Local", func(t *testing.T) {
		existing := &corev1.Service{Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			HealthCheckNodePort:   30123,
		}}
		desired := &corev1.Service{Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
		}}

		mergeServiceInto(existing, desired)

		require.EqualValues(t, 30123, existing.Spec.HealthCheckNodePort)
	})

	t.Run("wiped when policy no longer needs it", func(t *testing.T) {
		existing := &corev1.Service{Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			HealthCheckNodePort:   30123,
		}}
		desired := &corev1.Service{Spec: corev1.ServiceSpec{
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyCluster,
		}}

		mergeServiceInto(existing, desired)

		require.Zero(t, existing.Spec.HealthCheckNodePort)
	})
}
