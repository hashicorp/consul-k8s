package deployer

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestGatewayDeployment(t *testing.T) {
	cases := map[string]struct {
		gateway            Gateway
		expectedDeployment *appsv1.Deployment
	}{}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			deployment := tc.gateway.Deployment()
			require.Equal(t, tc.expectedDeployment, deployment)
		})
	}
}

func TestGatewayService(t *testing.T) {
	cases := map[string]struct {
		gateway         Gateway
		expectedService *corev1.Service
	}{}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			service := tc.gateway.Service()
			require.Equal(t, tc.expectedService, service)
		})
	}
}
