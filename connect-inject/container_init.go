package connectinject

import (
	"bytes"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
)

const (
	InjectInitCopyContainerName = "copy-consul-bin"
	InjectInitContainerName     = "consul-connect-inject-init"
)

type initContainerCommandData struct {
	ServiceName        string
	ServiceAccountName string
	AuthMethod         string
	// ConsulNamespace is the Consul namespace to register the service
	// and proxy in. An empty string indicates namespaces are not
	// enabled in Consul (necessary for OSS).
	ConsulNamespace           string
	NamespaceMirroringEnabled bool

	// The PEM-encoded CA certificate to use when
	// communicating with Consul clients
	ConsulCACert string
	// EnableMetrics adds a listener to Envoy where Prometheus will scrape
	// metrics from.
	EnableMetrics bool
	// todo: remove once endpoints controller supports metrics
	// PrometheusScrapeListener configures the listener on Envoy where
	// Prometheus will scrape metrics from.
	PrometheusScrapeListener string
	// PrometheusScrapePath configures the path on the listener on Envoy where
	// Prometheus will scrape metrics from.
	PrometheusScrapePath string
	// PrometheusBackendPort configures where the listener on Envoy will point to.
	PrometheusBackendPort string
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
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{"/bin/sh", "-ec", cmd},
	}
}

// containerInit returns the init container spec for registering the Consul
// service, setting up the Envoy bootstrap, etc.
func (h *Handler) containerInit(pod corev1.Pod, k8sNamespace string) (corev1.Container, error) {
	data := initContainerCommandData{
		AuthMethod:                h.AuthMethod,
		ConsulNamespace:           h.consulNamespace(k8sNamespace),
		NamespaceMirroringEnabled: h.EnableK8SNSMirroring,
		ConsulCACert:              h.ConsulCACert,
	}

	if data.AuthMethod != "" {
		data.ServiceAccountName = pod.Spec.ServiceAccountName
		data.ServiceName = pod.Annotations[annotationService]
	}

	// This determines how to configure the consul connect envoy command: what
	// metrics backend to use and what path to expose on the
	// envoy_prometheus_bind_addr listener for scraping.
	metricsServer, err := h.MetricsConfig.shouldRunMergedMetricsServer(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	if metricsServer {
		prometheusScrapePath := h.MetricsConfig.prometheusScrapePath(pod)
		mergedMetricsPort, err := h.MetricsConfig.mergedMetricsPort(pod)
		if err != nil {
			return corev1.Container{}, err
		}
		data.PrometheusScrapePath = prometheusScrapePath
		data.PrometheusBackendPort = mergedMetricsPort
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		{
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
		},
		Resources:    h.InitContainerResources,
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
	}, nil
}

// initContainerCommandTpl is the template for the command executed by
// the init container.
const initContainerCommandTpl = `
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
consul-k8s connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  {{- if .AuthMethod }}
  -acl-auth-method="{{ .AuthMethod }}" \
  -service-account-name="{{ .ServiceAccountName }}" \
  -service-name="{{ .ServiceName }}" \
  {{- if .ConsulNamespace }}
  {{- if .NamespaceMirroringEnabled }}
  {{- /* If namespace mirroring is enabled, the auth method is
         defined in the default namespace */}}
  -auth-method-namespace="default" \
  {{- else }}
  -auth-method-namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  {{- end }}
  {{- end }}
  {{- if .ConsulNamespace }}
  -consul-service-namespace="{{ .ConsulNamespace }}" \
  {{- end }}

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
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
