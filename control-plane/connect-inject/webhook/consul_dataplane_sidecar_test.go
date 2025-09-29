// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/metrics"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/agent/netutil"
)

const nodeName = "test-node"

func TestHandlerConsulDataplaneSidecar(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := map[string]struct {
		webhookSetupFunc     func(w *MeshWebhook)
		additionalExpCmdArgs string
	}{
		"default": {
			webhookSetupFunc:     nil,
			additionalExpCmdArgs: " -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with custom gRPC port": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.ConsulConfig.GRPCPort = 8602
			},
			additionalExpCmdArgs: " -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs and namespace mirroring": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-login-namespace=default -service-namespace=k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs and single destination namespace": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
				w.EnableNamespaces = true
				w.ConsulDestinationNamespace = "test-ns"
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-login-namespace=test-ns -service-namespace=test-ns -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs and partitions": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
				w.ConsulPartition = "test-part"
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-login-partition=test-part -service-partition=test-part -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with TLS and CA cert provided": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.TLSEnabled = true
				w.ConsulTLSServerName = "server.dc1.consul"
				w.ConsulCACert = "consul-ca-cert"
			},
			additionalExpCmdArgs: " -tls-server-name=server.dc1.consul -ca-certs=/consul/connect-inject/consul-ca.pem -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with TLS and no CA cert provided": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.TLSEnabled = true
				w.ConsulTLSServerName = "server.dc1.consul"
			},
			additionalExpCmdArgs: " -tls-server-name=server.dc1.consul -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with single destination namespace": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.ConsulDestinationNamespace = "consul-namespace"
			},
			additionalExpCmdArgs: " -service-namespace=consul-namespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with namespace mirroring": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
			},
			additionalExpCmdArgs: " -service-namespace=k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with namespace mirroring prefix": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
				w.K8SNSMirroringPrefix = "foo-"
			},
			additionalExpCmdArgs: " -service-namespace=foo-k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with partitions": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.ConsulPartition = "partition-1"
			},
			additionalExpCmdArgs: " -service-partition=partition-1 -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with different log level": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.LogLevel = "debug"
			},
			additionalExpCmdArgs: " -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with different log level and log json": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.LogLevel = "debug"
				w.LogJSON = true
			},
			additionalExpCmdArgs: " -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"skip server watch enabled": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.SkipServerWatch = true
			},
			additionalExpCmdArgs: " -server-watch-disabled=true -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"custom prometheus scrape path": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.MetricsConfig.DefaultPrometheusScrapePath = "/scrape-path" // Simulate what would be passed as a flag
			},
			additionalExpCmdArgs: " -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/scrape-path",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := &MeshWebhook{
				ConsulAddress: "1.1.1.1",
				ConsulConfig:  &consul.Config{GRPCPort: 8502},
				LogLevel:      "info",
				LogJSON:       false,
			}
			if c.webhookSetupFunc != nil {
				c.webhookSetupFunc(w)
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						constants.AnnotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
						{
							Name: "web-side",
						},
						{
							Name: "auth-method-secret",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "service-account-secret",
									MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
								},
							},
						},
					},
					ServiceAccountName: "web",
					NodeName:           nodeName,
				},
			}

			container, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			expCmd := "-addresses 1.1.1.1 -envoy-admin-bind-address=127.0.0.1 -consul-dns-bind-addr=127.0.0.1 -xds-bind-addr=127.0.0.1 -grpc-port=" + strconv.Itoa(w.ConsulConfig.GRPCPort) +
				" -proxy-service-id-path=/consul/connect-inject/proxyid " +
				"-log-level=" + w.LogLevel + " -log-json=" + strconv.FormatBool(w.LogJSON) + " -envoy-concurrency=0" + " -graceful-addr=127.0.0.1" + c.additionalExpCmdArgs
			require.Equal(t, expCmd, strings.Join(container.Args, " "))

			if w.AuthMethod != "" {
				require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
					{
						Name:      volumeName,
						MountPath: "/consul/connect-inject",
					},
					{
						Name:      "service-account-secret",
						MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
					},
				})
			} else {
				require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
					{
						Name:      volumeName,
						MountPath: "/consul/connect-inject",
					},
				})
			}

			expectedProbe := &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.FromInt(constants.ProxyDefaultInboundPort),
					},
				},
				InitialDelaySeconds: 1,
			}
			require.Equal(t, expectedProbe, container.ReadinessProbe)
			require.Nil(t, container.StartupProbe)
			require.Len(t, container.Env, 10)
			require.Equal(t, container.Env[0].Name, "TMPDIR")
			require.Equal(t, container.Env[0].Value, "/consul/connect-inject")
			require.Equal(t, container.Env[2].Name, "DP_SERVICE_NODE_NAME")
			require.Equal(t, container.Env[2].Value, "$(NODE_NAME)-virtual")
			require.Equal(t, container.Env[3].Name, "POD_NAME")
			require.Equal(t, container.Env[4].Name, "POD_NAMESPACE")
			require.Equal(t, container.Env[5].Name, "POD_UID")
			require.Equal(t, container.Env[6].Name, "DP_CREDENTIAL_LOGIN_META")
			require.Equal(t, container.Env[6].Value, "pod=$(POD_NAMESPACE)/$(POD_NAME)")
			require.Equal(t, container.Env[7].Name, "DP_CREDENTIAL_LOGIN_META1")
			require.Equal(t, container.Env[7].Value, "pod=$(POD_NAMESPACE)/$(POD_NAME)")
			require.Equal(t, container.Env[8].Name, "DP_CREDENTIAL_LOGIN_META2")
			require.Equal(t, container.Env[8].Value, "pod-uid=$(POD_UID)")
			require.Equal(t, container.Env[9].Name, "HOST_IP")
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Concurrency(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := map[string]struct {
		annotations map[string]string
		expFlags    string
		expErr      string
	}{
		"default settings, no annotations": {
			annotations: map[string]string{
				constants.AnnotationService: "foo",
			},
			expFlags: "-envoy-concurrency=0",
		},
		"default settings, annotation override": {
			annotations: map[string]string{
				constants.AnnotationService:               "foo",
				constants.AnnotationEnvoyProxyConcurrency: "42",
			},
			expFlags: "-envoy-concurrency=42",
		},
		"default settings, invalid concurrency annotation negative number": {
			annotations: map[string]string{
				constants.AnnotationService:               "foo",
				constants.AnnotationEnvoyProxyConcurrency: "-42",
			},
			expErr: "unable to parse annotation \"consul.hashicorp.com/consul-envoy-proxy-concurrency\": strconv.ParseUint: parsing \"-42\": invalid syntax",
		},
		"default settings, not-parseable concurrency annotation": {
			annotations: map[string]string{
				constants.AnnotationService:               "foo",
				constants.AnnotationEnvoyProxyConcurrency: "not-int",
			},
			expErr: "unable to parse annotation \"consul.hashicorp.com/consul-envoy-proxy-concurrency\": strconv.ParseUint: parsing \"not-int\": invalid syntax",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := MeshWebhook{
				ConsulConfig: &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := h.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			if c.expErr != "" {
				require.EqualError(t, err, c.expErr)
			} else {
				require.NoError(t, err)
				require.Contains(t, strings.Join(container.Args, " "), c.expFlags)
			}
		})
	}
}

