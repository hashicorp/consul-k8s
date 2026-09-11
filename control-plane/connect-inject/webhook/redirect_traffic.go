// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/hashicorp/consul/sdk/iptables"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// addRedirectTrafficConfigAnnotation creates an iptables.Config in JSON format based on proxy configuration.
// iptables.Config:
//
//	ConsulDNSIP: an environment variable named RESOURCE_PREFIX_DNS_SERVICE_HOST where RESOURCE_PREFIX is the consul.fullname in helm.
//	ProxyUserID: a constant set in Annotations or read from namespace when using OpenShift
//	ProxyInboundPort: the service port or bind port
//	ProxyOutboundPort: default transparent proxy outbound port or transparent proxy outbound listener port
//	ExcludeInboundPorts: prometheus, envoy stats, expose paths, checks and excluded pod annotations
//	ExcludeOutboundPorts: pod annotations
//	ExcludeOutboundCIDRs: pod annotations
//	ExcludeUIDs: pod annotations
func (w *MeshWebhook) iptablesConfigJSON(ctx context.Context, pod corev1.Pod, ns corev1.Namespace) (string, error) {
	cfg := iptables.Config{}

	if !w.EnableOpenShift {
		cfg.ProxyUserID = strconv.Itoa(sidecarUserAndGroupID)

		// Add init container user ID to exclude from traffic redirection.
		cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, strconv.Itoa(initContainersUserAndGroupID))
	} else {
		// When using OpenShift, the uid and group are saved as an annotation on the namespace
		uid, err := common.GetDataplaneUID(ns, pod, w.ImageConsulDataplane, w.ImageConsulK8S)
		if err != nil {
			return "", err
		}
		cfg.ProxyUserID = strconv.FormatInt(uid, 10)

		// Exclude the user ID for the init container from traffic redirection.
		uid, err = common.GetConnectInitUID(ns, pod, w.ImageConsulDataplane, w.ImageConsulK8S)
		if err != nil {
			return "", err
		}
		cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, strconv.FormatInt(uid, 10))
	}

	// Set the proxy's inbound port.
	cfg.ProxyInboundPort = constants.ProxyDefaultInboundPort

	// Set the proxy's outbound port.
	cfg.ProxyOutboundPort = iptables.DefaultTProxyOutboundPort

	// If metrics are enabled, get the prometheusScrapePort and exclude it from the inbound ports
	enableMetrics, err := w.MetricsConfig.EnableMetrics(pod)
	if err != nil {
		return "", err
	}
	if enableMetrics {
		prometheusScrapePort, err := w.MetricsConfig.PrometheusScrapePort(pod)
		if err != nil {
			return "", err
		}
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, prometheusScrapePort)
	}

	// Exclude any overwritten liveness/readiness/startup ports from redirection.
	overwriteProbes, err := common.ShouldOverwriteProbes(pod, w.TProxyOverwriteProbes)
	if err != nil {
		return "", err
	}

	// Exclude the port on which the proxy health check port will be configured if
	// using the proxy health check for a service.
	if useProxyHealthCheck(pod) {
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(constants.ProxyDefaultHealthPort))
	}

	if overwriteProbes {
		// We don't use the loop index because this needs to line up w.overwriteProbes(),
		// which is performed after the sidecar is injected.
		idx := 0
		for _, container := range pod.Spec.Containers {
			// skip the "consul-dataplane" container from having its probes overridden
			if container.Name == sidecarContainer {
				continue
			}
			if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsLivenessPortsRangeStart+idx))
			}
			if container.ReadinessProbe != nil && container.ReadinessProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsReadinessPortsRangeStart+idx))
			}
			if container.StartupProbe != nil && container.StartupProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsStartupPortsRangeStart+idx))
			}
			idx++
		}
	}

	if common.IsAIAgent(pod) {
		aiCfg, err := common.AIConfigFromPodIF(ctx, w.Clientset, pod)
		if err != nil {
			return "", err
		}
		// Exclude all MCP-specific loopback ports from tproxy interception so
		// consul-mcp-gateway, the HITL server, and the MCP ext_proc listener
		// communicate directly without going through Envoy's redirect iptables.
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts,
			strconv.Itoa(aiCfg.Agent.MCP.Port),
			strconv.Itoa(aiCfg.Agent.MCP.HITL.Port),
			strconv.Itoa(aiCfg.Agent.Interceptor.Port),
		)
		cfg.ExcludeOutboundPorts = append(cfg.ExcludeOutboundPorts,
			strconv.Itoa(aiCfg.Agent.MCP.Port),
		)
	}

	// Exclude OBO sidecar ports from tproxy interception for all oauth-client
	// pods. Both OBO processes bind on loopback and are invoked by Envoy via
	// ext_proc gRPC — they must never be redirected through the iptables rules.
	//   :21102 — consul-obo-inbound   (inbound ext_proc)
	//   :21103 — consul-obo-outbound  (outbound ext_proc)
	if common.IsOAuthClient(pod) {
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts,
			strconv.Itoa(constants.DefaultOBOInboundPort),
		)
		cfg.ExcludeOutboundPorts = append(cfg.ExcludeOutboundPorts,
			strconv.Itoa(constants.DefaultOBOOutboundPort),
		)
	}

	// Inbound ports
	excludeInboundPorts := splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeInboundPorts, pod)
	cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, excludeInboundPorts...)

	// Outbound ports
	excludeOutboundPorts := splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeOutboundPorts, pod)
	cfg.ExcludeOutboundPorts = append(cfg.ExcludeOutboundPorts, excludeOutboundPorts...)

	// Outbound CIDRs
	excludeOutboundCIDRs := splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeOutboundCIDRs, pod)
	cfg.ExcludeOutboundCIDRs = append(cfg.ExcludeOutboundCIDRs, excludeOutboundCIDRs...)

	// UIDs
	excludeUIDs := splitCommaSeparatedItemsFromAnnotation(constants.AnnotationTProxyExcludeUIDs, pod)
	cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, excludeUIDs...)

	dnsEnabled, err := consulDNSEnabled(ns, pod, w.EnableConsulDNS, w.EnableTransparentProxy)
	if err != nil {
		return "", err
	}

	if dnsEnabled {
		// If Consul DNS is enabled, we find the environment variable that has the value
		// of the ClusterIP of the Consul DNS Service. constructDNSServiceHostName returns
		// the name of the env variable whose value is the ClusterIP of the Consul DNS Service.
		cfg.ConsulDNSIP = constants.Getv4orv6Str(consulDataplaneDNSBindHost, ipv6ConsulDataplaneDNSBindHost)
		cfg.ConsulDNSPort = consulDataplaneDNSBindPort
	}

	iptablesConfigJson, err := json.Marshal(&cfg)
	if err != nil {
		return "", fmt.Errorf("could not marshal iptables config: %w", err)
	}

	return string(iptablesConfigJson), nil
}

// addRedirectTrafficConfigAnnotation add the created iptables JSON config as an annotation on the provided pod.
func (w *MeshWebhook) addRedirectTrafficConfigAnnotation(ctx context.Context, pod *corev1.Pod, ns corev1.Namespace) error {
	iptablesConfig, err := w.iptablesConfigJSON(ctx, *pod, ns)
	if err != nil {
		return err
	}

	pod.Annotations[constants.AnnotationRedirectTraffic] = iptablesConfig

	return nil
}
