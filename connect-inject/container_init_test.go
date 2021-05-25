package connectinject

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		Handler Handler
		Cmd     string // Strings.Contains test
		CmdNot  string // Not contains
	}{
		// The first test checks the whole template. Subsequent tests check
		// the parts that change.
		{
			"Whole template by default",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
		},

		{
			"When auth method is set -service-account-name and -service-name are passed in",
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
			Handler{
				AuthMethod: "an-auth-method",
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="an-auth-method" \
  -service-account-name="a-service-account-name" \
  -service-name="web" \
`,
			"",
		},
		{
			"When running the merged metrics server, configures consul connect envoy command",
			func(pod *corev1.Pod) *corev1.Pod {
				// The annotations to enable metrics, enable merging, and
				// service metrics port make the condition to run the merged
				// metrics server true. When that is the case,
				// prometheusScrapePath and mergedMetricsPort should get
				// rendered as -prometheus-scrape-path and
				// -prometheus-backend-port to the consul connect envoy command.
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationEnableMetrics] = "true"
				pod.Annotations[annotationEnableMetricsMerging] = "true"
				pod.Annotations[annotationMergedMetricsPort] = "20100"
				pod.Annotations[annotationServiceMetricsPort] = "1234"
				pod.Annotations[annotationPrometheusScrapePort] = "22222"
				pod.Annotations[annotationPrometheusScrapePath] = "/scrape-path"
				return pod
			},
			Handler{},
			`# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -prometheus-scrape-path="/scrape-path" \
  -prometheus-backend-port="20100" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := tt.Handler
			pod := *tt.Pod(minimal())
			container, err := h.containerInit(testNS, pod)
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Contains(actual, tt.Cmd)
			if tt.CmdNot != "" {
				require.NotContains(actual, tt.CmdNot)
			}
		})
	}
}