// Test that we pass the dns proxy flag to dataplane correctly.
func TestHandlerConsulDataplaneSidecar_DNSProxy(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	// We only want the flag passed when DNS and tproxy are both enabled. DNS/tproxy can
	// both be enabled/disabled with annotations/labels on the pod and namespace and then globally
	// through the helm chart. To test this we use an outer loop with the possible DNS settings and then
	// and inner loop with possible tproxy settings.
	dnsCases := []struct {
		GlobalConsulDNS bool
		NamespaceDNS    *bool
		PodDNS          *bool
		ExpEnabled      bool
	}{
		{
			GlobalConsulDNS: false,
			ExpEnabled:      false,
		},
		{
			GlobalConsulDNS: true,
			ExpEnabled:      true,
		},
		{
			GlobalConsulDNS: false,
			NamespaceDNS:    boolPtr(true),
			ExpEnabled:      true,
		},
		{
			GlobalConsulDNS: false,
			PodDNS:          boolPtr(true),
			ExpEnabled:      true,
		},
	}
	tproxyCases := []struct {
		GlobalTProxy    bool
		NamespaceTProxy *bool
		PodTProxy       *bool
		ExpEnabled      bool
	}{
		{
			GlobalTProxy: false,
			ExpEnabled:   false,
		},
		{
			GlobalTProxy: true,
			ExpEnabled:   true,
		},
		{
			GlobalTProxy:    false,
			NamespaceTProxy: boolPtr(true),
			ExpEnabled:      true,
		},
		{
			GlobalTProxy: false,
			PodTProxy:    boolPtr(true),
			ExpEnabled:   true,
		},
	}

	// Outer loop is permutations of dns being enabled. Inner loop is permutations of tproxy being enabled.
	// Both must be enabled for dns to be enabled.
	for i, dnsCase := range dnsCases {
		for j, tproxyCase := range tproxyCases {
			t.Run(fmt.Sprintf("dns=%d,tproxy=%d", i, j), func(t *testing.T) {
				// Test setup.
				h := MeshWebhook{
					ConsulConfig:           &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
					EnableTransparentProxy: tproxyCase.GlobalTProxy,
					EnableConsulDNS:        dnsCase.GlobalConsulDNS,
				}
				pod := corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "web",
							},
						},
					},
				}
				if dnsCase.PodDNS != nil {
					pod.Annotations[constants.KeyConsulDNS] = strconv.FormatBool(*dnsCase.PodDNS)
				}
				if tproxyCase.PodTProxy != nil {
					pod.Annotations[constants.KeyTransparentProxy] = strconv.FormatBool(*tproxyCase.PodTProxy)
				}

				ns := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   k8sNamespace,
						Labels: map[string]string{},
					},
				}
				if dnsCase.NamespaceDNS != nil {
					ns.Labels[constants.KeyConsulDNS] = strconv.FormatBool(*dnsCase.NamespaceDNS)
				}
				if tproxyCase.NamespaceTProxy != nil {
					ns.Labels[constants.KeyTransparentProxy] = strconv.FormatBool(*tproxyCase.NamespaceTProxy)
				}

				// Actual test here.
				container, err := h.consulDataplaneSidecar(ns, pod, multiPortInfo{})
				require.NoError(t, err)
				// Flag should only be passed if both tproxy and dns are enabled.
				if tproxyCase.ExpEnabled && dnsCase.ExpEnabled {
					require.Contains(t, container.Args, "-consul-dns-bind-port=8600")
				} else {
					require.NotContains(t, container.Args, "-consul-dns-bind-port=8600")
				}
			})
		}
	}
}

