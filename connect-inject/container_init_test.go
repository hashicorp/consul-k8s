package connectinject

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const k8sNamespace = "k8snamespace"

func TestHandlerContainerInit(t *testing.T) {
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
				},
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
			"Only service, whole template",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
}
EOF

/bin/consul services register \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
			"",
		},

		{
			"Service port specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
}`,
			"",
		},

		{
			"Upstream",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      local_bind_port = 1234
    }
  }`,
			"",
		},

		{
			"Upstream datacenter specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234:dc1"
				return pod
			},
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      local_bind_port = 1234
      datacenter = "dc1"
    }
  }`,
			"",
		},

		{
			"No Upstream datacenter specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			"",
			`datacenter`,
		},

		{
			"Upstream prepared query",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "prepared_query:handle:1234"
				return pod
			},
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "prepared_query" 
      destination_name = "handle"
      local_bind_port = 1234
    }
  }`,
			"",
		},

		{
			"Single Tag specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[annotationTags] = "abc"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc"]

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc"]
}`,
			"",
		},

		{
			"Multiple Tags specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[annotationTags] = "abc,123"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123"]

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123"]
}`,
			"",
		},

		{
			"Tags using old annotation",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[annotationConnectTags] = "abc,123"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123"]

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123"]
}`,
			"",
		},

		{
			"Tags using old and new annotations",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[annotationTags] = "abc,123"
				pod.Annotations[annotationConnectTags] = "abc,123,def,456"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123","abc","123","def","456"]

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123","abc","123","def","456"]
}`,
			"",
		},

		{
			"No Tags specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			"",
			`tags`,
		},
		{
			"Metadata specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationPort] = "1234"
				pod.Annotations[fmt.Sprintf("%sname", annotationMeta)] = "abc"
				pod.Annotations[fmt.Sprintf("%sversion", annotationMeta)] = "2"
				return pod
			},
			`services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  meta = {
    name = "abc"
    version = "2"
  }

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    local_service_address = "127.0.0.1"
    local_service_port = 1234
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  meta = {
    name = "abc"
    version = "2"
  }
}`,
			"",
		},

		{
			"No Metadata specified",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			"",
			`meta`,
		},

		{
			"Central config",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			"",
			`meta`,
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			var h Handler
			container, err := h.containerInit(tt.Pod(minimal()), k8sNamespace)
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
			"Only service, whole template, default namespace",
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

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "default"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "default"
}
EOF

/bin/consul services register \
  -namespace="default" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -namespace="default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
			"",
		},

		{
			"Only service, whole template, non-default namespace",
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

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "non-default"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "non-default"
}
EOF

/bin/consul services register \
  -namespace="non-default" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
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

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "non-default"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "non-default"
}
EOF
/bin/consul login -method="auth-method" \
  -bearer-token-file="/var/run/secrets/kubernetes.io/serviceaccount/token" \
  -token-sink-file="/consul/connect-inject/acl-token" \
  -namespace="non-default" \
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
chmod 444 /consul/connect-inject/acl-token

/bin/consul services register \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="non-default" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
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

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "k8snamespace"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "k8snamespace"
}
EOF
/bin/consul login -method="auth-method" \
  -bearer-token-file="/var/run/secrets/kubernetes.io/serviceaccount/token" \
  -token-sink-file="/consul/connect-inject/acl-token" \
  -namespace="default" \
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
chmod 444 /consul/connect-inject/acl-token

/bin/consul services register \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
			"",
		},

		{
			"Whole template, service defaults and no auth method, non-default namespace",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				WriteServiceDefaults:       true,
				DefaultProtocol:            "http",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default",
			},
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "non-default"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "non-default"
}
EOF
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "web"
protocol = "http"
namespace = "non-default"
EOF
/bin/consul config write -cas -modify-index 0 \
  -namespace="non-default" \
  /consul/connect-inject/service-defaults.hcl || true

