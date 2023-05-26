// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	apigateway "github.com/hashicorp/consul-k8s/control-plane/api-gateway"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"k8s.io/utils/pointer"
)

const (
	injectInitContainerName      = "consul-connect-inject-init"
	initContainersUserAndGroupID = 5996
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
func initContainer(config apigateway.HelmConfig, name, namespace string) (corev1.Container, error) {
	data := initContainerCommandData{
		AuthMethod:         config.AuthMethod,
		LogLevel:           config.LogLevel,
		LogJSON:            config.LogJSON,
		ServiceName:        name,
		ServiceAccountName: name,
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	}

	var bearerTokenFile string
	if config.AuthMethod != "" {
		bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(initContainerCommandTpl)))

	if err := tpl.Execute(&buf, &data); err != nil {
		return corev1.Container{}, err
	}

	consulNamespace := namespaces.ConsulNamespace(namespace, config.EnableNamespaces, config.ConsulDestinationNamespace, config.EnableNamespaceMirroring, config.NamespaceMirroringPrefix)

	initContainerName := injectInitContainerName
	container := corev1.Container{
		Name:  initContainerName,
		Image: config.ImageConsulK8S,

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
				Value: config.ConsulConfig.Address,
			},
			{
				Name:  "CONSUL_GRPC_PORT",
				Value: strconv.Itoa(config.ConsulConfig.GRPCPort),
			},
			{
				Name:  "CONSUL_HTTP_PORT",
				Value: strconv.Itoa(config.ConsulConfig.HTTPPort),
			},
			{
				Name:  "CONSUL_API_TIMEOUT",
				Value: config.ConsulConfig.APITimeout.String(),
			},
			{
				Name:  "CONSUL_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
			},
		},
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
	}

	if config.TLSEnabled {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_USE_TLS",
				Value: "true",
			},
			corev1.EnvVar{
				Name:  "CONSUL_CACERT_PEM",
				Value: config.ConsulCACert,
			},
			corev1.EnvVar{
				Name:  "CONSUL_TLS_SERVER_NAME",
				Value: config.ConsulTLSServerName,
			})
	}

	if config.AuthMethod != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_AUTH_METHOD",
				Value: config.AuthMethod,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
				Value: bearerTokenFile,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_META",
				Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
			})

		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "CONSUL_LOGIN_NAMESPACE",
			Value: consulNamespace,
		})

		if config.ConsulPartition != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "CONSUL_LOGIN_PARTITION",
				Value: config.ConsulPartition,
			})
		}
	}
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "CONSUL_NAMESPACE",
			Value: consulNamespace,
		})

	if config.ConsulPartition != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_PARTITION",
				Value: config.ConsulPartition,
			})
	}

	container.SecurityContext = &corev1.SecurityContext{
		RunAsUser:    pointer.Int64(initContainersUserAndGroupID),
		RunAsGroup:   pointer.Int64(initContainersUserAndGroupID),
		RunAsNonRoot: pointer.Bool(true),
		Privileged:   pointer.Bool(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}

	return container, nil
}

// initContainerCommandTpl is the template for the command executed by
// the init container.
const initContainerCommandTpl = `
consul-k8s-control-plane connect-init -pod-name=${POD_NAME} -pod-namespace=${POD_NAMESPACE} \
	-gateway-kind="api-gateway" \
  -log-json={{ .LogJSON }} \
  {{- if .AuthMethod }}
  -service-account-name="{{ .ServiceAccountName }}" \
  {{- end }}
  -service-name="{{ .ServiceName }}"
`
