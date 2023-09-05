// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package endpoints

import (
	"testing"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestIsConsulDataplaneSupported(t *testing.T) {
	versions := map[string]struct {
		expIsConsulDataplaneSupported bool
	}{
		"":                    {false},
		"v1.0.0":              {true},
		"1.0.0":               {true},
		"v0.49.0":             {false},
		"0.49.0-beta2":        {false},
		"0.49.2":              {false},
		"v1.0.0-beta1":        {true},
		"v1.0.0-beta3":        {true},
		"v1.1.0-beta1":        {true},
		"v1.0.0-dev":          {true},
		"v1.0.0-dev (abcdef)": {true},
		"v1.0.0-dev+abcdef":   {true},
		"invalid":             {true},
	}

	for version, c := range versions {
		t.Run(version, func(t *testing.T) {
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Namespace:   "default",
					Annotations: map[string]string{},
				},
			}
			if version != "" {
				pod.ObjectMeta.Annotations[constants.LegacyAnnotationConsulK8sVersion] = version
			}

			require.Equal(t, c.expIsConsulDataplaneSupported, isConsulDataplaneSupported(pod))
		})
	}
}

func TestConsulClientForNodeAgent(t *testing.T) {
	cases := map[string]struct {
		tls              bool
		autoEncrypt      bool
		enableNamespaces bool
	}{
		"no tls and auto-encrypt":      {},
		"with tls but no auto-encrypt": {tls: true},
		"with tls and auto-encrypt":    {tls: true, autoEncrypt: true},
		"with namespaces":              {enableNamespaces: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Create test Consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
				c.Connect["enabled"] = true
			})
			testClient.TestServer.WaitForActiveCARoot(t)
			if c.tls {
				testClient.Cfg.APIClientConfig.Scheme = "https"
			}

			ctrl := Controller{
				ConsulClientConfig:     testClient.Cfg,
				EnableConsulNamespaces: c.enableNamespaces,
				// We are only testing with mirroring enabled because other cases are tested elsewhere.
				EnableNSMirroring: true,
				EnableAutoEncrypt: c.autoEncrypt,
			}

			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-ns",
				},
				Status: corev1.PodStatus{
					HostIP: "1.2.3.4",
				},
			}

			ccCfg, err := ctrl.consulClientCfgForNodeAgent(testClient.APIClient, pod, discovery.State{Token: "test-token"})
			require.NoError(t, err)
			require.Equal(t, "test-token", ccCfg.Token)
			if c.tls {
				require.Equal(t, "https", ccCfg.Scheme)
				require.Equal(t, "1.2.3.4:8501", ccCfg.Address)
				require.Empty(t, ccCfg.TLSConfig.Address)
				if c.autoEncrypt {
					caRoots, _, err := testClient.APIClient.Agent().ConnectCARoots(nil)
					require.NoError(t, err)
					require.Equal(t, []byte(caRoots.Roots[0].RootCertPEM), ccCfg.TLSConfig.CAPem)
				}
			} else {
				require.Equal(t, ccCfg.Address, "1.2.3.4:8500")
			}

			if c.enableNamespaces {
				require.Equal(t, "test-ns", ccCfg.Namespace)
			}
		})
	}
}

func TestUpdateHealthCheckOnConsulClient(t *testing.T) {
	cases := map[string]struct {
		checks         []*api.AgentServiceCheck
		updateToStatus string
		expError       string
	}{
		"service with one existing kubernetes health check becoming unhealthy": {
			checks: []*api.AgentServiceCheck{
				{
					CheckID:                "default/test-pod-test-service/kubernetes-health-check",
					Name:                   "Kubernetes Health Check",
					TTL:                    "100000h",
					Status:                 api.HealthPassing,
					SuccessBeforePassing:   1,
					FailuresBeforeCritical: 1,
				},
			},
			updateToStatus: api.HealthCritical,
		},
		"service with one existing kubernetes health check becoming healthy": {
			checks: []*api.AgentServiceCheck{
				{
					CheckID:                "default/test-pod-test-service/kubernetes-health-check",
					Name:                   "Kubernetes Health Check",
					TTL:                    "100000h",
					Status:                 api.HealthCritical,
					SuccessBeforePassing:   1,
					FailuresBeforeCritical: 1,
				},
			},
			updateToStatus: api.HealthPassing,
		},
		"service without health check is a no-op": {
			checks:         nil,
			updateToStatus: api.HealthPassing,
		},
		"service with more than one existing kubernetes health check becoming healthy": {
			checks: []*api.AgentServiceCheck{
				{
					CheckID:                "default/test-pod-test-service/kubernetes-health-check",
					Name:                   "Kubernetes Health Check",
					TTL:                    "100000h",
					Status:                 api.HealthCritical,
					SuccessBeforePassing:   1,
					FailuresBeforeCritical: 1,
				},
				{
					CheckID:                "default/test-pod-test-service/kubernetes-health-check-2",
					Name:                   "Kubernetes Health Check",
					TTL:                    "100000h",
					Status:                 api.HealthPassing,
					SuccessBeforePassing:   1,
					FailuresBeforeCritical: 1,
				},
			},
			updateToStatus: api.HealthPassing,
			expError:       "more than one Kubernetes health check found",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient

			consulSvcs := []*api.AgentServiceRegistration{
				{
					ID:      "test-pod-test-service",
					Name:    "test-service",
					Port:    80,
					Address: "1.2.3.4",
					Checks:  c.checks,
				},
				{
					Kind:    api.ServiceKindConnectProxy,
					ID:      "test-pod-test-service-sidecar-proxy",
					Name:    "test-service-sidecar-proxy",
					Port:    20000,
					Address: "1.2.3.4",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "test-service",
						DestinationServiceID:   "test-pod-test-service",
					},
				},
			}

			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					PodIP: "1.2.3.4",
				},
			}
			endpoints := corev1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Subsets: []corev1.EndpointSubset{
					{
						NotReadyAddresses: []corev1.EndpointAddress{
							{
								IP: "1.2.3.4",
								TargetRef: &corev1.ObjectReference{
									Kind:      "Pod",
									Name:      "test-pod",
									Namespace: "default",
								},
							},
						},
					},
				},
			}

			for _, svc := range consulSvcs {
				err := consulClient.Agent().ServiceRegister(svc)
				require.NoError(t, err)
			}

			ctrl := Controller{
				ConsulClientConfig: testClient.Cfg,
				Log:                logrtest.New(t),
			}

			err := ctrl.updateHealthCheckOnConsulClient(testClient.Cfg.APIClientConfig, pod, endpoints, c.updateToStatus)
			if c.expError == "" {
				require.NoError(t, err)
				status, agentHealthInfo, err := consulClient.Agent().AgentHealthServiceByName("test-service")
				require.NoError(t, err)
				if c.checks != nil {
					require.NotEmpty(t, agentHealthInfo)
					require.Equal(t, c.updateToStatus, status)
				} else {
					require.Empty(t, agentHealthInfo[0].Checks)
				}
			} else {
				require.EqualError(t, err, c.expError)
			}
		})
	}

}