func TestHandlerConsulDataplaneSidecar_ProxyHealthCheck(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	tests := map[string]struct {
		changeHook        func(*MeshWebhook)
		changePod         func(*corev1.Pod)
		expectedReadiness *corev1.Probe
		expectedStartup   *corev1.Probe
		expectedLiveness  *corev1.Probe
	}{
		"readiness-only": {
			changeHook: func(h *MeshWebhook) {},
			changePod:  func(p *corev1.Pod) {},
			expectedReadiness: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				InitialDelaySeconds: 1,
			},
		},
		"default-values": {
			changeHook: func(h *MeshWebhook) {
				h.DefaultSidecarProxyStartupFailureSeconds = 11
				h.DefaultSidecarProxyLivenessFailureSeconds = 22
			},
			changePod: func(p *corev1.Pod) {},
			expectedReadiness: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				InitialDelaySeconds: 1,
			},
			expectedStartup: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 11,
			},
			expectedLiveness: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 22,
			},
		},
		"override-default": {
			changeHook: func(h *MeshWebhook) {
				h.DefaultSidecarProxyStartupFailureSeconds = 11
				h.DefaultSidecarProxyLivenessFailureSeconds = 22
			},
			changePod: func(p *corev1.Pod) {
				p.ObjectMeta.Annotations[constants.AnnotationSidecarProxyStartupFailureSeconds] = "111"
				p.ObjectMeta.Annotations[constants.AnnotationSidecarProxyLivenessFailureSeconds] = "222"
			},
			expectedReadiness: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				InitialDelaySeconds: 1,
			},
			expectedStartup: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 111,
			},
			expectedLiveness: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Port: intstr.FromInt(21000),
						Path: "/ready",
					},
				},
				PeriodSeconds:    1,
				FailureThreshold: 222,
			},
		},
	}
	for tn, tc := range tests {
		t.Run(tn, func(t *testing.T) {
			hook := MeshWebhook{
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
				ConsulAddress: "1.1.1.1",
				LogLevel:      "info",
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationUseProxyHealthCheck: "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			tc.changeHook(&hook)
			tc.changePod(&pod)
			container, err := hook.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			require.Contains(t, container.Args, "-envoy-ready-bind-port=21000")
			require.Equal(t, tc.expectedReadiness, container.ReadinessProbe)
			require.Equal(t, tc.expectedStartup, container.StartupProbe)
			require.Equal(t, tc.expectedLiveness, container.LivenessProbe)
			require.Contains(t, container.Env, corev1.EnvVar{
				Name: "DP_ENVOY_READY_BIND_ADDRESS",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			})
			require.Contains(t, container.Ports, corev1.ContainerPort{
				Name:          "proxy-health-0",
				ContainerPort: 21000,
			})
		})
	}
}

func TestHandlerConsulDataplaneSidecar_ProxyHealthCheck_Multiport(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	h := MeshWebhook{
		ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
		ConsulAddress: "1.1.1.1",
		LogLevel:      "info",
	}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
			Annotations: map[string]string{
				constants.AnnotationService:             "web,web-admin",
				constants.AnnotationUseProxyHealthCheck: "true",
			},
		},

		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "web-admin-service-account",
				},
			},
			Containers: []corev1.Container{
				{
					Name: "web",
				},
				{
					Name: "web-side",
				},
				{
					Name: "web-admin",
				},
				{
					Name: "web-admin-side",
				},
				{
					Name: "auth-method-secret",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "service-account-secret",
							MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
						},
					},
				},
			},
			ServiceAccountName: "web",
		},
	}
	multiPortInfos := []multiPortInfo{
		{
			serviceIndex: 0,
			serviceName:  "web",
		},
		{
			serviceIndex: 1,
			serviceName:  "web-admin",
		},
	}
	expectedArgs := []string{
		"-envoy-ready-bind-port=21000",
		"-envoy-ready-bind-port=21001",
	}
	expectedProbe := []*corev1.Probe{
		{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(21000),
					Path: "/ready",
				},
			},
			InitialDelaySeconds: 1,
		},
		{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(21001),
					Path: "/ready",
				},
			},
			InitialDelaySeconds: 1,
		},
	}
	expectedPort := []corev1.ContainerPort{
		{
			Name:          "proxy-health-0",
			ContainerPort: 21000,
		},
		{
			Name:          "proxy-health-1",
			ContainerPort: 21001,
		},
	}
	expectedEnvVar := corev1.EnvVar{
		Name: "DP_ENVOY_READY_BIND_ADDRESS",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	}
	for i, info := range multiPortInfos {
		container, err := h.consulDataplaneSidecar(testNS, pod, info)
		require.NoError(t, err)
		require.Contains(t, container.Args, expectedArgs[i])
		require.Equal(t, expectedProbe[i], container.ReadinessProbe)
		require.Contains(t, container.Ports, expectedPort[i])
		require.Contains(t, container.Env, expectedEnvVar)
	}
}

