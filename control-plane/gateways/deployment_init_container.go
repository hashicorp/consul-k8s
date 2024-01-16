// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"

	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	injectInitContainerName      = "consul-mesh-init"
	initContainersUserAndGroupID = 5996
)

var (
	tpl = template.Must(template.New("root").Parse(strings.TrimSpace(initContainerCommandTpl)))
)

type initContainerCommandData struct {
	ServiceName        string
	ServiceAccountName string
	AuthMethod         string

	// Log settings for the connect-init command.
	LogLevel string
	LogJSON  bool
}

// initContainer returns the init container spec for connect-init that polls for the service and the connect proxy service to be registered
// so that it can save the proxy service id to the shared volume and boostrap Envoy with the proxy-id.
func (b *meshGatewayBuilder) initContainer() (corev1.Container, error) {
	data := initContainerCommandData{
		AuthMethod:         b.config.AuthMethod,
		LogLevel:           b.logLevelForInitContainer(),
		LogJSON:            b.config.LogJSON,
		ServiceName:        b.gateway.Name,
		ServiceAccountName: b.serviceAccountName(),
	}
	// Render the command
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, &data); err != nil {
		return corev1.Container{}, err
	}

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		{
			Name:      volumeName,
			MountPath: constants.MeshV2VolumePath,
		},
	}

	var bearerTokenFile string
	if b.config.AuthMethod != "" {
		bearerTokenFile = defaultBearerTokenFile
	}

	consulNamespace := namespaces.ConsulNamespace(b.gateway.Namespace, b.config.ConsulTenancyConfig.EnableConsulNamespaces, b.config.ConsulTenancyConfig.ConsulDestinationNamespace, b.config.ConsulTenancyConfig.EnableConsulNamespaces, b.config.ConsulTenancyConfig.NSMirroringPrefix)

	initContainerName := injectInitContainerName
	container := corev1.Container{
		Name:  initContainerName,
		Image: b.config.ImageConsulK8S,

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
				Value: b.config.ConsulConfig.Address,
			},
			{
				Name:  "CONSUL_GRPC_PORT",
				Value: strconv.Itoa(b.config.ConsulConfig.GRPCPort),
			},
			{
				Name:  "CONSUL_HTTP_PORT",
				Value: strconv.Itoa(b.config.ConsulConfig.HTTPPort),
			},
			{
				Name:  "CONSUL_API_TIMEOUT",
				Value: b.config.ConsulConfig.APITimeout.String(),
			},
			{
				Name:  "CONSUL_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
			},
		},
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", buf.String()},
		Resources:    initContainerResourcesOrDefault(b.gcc),
	}

	if b.config.AuthMethod != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_AUTH_METHOD",
				Value: b.config.AuthMethod,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_BEARER_TOKEN_FILE",
				Value: bearerTokenFile,
			},
			corev1.EnvVar{
				Name:  "CONSUL_LOGIN_META",
				Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
			})

		if b.config.ConsulTenancyConfig.ConsulPartition != "" {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "CONSUL_LOGIN_PARTITION",
				Value: b.config.ConsulTenancyConfig.ConsulPartition,
			})
		}
	}
	container.Env = append(container.Env,
		corev1.EnvVar{
			Name:  "CONSUL_NAMESPACE",
			Value: consulNamespace,
		})

	if b.config.TLSEnabled {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  constants.UseTLSEnvVar,
				Value: "true",
			},
			corev1.EnvVar{
				Name:  constants.CACertPEMEnvVar,
				Value: b.config.ConsulCACert,
			},
			corev1.EnvVar{
				Name:  constants.TLSServerNameEnvVar,
				Value: b.config.ConsulTLSServerName,
			})
	}

	if b.config.ConsulTenancyConfig.ConsulPartition != "" {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  "CONSUL_PARTITION",
				Value: b.config.ConsulTenancyConfig.ConsulPartition,
			})
	}

	return container, nil
}

func initContainerResourcesOrDefault(gcc *meshv2beta1.GatewayClassConfig) corev1.ResourceRequirements {
	if gcc != nil && gcc.Spec.Deployment.InitContainer != nil && gcc.Spec.Deployment.InitContainer.Resources != nil {
		return *gcc.Spec.Deployment.InitContainer.Resources
	}

	return corev1.ResourceRequirements{}
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
