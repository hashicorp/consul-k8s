package connectinject

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const k8sNamespace = "k8snamespace"

func TestHandlerContainerInit(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "test-namespace",
				Annotations: map[string]string{
					annotationService: "foo",
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
				},
			},
			Status: corev1.PodStatus{
				HostIP: "1.1.1.1",
				PodIP:  "2.2.2.2",
			},
		}
	}

	cases := []struct {
		Name    string
		Pod     func(*corev1.Pod) *corev1.Pod
		Webhook MeshWebhook
		ExpCmd  string // Strings.Contains test
		ExpEnv  []corev1.EnvVar
	}{
		{
			"default cmd and env",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
				LogLevel:      "info",
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "0s",
				},
			},
		},

		{
			"with auth method",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Spec.ServiceAccountName = "a-service-account-name"
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
					{
						Name:      "sa",
						MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
					},
				}
				return pod
			},
			MeshWebhook{
				AuthMethod:    "an-auth-method",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
				LogLevel:      "debug",
				LogJSON:       true,
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=debug \
  -log-json=true \
  -service-account-name="a-service-account-name" \
  -service-name="web" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "an-auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			w := tt.Webhook
			pod := *tt.Pod(minimal())
			container, err := w.containerInit(testNS, pod, multiPortInfo{})
			require.NoError(t, err)
			actual := strings.Join(container.Command, " ")
			require.Contains(t, actual, tt.ExpCmd)
			require.EqualValues(t, container.Env[2:], tt.ExpEnv)
		})
	}
}