func TestHandlerConsulDataplaneSidecar_Multiport(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	for _, aclsEnabled := range []bool{false, true} {
		name := fmt.Sprintf("acls enabled: %t", aclsEnabled)
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				ConsulAddress: "1.1.1.1",
				ConsulConfig:  &consul.Config{GRPCPort: 8502},
				LogLevel:      "info",
			}
			if aclsEnabled {
				w.AuthMethod = "test-auth-method"
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						constants.AnnotationService: "web,web-admin",
					},
				},

				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "web-admin-service-account",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "web",
						},
						{
							Name: "web-side",
						},
						{
							Name: "web-admin",
						},
						{
							Name: "web-admin-side",
						},
						{
							Name: "auth-method-secret",
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "service-account-secret",
									MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
								},
							},
						},
					},
					ServiceAccountName: "web",
				},
			}
			multiPortInfos := []multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			}
			expArgs := []string{
				"-addresses 1.1.1.1 -envoy-admin-bind-address=127.0.0.1 -consul-dns-bind-addr=127.0.0.1 -xds-bind-addr=127.0.0.1 -grpc-port=8502 -proxy-service-id-path=/consul/connect-inject/proxyid-web " +
					"-log-level=info -log-json=false -envoy-concurrency=0 -graceful-addr=127.0.0.1 -tls-disabled -envoy-admin-bind-port=19000 -graceful-port=20600 -telemetry-prom-scrape-path=/metrics -- --base-id 0",
				"-addresses 1.1.1.1 -envoy-admin-bind-address=127.0.0.1 -consul-dns-bind-addr=127.0.0.1 -xds-bind-addr=127.0.0.1 -grpc-port=8502 -proxy-service-id-path=/consul/connect-inject/proxyid-web-admin " +
					"-log-level=info -log-json=false -envoy-concurrency=0 -graceful-addr=127.0.0.1 -tls-disabled -envoy-admin-bind-port=19001 -graceful-port=20601 -telemetry-prom-scrape-path=/metrics -- --base-id 1",
			}
			if aclsEnabled {
				expArgs = []string{
					"-addresses 1.1.1.1 -envoy-admin-bind-address=127.0.0.1 -consul-dns-bind-addr=127.0.0.1 -xds-bind-addr=127.0.0.1 -grpc-port=8502 -proxy-service-id-path=/consul/connect-inject/proxyid-web " +
						"-log-level=info -log-json=false -envoy-concurrency=0 -graceful-addr=127.0.0.1 -credential-type=login -login-auth-method=test-auth-method " +
						"-login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token -tls-disabled -envoy-admin-bind-port=19000 -graceful-port=20600 -telemetry-prom-scrape-path=/metrics -- --base-id 0",
					"-addresses 1.1.1.1 -envoy-admin-bind-address=127.0.0.1 -consul-dns-bind-addr=127.0.0.1 -xds-bind-addr=127.0.0.1 -grpc-port=8502 -proxy-service-id-path=/consul/connect-inject/proxyid-web-admin " +
						"-log-level=info -log-json=false -envoy-concurrency=0 -graceful-addr=127.0.0.1 -credential-type=login -login-auth-method=test-auth-method " +
						"-login-bearer-token-path=/consul/serviceaccount-web-admin/token -tls-disabled -envoy-admin-bind-port=19001 -graceful-port=20601 -telemetry-prom-scrape-path=/metrics -- --base-id 1",
				}
			}
			expSAVolumeMounts := []corev1.VolumeMount{
				{
					Name:      "service-account-secret",
					MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
				},
				{
					Name:      "web-admin-service-account",
					MountPath: "/consul/serviceaccount-web-admin",
					ReadOnly:  true,
				},
			}

			for i, expCmd := range expArgs {
				container, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfos[i])
				require.NoError(t, err)
				require.Equal(t, expCmd, strings.Join(container.Args, " "))

				if w.AuthMethod != "" {
					require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
						{
							Name:      volumeName,
							MountPath: "/consul/connect-inject",
						},
						expSAVolumeMounts[i],
					})
				} else {
					require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
						{
							Name:      volumeName,
							MountPath: "/consul/connect-inject",
						},
					})
				}

				port := constants.ProxyDefaultInboundPort + i
				expectedProbe := &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.FromInt(port),
						},
					},
					InitialDelaySeconds: 1,
				}
				require.Equal(t, expectedProbe, container.ReadinessProbe)
				require.Nil(t, container.StartupProbe)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_withSecurityContext(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := map[string]struct {
		tproxyEnabled      bool
		openShiftEnabled   bool
		expSecurityContext *corev1.SecurityContext
	}{
		"tproxy disabled; openshift disabled": {
			tproxyEnabled:    false,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(sidecarUserAndGroupID)),
				RunAsGroup:               ptr.To(int64(sidecarUserAndGroupID)),
				RunAsNonRoot:             ptr.To(true),
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &corev1.Capabilities{
					Add:  []corev1.Capability{"NET_BIND_SERVICE"},
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
		"tproxy enabled; openshift disabled": {
			tproxyEnabled:    true,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(sidecarUserAndGroupID)),
				RunAsGroup:               ptr.To(int64(sidecarUserAndGroupID)),
				RunAsNonRoot:             ptr.To(true),
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &corev1.Capabilities{
					Add:  []corev1.Capability{"NET_BIND_SERVICE"},
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
		"tproxy disabled; openshift enabled": {
			tproxyEnabled:    false,
			openShiftEnabled: true,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(1000799998)),
				RunAsGroup:               ptr.To(int64(1000799998)),
				RunAsNonRoot:             ptr.To(true),
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &corev1.Capabilities{
					Add:  []corev1.Capability{"NET_BIND_SERVICE"},
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
		"tproxy enabled; openshift enabled": {
			tproxyEnabled:    true,
			openShiftEnabled: true,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(1000799998)),
				RunAsGroup:               ptr.To(int64(1000799998)),
				RunAsNonRoot:             ptr.To(true),
				ReadOnlyRootFilesystem:   ptr.To(true),
				AllowPrivilegeEscalation: ptr.To(false),
				Capabilities: &corev1.Capabilities{
					Add:  []corev1.Capability{"NET_BIND_SERVICE"},
					Drop: []corev1.Capability{"ALL"},
				},
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
		},
	}
	for name, c := range cases {
		ns := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:        k8sNamespace,
				Namespace:   k8sNamespace,
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
		}

		if c.openShiftEnabled {
			ns.Annotations[constants.AnnotationOpenShiftUIDRange] = "1000700000/100000"
			ns.Annotations[constants.AnnotationOpenShiftGroups] = "1000700000/100000"
		}
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableTransparentProxy: c.tproxyEnabled,
				EnableOpenShift:        c.openShiftEnabled,
				ConsulConfig:           &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: ns.Name,
					Annotations: map[string]string{
						constants.AnnotationService: "foo",
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			ec, err := w.consulDataplaneSidecar(ns, pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, c.expSecurityContext, ec.SecurityContext)
		})
	}
}

