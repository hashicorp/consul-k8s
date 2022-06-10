package connectinject

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const (
	InjectInitCopyContainerName = "copy-consul-bin"
	InjectInitContainerName     = "consul-connect-inject-init"
	rootUserAndGroupID          = 0
	envoyUserAndGroupID         = 5995
	copyContainerUserAndGroupID = 5996
	netAdminCapability          = "NET_ADMIN"
	dnsServiceHostEnvSuffix     = "DNS_SERVICE_HOST"
)

type initContainerCommandData struct {
	ServiceName        string
	ServiceAccountName string
	AuthMethod         string
	// ConsulPartition is the Consul admin partition to register the service
	// and proxy in. An empty string indicates partitions are not
	// enabled in Consul (necessary for OSS).
	ConsulPartition string
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
	// PrometheusScrapePath configures the path on the listener on Envoy where
	// Prometheus will scrape metrics from.
	PrometheusScrapePath string
	// PrometheusBackendPort configures where the listener on Envoy will point to.
	PrometheusBackendPort string
	// EnvoyUID is the Linux user id that will be used when tproxy is enabled.
	EnvoyUID int

	// EnableTransparentProxy configures this init container to run in transparent proxy mode,
	// i.e. run consul connect redirect-traffic command and add the required privileges to the
	// container to do that.
	EnableTransparentProxy bool

	// TProxyExcludeInboundPorts is a list of inbound ports to exclude from traffic redirection via
	// the consul connect redirect-traffic command.
	TProxyExcludeInboundPorts []string

	// TProxyExcludeOutboundPorts is a list of outbound ports to exclude from traffic redirection via
	// the consul connect redirect-traffic command.
	TProxyExcludeOutboundPorts []string

	// TProxyExcludeOutboundCIDRs is a list of outbound CIDRs to exclude from traffic redirection via
	// the consul connect redirect-traffic command.
	TProxyExcludeOutboundCIDRs []string

	// TProxyExcludeUIDs is a list of additional user IDs to exclude from traffic redirection via
	// the consul connect redirect-traffic command.
	TProxyExcludeUIDs []string

	// ConsulDNSClusterIP is the IP of the Consul DNS Service.
	ConsulDNSClusterIP string

	// MultiPort determines whether this is a multi port Pod, which configures the init container to be specific to one
	// of the services on the multi port Pod.
	MultiPort bool

	// EnvoyAdminPort configures the admin port of the Envoy sidecar. This will be unique per service in a multi port
	// Pod.
	EnvoyAdminPort int

	// BearerTokenFile configures where the service account token can be found. This will be unique per service in a
	// multi port Pod.
	BearerTokenFile string

	// ConsulAPITimeout is the duration that the consul API client will
	// wait for a response from the API before cancelling the request.
	ConsulAPITimeout time.Duration
}

// initCopyContainer returns the init container spec for the copy container which places
// the consul binary into the shared volume.
func (w *ConnectWebhook) initCopyContainer() corev1.Container {
	// Copy the Consul binary from the image to the shared volume.
	cmd := "cp /bin/consul /consul/connect-inject/consul"
	container := corev1.Container{
		Name:      InjectInitCopyContainerName,
		Image:     w.ImageConsul,
		Resources: w.InitContainerResources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{"/bin/sh", "-ec", cmd},
	}
	// If running on OpenShift, don't set the security context and instead let OpenShift set a random user/group for us.
	if !w.EnableOpenShift {
		container.SecurityContext = &corev1.SecurityContext{
			// Set RunAsUser because the default user for the consul container is root and we want to run non-root.
			RunAsUser:              pointerToInt64(copyContainerUserAndGroupID),
			RunAsGroup:             pointerToInt64(copyContainerUserAndGroupID),
			RunAsNonRoot:           pointerToBool(true),
			ReadOnlyRootFilesystem: pointerToBool(true),
		}
	}
	return container
}

