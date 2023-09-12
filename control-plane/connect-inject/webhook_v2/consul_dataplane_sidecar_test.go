// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook_v2

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
	"k8s.io/utils/pointer"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/lifecycle"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const nodeName = "test-node"

func TestHandlerConsulDataplaneSidecar(t *testing.T) {
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
				"-login-namespace=default -proxy-namespace=k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs and single destination namespace": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
				w.EnableNamespaces = true
				w.ConsulDestinationNamespace = "test-ns"
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-login-namespace=test-ns -proxy-namespace=test-ns -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with ACLs and partitions": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.AuthMethod = "test-auth-method"
				w.ConsulPartition = "test-part"
			},
			additionalExpCmdArgs: " -credential-type=login -login-auth-method=test-auth-method -login-bearer-token-path=/var/run/secrets/kubernetes.io/serviceaccount/token " +
				"-login-partition=test-part -proxy-partition=test-part -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
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
			additionalExpCmdArgs: " -proxy-namespace=consul-namespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with namespace mirroring": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
			},
			additionalExpCmdArgs: " -proxy-namespace=k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with namespace mirroring prefix": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.EnableNamespaces = true
				w.EnableK8SNSMirroring = true
				w.K8SNSMirroringPrefix = "foo-"
			},
			additionalExpCmdArgs: " -proxy-namespace=foo-k8snamespace -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
		},
		"with partitions": {
			webhookSetupFunc: func(w *MeshWebhook) {
				w.ConsulPartition = "partition-1"
			},
			additionalExpCmdArgs: " -proxy-partition=partition-1 -tls-disabled -graceful-port=20600 -telemetry-prom-scrape-path=/metrics",
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

			container, err := w.consulDataplaneSidecar(testNS, pod)
			require.NoError(t, err)
			expCmd := "-addresses 1.1.1.1 -grpc-port=" + strconv.Itoa(w.ConsulConfig.GRPCPort) +
				" -log-level=" + w.LogLevel + " -log-json=" + strconv.FormatBool(w.LogJSON) + " -envoy-concurrency=0" + c.additionalExpCmdArgs
			require.Equal(t, expCmd, strings.Join(container.Args, " "))

			if w.AuthMethod != "" {
				require.Equal(t, container.VolumeMounts, []corev1.VolumeMount{
					{
						Name:      volumeName,
						MountPath: "/consul/mesh-inject",
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
						MountPath: "/consul/mesh-inject",
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
			require.Len(t, container.Env, 7)
			require.Equal(t, container.Env[0].Name, "TMPDIR")
			require.Equal(t, container.Env[0].Value, "/consul/mesh-inject")
			require.Equal(t, container.Env[2].Name, "POD_NAME")
			require.Equal(t, container.Env[3].Name, "POD_NAMESPACE")
			require.Equal(t, container.Env[4].Name, "DP_PROXY_ID")
			require.Equal(t, container.Env[4].Value, "$(POD_NAME)")
			require.Equal(t, container.Env[5].Name, "DP_CREDENTIAL_LOGIN_META")
			require.Equal(t, container.Env[5].Value, "pod=$(POD_NAMESPACE)/$(POD_NAME)")
			require.Equal(t, container.Env[6].Name, "DP_CREDENTIAL_LOGIN_META1")
			require.Equal(t, container.Env[6].Value, "pod=$(POD_NAMESPACE)/$(POD_NAME)")
		})
	}
}

func TestHandlerConsulDataplaneSidecar_Concurrency(t *testing.T) {
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
			container, err := h.consulDataplaneSidecar(testNS, pod)
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
				container, err := h.consulDataplaneSidecar(ns, pod)
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
	h := MeshWebhook{
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
	container, err := h.consulDataplaneSidecar(testNS, pod)
	expectedProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(21000),
				Path: "/ready",
			},
		},
		InitialDelaySeconds: 1,
	}
	require.NoError(t, err)
	require.Contains(t, container.Args, "-envoy-ready-bind-port=21000")
	require.Equal(t, expectedProbe, container.ReadinessProbe)
	require.Contains(t, container.Env, corev1.EnvVar{
		Name: "DP_ENVOY_READY_BIND_ADDRESS",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	})
	require.Contains(t, container.Ports, corev1.ContainerPort{
		Name:          "proxy-health",
		ContainerPort: 21000,
	})
}

