// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gateways

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	corev1 "k8s.io/api/core/v1"
)

const (
	allCapabilities              = "ALL"
	netBindCapability            = "NET_BIND_SERVICE"
	consulDataplaneDNSBindHost   = "127.0.0.1"
	consulDataplaneDNSBindPort   = 8600
	defaultPrometheusScrapePath  = "/metrics"
	defaultEnvoyProxyConcurrency = "1"
	volumeName                   = "consul-mesh-inject-data"
)

func (b *gatewayBuilder[T]) consulDataplaneContainer(containerConfig v2beta1.GatewayClassContainerConfig) (corev1.Container, error) {
	// Extract the service account token's volume mount.
	var (
		err             error
		bearerTokenFile string
	)

	resources := containerConfig.Resources

	if b.config.AuthMethod != "" {
		bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	args, err := b.dataplaneArgs(bearerTokenFile)
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
		Name:  b.gateway.GetName(),
		Image: b.config.ImageDataplane,

		// We need to set tmp dir to an ephemeral volume that we're mounting so that
		// consul-dataplane can write files to it. Otherwise, it wouldn't be able to
		// because we set file system to be read-only.

		// TODO(nathancoleman): I don't believe consul-dataplane needs to write anymore, investigate.
		Env: []corev1.EnvVar{
			{
				Name: envDPProxyId,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: envPodNamespace,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			{
				Name:  envTmpDir,
				Value: constants.MeshV2VolumePath,
			},
			{
				Name: envNodeName,
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
			{
				Name:  envDPCredentialLoginMeta,
				Value: "pod=$(POD_NAMESPACE)/$(DP_PROXY_ID)",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: constants.MeshV2VolumePath,
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

	container.Ports = append(container.Ports, b.gateway.ListenersToContainerPorts(containerConfig.PortModifier, containerConfig.HostPort)...)

	// Configure the resource requests and limits for the proxy if they are set.
	if resources != nil {
		container.Resources = *resources
	}

	container.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		// Drop any Linux capabilities you'd get other than NET_BIND_SERVICE.
		// FUTURE: We likely require some additional capability in order to support
		//   MeshGateway's host network option.
		Capabilities: &corev1.Capabilities{
			Add:  []corev1.Capability{netBindCapability},
			Drop: []corev1.Capability{allCapabilities},
		},
		ReadOnlyRootFilesystem: ptr.To(true),
		RunAsNonRoot:           ptr.To(true),
	}

	return container, nil
}

func (b *gatewayBuilder[T]) dataplaneArgs(bearerTokenFile string) ([]string, error) {
	args := []string{
		"-addresses", b.config.ConsulConfig.Address,
		"-grpc-port=" + strconv.Itoa(b.config.ConsulConfig.GRPCPort),
		"-log-level=" + b.logLevelForDataplaneContainer(),
		"-log-json=" + strconv.FormatBool(b.config.LogJSON),
		"-envoy-concurrency=" + defaultEnvoyProxyConcurrency,
	}

	consulNamespace := namespaces.ConsulNamespace(b.gateway.GetNamespace(), b.config.ConsulTenancyConfig.EnableConsulNamespaces, b.config.ConsulTenancyConfig.ConsulDestinationNamespace, b.config.ConsulTenancyConfig.EnableConsulNamespaces, b.config.ConsulTenancyConfig.NSMirroringPrefix)

	if b.config.AuthMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+b.config.AuthMethod,
			"-login-bearer-token-path="+bearerTokenFile,
			"-login-meta="+fmt.Sprintf("gateway=%s/%s", b.gateway.GetNamespace(), b.gateway.GetName()),
		)
		if b.config.ConsulTenancyConfig.ConsulPartition != "" {
			args = append(args, "-login-partition="+b.config.ConsulTenancyConfig.ConsulPartition)
		}
	}
	if b.config.SkipServerWatch {
		args = append(args, "-server-watch-disabled=true")
	}
	if b.config.ConsulTenancyConfig.EnableConsulNamespaces {
		args = append(args, "-proxy-namespace="+consulNamespace)
	}
	if b.config.ConsulTenancyConfig.ConsulPartition != "" {
		args = append(args, "-proxy-partition="+b.config.ConsulTenancyConfig.ConsulPartition)
	}

	args = append(args, buildTLSArgs(b.config)...)

	// Configure the readiness port on the dataplane sidecar if proxy health checks are enabled.
	args = append(args, fmt.Sprintf("%s=%d", "-envoy-ready-bind-port", constants.ProxyDefaultHealthPort))

	args = append(args, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000))

	return args, nil
}

func buildTLSArgs(config GatewayConfig) []string {
	if !config.TLSEnabled {
		return []string{"-tls-disabled"}
	}
	tlsArgs := make([]string, 0, 2)

	if config.ConsulTLSServerName != "" {
		tlsArgs = append(tlsArgs, fmt.Sprintf("-tls-server-name=%s", config.ConsulTLSServerName))
	}
	if config.ConsulCACert != "" {
		tlsArgs = append(tlsArgs, fmt.Sprintf("-ca-certs=%s", constants.ConsulCAFile))
	}

	return tlsArgs
}
