// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestProbesFromGateway_NoAnnotations(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.Nil(t, probes)
}

func TestProbesFromGateway_ValidHTTPGet(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"httpGet": {
						"path": "/ready",
						"port": 20000
					},
					"initialDelaySeconds": 10,
					"periodSeconds": 20
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes)
	require.NotNil(t, probes.Liveness)
	require.Equal(t, "/ready", probes.Liveness.HTTPGet.Path)
	require.Equal(t, intstr.FromInt(20000), probes.Liveness.HTTPGet.Port)
	require.Equal(t, int32(10), probes.Liveness.InitialDelaySeconds)
	require.Equal(t, int32(20), probes.Liveness.PeriodSeconds)
	require.Nil(t, probes.Readiness)
	require.Nil(t, probes.Startup)
}

func TestProbesFromGateway_ValidTCPSocket(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationReadinessProbe: `{
					"tcpSocket": {
						"port": 8080
					},
					"periodSeconds": 5
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes)
	require.NotNil(t, probes.Readiness)
	require.NotNil(t, probes.Readiness.TCPSocket)
	require.Equal(t, intstr.FromInt(8080), probes.Readiness.TCPSocket.Port)
	require.Nil(t, probes.Liveness)
	require.Nil(t, probes.Startup)
}

func TestProbesFromGateway_ValidExec(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationStartupProbe: `{
					"exec": {
						"command": ["cat", "/tmp/healthy"]
					},
					"failureThreshold": 30
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes)
	require.NotNil(t, probes.Startup)
	require.NotNil(t, probes.Startup.Exec)
	require.Equal(t, []string{"cat", "/tmp/healthy"}, probes.Startup.Exec.Command)
	require.Equal(t, int32(30), probes.Startup.FailureThreshold)
	require.Nil(t, probes.Liveness)
	require.Nil(t, probes.Readiness)
}

func TestProbesFromGateway_AllThreeProbes(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"httpGet": {"path": "/live", "port": 8080}
				}`,
				AnnotationReadinessProbe: `{
					"tcpSocket": {"port": 8080}
				}`,
				AnnotationStartupProbe: `{
					"exec": {"command": ["echo", "ready"]}
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes)
	require.NotNil(t, probes.Liveness)
	require.NotNil(t, probes.Readiness)
	require.NotNil(t, probes.Startup)
}

func TestProbesFromGateway_InvalidJSON(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{invalid json}`,
			},
		},
	}

	_, err := ProbesFromGateway(gateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid liveness probe JSON")
}

func TestProbesFromGateway_MultipleHandlers(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"httpGet": {"path": "/ready", "port": 20000},
					"tcpSocket": {"port": 20000}
				}`,
			},
		},
	}

	_, err := ProbesFromGateway(gateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exactly one handler")
}

func TestProbesFromGateway_NoHandler(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"initialDelaySeconds": 10
				}`,
			},
		},
	}

	_, err := ProbesFromGateway(gateway)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one handler")
}

func TestProbesFromGateway_SanitizeLivenessSuccessThreshold(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: `{
					"httpGet": {"path": "/ready", "port": 20000},
					"successThreshold": 5
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes.Liveness)
	require.Equal(t, int32(1), probes.Liveness.SuccessThreshold)
}

func TestProbesFromGateway_SanitizeStartupSuccessThreshold(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationStartupProbe: `{
					"tcpSocket": {"port": 20000},
					"successThreshold": 3
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes.Startup)
	require.Equal(t, int32(1), probes.Startup.SuccessThreshold)
}

func TestProbesFromGateway_ReadinessKeepsSuccessThreshold(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationReadinessProbe: `{
					"httpGet": {"path": "/ready", "port": 20000},
					"successThreshold": 3
				}`,
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.NotNil(t, probes.Readiness)
	require.Equal(t, int32(3), probes.Readiness.SuccessThreshold)
}

func TestProbesFromGateway_EmptyAnnotationValue(t *testing.T) {
	t.Parallel()

	gateway := &gwv1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-gateway",
			Annotations: map[string]string{
				AnnotationLivenessProbe: "",
			},
		},
	}

	probes, err := ProbesFromGateway(gateway)
	require.NoError(t, err)
	require.Nil(t, probes)
}