func TestHandlerContainerInit_transparentProxy(t *testing.T) {
	cases := map[string]struct {
		globalEnabled          bool
		cniEnabled             bool
		annotations            map[string]string
		expectedContainsCmd    string
		expectedNotContainsCmd string
		namespaceLabel         map[string]string
	}{
		"enabled globally, ns not set, annotation not provided, cni disabled": {
			true,
			false,
			nil,
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"enabled globally, ns not set, annotation is false, cni disabled": {
			true,
			false,
			map[string]string{keyTransparentProxy: "false"},
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"enabled globally, ns not set, annotation is true, cni disabled": {
			true,
			false,
			map[string]string{keyTransparentProxy: "true"},
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"disabled globally, ns not set, annotation not provided, cni disabled": {
			false,
			false,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"disabled globally, ns not set, annotation is false, cni disabled": {
			false,
			false,
			map[string]string{keyTransparentProxy: "false"},
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"disabled globally, ns not set, annotation is true, cni disabled": {
			false,
			false,
			map[string]string{keyTransparentProxy: "true"},
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"exclude-inbound-ports, ns is not set, annotation is provided, cni disabled": {
			true,
			false,
			map[string]string{
				keyTransparentProxy:                 "true",
				annotationTProxyExcludeInboundPorts: "9090,9091",
			},
			`/consul/connect-inject/consul connect redirect-traffic \
  -exclude-inbound-port="9090" \
  -exclude-inbound-port="9091" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"exclude-outbound-ports, ns is not set, annotation is provided, cni disabled": {
			true,
			false,
			map[string]string{
				keyTransparentProxy:                  "true",
				annotationTProxyExcludeOutboundPorts: "9090,9091",
			},
			`/consul/connect-inject/consul connect redirect-traffic \
  -exclude-outbound-port="9090" \
  -exclude-outbound-port="9091" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"exclude-outbound-cidrs annotation is provided, cni disabled": {
			true,
			false,
			map[string]string{
				keyTransparentProxy:                  "true",
				annotationTProxyExcludeOutboundCIDRs: "1.1.1.1,2.2.2.2/24",
			},
			`/consul/connect-inject/consul connect redirect-traffic \
  -exclude-outbound-cidr="1.1.1.1" \
  -exclude-outbound-cidr="2.2.2.2/24" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"exclude-uids annotation is provided, ns is not set, cni disabled": {
			true,
			false,
			map[string]string{
				keyTransparentProxy:         "true",
				annotationTProxyExcludeUIDs: "6000,7000",
			},
			`/consul/connect-inject/consul connect redirect-traffic \
  -exclude-uid="6000" \
  -exclude-uid="7000" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"disabled globally, ns enabled, annotation not set, cni disabled": {
			false,
			false,
			nil,
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			map[string]string{keyTransparentProxy: "true"},
		},
		"enabled globally, ns disabled, annotation not set, cni disabled": {
			true,
			false,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			map[string]string{keyTransparentProxy: "false"},
		},
		"disabled globally, ns enabled, annotation not set, cni enabled": {
			false,
			true,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			map[string]string{keyTransparentProxy: "true"},
		},

		"enabled globally, ns not set, annotation not set, cni enabled": {
			true,
			true,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableTransparentProxy: c.globalEnabled,
				EnableCNI:              c.cniEnabled,
				ConsulConfig:           &consul.Config{HTTPPort: 8500},
			}
			pod := minimal()
			pod.Annotations = c.annotations

			expectedSecurityContext := &corev1.SecurityContext{}
			if !c.cniEnabled {
				expectedSecurityContext.RunAsUser = pointer.Int64(0)
				expectedSecurityContext.RunAsGroup = pointer.Int64(0)
				expectedSecurityContext.RunAsNonRoot = pointer.Bool(false)
				expectedSecurityContext.Privileged = pointer.Bool(true)
				expectedSecurityContext.Capabilities = &corev1.Capabilities{
					Add: []corev1.Capability{netAdminCapability},
				}
			} else {

				expectedSecurityContext.RunAsUser = pointer.Int64(initContainersUserAndGroupID)
				expectedSecurityContext.RunAsGroup = pointer.Int64(initContainersUserAndGroupID)
				expectedSecurityContext.RunAsNonRoot = pointer.Bool(true)
				expectedSecurityContext.Privileged = pointer.Bool(false)
				expectedSecurityContext.Capabilities = &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				}
			}
			ns := testNS
			ns.Labels = c.namespaceLabel
			container, err := w.containerInit(ns, *pod, multiPortInfo{})
			require.NoError(t, err)
			actualCmd := strings.Join(container.Command, " ")

			if c.expectedContainsCmd != "" {
				require.Equal(t, expectedSecurityContext, container.SecurityContext)
				require.Contains(t, actualCmd, c.expectedContainsCmd)
			} else {
				if !c.cniEnabled {
					require.Nil(t, container.SecurityContext)
				} else {
					require.Equal(t, expectedSecurityContext, container.SecurityContext)
				}
				require.NotContains(t, actualCmd, c.expectedNotContainsCmd)
			}
		})
	}
}

func TestHandlerContainerInit_consulDNS(t *testing.T) {
	cases := map[string]struct {
		globalEnabled       bool
		annotations         map[string]string
		expectedContainsCmd string
		namespaceLabel      map[string]string
	}{
		"enabled globally, ns not set, annotation not provided": {
			globalEnabled: true,
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -consul-dns-ip="10.0.34.16" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"enabled globally, ns not set, annotation is false": {
			globalEnabled: true,
			annotations:   map[string]string{keyConsulDNS: "false"},
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"enabled globally, ns not set, annotation is true": {
			globalEnabled: true,
			annotations:   map[string]string{keyConsulDNS: "true"},
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -consul-dns-ip="10.0.34.16" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"disabled globally, ns not set, annotation not provided": {
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"disabled globally, ns not set, annotation is false": {
			annotations: map[string]string{keyConsulDNS: "false"},
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"disabled globally, ns not set, annotation is true": {
			annotations: map[string]string{keyConsulDNS: "true"},
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -consul-dns-ip="10.0.34.16" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
		"disabled globally, ns enabled, annotation not set": {
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -consul-dns-ip="10.0.34.16" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			namespaceLabel: map[string]string{keyConsulDNS: "true"},
		},
		"enabled globally, ns disabled, annotation not set": {
			globalEnabled: true,
			expectedContainsCmd: `/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			namespaceLabel: map[string]string{keyConsulDNS: "false"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableConsulDNS:        c.globalEnabled,
				EnableTransparentProxy: true,
				ResourcePrefix:         "consul-consul",
				ConsulConfig:           &consul.Config{HTTPPort: 8500},
			}
			os.Setenv("CONSUL_CONSUL_DNS_SERVICE_HOST", "10.0.34.16")
			defer os.Unsetenv("CONSUL_CONSUL_DNS_SERVICE_HOST")

			pod := minimal()
			pod.Annotations = c.annotations

			ns := testNS
			ns.Labels = c.namespaceLabel
			container, err := w.containerInit(ns, *pod, multiPortInfo{})
			require.NoError(t, err)
			actualCmd := strings.Join(container.Command, " ")

			require.Contains(t, actualCmd, c.expectedContainsCmd)
		})
	}
}

func TestHandler_constructDNSServiceHostName(t *testing.T) {
	cases := []struct {
		prefix string
		result string
	}{
		{
			prefix: "consul-consul",
			result: "CONSUL_CONSUL_DNS_SERVICE_HOST",
		},
		{
			prefix: "release",
			result: "RELEASE_DNS_SERVICE_HOST",
		},
		{
			prefix: "consul-dc1",
			result: "CONSUL_DC1_DNS_SERVICE_HOST",
		},
	}

	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			w := MeshWebhook{ResourcePrefix: c.prefix}
			require.Equal(t, c.result, w.constructDNSServiceHostName())
		})
	}
}

func TestHandlerContainerInit_namespacesAndPartitionsEnabled(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotationService: "foo",
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
			},
		}
	}

	cases := []struct {
		Name    string
		Pod     func(*corev1.Pod) *corev1.Pod
		Webhook MeshWebhook
		Cmd     string
		ExpEnv  []corev1.EnvVar
	}{
		{
			"default namespace, no partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				ConsulPartition:            "",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "default",
				},
			},
		},
		{
			"default namespace, default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				ConsulPartition:            "default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "default",
				},
			},
		},
		{
			"non-default namespace, no partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
			},
		},
		{
			"non-default namespace, non-default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "non-default-part",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "non-default-part",
				},
			},
		},
		{
			"auth method, non-default namespace, mirroring disabled, default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = ""
				return pod
			},
			MeshWebhook{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				ConsulPartition:            "default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
				{
					Name:  "CONSUL_LOGIN_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_LOGIN_PARTITION",
					Value: "default",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "default",
				},
			},
		},
		{
			"auth method, non-default namespace, mirroring enabled, non-default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = ""
				return pod
			},
			MeshWebhook{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
				ConsulPartition:            "non-default",
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="" \`,
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_ADDRESSES",
					Value: "10.0.0.0",
				},
				{
					Name:  "CONSUL_GRPC_PORT",
					Value: "8502",
				},
				{
					Name:  "CONSUL_HTTP_PORT",
					Value: "8500",
				},
				{
					Name:  "CONSUL_API_TIMEOUT",
					Value: "5s",
				},
				{
					Name:  "CONSUL_LOGIN_AUTH_METHOD",
					Value: "auth-method",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_META",
					Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
				},
				{
					Name:  "CONSUL_LOGIN_NAMESPACE",
					Value: "default",
				},
				{
					Name:  "CONSUL_LOGIN_PARTITION",
					Value: "non-default",
				},
				{
					Name:  "CONSUL_NAMESPACE",
					Value: "k8snamespace",
				},
				{
					Name:  "CONSUL_PARTITION",
					Value: "non-default",
				},
			},
		},
		{
			"default namespace, tproxy enabled, no partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				ConsulPartition:            "",
				EnableTransparentProxy:     true,
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -namespace="default" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		{
			"non-default namespace, tproxy enabled, default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				EnableNamespaces:           true,
				ConsulPartition:            "default",
				ConsulDestinationNamespace: "non-default",
				EnableTransparentProxy:     true,
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -partition="default" \
  -namespace="non-default" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},

		{
			"auth method, non-default namespace, mirroring enabled, tproxy enabled, non-default partition",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			MeshWebhook{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulPartition:            "non-default",
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
				EnableTransparentProxy:     true,
				ConsulAddress:              "10.0.0.0",
				ConsulConfig:               &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			},
			`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="web" \

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -token-file="/consul/connect-inject/acl-token" \
  -partition="non-default" \
  -namespace="k8snamespace" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			h := tt.Webhook
			h.LogLevel = "info"
			container, err := h.containerInit(testNS, *tt.Pod(minimal()), multiPortInfo{})
			require.NoError(t, err)
			actual := strings.Join(container.Command, " ")
			require.Equal(t, tt.Cmd, actual)
			if tt.ExpEnv != nil {
				require.Equal(t, tt.ExpEnv, container.Env[2:])
			}
		})
	}
}

func TestHandlerContainerInit_Multiport(t *testing.T) {
	minimal := func() *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotationService: "web,web-admin",
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
	}

	cases := []struct {
		Name              string
		Pod               func(*corev1.Pod) *corev1.Pod
		Webhook           MeshWebhook
		NumInitContainers int
		MultiPortInfos    []multiPortInfo
		Cmd               []string // Strings.Contains test
		ExpEnvVars        []corev1.EnvVar
	}{
		{
			"Whole template, multiport",
			func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MeshWebhook{
				LogLevel:      "info",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502},
			},
			2,
			[]multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			},
			[]string{`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web \
  -service-name="web" \`,

				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web-admin \
  -service-name="web-admin" \`,
			},
			nil,
		},
		{
			"Whole template, multiport, auth method",
			func(pod *corev1.Pod) *corev1.Pod {
				return pod
			},
			MeshWebhook{
				AuthMethod:    "auth-method",
				ConsulAddress: "10.0.0.0",
				ConsulConfig:  &consul.Config{HTTPPort: 8500, GRPCPort: 8502, APITimeout: 5 * time.Second},
				LogLevel:      "info",
			},
			2,
			[]multiPortInfo{
				{
					serviceIndex: 0,
					serviceName:  "web",
				},
				{
					serviceIndex: 1,
					serviceName:  "web-admin",
				},
			},
			[]string{`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -service-account-name="web" \
  -service-name="web" \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web \`,

				`/bin/sh -ec consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name=k8s-service-mesh \
  -log-level=info \
  -log-json=false \
  -service-account-name="web-admin" \
  -service-name="web-admin" \
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-web-admin \`,
			},
			[]corev1.EnvVar{
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/var/run/secrets/kubernetes.io/serviceaccount/token",
				},
				{
					Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
					Value: "/consul/serviceaccount-web-admin/token",
				},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			h := tt.Webhook
			for i := 0; i < tt.NumInitContainers; i++ {
				container, err := h.containerInit(testNS, *tt.Pod(minimal()), tt.MultiPortInfos[i])
				require.NoError(t, err)
				actual := strings.Join(container.Command, " ")
				require.Equal(t, tt.Cmd[i], actual)
				if tt.ExpEnvVars != nil {
					require.Contains(t, container.Env, tt.ExpEnvVars[i])
				}
			}
		})
	}
}

// If TLSEnabled is set,
// Consul addresses should use HTTPS
// and CA cert should be set as env variable if provided.
// Additionally, test that the init container is correctly configured
// when http or gRPC ports are different from defaults.
func TestHandlerContainerInit_WithTLSAndCustomPorts(t *testing.T) {
	for _, caProvided := range []bool{true, false} {
		name := fmt.Sprintf("ca provided: %t", caProvided)
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				ConsulAddress: "10.0.0.0",
				TLSEnabled:    true,
				ConsulConfig:  &consul.Config{HTTPPort: 443, GRPCPort: 8503},
			}
			if caProvided {
				w.ConsulCACert = "consul-ca-cert"
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService: "foo",
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
			container, err := w.containerInit(testNS, *pod, multiPortInfo{})
			require.NoError(t, err)
			require.Equal(t, "CONSUL_ADDRESSES", container.Env[2].Name)
			require.Equal(t, w.ConsulAddress, container.Env[2].Value)
			require.Equal(t, "CONSUL_GRPC_PORT", container.Env[3].Name)
			require.Equal(t, fmt.Sprintf("%d", w.ConsulConfig.GRPCPort), container.Env[3].Value)
			require.Equal(t, "CONSUL_HTTP_PORT", container.Env[4].Name)
			require.Equal(t, fmt.Sprintf("%d", w.ConsulConfig.HTTPPort), container.Env[4].Value)
			if w.TLSEnabled {
				require.Equal(t, "CONSUL_USE_TLS", container.Env[6].Name)
				require.Equal(t, "true", container.Env[6].Value)
				if caProvided {
					require.Equal(t, "CONSUL_CACERT_PEM", container.Env[7].Name)
					require.Equal(t, "consul-ca-cert", container.Env[7].Value)
				} else {
					for _, ev := range container.Env {
						if ev.Name == "CONSUL_CACERT_PEM" {
							require.Empty(t, ev.Value)
						}
					}
				}
			}

		})
	}
}

func TestHandlerContainerInit_Resources(t *testing.T) {
	w := MeshWebhook{
		InitContainerResources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("10Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("25Mi"),
			},
		},
		ConsulConfig: &consul.Config{HTTPPort: 8500, APITimeout: 5 * time.Second},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationService: "foo",
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
	container, err := w.containerInit(testNS, *pod, multiPortInfo{})
	require.NoError(t, err)
	require.Equal(t, corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("20m"),
			corev1.ResourceMemory: resource.MustParse("25Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("10m"),
			corev1.ResourceMemory: resource.MustParse("10Mi"),
		},
	}, container.Resources)
}

// Test that the init copy container has the correct command and SecurityContext.
func TestHandlerInitCopyContainer(t *testing.T) {
	openShiftEnabledCases := []bool{false, true}

	for _, openShiftEnabled := range openShiftEnabledCases {
		t.Run(fmt.Sprintf("openshift enabled: %t", openShiftEnabled), func(t *testing.T) {
			w := MeshWebhook{EnableOpenShift: openShiftEnabled}

			container := w.initCopyContainer()

			if openShiftEnabled {
				require.Nil(t, container.SecurityContext)
			} else {
				expectedSecurityContext := &corev1.SecurityContext{
					RunAsUser:              pointer.Int64(initContainersUserAndGroupID),
					RunAsGroup:             pointer.Int64(initContainersUserAndGroupID),
					RunAsNonRoot:           pointer.Bool(true),
					ReadOnlyRootFilesystem: pointer.Bool(true),
				}
				require.Equal(t, expectedSecurityContext, container.SecurityContext)
			}

			actual := strings.Join(container.Command, " ")
			require.Contains(t, actual, `cp /bin/consul /consul/connect-inject/consul`)
		})
	}
}

var testNS = corev1.Namespace{
	ObjectMeta: metav1.ObjectMeta{
		Name: k8sNamespace,
	},
}
