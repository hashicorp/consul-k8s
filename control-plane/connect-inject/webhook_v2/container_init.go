// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook_v2

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	injectInitContainerName      = "consul-mesh-init"
	rootUserAndGroupID           = 0
	sidecarUserAndGroupID        = 5995
	initContainersUserAndGroupID = 5996
	netAdminCapability           = "NET_ADMIN"
)

type initContainerCommandData struct {
	ServiceName        string
	ServiceAccountName string
	AuthMethod         string

	// Log settings for the connect-init command.
	LogLevel string
	LogJSON  bool
}

// containerInit returns the init container spec for connect-init that polls for the service and the connect proxy service to be registered
// so that it can save the proxy service id to the shared volume and boostrap Envoy with the proxy-id.
func (w *MeshWebhook) containerInit(namespace corev1.Namespace, pod corev1.Pod) (corev1.Container, error) {
	// Check if tproxy is enabled on this pod.
	tproxyEnabled, err := common.TransparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
	if err != nil {
		return corev1.Container{}, err
	}

	data := initContainerCommandData{
		AuthMethod: w.AuthMethod,
		LogLevel:   w.LogLevel,
		LogJSON:    w.LogJSON,
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	}

	data.ServiceName = pod.Annotations[constants.AnnotationService]
	var bearerTokenFile string
	if w.AuthMethod != "" {
		data.ServiceAccountName = pod.Spec.ServiceAccountName
		// Extract the service account token's volume mount
		var saTokenVolumeMount corev1.VolumeMount
		saTokenVolumeMount, bearerTokenFile, err = findServiceAccountVolumeMount(pod)
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

	initContainerName := injectInitContainerName
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
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
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
			{
				Name:  "CONSUL_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
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

	// OpenShift without CNI is the only environment where privileged must be true.
	privileged := false
	if w.EnableOpenShift && !w.EnableCNI {
		privileged = true
	}

	if tproxyEnabled {
		if !w.EnableCNI {
			// Set redirect traffic config for the container so that we can apply iptables rules.
			redirectTrafficConfig, err := w.iptablesConfigJSON(pod, namespace)
			if err != nil {
				return corev1.Container{}, err
			}
			container.Env = append(container.Env,
				corev1.EnvVar{
					Name:  "CONSUL_REDIRECT_TRAFFIC_CONFIG",
					Value: redirectTrafficConfig,
				})

			// Running consul connect redirect-traffic with iptables
			// requires both being a root user and having NET_ADMIN capability.
			container.SecurityContext = &corev1.SecurityContext{
				RunAsUser:  pointer.Int64(rootUserAndGroupID),
				RunAsGroup: pointer.Int64(rootUserAndGroupID),
				// RunAsNonRoot overrides any setting in the Pod so that we can still run as root here as required.
				RunAsNonRoot: pointer.Bool(false),
				Privileged:   pointer.Bool(privileged),
				Capabilities: &corev1.Capabilities{
					Add: []corev1.Capability{netAdminCapability},
				},
			}
		} else {
			container.SecurityContext = &corev1.SecurityContext{
				RunAsUser:    pointer.Int64(initContainersUserAndGroupID),
				RunAsGroup:   pointer.Int64(initContainersUserAndGroupID),
				RunAsNonRoot: pointer.Bool(true),
				Privileged:   pointer.Bool(privileged),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			}
		}
	}

	return container, nil
}

// consulDNSEnabled returns true if Consul DNS should be enabled for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool or if we are unable
// to read the pod's namespace label when it exists.
func consulDNSEnabled(namespace corev1.Namespace, pod corev1.Pod, globalDNSEnabled bool, globalTProxyEnabled bool) (bool, error) {
	// DNS is only possible when tproxy is also enabled because it relies
	// on traffic being redirected.
	tproxy, err := common.TransparentProxyEnabled(namespace, pod, globalTProxyEnabled)
	if err != nil {
		return false, err
	}
	if !tproxy {
		return false, nil
	}

	// First check to see if the pod annotation exists to override the namespace or global settings.
	if raw, ok := pod.Annotations[constants.KeyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	// Next see if the namespace has been defaulted.
	if raw, ok := namespace.Labels[constants.KeyConsulDNS]; ok {
		return strconv.ParseBool(raw)
	}
	// Else fall back to the global default.
	return globalDNSEnabled, nil
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
  -log-level={{ .LogLevel }} \
  -log-json={{ .LogJSON }} \
  {{- if .AuthMethod }}
  -service-account-name="{{ .ServiceAccountName }}" \
  -service-name="{{ .ServiceName }}" \
  {{- end }}
`
