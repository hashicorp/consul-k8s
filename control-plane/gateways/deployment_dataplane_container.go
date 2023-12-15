// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"

	corev1 "k8s.io/api/core/v1"
)

const (
	allCapabilities              = "ALL"
	netBindCapability            = "NET_BIND_SERVICE"
	consulDataplaneDNSBindHost   = "127.0.0.1"
	consulDataplaneDNSBindPort   = 8600
	defaultPrometheusScrapePath  = "/metrics"
	defaultEnvoyProxyConcurrency = "1"
	volumeName                   = "consul-connect-inject-data"
)

func consulDataplaneContainer(config GatewayConfig, containerConfig v2beta1.GatewayClassContainerConfig, name, namespace string) (corev1.Container, error) {
	// Extract the service account token's volume mount.
	var (
		err             error
		bearerTokenFile string
	)

	resources := containerConfig.Resources

	if config.AuthMethod != "" {
		bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	args, err := getDataplaneArgs(namespace, config, bearerTokenFile, name)
	if err != nil {
		return corev1.Container{}, err
	}

	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Port: intstr.FromInt(constants.ProxyDefaultHealthPort),
				Path: "/ready",
			},
		},
		InitialDelaySeconds: 1,
	}

	container := corev1.Container{
		Name:  name,
		Image: config.ImageDataplane,

		// We need to set tmp dir to an ephemeral volume that we're mounting so that
		// consul-dataplane can write files to it. Otherwise, it wouldn't be able to
		// because we set file system to be read-only.

		// TODO(nathancoleman): I don't believe consul-dataplane needs to write anymore, investigate.
		Env: []corev1.EnvVar{
			{
				Name:  "TMPDIR",
				Value: "/consul/connect-inject",
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
				Name:  "DP_SERVICE_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Args:           args,
		ReadinessProbe: probe,
	}

	// Configure the Readiness Address for the proxy's health check to be the Pod IP.
	container.Env = append(container.Env, corev1.EnvVar{
		Name: "DP_ENVOY_READY_BIND_ADDRESS",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	})
	// Configure the port on which the readiness probe will query the proxy for its health.
	container.Ports = append(container.Ports, corev1.ContainerPort{
		Name:          "proxy-health",
		ContainerPort: int32(constants.ProxyDefaultHealthPort),
	})

	// Configure the wan port.
	wanPort := corev1.ContainerPort{
		Name:          "wan",
		ContainerPort: int32(constants.DefaultWANPort),
		HostPort:      containerConfig.HostPort,
	}

	wanPort.ContainerPort = 443 + containerConfig.PortModifier

	container.Ports = append(container.Ports, wanPort)

	// Configure the resource requests and limits for the proxy if they are set.
	if resources != nil {
		container.Resources = *resources
	}

	container.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: pointer.Bool(false),
		// Drop any Linux capabilities you'd get other than NET_BIND_SERVICE.
		// FUTURE: We likely require some additional capability in order to support
		//   MeshGateway's host network option.
		Capabilities: &corev1.Capabilities{
			Add:  []corev1.Capability{netBindCapability},
			Drop: []corev1.Capability{allCapabilities},
		},
		ReadOnlyRootFilesystem: pointer.Bool(true),
		RunAsNonRoot:           pointer.Bool(true),
	}

	return container, nil
}

func getDataplaneArgs(namespace string, config GatewayConfig, bearerTokenFile string, name string) ([]string, error) {
	proxyIDFileName := "/consul/connect-inject/proxyid"

	args := []string{
		"-addresses", config.ConsulConfig.Address,
		"-grpc-port=" + strconv.Itoa(config.ConsulConfig.GRPCPort),
		"-proxy-service-id-path=" + proxyIDFileName,
		"-log-level=" + config.LogLevel,
		"-log-json=" + strconv.FormatBool(config.LogJSON),
		"-envoy-concurrency=" + defaultEnvoyProxyConcurrency,
	}

	consulNamespace := namespaces.ConsulNamespace(namespace, config.ConsulTenancyConfig.EnableConsulNamespaces, config.ConsulTenancyConfig.ConsulDestinationNamespace, config.ConsulTenancyConfig.EnableConsulNamespaces, config.ConsulTenancyConfig.NSMirroringPrefix)

	if config.AuthMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+config.AuthMethod,
			"-login-bearer-token-path="+bearerTokenFile,
			"-login-meta="+fmt.Sprintf("gateway=%s/%s", namespace, name),
		)
		if config.ConsulTenancyConfig.ConsulPartition != "" {
			args = append(args, "-login-partition="+config.ConsulTenancyConfig.ConsulPartition)
		}
	}
	if config.ConsulTenancyConfig.EnableConsulNamespaces {
		args = append(args, "-service-namespace="+consulNamespace)
	}
	if config.ConsulTenancyConfig.ConsulPartition != "" {
		args = append(args, "-service-partition="+config.ConsulTenancyConfig.ConsulPartition)
	}

	args = append(args, "-tls-disabled")

	// Configure the readiness port on the dataplane sidecar if proxy health checks are enabled.
	args = append(args, fmt.Sprintf("%s=%d", "-envoy-ready-bind-port", constants.ProxyDefaultHealthPort))

	args = append(args, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000))

	return args, nil
}
