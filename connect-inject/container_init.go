package connectinject

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	capi "github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
)

const (
	InjectInitCopyContainerName = "copy-consul-bin"
	InjectInitContainerName     = "consul-connect-inject-init"
	MetaKeyPodName              = "pod-name"
	MetaKeyKubeNS               = "k8s-namespace"
)

type initContainerCommandData struct {
	ServiceName      string
	ProxyServiceName string
	ServicePort      int32
	// ServiceProtocol is the protocol for the service-defaults config
	// that will be written if WriteServiceDefaults is true.
	ServiceProtocol string
	AuthMethod      string
	// WriteServiceDefaults controls whether a service-defaults config is
	// written for this service.
	WriteServiceDefaults bool
	// ConsulNamespace is the Consul namespace to register the service
	// and proxy in. An empty string indicates namespaces are not
	// enabled in Consul (necessary for OSS).
	ConsulNamespace           string
	NamespaceMirroringEnabled bool
	Upstreams                 []initContainerCommandUpstreamData
	Tags                      string
	Meta                      map[string]string
	MetaKeyPodName            string
	MetaKeyKubeNS             string

	// The PEM-encoded CA certificate to use when
	// communicating with Consul clients
	ConsulCACert string
	// EnableMetrics adds a listener to Envoy where Prometheus will scrape
	// metrics from.
	EnableMetrics bool
	// PrometheusScrapeListener configures the listener on Envoy where
	// Prometheus will scrape metrics from.
	PrometheusScrapeListener string
	// PrometheusScrapePath configures the path on the listener on Envoy where
	// Prometheus will scrape metrics from.
	PrometheusScrapePath string
	// PrometheusBackendPort configures where the listener on Envoy will point to.
	PrometheusBackendPort string
}

type initContainerCommandUpstreamData struct {
	Name                    string
	LocalPort               int32
	ConsulUpstreamNamespace string
	Datacenter              string
	Query                   string
}