// Test that if the user specifies a pod security context with the same uid as `sidecarUserAndGroupID` that we return
// an error to the meshWebhook.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicatePodSecurityContextUID(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	require := require.New(t)
	w := MeshWebhook{
		ConsulConfig: &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
	}
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "web",
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser: ptr.To(int64(sidecarUserAndGroupID)),
			},
		},
	}
	_, err := w.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
	require.EqualError(
		err,
		fmt.Sprintf("pod's security context cannot have the same UID as consul-dataplane: %v", sidecarUserAndGroupID),
	)
}

// Test that if the user specifies a container with security context with the same uid as `sidecarUserAndGroupID` that we
// return an error to the meshWebhook. If a container using the consul-dataplane image has the same uid, we don't return an error
// because in multiport pod there can be multiple consul-dataplane sidecars.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicateContainerSecurityContextUID(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := []struct {
		name          string
		pod           corev1.Pod
		webhook       MeshWebhook
		expErr        bool
		expErrMessage string
	}{
		{
			name: "fails with non consul-dataplane image",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							// Setting RunAsUser: 1 should succeed.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: ptr.To(int64(1)),
							},
						},
						{
							Name: "app",
							// Setting RunAsUser: 5995 should fail.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: ptr.To(int64(sidecarUserAndGroupID)),
							},
							Image: "not-consul-dataplane",
						},
					},
				},
			},
			webhook: MeshWebhook{},
			expErr:  true,
			expErrMessage: fmt.Sprintf(
				"container \"app\" has runAsUser set to the same UID \"%d\" as consul-dataplane which is not allowed",
				sidecarUserAndGroupID,
			),
		},
		{
			name: "doesn't fail with envoy image",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
							// Setting RunAsUser: 1 should succeed.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: ptr.To(int64(1)),
							},
						},
						{
							Name: "sidecar",
							// Setting RunAsUser: 5995 should succeed if the image matches h.ImageConsulDataplane.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: ptr.To(int64(sidecarUserAndGroupID)),
							},
							Image: "envoy",
						},
					},
				},
			},
			webhook: MeshWebhook{
				ImageConsulDataplane: "envoy",
			},
			expErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.webhook.ConsulConfig = &consul.Config{HTTPPort: 8500, GRPCPort: 8502}
			_, err := tc.webhook.consulDataplaneSidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expErr {
				require.EqualError(t, err, tc.expErrMessage)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test that we can pass extra args to envoy via the extraEnvoyArgs flag
// or via pod annotations. When arguments are passed in both ways, the
// arguments set via pod annotations are used.
func TestHandlerConsulDataplaneSidecar_EnvoyExtraArgs(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := []struct {
		name              string
		envoyExtraArgs    string
		pod               *corev1.Pod
		expectedExtraArgs string
	}{
		{
			name:              "no extra options provided",
			envoyExtraArgs:    "",
			pod:               &corev1.Pod{},
			expectedExtraArgs: "",
		},
		{
			name:              "via flag: extra log-level option",
			envoyExtraArgs:    "--log-level debug",
			pod:               &corev1.Pod{},
			expectedExtraArgs: "-- --log-level debug",
		},
		{
			name:              "via flag: multiple arguments with quotes",
			envoyExtraArgs:    "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
			pod:               &corev1.Pod{},
			expectedExtraArgs: "-- --log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
		},
		{
			name:           "via annotation: multiple arguments with quotes",
			envoyExtraArgs: "",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedExtraArgs: "-- --log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
		},
		{
			name:           "via flag and annotation: should prefer setting via the annotation",
			envoyExtraArgs: "this should be overwritten",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationEnvoyExtraArgs: "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
					},
				},
			},
			expectedExtraArgs: "-- --log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := MeshWebhook{
				ImageConsul:          "hashicorp/consul:latest",
				ImageConsulDataplane: "hashicorp/consul-k8s:latest",
				ConsulConfig:         &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
				EnvoyExtraArgs:       tc.envoyExtraArgs,
			}

			c, err := h.consulDataplaneSidecar(testNS, *tc.pod, multiPortInfo{})
			require.NoError(t, err)
			require.Contains(t, strings.Join(c.Args, " "), tc.expectedExtraArgs)
		})
	}
}

