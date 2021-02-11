package connectinject

import (
	"fmt"
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }

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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
  }
}
`,
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
			"Multiple upstream services",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "db:1234, db:2345, db:3456"
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
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      local_bind_port = 2345
    }
    upstreams {
      destination_type = "service" 
      destination_name = "db"
      local_bind_port = 3456
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
			"Upstream prepared queries and non-query",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				pod.Annotations[annotationUpstreams] = "prepared_query:handle:8200, servicename:8201, prepared_query:6687bd19-5654-76be-d764:8202"
				return pod
			},
			`proxy {
    destination_service_name = "web"
    destination_service_id = "${SERVICE_ID}"
    upstreams {
      destination_type = "prepared_query" 
      destination_name = "handle"
      local_bind_port = 8200
    }
    upstreams {
      destination_type = "service" 
      destination_name = "servicename"
      local_bind_port = 8201
    }
    upstreams {
      destination_type = "prepared_query" 
      destination_name = "6687bd19-5654-76be-d764"
      local_bind_port = 8202
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  tags = ["abc","123","abc","123","def","456"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  tags = ["abc","123","abc","123","def","456"]
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 1234
  meta = {
    name = "abc"
    version = "2"
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  meta = {
    name = "abc"
    version = "2"
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
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
    alias_service = "${SERVICE_ID}"
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
			`  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
`,
			"",
		},

		{
			"Central config",
			func(pod *corev1.Pod) *corev1.Pod {
				pod.Annotations[annotationService] = "web"
				return pod
			},
			`  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
`,
			"",
		},
	}

	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			require := require.New(t)

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

			h := Handler{
				ConsulClient: consulClient,
			}
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }

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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "non-default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "non-default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }

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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "non-default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "non-default"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }

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
    alias_service = "${SERVICE_ID}"
  }
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
  id   = "${SERVICE_ID}"
  name = "web"
  address = "${POD_IP}"
  port = 0
  namespace = "k8snamespace"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "web-sidecar-proxy"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  namespace = "k8snamespace"
  meta = {
    pod-name = "${POD_NAME}"
    k8s-namespace = "${POD_NAMESPACE}"
  }

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
    alias_service = "${SERVICE_ID}"
  }
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
	container, err := h.containerInit(pod, k8sNamespace)
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

	_, err := h.containerInit(pod, k8sNamespace)
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

	_, err := h.containerInit(pod, k8sNamespace)
	require.NoError(err)
}

// Test errors for when the mesh gateway mode isn't local or remote and an
// upstream is using a datacenter.
func TestHandlerContainerInit_MeshGatewayModeErrors(t *testing.T) {
	cases := map[string]struct {
		ConsulDown         bool
		UpstreamAnnotation string
		ProxyDefaults      *capi.ProxyConfigEntry
		ExpError           string
	}{
		"no upstreams": {
			UpstreamAnnotation: "",
			ProxyDefaults:      nil,
			ExpError:           "",
		},
		"upstreams without datacenter": {
			UpstreamAnnotation: "foo:1234,bar:4567",
			ProxyDefaults:      nil,
			ExpError:           "",
		},
		"no proxy defaults": {
			UpstreamAnnotation: "foo:1234:dc2",
			ProxyDefaults:      nil,
			ExpError:           "upstream \"foo:1234:dc2\" is invalid: there is no ProxyDefaults config to set mesh gateway mode",
		},
		"consul is down but upstream does not have datacenter": {
			ConsulDown:         true,
			UpstreamAnnotation: "foo:1234",
			ExpError:           "",
		},
		"consul is down": {
			ConsulDown:         true,
			UpstreamAnnotation: "foo:1234:dc2",
			ExpError:           "",
		},
		"mesh gateway mode is empty": {
			UpstreamAnnotation: "foo:1234:dc2",
			ProxyDefaults: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: "",
				},
			},
			ExpError: "upstream \"foo:1234:dc2\" is invalid: ProxyDefaults mesh gateway mode is neither \"local\" nor \"remote\"",
		},
		"mesh gateway mode is none": {
			UpstreamAnnotation: "foo:1234:dc2",
			ProxyDefaults: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeNone,
				},
			},
			ExpError: "upstream \"foo:1234:dc2\" is invalid: ProxyDefaults mesh gateway mode is neither \"local\" nor \"remote\"",
		},
		"mesh gateway mode is local": {
			UpstreamAnnotation: "foo:1234:dc2",
			ProxyDefaults: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
			},
			ExpError: "",
		},
		"mesh gateway mode is remote": {
			UpstreamAnnotation: "foo:1234:dc2",
			ProxyDefaults: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: capi.ProxyConfigGlobal,
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
			},
			ExpError: "",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(err)
			defer consul.Stop()
			consul.WaitForLeader(t)

			httpAddr := consul.HTTPAddr
			if c.ConsulDown {
				httpAddr = "hostname.does.not.exist"
			}
			consulClient, err := capi.NewClient(&capi.Config{
				Address: httpAddr,
			})
			require.NoError(err)

			if c.ProxyDefaults != nil {
				written, _, err := consulClient.ConfigEntries().Set(c.ProxyDefaults, nil)
				require.NoError(err)
				require.True(written)
			}

			h := Handler{
				ConsulClient: consulClient,
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotationService:   "foo",
						annotationUpstreams: c.UpstreamAnnotation,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
						},
					},
				},
			}
			_, err = h.containerInit(pod, k8sNamespace)
			if c.ExpError == "" {
				require.NoError(err)
			} else {
				require.EqualError(err, c.ExpError)
			}
		})
	}

}