func TestHandlerConsulDataplaneSidecar_withSecurityContext(t *testing.T) {
	cases := map[string]struct {
		tproxyEnabled      bool
		openShiftEnabled   bool
		expSecurityContext *corev1.SecurityContext
	}{
		"tproxy disabled; openshift disabled": {
			tproxyEnabled:    false,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
		"tproxy enabled; openshift disabled": {
			tproxyEnabled:    true,
			openShiftEnabled: false,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
		"tproxy disabled; openshift enabled": {
			tproxyEnabled:      false,
			openShiftEnabled:   true,
			expSecurityContext: nil,
		},
		"tproxy enabled; openshift enabled": {
			tproxyEnabled:    true,
			openShiftEnabled: true,
			expSecurityContext: &corev1.SecurityContext{
				RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
				RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
				RunAsNonRoot:           pointer.Bool(true),
				ReadOnlyRootFilesystem: pointer.Bool(true),
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableTransparentProxy: c.tproxyEnabled,
				EnableOpenShift:        c.openShiftEnabled,
				ConsulConfig:           &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
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
			ec, err := w.consulDataplaneSidecar(testNS, pod)
			require.NoError(t, err)
			require.Equal(t, c.expSecurityContext, ec.SecurityContext)
		})
	}
}

// Test that if the user specifies a pod security context with the same uid as `sidecarUserAndGroupID` that we return
// an error to the meshWebhook.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicatePodSecurityContextUID(t *testing.T) {
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
				RunAsUser: pointer.Int64(sidecarUserAndGroupID),
			},
		},
	}
	_, err := w.consulDataplaneSidecar(testNS, pod)
	require.EqualError(err, fmt.Sprintf("pod's security context cannot have the same UID as consul-dataplane: %v", sidecarUserAndGroupID))
}

// Test that if the user specifies a container with security context with the same uid as `sidecarUserAndGroupID` that we
// return an error to the meshWebhook. If a container using the consul-dataplane image has the same uid, we don't return an error
// because in multiport pod there can be multiple consul-dataplane sidecars.
func TestHandlerConsulDataplaneSidecar_FailsWithDuplicateContainerSecurityContextUID(t *testing.T) {
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
								RunAsUser: pointer.Int64(1),
							},
						},
						{
							Name: "app",
							// Setting RunAsUser: 5995 should fail.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(sidecarUserAndGroupID),
							},
							Image: "not-consul-dataplane",
						},
					},
				},
			},
			webhook:       MeshWebhook{},
			expErr:        true,
			expErrMessage: fmt.Sprintf("container \"app\" has runAsUser set to the same UID \"%d\" as consul-dataplane which is not allowed", sidecarUserAndGroupID),
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
								RunAsUser: pointer.Int64(1),
							},
						},
						{
							Name: "sidecar",
							// Setting RunAsUser: 5995 should succeed if the image matches h.ImageConsulDataplane.
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: pointer.Int64(sidecarUserAndGroupID),
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
			_, err := tc.webhook.consulDataplaneSidecar(testNS, tc.pod)
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

			c, err := h.consulDataplaneSidecar(testNS, *tc.pod)
			require.NoError(t, err)
			require.Contains(t, strings.Join(c.Args, " "), tc.expectedExtraArgs)
		})
	}
}

func TestHandlerConsulDataplaneSidecar_UserVolumeMounts(t *testing.T) {
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
					MountPath: "/consul/mesh-inject",
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
			c, err := h.consulDataplaneSidecar(testNS, tc.pod)
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
			container, err := c.webhook.consulDataplaneSidecar(testNS, pod)
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

func TestHandlerConsulDataplaneSidecar_Lifecycle(t *testing.T) {
	gracefulShutdownSeconds := 10
	gracefulPort := "20307"
	gracefulShutdownPath := "/exit"

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
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
				},
			},
			annotations: nil,
			expCmdArgs:  "graceful-port=20307 -shutdown-drain-listeners -shutdown-grace-period-seconds=10 -graceful-shutdown-path=/exit",
		},
		{
			name:    "no defaults, all annotations",
			webhook: MeshWebhook{},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle:                       "true",
				constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners: "true",
				constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds:   fmt.Sprint(gracefulShutdownSeconds),
				constants.AnnotationSidecarProxyLifecycleGracefulPort:                 gracefulPort,
				constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath:         gracefulShutdownPath,
			},
			expCmdArgs: "-graceful-port=20307 -shutdown-drain-listeners -shutdown-grace-period-seconds=10 -graceful-shutdown-path=/exit",
		},
		{
			name: "annotations override defaults",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         false,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
				},
			},
			annotations: map[string]string{
				constants.AnnotationEnableSidecarProxyLifecycle:                       "true",
				constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners: "false",
				constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds:   fmt.Sprint(gracefulShutdownSeconds + 5),
				constants.AnnotationSidecarProxyLifecycleGracefulPort:                 "20317",
				constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath:         "/foo",
			},
			expCmdArgs: "-graceful-port=20317 -shutdown-grace-period-seconds=15 -graceful-shutdown-path=/foo",
		},
		{
			name: "lifecycle disabled, no annotations",
			webhook: MeshWebhook{
				LifecycleConfig: lifecycle.Config{
					DefaultEnableProxyLifecycle:         false,
					DefaultEnableShutdownDrainListeners: true,
					DefaultShutdownGracePeriodSeconds:   gracefulShutdownSeconds,
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
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
					DefaultGracefulPort:                 gracefulPort,
					DefaultGracefulShutdownPath:         gracefulShutdownPath,
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
			container, err := c.webhook.consulDataplaneSidecar(testNS, pod)
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