func TestHandlerConsulDataplaneSidecar_UserVolumeMounts(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := []struct {
		name                          string
		pod                           corev1.Pod
		expectedContainerVolumeMounts []corev1.VolumeMount
		expErr                        string
	}{
		{
			name: "able to set a sidecar container volume mount via annotation",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationEnvoyExtraArgs:               "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
						constants.AnnotationConsulSidecarUserVolumeMount: "[{\"name\": \"tls-cert\", \"mountPath\": \"/custom/path\"}, {\"name\": \"tls-ca\", \"mountPath\": \"/custom/path2\"}]",
					},
				},
			},
			expectedContainerVolumeMounts: []corev1.VolumeMount{
				{
					Name:      "consul-connect-inject-data",
					MountPath: "/consul/connect-inject",
				},
				{
					Name:      "tls-cert",
					MountPath: "/custom/path",
				},
				{
					Name:      "tls-ca",
					MountPath: "/custom/path2",
				},
			},
		},
		{
			name: "invalid annotation results in error",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationEnvoyExtraArgs:               "--log-level debug --admin-address-path \"/tmp/consul/foo bar\"",
						constants.AnnotationConsulSidecarUserVolumeMount: "[abcdefg]",
					},
				},
			},
			expErr: "invalid character 'a' looking ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := MeshWebhook{
				ImageConsul:          "hashicorp/consul:latest",
				ImageConsulDataplane: "hashicorp/consul-k8s:latest",
				ConsulConfig:         &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			}
			c, err := h.consulDataplaneSidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expErr == "" {
				require.NoError(t, err)
				require.Equal(t, tc.expectedContainerVolumeMounts, c.VolumeMounts)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expErr)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Resources(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	mem1 := resource.MustParse("100Mi")
	mem2 := resource.MustParse("200Mi")
	cpu1 := resource.MustParse("100m")
	cpu2 := resource.MustParse("200m")
	zero := resource.MustParse("0")

	cases := map[string]struct {
		webhook      MeshWebhook
		annotations  map[string]string
		expResources corev1.ResourceRequirements
		expErr       string
	}{
		"no defaults, no annotations": {
			webhook:     MeshWebhook{},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits:   corev1.ResourceList{},
				Requests: corev1.ResourceList{},
			},
		},
		"all defaults, no annotations": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    cpu1,
				DefaultProxyCPULimit:      cpu2,
				DefaultProxyMemoryRequest: mem1,
				DefaultProxyMemoryLimit:   mem2,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"no defaults, all annotations": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyCPURequest:    "100m",
				constants.AnnotationSidecarProxyMemoryRequest: "100Mi",
				constants.AnnotationSidecarProxyCPULimit:      "200m",
				constants.AnnotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"annotations override defaults": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyCPURequest:    "100m",
				constants.AnnotationSidecarProxyMemoryRequest: "100Mi",
				constants.AnnotationSidecarProxyCPULimit:      "200m",
				constants.AnnotationSidecarProxyMemoryLimit:   "200Mi",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    cpu2,
					corev1.ResourceMemory: mem2,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    cpu1,
					corev1.ResourceMemory: mem1,
				},
			},
		},
		"defaults set to zero, no annotations": {
			webhook: MeshWebhook{
				DefaultProxyCPURequest:    zero,
				DefaultProxyCPULimit:      zero,
				DefaultProxyMemoryRequest: zero,
				DefaultProxyMemoryLimit:   zero,
			},
			annotations: nil,
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"annotations set to 0": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyCPURequest:    "0",
				constants.AnnotationSidecarProxyMemoryRequest: "0",
				constants.AnnotationSidecarProxyCPULimit:      "0",
				constants.AnnotationSidecarProxyMemoryLimit:   "0",
			},
			expResources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    zero,
					corev1.ResourceMemory: zero,
				},
			},
		},
		"invalid cpu request": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyCPURequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid cpu limit": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyCPULimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-cpu-limit:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory request": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyMemoryRequest: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-request:\"invalid\": quantities must match the regular expression",
		},
		"invalid memory limit": {
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationSidecarProxyMemoryLimit: "invalid",
			},
			expErr: "parsing annotation consul.hashicorp.com/sidecar-proxy-memory-limit:\"invalid\": quantities must match the regular expression",
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			c.webhook.ConsulConfig = &consul.Config{HTTPPort: 8500, GRPCPort: 8502}
			require := require.New(tt)
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := c.webhook.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			if c.expErr != "" {
				require.NotNil(err)
				require.Contains(err.Error(), c.expErr)
			} else {
				require.NoError(err)
				require.Equal(c.expResources, container.Resources)
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Metrics(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := []struct {
		name       string
		pod        corev1.Pod
		expCmdArgs string
		expPorts   []corev1.ContainerPort
		expErr     string
	}{
		{
			name:       "default",
			pod:        corev1.Pod{},
			expCmdArgs: "",
		},
		{
			name: "turning on merged metrics",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20100",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
					},
				},
			},
			expCmdArgs: "-telemetry-prom-scrape-path=/scrape-path -telemetry-prom-merge-port=20100 -telemetry-prom-service-metrics-url=http://127.0.0.1:1234/metrics",
			expPorts: []corev1.ContainerPort{
				{
					Name:          "prometheus",
					ContainerPort: 20200,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		{
			name: "metrics with prometheus port override",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20123",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
						constants.AnnotationPrometheusScrapePort: "6789",
					},
				},
			},
			expCmdArgs: "-telemetry-prom-scrape-path=/scrape-path -telemetry-prom-merge-port=20123 -telemetry-prom-service-metrics-url=http://127.0.0.1:1234/metrics",
			expPorts: []corev1.ContainerPort{
				{
					Name:          "prometheus",
					ContainerPort: 6789,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		{
			name: "merged metrics with TLS enabled",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20100",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
						constants.AnnotationPrometheusCAFile:     "/certs/ca.crt",
						constants.AnnotationPrometheusCAPath:     "/certs/ca",
						constants.AnnotationPrometheusCertFile:   "/certs/server.crt",
						constants.AnnotationPrometheusKeyFile:    "/certs/key.pem",
					},
				},
			},
			expCmdArgs: "-telemetry-prom-scrape-path=/scrape-path -telemetry-prom-merge-port=20100 -telemetry-prom-service-metrics-url=http://127.0.0.1:1234/metrics -telemetry-prom-ca-certs-file=/certs/ca.crt -telemetry-prom-ca-certs-path=/certs/ca -telemetry-prom-cert-file=/certs/server.crt -telemetry-prom-key-file=/certs/key.pem",
			expPorts: []corev1.ContainerPort{
				{
					Name:          "prometheus",
					ContainerPort: 20200,
					Protocol:      corev1.ProtocolTCP,
				},
			},
		},
		{
			name: "merge metrics with TLS enabled, missing CA gives an error",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20100",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
						constants.AnnotationPrometheusCertFile:   "/certs/server.crt",
						constants.AnnotationPrometheusKeyFile:    "/certs/key.pem",
					},
				},
			},
			expCmdArgs: "",
			expErr: fmt.Sprintf(
				"must set one of %q or %q when providing prometheus TLS config",
				constants.AnnotationPrometheusCAFile,
				constants.AnnotationPrometheusCAPath,
			),
		},
		{
			name: "merge metrics with TLS enabled, missing cert gives an error",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20100",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
						constants.AnnotationPrometheusCAFile:     "/certs/ca.crt",
						constants.AnnotationPrometheusKeyFile:    "/certs/key.pem",
					},
				},
			},
			expCmdArgs: "",
			expErr:     fmt.Sprintf("must set %q when providing prometheus TLS config", constants.AnnotationPrometheusCertFile),
		},
		{
			name: "merge metrics with TLS enabled, missing key file gives an error",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						constants.AnnotationService:              "web",
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationEnableMetricsMerging: "true",
						constants.AnnotationMergedMetricsPort:    "20100",
						constants.AnnotationPort:                 "1234",
						constants.AnnotationPrometheusScrapePath: "/scrape-path",
						constants.AnnotationPrometheusCAPath:     "/certs/ca",
						constants.AnnotationPrometheusCertFile:   "/certs/server.crt",
					},
				},
			},
			expCmdArgs: "",
			expErr:     fmt.Sprintf("must set %q when providing prometheus TLS config", constants.AnnotationPrometheusKeyFile),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := MeshWebhook{
				ConsulConfig: &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
				MetricsConfig: metrics.Config{
					// These are all the default values passed from the CLI
					DefaultPrometheusScrapePort: "20200",
					DefaultPrometheusScrapePath: "/metrics",
					DefaultMergedMetricsPort:    "20100",
				},
			}
			container, err := h.consulDataplaneSidecar(testNS, c.pod, multiPortInfo{})
			if c.expErr != "" {
				require.NotNil(t, err)
				require.Contains(t, err.Error(), c.expErr)
			} else {
				require.NoError(t, err)
				require.Contains(t, strings.Join(container.Args, " "), c.expCmdArgs)
				if c.expPorts != nil {
					require.ElementsMatch(t, container.Ports, c.expPorts)
				}
			}
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Lifecycle(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	gracefulShutdownSeconds := 10
	gracefulStartupSeconds := 10
	gracefulPort := "20307"
	gracefulShutdownPath := "/exit"
	gracefulStartupPath := "/start"

	cases := []struct {
		name        string
		webhook     MeshWebhook
		annotations map[string]string
		expCmdArgs  string
		expErr      string
	}{
		{
			name:        "no defaults, no annotations",
			webhook:     MeshWebhook{},
			annotations: nil,
			expCmdArgs:  "",
		},
		{
			name: "all defaults, no annotations",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         true,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultStartupGracePeriodSeconds:    gracefulStartupSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
					DefaultGracefulStartupPath:          gracefulStartupPath,
				},
			},
			annotations: nil,
			expCmdArgs:  "graceful-port=20307 -shutdown-drain-listeners -shutdown-grace-period-seconds=10 -graceful-shutdown-path=/exit -startup-grace-period-seconds=10 -graceful-startup-path=/start",
		},
		{
			name:    "no defaults, all annotations",
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle:                       "true",
				constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners: "true",
				constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds:   fmt.Sprint(gracefulShutdownSeconds),
				constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds:    fmt.Sprint(gracefulStartupSeconds),
				constants.AnnotationSidecarProxyLifecycleGracefulPort:                 gracefulPort,
				constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath:         gracefulShutdownPath,
				constants.AnnotationSidecarProxyLifecycleGracefulStartupPath:          gracefulStartupPath,
			},
			expCmdArgs: "-graceful-port=20307 -shutdown-drain-listeners -shutdown-grace-period-seconds=10 -graceful-shutdown-path=/exit -startup-grace-period-seconds=10 -graceful-startup-path=/start",
		},
		{
			name: "annotations override defaults",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         false,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultStartupGracePeriodSeconds:    gracefulStartupSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
					DefaultGracefulStartupPath:          gracefulStartupPath,
				},
			},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle:                       "true",
				constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners: "false",
				constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds:   fmt.Sprint(gracefulShutdownSeconds + 5),
				constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds:    fmt.Sprint(gracefulStartupSeconds + 5),
				constants.AnnotationSidecarProxyLifecycleGracefulPort:                 "20317",
				constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath:         "/foo",
				constants.AnnotationSidecarProxyLifecycleGracefulStartupPath:          "/bar",
			},
			expCmdArgs: "-graceful-port=20317 -shutdown-grace-period-seconds=15 -graceful-shutdown-path=/foo -startup-grace-period-seconds=15 -graceful-startup-path=/bar",
		},
		{
			name: "lifecycle disabled, no annotations",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         false,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultStartupGracePeriodSeconds:    gracefulStartupSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
					DefaultGracefulStartupPath:          gracefulStartupPath,
				},
			},
			annotations: nil,
			expCmdArgs:  "-graceful-port=20307",
		},
		{
			name: "lifecycle enabled, defaults omited, no annotations",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle: true,
				},
			},
			annotations: nil,
			expCmdArgs:  "",
		},
		{
			name: "annotations disable lifecycle default",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         true,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultStartupGracePeriodSeconds:    gracefulStartupSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
					DefaultGracefulStartupPath:          gracefulStartupPath,
				},
			},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle: "false",
			},
			expCmdArgs: "-graceful-port=20307",
		},
		{
			name: "annotations skip graceful shutdown",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         false,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
				},
			},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle:                       "false",
				constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners: "false",
				constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds:   "0",
			},
			expCmdArgs: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.webhook.ConsulConfig = &consul.Config{HTTPPort: 8500, GRPCPort: 8502}
			require := require.New(t)
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: c.annotations,
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "web",
						},
					},
				},
			}
			container, err := c.webhook.consulDataplaneSidecar(testNS, pod, multiPortInfo{})
			if c.expErr != "" {
				require.NotNil(err)
				require.Contains(err.Error(), c.expErr)
			} else {
				require.NoError(err)
				require.Contains(strings.Join(container.Args, " "), c.expCmdArgs)
			}
		})
	}
}

