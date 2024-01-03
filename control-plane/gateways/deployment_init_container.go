// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	injectInitContainerName      = "consul-mesh-init"
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
func initContainer(config GatewayConfig, name, namespace string) (corev1.Container, error) {
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
			MountPath: constants.MeshV2VolumePath,
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

	consulNamespace := namespaces.ConsulNamespace(namespace, config.ConsulTenancyConfig.EnableConsulNamespaces, config.ConsulTenancyConfig.ConsulDestinationNamespace, config.ConsulTenancyConfig.EnableConsulNamespaces, config.ConsulTenancyConfig.NSMirroringPrefix)

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

		if config.ConsulTenancyConfig.ConsulPartition != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "CONSUL_LOGIN_PARTITION",
				Value: config.ConsulTenancyConfig.ConsulPartition,
			})
		}
	}
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "CONSUL_NAMESPACE",
			Value: consulNamespace,
		})

	if config.TLSEnabled {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  constants.UseTLSEnvVar,
				Value: "true",
			},
			corev1.EnvVar{
				Name:  constants.CACertPEMEnvVar,
				Value: config.ConsulCACert,
			},
			corev1.EnvVar{
				Name:  constants.TLSServerNameEnvVar,
				Value: config.ConsulTLSServerName,
			})
	}

	if config.ConsulTenancyConfig.ConsulPartition != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_PARTITION",
				Value: config.ConsulTenancyConfig.ConsulPartition,
			})
	}

	return container, nil
}

// initContainerCommandTpl is the template for the command executed by
// the init container.
// TODO @GatewayManagement parametrize gateway kind.
const initContainerCommandTpl = `
consul-k8s-control-plane mesh-init \
  -proxy-name=${POD_NAME} \
  -namespace=${POD_NAMESPACE} \
  {{- with .LogLevel }}
  -log-level={{ . }} \
  {{- end }}
  -log-json={{ .LogJSON }}
`