/bin/consul services register \
  -namespace="non-default" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -namespace="non-default" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
			"",
		},

		{
			"Whole template, service defaults and auth method, non-default namespace, mirroring enabled",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			Handler{
				AuthMethod:                 "auth-method",
				WriteServiceDefaults:       true,
				DefaultProtocol:            "http",
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "non-default", // Overridden by mirroring
				EnableK8SNSMirroring:       true,
			},
			k8sNamespace,
			`/bin/sh -ec 
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "k8snamespace"

  proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
  }

  checks {
    name = "Proxy Public Listener"
    tcp = "${POD_IP}:20000"
    interval = "10s"
    deregister_critical_service_after = "10m"
  }

  checks {
    name = "Destination Alias"
    alias_service = "web"
  }
}

services {
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "k8snamespace"
}
EOF
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "web"
protocol = "http"
namespace = "k8snamespace"
EOF
/bin/consul login -method="auth-method" \
  -bearer-token-file="/var/run/secrets/kubernetes.io/serviceaccount/token" \
  -token-sink-file="/consul/connect-inject/acl-token" \
  -namespace="default" \
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
chmod 444 /consul/connect-inject/acl-token
/bin/consul config write -cas -modify-index 0 \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  /consul/connect-inject/service-defaults.hcl || true

/bin/consul services register \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -token-file="/consul/connect-inject/acl-token" \
  -namespace="k8snamespace" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`,
			"",
		},

		{
			"Upstream namespace",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db.namespace:1234"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
			},
			k8sNamespace,
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      destination_namespace = "namespace"
      local_bind_port = 1234
    }
  }`,
			"",
		},

		{
			"Upstream no namespace",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234"
				return pod
			},
			Handler{
				EnableNamespaces:           true,
				ConsulDestinationNamespace: "default",
			},
			k8sNamespace,
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      local_bind_port = 1234
    }
  }`,
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

			h := tt.Handler
			container, err := h.containerInit(tt.Pod(minimal()), k8sNamespace)
			require.NoError(err)
			actual := strings.Join(container.Command, " ")
			require.Contains(actual, tt.Cmd)
			if tt.CmdNot != "" {
				require.NotContains(actual, tt.CmdNot)
			}
		})
	}
}

// Test that we write service-defaults config and use the default protocol.
func TestHandlerContainerInit_writeServiceDefaultsDefaultProtocol(t *testing.T) {
	require := require.New(t)
	h := Handler{
		WriteServiceDefaults: true,
		DefaultProtocol:      "grpc",
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
	container, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "foo"
protocol = "grpc"
EOF
/bin/consul config write -cas -modify-index 0 \
  /consul/connect-inject/service-defaults.hcl || true

/bin/consul services register \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`)
}

// Test that we write service-defaults config and use the protocol from the Pod.
func TestHandlerContainerInit_writeServiceDefaultsPodProtocol(t *testing.T) {
	require := require.New(t)
	h := Handler{
		WriteServiceDefaults: true,
		DefaultProtocol:      "http",
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotationService:  "foo",
				annotationProtocol: "grpc",
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
	container, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "foo"
protocol = "grpc"
EOF
/bin/consul config write -cas -modify-index 0 \
  /consul/connect-inject/service-defaults.hcl || true

/bin/consul services register \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul`)
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
		},
	}
	container, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
/bin/consul login -method="release-name-consul-k8s-auth-method" \
  -bearer-token-file="/var/run/secrets/kubernetes.io/serviceaccount/token" \
  -token-sink-file="/consul/connect-inject/acl-token" \
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
chmod 444 /consul/connect-inject/acl-token

/bin/consul services register \
  -token-file="/consul/connect-inject/acl-token" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -token-file="/consul/connect-inject/acl-token" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`)
}

func TestHandlerContainerInit_authMethodAndCentralConfig(t *testing.T) {
	require := require.New(t)
	h := Handler{
		AuthMethod:           "release-name-consul-k8s-auth-method",
		WriteServiceDefaults: true,
		DefaultProtocol:      "grpc",
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
		},
	}
	container, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.Contains(actual, `
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "foo"
protocol = "grpc"
EOF
/bin/consul login -method="release-name-consul-k8s-auth-method" \
  -bearer-token-file="/var/run/secrets/kubernetes.io/serviceaccount/token" \
  -token-sink-file="/consul/connect-inject/acl-token" \
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
chmod 444 /consul/connect-inject/acl-token
/bin/consul config write -cas -modify-index 0 \
  -token-file="/consul/connect-inject/acl-token" \
  /consul/connect-inject/service-defaults.hcl || true

/bin/consul services register \
  -token-file="/consul/connect-inject/acl-token" \
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  -token-file="/consul/connect-inject/acl-token" \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml
`)
}

// If the default protocol is empty and no protocol is set on the Pod,
// we expect no service-defaults config to be written.
func TestHandlerContainerInit_noDefaultProtocol(t *testing.T) {
	require := require.New(t)
	h := Handler{
		WriteServiceDefaults: true,
		DefaultProtocol:      "",
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
	container, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
	actual := strings.Join(container.Command, " ")
	require.NotContains(actual, `
# Create the service-defaults config for the service
cat <<EOF >/consul/connect-inject/service-defaults.hcl
kind = "service-defaults"
name = "foo"
protocol = ""
EOF`)
	require.NotContains(actual, `
/bin/consul config write -cas -modify-index 0 \
  -token-file="/consul/connect-inject/acl-token" \
  /consul/connect-inject/service-defaults.hcl || true`)
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
	container, err := h.containerInit(pod, k8sNamespace)
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