// containerInitCopyContainer returns the init container spec for the copy container which places
// the consul binary into the shared volume.
func (h *Handler) containerInitCopyContainer() corev1.Container {
	// Copy the Consul binary from the image to the shared volume.
	cmd := "cp /bin/consul /consul/connect-inject/consul"
	return corev1.Container{
		Name:      InjectInitCopyContainerName,
		Image:     h.ImageConsul,
		Resources: h.InitContainerResources,
		VolumeMounts: []corev1.VolumeMount{
			corev1.VolumeMount{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{"/bin/sh", "-ec", cmd},
	}
}

// containerInit returns the init container spec for registering the Consul
// service, setting up the Envoy bootstrap, etc.
func (h *Handler) containerInit(pod *corev1.Pod, k8sNamespace string) (corev1.Container, error) {
	data := initContainerCommandData{
		ServiceName:               pod.Annotations[annotationService],
		ProxyServiceName:          fmt.Sprintf("%s-sidecar-proxy", pod.Annotations[annotationService]),
		AuthMethod:                h.AuthMethod,
		ConsulNamespace:           h.consulNamespace(k8sNamespace),
		NamespaceMirroringEnabled: h.EnableK8SNSMirroring,
		ConsulCACert:              h.ConsulCACert,
		MetaKeyPodName:            MetaKeyPodName,
		MetaKeyKubeNS:             MetaKeyKubeNS,
	}
	if data.ServiceName == "" {
		// Assertion, since we call defaultAnnotations above and do
		// not mutate pods without a service specified.
		panic("No service found. This should be impossible since we default it.")
	}

	// When ACLs are enabled, the ACL token returned from `consul login` is only
	// valid for a service with the same name as the ServiceAccountName.
	if data.AuthMethod != "" && data.ServiceName != pod.Spec.ServiceAccountName {
		return corev1.Container{}, fmt.Errorf("serviceAccountName %q does not match service name %q", pod.Spec.ServiceAccountName, data.ServiceName)
	}

	// If a port is specified, then we determine the value of that port
	// and register that port for the host service.
	if raw, ok := pod.Annotations[annotationPort]; ok && raw != "" {
		if port, _ := portValue(pod, raw); port > 0 {
			data.ServicePort = port
		}
	}

	// If metrics are enabled, the init container should set up
	// envoy_prometheus_bind_addr so there's a listener on 0.0.0.0 that points
	// to a metrics backend. The metrics backend is determined by the call to
	// h.shouldRunMergedMetricsServer(). If there is a merged metrics server,
	// the backend would be that server. If we are not running the merged
	// metrics server, the backend should just be the Envoy metrics endpoint.
	enableMetrics, err := h.enableMetrics(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	data.EnableMetrics = enableMetrics

	prometheusScrapePort, err := h.prometheusScrapePort(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	data.PrometheusScrapeListener = fmt.Sprintf("0.0.0.0:%s", prometheusScrapePort)

	// This determines how to configure the consul connect envoy command: what
	// metrics backend to use and what path to expose on the
	// envoy_prometheus_bind_addr listener for scraping.
	run, err := h.shouldRunMergedMetricsServer(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	if run {
		prometheusScrapePath := h.prometheusScrapePath(pod)
		mergedMetricsPort, err := h.mergedMetricsPort(pod)
		if err != nil {
			return corev1.Container{}, err
		}
		data.PrometheusScrapePath = prometheusScrapePath
		data.PrometheusBackendPort = mergedMetricsPort
	}

	var tags []string
	if raw, ok := pod.Annotations[annotationTags]; ok && raw != "" {
		tags = strings.Split(raw, ",")
	}
	// Get the tags from the deprecated tags annotation and combine.
	if raw, ok := pod.Annotations[annotationConnectTags]; ok && raw != "" {
		tags = append(tags, strings.Split(raw, ",")...)
	}

	if len(tags) > 0 {
		// Create json array from the annotations since we're going to output
		// this in an HCL config file and HCL arrays are json formatted.
		jsonTags, err := json.Marshal(tags)
		if err != nil {
			h.Log.Error("Error json marshaling tags", "err", err, "Tags", tags)
		} else {
			data.Tags = string(jsonTags)
		}
	}

	// If there is metadata specified split into a map and create.
	data.Meta = make(map[string]string)
	for k, v := range pod.Annotations {
		if strings.HasPrefix(k, annotationMeta) && strings.TrimPrefix(k, annotationMeta) != "" {
			data.Meta[strings.TrimPrefix(k, annotationMeta)] = v
		}
	}

	// If upstreams are specified, configure those
	if raw, ok := pod.Annotations[annotationUpstreams]; ok && raw != "" {
		for _, raw := range strings.Split(raw, ",") {
			parts := strings.SplitN(raw, ":", 3)

			var datacenter, service_name, prepared_query, namespace string
			var port int32
			if strings.TrimSpace(parts[0]) == "prepared_query" {
				port, _ = portValue(pod, strings.TrimSpace(parts[2]))
				prepared_query = strings.TrimSpace(parts[1])
			} else {
				port, _ = portValue(pod, strings.TrimSpace(parts[1]))

				// Parse the namespace if provided
				if data.ConsulNamespace != "" {
					pieces := strings.SplitN(parts[0], ".", 2)
					service_name = pieces[0]

					if len(pieces) > 1 {
						namespace = pieces[1]
					}
				} else {
					service_name = strings.TrimSpace(parts[0])
				}

				// parse the optional datacenter
				if len(parts) > 2 {
					datacenter = strings.TrimSpace(parts[2])

					// Check if there's a proxy defaults config with mesh gateway
					// mode set to local or remote. This helps users from
					// accidentally forgetting to set a mesh gateway mode
					// and then being confused as to why their traffic isn't
					// routing.
					entry, _, err := h.ConsulClient.ConfigEntries().Get(capi.ProxyDefaults, capi.ProxyConfigGlobal, nil)
					if err != nil && strings.Contains(err.Error(), "Unexpected response code: 404") {
						return corev1.Container{}, fmt.Errorf("upstream %q is invalid: there is no ProxyDefaults config to set mesh gateway mode", raw)
					} else if err == nil {
						mode := entry.(*capi.ProxyConfigEntry).MeshGateway.Mode
						if mode != capi.MeshGatewayModeLocal && mode != capi.MeshGatewayModeRemote {
							return corev1.Container{}, fmt.Errorf("upstream %q is invalid: ProxyDefaults mesh gateway mode is neither %q nor %q", raw, capi.MeshGatewayModeLocal, capi.MeshGatewayModeRemote)
						}
					}
					// NOTE: If we can't reach Consul we don't error out because
					// that would fail the pod scheduling and this is a nice-to-have
					// check, not something that should block during a Consul hiccup.
				}
			}

			if port > 0 {
				upstream := initContainerCommandUpstreamData{
					Name:       service_name,
					LocalPort:  port,
					Datacenter: datacenter,
					Query:      prepared_query,
				}

				// Add namespace to upstream
				if namespace != "" {
					upstream.ConsulUpstreamNamespace = namespace
				}

				data.Upstreams = append(data.Upstreams, upstream)
			}
		}
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	}

	if h.AuthMethod != "" {
		// Extract the service account token's volume mount
		saTokenVolumeMount, err := findServiceAccountVolumeMount(pod)
		if err != nil {
			return corev1.Container{}, err
		}

		// Append to volume mounts
		volMounts = append(volMounts, saTokenVolumeMount)
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		initContainerCommandTpl)))
	err = tpl.Execute(&buf, &data)
	if err != nil {
		return corev1.Container{}, err
	}

	return corev1.Container{
		Name:  InjectInitContainerName,
		Image: h.ImageConsulK8S,
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
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			{
				Name:  "SERVICE_ID",
				Value: fmt.Sprintf("$(POD_NAME)-%s", data.ServiceName),
			},
			{
				Name:  "PROXY_SERVICE_ID",
				Value: fmt.Sprintf("$(POD_NAME)-%s", data.ProxyServiceName),
			},
		},
		Resources:    h.InitContainerResources,
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
	}, nil
}

// initContainerCommandTpl is the template for the command executed by
// the init container.
// Note: the order of the services in the service.hcl file is important,
// and the connect-proxy service should come after the "main" service
// because its alias health check depends on the main service to exist.
const initContainerCommandTpl = `
{{- if .AuthMethod }}
consul-k8s connect-init -method="{{ .AuthMethod }}" \
  {{- if.ConsulNamespace }}
  {{- if .NamespaceMirroringEnabled }}
  {{- /* If namespace mirroring is enabled, the auth method is
         defined in the default namespace */}}
  -namespace="default" \
  {{- else }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  {{- end }}
  -meta="pod=${POD_NAMESPACE}/${POD_NAME}"
{{- end }}
{{- if .ConsulCACert}}
export CONSUL_HTTP_ADDR="https://${HOST_IP}:8501"
export CONSUL_GRPC_ADDR="https://${HOST_IP}:8502"
export CONSUL_CACERT=/consul/connect-inject/consul-ca.pem
cat <<EOF >/consul/connect-inject/consul-ca.pem
{{ .ConsulCACert }}
EOF
{{- else}}
export CONSUL_HTTP_ADDR="${HOST_IP}:8500"
export CONSUL_GRPC_ADDR="${HOST_IP}:8502"
{{- end}}

# Register the service. The HCL is stored in the volume so that
# the preStop hook can access it to deregister the service.
cat <<EOF >/consul/connect-inject/service.hcl
services {
  id   = "${SERVICE_ID}"
  name = "{{ .ServiceName }}"
  address = "${POD_IP}"
  port = {{ .ServicePort }}
  {{- if .ConsulNamespace }}
  namespace = "{{ .ConsulNamespace }}"
  {{- end }}
  {{- if .Tags}}
  tags = {{.Tags}}
  {{- end}}
  meta = {
    {{- if .Meta}}
    {{- range $key, $value := .Meta }}
    {{$key}} = "{{$value}}"
    {{- end }}
    {{- end }}
    {{ .MetaKeyPodName }} = "${POD_NAME}"
    {{ .MetaKeyKubeNS }} = "${POD_NAMESPACE}"
  }
}

services {
  id   = "${PROXY_SERVICE_ID}"
  name = "{{ .ProxyServiceName }}"
  kind = "connect-proxy"
  address = "${POD_IP}"
  port = 20000
  {{- if .ConsulNamespace }}
  namespace = "{{ .ConsulNamespace }}"
  {{- end }}
  {{- if .Tags}}
  tags = {{.Tags}}
  {{- end}}
  meta = {
    {{- if .Meta}}
    {{- range $key, $value := .Meta }}
    {{$key}} = "{{$value}}"
    {{- end }}
    {{- end }}
    {{ .MetaKeyPodName }} = "${POD_NAME}"
    {{ .MetaKeyKubeNS }} = "${POD_NAMESPACE}"
  }

  proxy {
	{{- if .EnableMetrics }}
	config {
	  envoy_prometheus_bind_addr = "{{ .PrometheusScrapeListener }}"
	}
	{{- end }}
    destination_service_name = "{{ .ServiceName }}"
    destination_service_id = "${SERVICE_ID}"
    {{- if (gt .ServicePort 0) }}
    local_service_address = "127.0.0.1"
    local_service_port = {{ .ServicePort }}
    {{- end }}
    {{- range .Upstreams }}
    upstreams {
      {{- if .Name }}
      destination_type = "service" 
      destination_name = "{{ .Name }}"
      {{- end}}
      {{- if .Query }}
      destination_type = "prepared_query" 
      destination_name = "{{ .Query}}"
      {{- end}}
      {{- if .ConsulUpstreamNamespace }}
      destination_namespace = "{{ .ConsulUpstreamNamespace }}"
      {{- end}}
      local_bind_port = {{ .LocalPort }}
      {{- if .Datacenter }}
      datacenter = "{{ .Datacenter }}"
      {{- end}}
    }
    {{- end }}
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
/consul/connect-inject/consul services register \
  {{- if .AuthMethod }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  /consul/connect-inject/service.hcl

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="${PROXY_SERVICE_ID}" \
  {{- if .PrometheusScrapePath }}
  -prometheus-scrape-path="{{ .PrometheusScrapePath }}" \
  {{- end }}
  {{- if .PrometheusBackendPort }}
  -prometheus-backend-port="{{ .PrometheusBackendPort }}" \
  {{- end }}
  {{- if .AuthMethod }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  -bootstrap > /consul/connect-inject/envoy-bootstrap.yaml`
