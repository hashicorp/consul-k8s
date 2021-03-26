package connectinject

import (
	"strings"
	"testing"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
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
		Name   string
		Pod    func(*corev1.Pod) *corev1.Pod
		Cmd    string // Strings.Contains test
		CmdNot string // Not contains
	}{
		// The first test checks the whole template. Subsequent tests check
		// the parts that change.
		{
			"Whole template by default",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
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

			h := Handler{}
			pod := *tt.Pod(minimal())
			container, err := h.containerInit(pod, k8sNamespace)
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Contains(actual, tt.Cmd)
			if tt.CmdNot != "" {
				require.NotContains(actual, tt.CmdNot)
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
		Name         string
		Pod          func(*corev1.Pod) *corev1.Pod
		Handler      Handler
		K8sNamespace string
		Cmd          string // Strings.Contains test
		CmdNot       string // Not contains
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
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
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
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
		},

		{
			"Whole template, auth method, non-default namespace, mirroring disabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
			},
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="auth-method" \
  -namespace="non-default"

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
		},

		{
			"Whole template, auth method, non-default namespace, mirroring enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
			},
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -acl-auth-method="auth-method" \
  -namespace="default"

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`,
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := tt.Handler

			// Create a Consul server/client and proxy-defaults config because
			// the handler will call out to Consul if the upstream uses a datacenter.
			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(err)
			defer consul.Stop()
			consul.WaitForLeader(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(err)
			written, _, err := consulClient.ConfigEntries().Set(&capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
			}, nil)
			require.NoError(err)
			require.True(written)
			h.ConsulClient = consulClient

			container, err := h.containerInit(*tt.Pod(minimal()), k8sNamespace)
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Contains(actual, tt.Cmd)
			if tt.CmdNot != "" {
				require.NotContains(actual, tt.CmdNot)
			}
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
	container, err := h.containerInit(*pod, k8sNamespace)
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
	container, err := h.containerInit(*pod, k8sNamespace)
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
	container, err := h.containerInit(*pod, k8sNamespace)
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

func TestHandlerContainerInit_MismatchedServiceNameServiceAccountNameWithACLsEnabled(t *testing.T) {
	require := require.New(t)
	h := Handler{
		AuthMethod: "auth-method",
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
					Name: "serviceName",
				},
			},
			ServiceAccountName: "notServiceName",
		},
	}

	_, err := h.containerInit(*pod, k8sNamespace)
	require.EqualError(err, `serviceAccountName "notServiceName" does not match service name "foo"`)
}

func TestHandlerContainerInit_MismatchedServiceNameServiceAccountNameWithACLsDisabled(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationService: "foo",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "serviceName",
				},
			},
			ServiceAccountName: "notServiceName",
		},
	}

	_, err := h.containerInit(*pod, k8sNamespace)
	require.NoError(err)
}

// Test that the init copy container has the correct command.
func TestHandlerContainerInitCopyContainer(t *testing.T) {
	require := require.New(t)
	h := Handler{}
	container := h.containerInitCopyContainer()
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `cp /bin/consul /consul/connect-inject/consul`)
}