func TestHandlerContainerInit_transparentProxy(t *testing.T) {
	cases := map[string]struct {
		globalEnabled          bool
		annotations            map[string]string
		expectedContainsCmd    string
		expectedNotContainsCmd string
		namespaceLabel         map[string]string
	}{
		"enabled globally, ns not set, annotation not provided": {
			true,
			nil,
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"enabled globally, ns not set, annotation is false": {
			true,
			map[string]string{keyTransparentProxy: "false"},
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"enabled globally, ns not set, annotation is true": {
			true,
			map[string]string{keyTransparentProxy: "true"},
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"disabled globally, ns not set, annotation not provided": {
			false,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"disabled globally, ns not set, annotation is false": {
			false,
			map[string]string{keyTransparentProxy: "false"},
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			nil,
		},
		"disabled globally, ns not set, annotation is true": {
			false,
			map[string]string{keyTransparentProxy: "true"},
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			nil,
		},
		"exclude-inbound-ports, ns is not set, annotation is provided": {
			true,
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
		"exclude-outbound-ports, ns is not set, annotation is provided": {
			true,
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
		"exclude-outbound-cidrs annotation is provided": {
			true,
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
		"exclude-uids annotation is provided, ns is not set": {
			true,
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
		"disabled globally, ns enabled, annotation not set": {
			false,
			nil,
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			"",
			map[string]string{keyTransparentProxy: "true"},
		},
		"enabled globally, ns disabled, annotation not set": {
			true,
			nil,
			"",
			`/consul/connect-inject/consul connect redirect-traffic \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
			map[string]string{keyTransparentProxy: "false"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := Handler{EnableTransparentProxy: c.globalEnabled}
			pod := minimal()
			pod.Annotations = c.annotations

			expectedSecurityContext := &corev1.SecurityContext{
				RunAsUser:  pointerToInt64(0),
				RunAsGroup: pointerToInt64(0),
				Privileged: pointerToBool(true),
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{netAdminCapability},
				},
				RunAsNonRoot: pointerToBool(false),
			}
			ns := testNS
			ns.Labels = c.namespaceLabel
			container, err := h.containerInit(ns, *pod)
			require.NoError(t, err)
			actualCmd := strings.Join(container.Command, " ")

			if c.expectedContainsCmd != "" {
				require.Equal(t, expectedSecurityContext, container.SecurityContext)
				require.Contains(t, actualCmd, c.expectedContainsCmd)
			} else {
				require.Nil(t, container.SecurityContext)
				require.NotContains(t, actualCmd, c.expectedNotContainsCmd)
			}
		})
	}
}

func TestHandlerContainerInit_namespacesEnabled(t *testing.T) {
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
		Handler Handler
		Cmd     string // Strings.Contains test
	}{
		{
			"whole template, default namespace",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-service-namespace="default" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
		},

		{
			"whole template, non-default namespace",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-service-namespace="non-default" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
		},

		{
			"Whole template, auth method, non-default namespace, mirroring disabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = ""
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="auth-method" \
  -service-account-name="web" \
  -service-name="" \
  -auth-method-namespace="non-default" \
  -consul-service-namespace="non-default" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
		},
		{
			"Whole template, auth method, non-default namespace, mirroring enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = ""
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="auth-method" \
  -service-account-name="web" \
  -service-name="" \
  -auth-method-namespace="default" \
  -consul-service-namespace="k8snamespace" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
		},
		{
			"whole template, default namespace, tproxy enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
				EnableTransparentProxy:     true,
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-service-namespace="default" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -namespace="default" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},

		{
			"whole template, non-default namespace, tproxy enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
				EnableTransparentProxy:     true,
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-service-namespace="non-default" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -namespace="non-default" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},

		{
			"Whole template, auth method, non-default namespace, mirroring enabled, tproxy enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
				EnableTransparentProxy:     true,
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="auth-method" \
  -service-account-name="web" \
  -service-name="web" \
  -auth-method-namespace="default" \
  -consul-service-namespace="k8snamespace" \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  -namespace="k8snamespace" \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid=5995`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := tt.Handler
			container, err := h.containerInit(testNS, *tt.Pod(minimal()))
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Equal(tt.Cmd, actual)
		})
	}
}

func TestHandlerContainerInit_authMethod(t *testing.T) {
	require := require.New(t)
	h := Handler{
		AuthMethod: "release-name-consul-k8s-auth-method",
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
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "default-token-podid",
							ReadOnly:  true,
							MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
						},
					},
				},
			},
			ServiceAccountName: "foo",
		},
	}
	container, err := h.containerInit(testNS, *pod)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="release-name-consul-k8s-auth-method"`)
	require.Contains(actual, `
# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`)
}

// If Consul CA cert is set,
// Consul addresses should use HTTPS
// and CA cert should be set as env variable
func TestHandlerContainerInit_WithTLS(t *testing.T) {
	require := require.New(t)
	h := Handler{
		ConsulCACert: "consul-ca-cert",
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
	container, err := h.containerInit(testNS, *pod)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
export CONSUL_HTTP_ADDR="https://${HOST_IP}:8501"
export CONSUL_GRPC_ADDR="https://${HOST_IP}:8502"
export CONSUL_CACERT=/consul/connect-inject/consul-ca.pem
cat <<EOF >/consul/connect-inject/consul-ca.pem
consul-ca-cert
EOF`)
	require.NotContains(actual, `
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"`)
}

func TestHandlerContainerInit_Resources(t *testing.T) {
	require := require.New(t)
	h := Handler{
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
	container, err := h.containerInit(testNS, *pod)
	require.NoError(err)
	require.Equal(corev1.ResourceRequirements{
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
			h := Handler{EnableOpenShift: openShiftEnabled}

			container := h.initCopyContainer()

			if openShiftEnabled {
				require.Nil(t, container.SecurityContext)
			} else {
				expectedSecurityContext := &corev1.SecurityContext{
					RunAsUser:              pointerToInt64(copyContainerUserAndGroupID),
					RunAsGroup:             pointerToInt64(copyContainerUserAndGroupID),
					RunAsNonRoot:           pointerToBool(true),
					ReadOnlyRootFilesystem: pointerToBool(true),
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