// boolPtr returns pointer to b.
func boolPtr(b bool) *bool {
	return &b
}

func TestHandlerConsulDataplaneSidecar_LifecycleConfig(t *testing.T) {
	netutil.GetAgentBindAddrFunc = netutil.GetMockGetAgentBindAddrFunc("0.0.0.0")
	cases := map[string]struct {
		pod                          corev1.Pod
		defaultProbeTimeout          int
		defaultProbeFailureThreshold int
		lifecycleConfig              lifecycle.Config
		expectedRestartPolicy        *corev1.ContainerRestartPolicy
		expectedStartupProbe         *corev1.Probe
		expectedError                string
	}{
		"when lifecycle enabled": {
			lifecycleConfig: lifecycle.Config{
				DefaultEnableConsulDataplaneAsSidecar: true,
			},
			defaultProbeTimeout:          5,
			defaultProbeFailureThreshold: 3,
			expectedRestartPolicy:        ptr.To(corev1.ContainerRestartPolicyAlways),
			expectedStartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/usr/local/bin/consul-dataplane",
							"-check-proxy-health",
						},
					},
				},
				TimeoutSeconds:   5,
				FailureThreshold: 3,
			},
		},
		"when lifecycle disabled": {
			lifecycleConfig: lifecycle.Config{
				DefaultEnableConsulDataplaneAsSidecar: false,
			},
			expectedRestartPolicy: nil,
			expectedStartupProbe:  nil,
		},
		"with custom probe settings from annotations": {
			lifecycleConfig: lifecycle.Config{
				DefaultEnableConsulDataplaneAsSidecar: true,
			},
			defaultProbeTimeout:          5,
			defaultProbeFailureThreshold: 3,
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"consul.hashicorp.com/sidecar-probe-check-timeout-seconds": "10",
						"consul.hashicorp.com/sidecar-probe-failure-threshold":     "6",
					},
				},
			},
			expectedRestartPolicy: ptr.To(corev1.ContainerRestartPolicyAlways),
			expectedStartupProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{
							"/usr/local/bin/consul-dataplane",
							"-check-proxy-health",
						},
					},
				},
				TimeoutSeconds:   10,
				FailureThreshold: 6,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				ImageConsulDataplane:                   "consul-dataplane:latest",
				DefaultSidecarProbeCheckTimeoutSeconds: tc.defaultProbeTimeout,
				DefaultSidecarProbeFailureThreshold:    tc.defaultProbeFailureThreshold,
				LifecycleConfig:                        tc.lifecycleConfig,
			}
			w.ConsulConfig = &consul.Config{HTTPPort: 8500, GRPCPort: 8502}
			container, err := w.consulDataplaneSidecar(testNS, tc.pod, multiPortInfo{})
			if tc.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedRestartPolicy, container.RestartPolicy)
			if tc.expectedStartupProbe == nil {
				require.Nil(t, container.StartupProbe)
			} else {
				require.NotNil(t, container.StartupProbe)
				require.Equal(t, tc.expectedStartupProbe.ProbeHandler, container.StartupProbe.ProbeHandler)
				require.Equal(t, tc.expectedStartupProbe.TimeoutSeconds, container.StartupProbe.TimeoutSeconds)
				require.Equal(t, tc.expectedStartupProbe.FailureThreshold, container.StartupProbe.FailureThreshold)
			}
		})
	}
}

