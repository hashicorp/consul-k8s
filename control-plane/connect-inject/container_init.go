package connectinject

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

const (
	InjectInitCopyContainerName  = "copy-consul-bin"
	InjectInitContainerName      = "consul-connect-inject-init"
	rootUserAndGroupID           = 0
	sidecarUserAndGroupID        = 5995
	initContainersUserAndGroupID = 5996
	netAdminCapability           = "NET_ADMIN"
	dnsServiceHostEnvSuffix      = "DNS_SERVICE_HOST"
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
	ConsulNamespace string

	// ConsulNodeName is the node name in Consul where services are registered.
	ConsulNodeName string

	// EnvoyUID is the Linux user id that will be used when tproxy is enabled.
	EnvoyUID int

	// EnableTransparentProxy configures this init container to run in transparent proxy mode,
	// i.e. run consul connect redirect-traffic command and add the required privileges to the
	// container to do that.
	EnableTransparentProxy bool

	// EnableCNI configures this init container to skip the redirect-traffic command as traffic
	// redirection is handled by the CNI plugin on pod creation.
	EnableCNI bool

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

	// Log settings for the connect-init command.
	LogLevel string
	LogJSON  bool
}

// initCopyContainer returns the init container spec for the copy container which places
// the consul binary into the shared volume.
func (w *MeshWebhook) initCopyContainer() corev1.Container {
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
			RunAsUser:              pointer.Int64(initContainersUserAndGroupID),
			RunAsGroup:             pointer.Int64(initContainersUserAndGroupID),
			RunAsNonRoot:           pointer.Bool(true),
			ReadOnlyRootFilesystem: pointer.Bool(true),
		}
	}
	return container
}

// containerInit returns the init container spec for connect-init that polls for the service and the connect proxy service to be registered
// so that it can save the proxy service id to the shared volume and boostrap Envoy with the proxy-id.
func (w *MeshWebhook) containerInit(namespace corev1.Namespace, pod corev1.Pod, mpi multiPortInfo) (corev1.Container, error) {
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
		ConsulNodeName:             ConsulNodeName,
		EnableTransparentProxy:     tproxyEnabled,
		EnableCNI:                  w.EnableCNI,
		TProxyExcludeInboundPorts:  splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeInboundPorts, pod),
		TProxyExcludeOutboundPorts: splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundPorts, pod),
		TProxyExcludeOutboundCIDRs: splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundCIDRs, pod),
		TProxyExcludeUIDs:          splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeUIDs, pod),
		ConsulDNSClusterIP:         consulDNSClusterIP,
		EnvoyUID:                   sidecarUserAndGroupID,
		MultiPort:                  multiPort,
		LogLevel:                   w.LogLevel,
		LogJSON:                    w.LogJSON,
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
	var bearerTokenFile string
	if w.AuthMethod != "" {
		if multiPort {
			// If multi port then we require that the service account name
			// matches the service name.
			data.ServiceAccountName = mpi.serviceName
		} else {
			data.ServiceAccountName = pod.Spec.ServiceAccountName
		}
		// Extract the service account token's volume mount
		var saTokenVolumeMount corev1.VolumeMount
		saTokenVolumeMount, bearerTokenFile, err = findServiceAccountVolumeMount(pod, mpi.serviceName)
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

	initContainerName := InjectInitContainerName
	if multiPort {
		initContainerName = fmt.Sprintf("%s-%s", InjectInitContainerName, mpi.serviceName)
	}
	container := corev1.Container{
		Name:  initContainerName,
		Image: w.ImageConsulK8S,
		Env: []corev1.EnvVar{
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
				Name:  "CONSUL_ADDRESSES",
				Value: w.ConsulAddress,
			},
			{
				Name:  "CONSUL_GRPC_PORT",
				Value: strconv.Itoa(w.ConsulConfig.GRPCPort),
			},
			{
				Name:  "CONSUL_HTTP_PORT",
				Value: strconv.Itoa(w.ConsulConfig.HTTPPort),
			},
			{
				Name:  "CONSUL_API_TIMEOUT",
				Value: w.ConsulConfig.APITimeout.String(),
			},
		},
		Resources:    w.InitContainerResources,
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
	}

	if w.TLSEnabled {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_USE_TLS",
				Value: "true",
			},
			corev1.EnvVar{
				Name:  "CONSUL_CACERT_PEM",
				Value: w.ConsulCACert,
			},
			corev1.EnvVar{
				Name:  "CONSUL_TLS_SERVER_NAME",
				Value: w.ConsulTLSServerName,
			})
	}

	if w.AuthMethod != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_AUTH_METHOD",
				Value: w.AuthMethod,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
				Value: bearerTokenFile,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_META",
				Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
			})

		if w.EnableNamespaces {
			if w.EnableK8SNSMirroring {
				container.Env = append(container.Env,
					corev1.EnvVar{
						Name:  "CONSUL_LOGIN_NAMESPACE",
						Value: "default",
					})
			} else {
				container.Env = append(container.Env,
					corev1.EnvVar{
						Name:  "CONSUL_LOGIN_NAMESPACE",
						Value: w.consulNamespace(namespace.Name),
					})
			}
		}

		if w.ConsulPartition != "" {
			container.Env = append(container.Env,
				corev1.EnvVar{
					Name:  "CONSUL_LOGIN_PARTITION",
					Value: w.ConsulPartition,
				})
		}
	}
	if w.EnableNamespaces {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_NAMESPACE",
				Value: w.consulNamespace(namespace.Name),
			})
	}

	if w.ConsulPartition != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_PARTITION",
				Value: w.ConsulPartition,
			})
	}

	if tproxyEnabled {
		// Running consul connect redirect-traffic with iptables
		// requires both being a root user and having NET_ADMIN capability.
		if !w.EnableCNI {
			container.SecurityContext = &corev1.SecurityContext{
				RunAsUser:  pointer.Int64(rootUserAndGroupID),
				RunAsGroup: pointer.Int64(rootUserAndGroupID),
				// RunAsNonRoot overrides any setting in the Pod so that we can still run as root here as required.
				RunAsNonRoot: pointer.Bool(false),
				Privileged:   pointer.Bool(true),
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{netAdminCapability},
				},
			}
		} else {
			container.SecurityContext = &corev1.SecurityContext{
				RunAsUser:    pointer.Int64(initContainersUserAndGroupID),
				RunAsGroup:   pointer.Int64(initContainersUserAndGroupID),
				RunAsNonRoot: pointer.Bool(true),
				Privileged:   pointer.Bool(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			}
		}
	}

	return container, nil
}

// constructDNSServiceHostName use the resource prefix and the DNS Service hostname suffix to construct the
// key of the env variable whose value is the cluster IP of the Consul DNS Service.
// It translates "resource-prefix" into "RESOURCE_PREFIX_DNS_SERVICE_HOST".
func (w *MeshWebhook) constructDNSServiceHostName() string {
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
consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
  -consul-node-name={{ .ConsulNodeName }} \
  -log-level={{ .LogLevel }} \
  -log-json={{ .LogJSON }} \
  {{- if .AuthMethod }}
  -service-account-name="{{ .ServiceAccountName }}" \
  -service-name="{{ .ServiceName }}" \
  {{- end }}
  {{- if .MultiPort }}
  -multiport=true \
  -proxy-id-file=/consul/connect-inject/proxyid-{{ .ServiceName }} \
  {{- if not .AuthMethod }}
  -service-name="{{ .ServiceName }}" \
  {{- end }}
  {{- end }}

{{- if .EnableTransparentProxy }}
{{- if not .EnableCNI }}
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
{{- end }}
`
