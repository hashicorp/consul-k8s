// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	ctrlCommon "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
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

// initContainer returns the init container spec for connect-init that polls for the service and the connect proxy service to be registered
// so that it can save the proxy service id to the shared volume and boostrap Envoy with the proxy-id.
func (g Gatekeeper) initContainer(config common.HelmConfig, name, namespace string) (corev1.Container, error) {
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

	if config.InitContainerResources != nil {
		container.Resources = *config.InitContainerResources
	}

	uid := int64(initContainersUserAndGroupID)
	gid := int64(initContainersUserAndGroupID)

	// In Openshift we let Openshift set the UID and GID
	if config.EnableOpenShift {
		ns := &corev1.Namespace{}
		err := g.Client.Get(context.Background(), client.ObjectKey{Name: namespace}, ns)
		if err != nil {
			g.Log.Error(err, "error fetching namespace metadata for deployment")
			return corev1.Container{}, fmt.Errorf("error getting namespace metadata for deployment: %s", err)
		}

		// We need to get the userID for the init container. We do not care about what is already defined on the pod
		// for gateways, as there is no application container that could have taken a UID.
		uid, err = ctrlCommon.GetConnectInitUID(*ns, corev1.Pod{}, config.ImageDataplane, config.ImageConsulK8S)
		if err != nil {
			return corev1.Container{}, err
		}

		gid, err = ctrlCommon.GetConnectInitGroupID(*ns, corev1.Pod{}, config.ImageDataplane, config.ImageConsulK8S)
		if err != nil {
			return corev1.Container{}, err
		}
	}

	container.SecurityContext = &corev1.SecurityContext{
		RunAsUser:    ptr.To(uid),
		RunAsGroup:   ptr.To(gid),
		RunAsNonRoot: ptr.To(true),
		Privileged:   ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
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
