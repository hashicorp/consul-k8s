package connectinject

import (
	"bytes"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
)

type initContainerCommandData struct {
	PodName     string
	ServiceName string
	ServicePort int32
	Upstreams   []initContainerCommandUpstreamData
}

type initContainerCommandUpstreamData struct {
	Name      string
	LocalPort int32
}

// containerInit returns the init container spec for registering the Consul
// service, setting up the Envoy bootstrap, etc.
func (h *Handler) containerInit(pod *corev1.Pod) (corev1.Container, error) {
	data := initContainerCommandData{
		PodName:     pod.Name,
		ServiceName: pod.Annotations[annotationService],
	}
	if data.ServiceName == "" {
		// Assertion, since we call defaultAnnotations above and do
		// not mutate pods without a service specified.
		panic("No service found. This should be impossible since we default it.")
	}

	// If a port is specified, then we determine the value of that port
	// and register that port for the host service.
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		if port, _ := portValue(pod, raw); port > 0 {
			data.ServicePort = port
		}
	}

	// If upstreams are specified, configure those
	if raw, ok := pod.Annotations[annotationUpstreams]; ok && raw != "" {
		for _, raw := range strings.Split(raw, ",") {
			parts := strings.SplitN(raw, ":", 2)
			port, _ := portValue(pod, strings.TrimSpace(parts[1]))
			if port > 0 {
				data.Upstreams = append(data.Upstreams, initContainerCommandUpstreamData{
					Name:      strings.TrimSpace(parts[0]),
					LocalPort: port,
				})
			}
		}
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		initContainerCommandTpl)))
	err := tpl.Execute(&buf, &data)
	if err != nil {
		return corev1.Container{}, err
	}

	return corev1.Container{
		Name:  "consul-connect-inject-init",
		Image: h.ImageConsul,
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{"/bin/sh", "-ec", buf.String()},
	}, nil
}

// initContainerCommandTpl is the template for the command executed by
// the init container.
const initContainerCommandTpl = `
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "{{ .PodName }}-{{ .ServiceName }}"
  name = "{{ .ServiceName }}"
  {{ if (gt .ServicePort 0) -}}
  port = {{ .ServicePort }}
  {{ end -}}

  connect {
    sidecar_service {
      checks {
        name = "Connect Sidecar Alias"
        alias_service = "{{ .ServiceName }}"
      }

      proxy {
        {{ range .Upstreams -}}
        upstreams {
          destination_name = "{{ .Name }}"
          local_bind_port = {{ .LocalPort }}
        }
        {{ end }}
      }
    }
  }
}
EOF

/bin/consul services register /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/bin/consul connect envoy \
  -sidecar-for={{ .ServiceName }} \
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml

# Copy the Consul binary
cp /bin/consul /consul/connect-inject/consul
`