// containerInit returns the init container spec for connect-init that polls for the service and the connect proxy service to be registered
// so that it can save the proxy service id to the shared volume and boostrap Envoy with the proxy-id.
func (w *ConnectWebhook) containerInit(namespace corev1.Namespace, pod corev1.Pod, mpi multiPortInfo) (corev1.Container, error) {
	// Check if tproxy is enabled on this pod.
	tproxyEnabled, err := transparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
	if err != nil {
		return corev1.Container{}, err
	}

	dnsEnabled, err := consulDNSEnabled(namespace, pod, w.EnableConsulDNS)
	if err != nil {
		return corev1.Container{}, err
	}

	var consulDNSClusterIP string
	if dnsEnabled {
		// If Consul DNS is enabled, we find the environment variable that has the value
		// of the ClusterIP of the Consul DNS Service. constructDNSServiceHostName returns
		// the name of the env variable whose value is the ClusterIP of the Consul DNS Service.
		consulDNSClusterIP = os.Getenv(w.constructDNSServiceHostName())
		if consulDNSClusterIP == "" {
			return corev1.Container{}, fmt.Errorf("environment variable %s is not found", w.constructDNSServiceHostName())
		}
	}

	multiPort := mpi.serviceName != ""

	data := initContainerCommandData{
		AuthMethod:                 w.AuthMethod,
		ConsulPartition:            w.ConsulPartition,
		ConsulNamespace:            w.consulNamespace(namespace.Name),
		NamespaceMirroringEnabled:  w.EnableK8SNSMirroring,
		ConsulCACert:               w.ConsulCACert,
		EnableTransparentProxy:     tproxyEnabled,
		TProxyExcludeInboundPorts:  splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeInboundPorts, pod),
		TProxyExcludeOutboundPorts: splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundPorts, pod),
		TProxyExcludeOutboundCIDRs: splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundCIDRs, pod),
		TProxyExcludeUIDs:          splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeUIDs, pod),
		ConsulDNSClusterIP:         consulDNSClusterIP,
		EnvoyUID:                   envoyUserAndGroupID,
		MultiPort:                  multiPort,
		EnvoyAdminPort:             19000 + mpi.serviceIndex,
		ConsulAPITimeout:           w.ConsulAPITimeout,
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	}

	if multiPort {
		data.ServiceName = mpi.serviceName
	} else {
		data.ServiceName = pod.Annotations[annotationService]
	}
	if w.AuthMethod != "" {
		if multiPort {
			// If multi port then we require that the service account name
			// matches the service name.
			data.ServiceAccountName = mpi.serviceName
		} else {
			data.ServiceAccountName = pod.Spec.ServiceAccountName
		}
		// Extract the service account token's volume mount
		saTokenVolumeMount, bearerTokenFile, err := findServiceAccountVolumeMount(pod, multiPort, mpi.serviceName)
		if err != nil {
			return corev1.Container{}, err
		}
		data.BearerTokenFile = bearerTokenFile

		// Append to volume mounts
		volMounts = append(volMounts, saTokenVolumeMount)
	}

	// This determines how to configure the consul connect envoy command: what
	// metrics backend to use and what path to expose on the
	// envoy_prometheus_bind_addr listener for scraping.
	metricsServer, err := w.MetricsConfig.shouldRunMergedMetricsServer(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	if metricsServer {
		prometheusScrapePath := w.MetricsConfig.prometheusScrapePath(pod)
		mergedMetricsPort, err := w.MetricsConfig.mergedMetricsPort(pod)
		if err != nil {
			return corev1.Container{}, err
		}
		data.PrometheusScrapePath = prometheusScrapePath
		data.PrometheusBackendPort = mergedMetricsPort
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		initContainerCommandTpl)))
	err = tpl.Execute(&buf, &data)
	if err != nil {
		return corev1.Container{}, err
	}

	initContainerName := InjectInitContainerName
	if multiPort {
		initContainerName = fmt.Sprintf("%s-%s", InjectInitContainerName, mpi.serviceName)
	}
	container := corev1.Container{
		Name:  initContainerName,
		Image: w.ImageConsulK8S,
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
		Resources:    w.InitContainerResources,
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
	}

	if tproxyEnabled {
		// Running consul connect redirect-traffic with iptables
		// requires both being a root user and having NET_ADMIN capability.
		container.SecurityContext = &corev1.SecurityContext{
			RunAsUser:  pointerToInt64(rootUserAndGroupID),
			RunAsGroup: pointerToInt64(rootUserAndGroupID),
			// RunAsNonRoot overrides any setting in the Pod so that we can still run as root here as required.
			RunAsNonRoot: pointerToBool(false),
			Privileged:   pointerToBool(true),
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{netAdminCapability},
			},
		}
	}

	return container, nil
}

// constructDNSServiceHostName use the resource prefix and the DNS Service hostname suffix to construct the
// key of the env variable whose value is the cluster IP of the Consul DNS Service.
// It translates "resource-prefix" into "RESOURCE_PREFIX_DNS_SERVICE_HOST".
func (w *ConnectWebhook) constructDNSServiceHostName() string {
	upcaseResourcePrefix := strings.ToUpper(w.ResourcePrefix)
	upcaseResourcePrefixWithUnderscores := strings.ReplaceAll(upcaseResourcePrefix, "-", "_")
	return strings.Join([]string{upcaseResourcePrefixWithUnderscores, dnsServiceHostEnvSuffix}, "_")
}