func TestMeshWebhook_getSidecarProbePeriodSeconds(t *testing.T) {
	w := &MeshWebhook{DefaultSidecarProbePeriodSeconds: 5}
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	require.Equal(t, int32(5), w.getSidecarProbePeriodSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbePeriodSeconds] = "10"
	require.Equal(t, int32(10), w.getSidecarProbePeriodSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbePeriodSeconds] = "0"
	require.Equal(t, int32(0), w.getSidecarProbePeriodSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbePeriodSeconds] = "invalid"
	require.Equal(t, int32(0), w.getSidecarProbePeriodSeconds(pod))
}

func TestMeshWebhook_getSidecarProbeFailureThreshold(t *testing.T) {
	w := &MeshWebhook{DefaultSidecarProbeFailureThreshold: 3}
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	require.Equal(t, int32(3), w.getSidecarProbeFailureThreshold(pod))

	pod.Annotations[constants.AnnotationSidecarProbeFailureThreshold] = "8"
	require.Equal(t, int32(8), w.getSidecarProbeFailureThreshold(pod))

	pod.Annotations[constants.AnnotationSidecarProbeFailureThreshold] = "0"
	require.Equal(t, int32(0), w.getSidecarProbeFailureThreshold(pod))

	pod.Annotations[constants.AnnotationSidecarProbeFailureThreshold] = "invalid"
	require.Equal(t, int32(0), w.getSidecarProbeFailureThreshold(pod))
}

func TestMeshWebhook_getSidecarProbeTimeoutSeconds(t *testing.T) {
	w := &MeshWebhook{DefaultSidecarProbeCheckTimeoutSeconds: 7}
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}

	require.Equal(t, int32(7), w.getSidecarProbeTimeoutSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbeCheckTimeoutSeconds] = "12"
	require.Equal(t, int32(12), w.getSidecarProbeTimeoutSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbeCheckTimeoutSeconds] = "0"
	require.Equal(t, int32(0), w.getSidecarProbeTimeoutSeconds(pod))

	pod.Annotations[constants.AnnotationSidecarProbeCheckTimeoutSeconds] = "invalid"
	require.Equal(t, int32(0), w.getSidecarProbeTimeoutSeconds(pod))
}
