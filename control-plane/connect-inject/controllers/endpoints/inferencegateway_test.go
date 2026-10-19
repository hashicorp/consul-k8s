// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

func TestInferenceGatewayRegistrationNamespace(t *testing.T) {
	for _, tc := range []struct {
		name, destination, prefix, annotation, expected string
		enabled, mirror                                 bool
	}{
		{name: "OSS ignores namespace annotation", annotation: "ignored"},
		{name: "default", enabled: true, destination: "default", expected: "default"},
		{name: "destination", enabled: true, destination: "shared", expected: "shared"},
		{name: "mirrored", enabled: true, mirror: true, expected: "ns-a"},
		{name: "mirrored prefix", enabled: true, mirror: true, prefix: "k8s-", expected: "k8s-ns-a"},
		{name: "explicit annotation", enabled: true, destination: "default", annotation: "gateway-ns", expected: "gateway-ns"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := &Controller{
				EnableConsulNamespaces: tc.enabled, ConsulDestinationNamespace: tc.destination,
				EnableNSMirroring: tc.mirror, NSMirroringPrefix: tc.prefix,
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-pod", Namespace: "ns-a",
					Annotations: map[string]string{
						constants.AnnotationGatewayKind:              inferenceGateway,
						constants.AnnotationGatewayConsulServiceName: "igw-identity",
						constants.AnnotationGatewayNamespace:         tc.annotation,
					},
				},
			}
			reg, err := controller.createGatewayRegistrations(pod, "10.0.0.1", corev1.Endpoints{}, capi.HealthPassing)
			require.NoError(t, err)
			require.Equal(t, tc.expected, reg.Service.Namespace)
			require.Equal(t, tc.expected, reg.Check.Namespace)
			require.Equal(t, "igw-identity", reg.Service.Service)
			require.Equal(t, capi.ServiceKindInferenceGateway, reg.Service.Kind)
			require.Equal(t, "ns-a/foo-pod", reg.Service.ID)
			require.Equal(t, reg.Service.ID, reg.Check.ServiceID)
			require.Equal(t, consulHealthCheckID(pod.Namespace, reg.Service.ID), reg.Check.CheckID)
			pod.Namespace = "ns-b"
			other, err := controller.createGatewayRegistrations(pod, "10.0.0.2", corev1.Endpoints{}, capi.HealthPassing)
			require.NoError(t, err)
			require.NotEqual(t, reg.Service.ID, other.Service.ID)
			require.NotEqual(t, reg.Check.CheckID, other.Check.CheckID)
		})
	}
}