// transparentProxyEnabled returns true if transparent proxy should be enabled for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool or if we are unable
// to read the pod's namespace label when it exists.
func transparentProxyEnabled(namespace corev1.Namespace, pod corev1.Pod, globalEnabled bool) (bool, error) {
	// First check to see if the pod annotation exists to override the namespace or global settings.
	if raw, ok := pod.Annotations[keyTransparentProxy]; ok {
		return strconv.ParseBool(raw)
	}
	// Next see if the namespace has been defaulted.
	if raw, ok := namespace.Labels[keyTransparentProxy]; ok {
		return strconv.ParseBool(raw)
	}
	// Else fall back to the global default.
	return globalEnabled, nil
}

// consulDNSEnabled returns true if Consul DNS should be enabled for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool or if we are unable
// to read the pod's namespace label when it exists.
func consulDNSEnabled(namespace corev1.Namespace, pod corev1.Pod, globalEnabled bool) (bool, error) {
	// First check to see if the pod annotation exists to override the namespace or global settings.
	if raw, ok := pod.Annotations[keyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	// Next see if the namespace has been defaulted.
	if raw, ok := namespace.Labels[keyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	// Else fall back to the global default.
	return globalEnabled, nil
}

// pointerToInt64 takes an int64 and returns a pointer to it.
func pointerToInt64(i int64) *int64 {
	return &i
}

// pointerToBool takes a bool and returns a pointer to it.
func pointerToBool(b bool) *bool {
	return &b
}

// splitCommaSeparatedItemsFromAnnotation takes an annotation and a pod
// and returns the comma-separated value of the annotation as a list of strings.
func splitCommaSeparatedItemsFromAnnotation(annotation string, pod corev1.Pod) []string {
	var items []string
	if raw, ok := pod.Annotations[annotation]; ok {
		items = append(items, strings.Split(raw, ",")...)
	}

	return items
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
consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-api-timeout={{ .ConsulAPITimeout }} \
  {{- if .AuthMethod }}
  -acl-auth-method="{{ .AuthMethod }}" \
  -service-account-name="{{ .ServiceAccountName }}" \
  -service-name="{{ .ServiceName }}" \
  -bearer-token-file={{ .BearerTokenFile }} \
  {{- if .MultiPort }}
  -acl-token-sink=/consul/connect-inject/acl-token-{{ .ServiceName }} \
  {{- end }}
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
  {{- if .MultiPort }}
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-{{ .ServiceName }} \
  {{- if not .AuthMethod }}
  -service-name="{{ .ServiceName }}" \
  {{- end }}
  {{- end }}
  {{- if .ConsulPartition }}
  -partition="{{ .ConsulPartition }}" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -consul-service-namespace="{{ .ConsulNamespace }}" \
  {{- end }}

# Generate the envoy bootstrap code
/consul/connect-inject/consul connect envoy \
  {{- if .MultiPort }}
  -proxy-id="$(cat /consul/connect-inject/proxyid-{{.ServiceName}})" \
  {{- else }}
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  {{- end }}
  {{- if .PrometheusScrapePath }}
  -prometheus-scrape-path="{{ .PrometheusScrapePath }}" \
  {{- end }}
  {{- if .PrometheusBackendPort }}
  -prometheus-backend-port="{{ .PrometheusBackendPort }}" \
  {{- end }}
  {{- if .AuthMethod }}
  {{- if .MultiPort }}
  -token-file="/consul/connect-inject/acl-token-{{ .ServiceName }}" \
  {{- else }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  {{- end }}
  {{- if .ConsulPartition }}
  -partition="{{ .ConsulPartition }}" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  {{- if .MultiPort }}
  -admin-bind=127.0.0.1:{{ .EnvoyAdminPort }} \
  {{- end }}
  -bootstrap > {{ if .MultiPort }}/consul/connect-inject/envoy-bootstrap-{{.ServiceName}}.yaml{{ else }}/consul/connect-inject/envoy-bootstrap.yaml{{ end }}


{{- if .EnableTransparentProxy }}
{{- /* The newline below is intentional to allow extra space
       in the rendered template between this and the previous commands. */}}

# Apply traffic redirection rules.
/consul/connect-inject/consul connect redirect-traffic \
  {{- if .AuthMethod }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  {{- if .ConsulPartition }}
  -partition="{{ .ConsulPartition }}" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  {{- if .ConsulDNSClusterIP }}
  -consul-dns-ip="{{ .ConsulDNSClusterIP }}" \
  {{- end }}
  {{- range .TProxyExcludeInboundPorts }}
  -exclude-inbound-port="{{ . }}" \
  {{- end }}
  {{- range .TProxyExcludeOutboundPorts }}
  -exclude-outbound-port="{{ . }}" \
  {{- end }}
  {{- range .TProxyExcludeOutboundCIDRs }}
  -exclude-outbound-cidr="{{ . }}" \
  {{- end }}
  {{- range .TProxyExcludeUIDs }}
  -exclude-uid="{{ . }}" \
  {{- end }}
  -proxy-id="$(cat /consul/connect-inject/proxyid)" \
  -proxy-uid={{ .EnvoyUID }}
{{- end }}
`
