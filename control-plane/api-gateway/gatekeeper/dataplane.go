// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
)

const (
	allCapabilities              = "ALL"
	netBindCapability            = "NET_BIND_SERVICE"
	consulDataplaneDNSBindHost   = "127.0.0.1"
	consulDataplaneDNSBindPort   = 8600
	defaultEnvoyProxyConcurrency = 1
	volumeNameForConnectInject   = "consul-connect-inject-data"
	volumeNameForTLSCerts        = "consul-gateway-tls-certificates"
)

func consulDataplaneContainer(metrics common.MetricsConfig, config common.HelmConfig, gcc v1alpha1.GatewayClassConfig, gateway gwv1beta1.Gateway, mounts []corev1.VolumeMount) (corev1.Container, error) {
	// Extract the service account token's volume mount.
	var (
		err             error
		bearerTokenFile string
	)

	if config.AuthMethod != "" {
		bearerTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	args, err := getDataplaneArgs(metrics, gateway.Namespace, config, bearerTokenFile, gateway.Name)
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
		Name:            gateway.Name,
		Image:           config.ImageDataplane,
		ImagePullPolicy: corev1.PullPolicy(config.GlobalImagePullPolicy),

		// We need to set tmp dir to an ephemeral volume that we're mounting so that
		// consul-dataplane can write files to it. Otherwise, it wouldn't be able to
		// because we set file system to be read-only.
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
		VolumeMounts:   mounts,
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

	if metrics.Enabled {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "prometheus",
			ContainerPort: int32(metrics.Port),
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Configure the resource requests and limits for the proxy if they are set.
	if gcc.Spec.DeploymentSpec.Resources != nil {
		container.Resources = *gcc.Spec.DeploymentSpec.Resources
	}

	// For backwards-compatibility, we allow privilege escalation if port mapping
	// is disabled and the Gateway utilizes a privileged port (< 1024).
	usingPrivilegedPorts := false
	if gcc.Spec.MapPrivilegedContainerPorts == 0 {
		for _, listener := range gateway.Spec.Listeners {
			if listener.Port < 1024 {
				usingPrivilegedPorts = true
				break
			}
		}
	}

	container.SecurityContext = &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(usingPrivilegedPorts),
		ReadOnlyRootFilesystem:   ptr.To(true),
		RunAsNonRoot:             ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		// Drop any Linux capabilities you'd get as root other than NET_BIND_SERVICE.
		// NET_BIND_SERVICE is a requirement for consul-dataplane, even though we don't
		// bind to privileged ports.
		Capabilities: &corev1.Capabilities{
			Add:  []corev1.Capability{netBindCapability},
			Drop: []corev1.Capability{allCapabilities},
		},
	}

	return container, nil
}

func getDataplaneArgs(metrics common.MetricsConfig, namespace string, config common.HelmConfig, bearerTokenFile string, name string) ([]string, error) {
	proxyIDFileName := "/consul/connect-inject/proxyid"
	envoyConcurrency := defaultEnvoyProxyConcurrency

	args := []string{
		"-addresses", config.ConsulConfig.Address,
		"-grpc-port=" + strconv.Itoa(config.ConsulConfig.GRPCPort),
		"-proxy-service-id-path=" + proxyIDFileName,
		"-log-level=" + config.LogLevel,
		"-log-json=" + strconv.FormatBool(config.LogJSON),
		"-envoy-concurrency=" + strconv.Itoa(envoyConcurrency),
	}

	consulNamespace := namespaces.ConsulNamespace(namespace, config.EnableNamespaces, config.ConsulDestinationNamespace, config.EnableNamespaceMirroring, config.NamespaceMirroringPrefix)

	if config.AuthMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+config.AuthMethod,
			"-login-bearer-token-path="+bearerTokenFile,
			"-login-meta="+fmt.Sprintf("gateway=%s/%s", namespace, name),
		)
		if config.ConsulPartition != "" {
			args = append(args, "-login-partition="+config.ConsulPartition)
		}
	}
	if config.EnableNamespaces {
		args = append(args, "-service-namespace="+consulNamespace)
	}
	if config.ConsulPartition != "" {
		args = append(args, "-service-partition="+config.ConsulPartition)
	}
	if config.TLSEnabled {
		if config.ConsulTLSServerName != "" {
			args = append(args, "-tls-server-name="+config.ConsulTLSServerName)
		}
		if config.ConsulCACert != "" {
			args = append(args, "-ca-certs="+constants.LegacyConsulCAFile)
		}
	} else {
		args = append(args, "-tls-disabled")
	}

	// Configure the readiness port on the dataplane sidecar if proxy health checks are enabled.
	args = append(args, fmt.Sprintf("%s=%d", "-envoy-ready-bind-port", constants.ProxyDefaultHealthPort))

	args = append(args, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000))

	if metrics.Enabled {
		// Set up metrics collection.
		args = append(args, "-telemetry-prom-scrape-path="+metrics.Path)
	}

	return args, nil
}
