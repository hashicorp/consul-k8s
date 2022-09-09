package connectinject

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/hashicorp/consul/sdk/iptables"
	corev1 "k8s.io/api/core/v1"
)

// addRedirectTrafficConfigAnnotation creates an iptables.Config based on proxy configuration.
// iptables.Config:
//   ConsulDNSIP: an environment variable named RESOURCE_PREFIX_DNS_SERVICE_HOST where RESOURCE_PREFIX is the consul.fullname in helm.
//   ProxyUserID: a constant set in Annotations
//   ProxyInboundPort: the service port or bind port
//   ProxyOutboundPort: default transparent proxy outbound port or transparent proxy outbound listener port
//   ExcludeInboundPorts: prometheus, envoy stats, expose paths, checks and excluded pod annotations
//   ExcludeOutboundPorts: pod annotations
//   ExcludeOutboundCIDRs: pod annotations
//   ExcludeUIDs: pod annotations
func (w *MeshWebhook) addRedirectTrafficConfigAnnotation(pod *corev1.Pod, ns corev1.Namespace) error {
	cfg := iptables.Config{
		ProxyUserID: strconv.Itoa(envoyUserAndGroupID),
	}

	// Set the proxy's inbound port.
	cfg.ProxyInboundPort = proxyDefaultInboundPort

	// Set the proxy's outbound port.
	cfg.ProxyOutboundPort = iptables.DefaultTProxyOutboundPort

	// If metrics are enabled, get the prometheusScrapePort and exclude it from the inbound ports
	enableMetrics, err := w.MetricsConfig.enableMetrics(*pod)
	if err != nil {
		return err
	}
	if enableMetrics {
		prometheusScrapePort, err := w.MetricsConfig.prometheusScrapePort(*pod)
		if err != nil {
			return err
		}
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, prometheusScrapePort)
	}

	// Exclude any overwritten liveness/readiness/startup ports from redirection.
	overwriteProbes, err := shouldOverwriteProbes(*pod, w.TProxyOverwriteProbes)
	if err != nil {
		return err
	}

	if overwriteProbes {
		for i, container := range pod.Spec.Containers {
			// skip the "envoy-sidecar" container from having its probes overridden
			if container.Name == envoySidecarContainer {
				continue
			}
			if container.LivenessProbe != nil && container.LivenessProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsLivenessPortsRangeStart+i))
			}
			if container.ReadinessProbe != nil && container.ReadinessProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsReadinessPortsRangeStart+i))
			}
			if container.StartupProbe != nil && container.StartupProbe.HTTPGet != nil {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposedPathsStartupPortsRangeStart+i))
			}
		}
	}

	// Inbound ports
	excludeInboundPorts := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeInboundPorts, *pod)
	cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, excludeInboundPorts...)

	// Outbound ports
	excludeOutboundPorts := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundPorts, *pod)
	cfg.ExcludeOutboundPorts = append(cfg.ExcludeOutboundPorts, excludeOutboundPorts...)

	// Outbound CIDRs
	excludeOutboundCIDRs := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundCIDRs, *pod)
	cfg.ExcludeOutboundCIDRs = append(cfg.ExcludeOutboundCIDRs, excludeOutboundCIDRs...)

	// UIDs
	excludeUIDs := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeUIDs, *pod)
	cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, excludeUIDs...)

	// Add init container user ID to exclude from traffic redirection.
	cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, strconv.Itoa(initContainersUserAndGroupID))

	dnsEnabled, err := consulDNSEnabled(ns, *pod, w.EnableConsulDNS)
	if err != nil {
		return err
	}

	var consulDNSClusterIP string
	if dnsEnabled {
		// If Consul DNS is enabled, we find the environment variable that has the value
		// of the ClusterIP of the Consul DNS Service. constructDNSServiceHostName returns
		// the name of the env variable whose value is the ClusterIP of the Consul DNS Service.
		consulDNSClusterIP = os.Getenv(w.constructDNSServiceHostName())
		if consulDNSClusterIP == "" {
			return fmt.Errorf("environment variable %s not found", w.constructDNSServiceHostName())
		}
		cfg.ConsulDNSIP = consulDNSClusterIP
	}

	iptablesConfigJson, err := json.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("could not marshal iptables config: %w", err)
	}

	pod.Annotations[annotationRedirectTraffic] = string(iptablesConfigJson)

	return nil
}
