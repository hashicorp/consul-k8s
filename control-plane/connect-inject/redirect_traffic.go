package connectinject

import (
	"fmt"
	"net"
	"strconv"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/mitchellh/mapstructure"
	corev1 "k8s.io/api/core/v1"
)

// trafficRedirectProxyConfig is a snippet of xds/config.go
// with only the configuration values that we need to parse from Proxy.Config
// to apply traffic redirection rules.
type trafficRedirectProxyConfig struct {
	BindPort           int    `mapstructure:"bind_port"`
	PrometheusBindAddr string `mapstructure:"envoy_prometheus_bind_addr"`
	StatsBindAddr      string `mapstructure:"envoy_stats_bind_addr"`
}

// createRedirectTrafficConfig creates an iptables.Config based on proxy configuration.
// iptables.Config:
//   ConsulDNSIP: an environment variable named RESOURCE_PREFIX_DNS_SERVICE_HOST where RESOURCE_PREFIX is the consul.fullname in helm.
//   ProxyUserID: a constant set in Annotations
//   ProxyInboundPort: the service port or bind port
//   ProxyOutboundPort: default transparent proxy outbound port or transparent proxy outbound listener port
//   ExcludeInboundPorts: prometheus, envoy stats, expose paths, checks and excluded pod annotations
//   ExcludeOutboundPorts: pod annotations
//   ExcludeOutboundCIDRs: pod annotations
//   ExcludeUIDs: pod annotations
//   NetNS: Net Namespace, passed to the CNI plugin as CNI_NETNS
func createRedirectTrafficConfig(svc *api.AgentServiceRegistration, checks map[string]*api.AgentCheck) (iptables.Config, error) {
	cfg := iptables.Config{
		ProxyUserID: strconv.Itoa(envoyUserAndGroupID),
	}

	if svc.Proxy == nil {
		return iptables.Config{}, fmt.Errorf("service %s is not a proxy service", svc.ID)
	}

	// Decode proxy's opaque config so that we can use it later to configure
	// traffic redirection with iptables.
	var trCfg trafficRedirectProxyConfig
	if err := mapstructure.WeakDecode(svc.Proxy.Config, &trCfg); err != nil {
		return iptables.Config{}, fmt.Errorf("failed parsing Proxy.Config: %s", err)
	}

	// Set the proxy's inbound port.
	cfg.ProxyInboundPort = svc.Port
	if trCfg.BindPort != 0 {
		cfg.ProxyInboundPort = trCfg.BindPort
	}

	// Set the proxy's outbound port.
	cfg.ProxyOutboundPort = iptables.DefaultTProxyOutboundPort
	if svc.Proxy.TransparentProxy != nil && svc.Proxy.TransparentProxy.OutboundListenerPort != 0 {
		cfg.ProxyOutboundPort = svc.Proxy.TransparentProxy.OutboundListenerPort
	}

	// Exclude envoy_prometheus_bind_addr port from inbound redirection rules.
	if trCfg.PrometheusBindAddr != "" {
		_, port, err := net.SplitHostPort(trCfg.PrometheusBindAddr)
		if err != nil {
			return iptables.Config{}, fmt.Errorf(
				"failed parsing host and port from envoy_prometheus_bind_addr: %s",
				err,
			)
		}
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, port)
	}

	// Exclude envoy_stats_bind_addr port from inbound redirection rules.
	if trCfg.StatsBindAddr != "" {
		_, port, err := net.SplitHostPort(trCfg.StatsBindAddr)
		if err != nil {
			return iptables.Config{}, fmt.Errorf("failed parsing host and port from envoy_stats_bind_addr: %s", err)
		}
		cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, port)
	}

	// Exclude the ListenerPort from Expose configs from inbound traffic redirection.
	for _, exposePath := range svc.Proxy.Expose.Paths {
		if exposePath.ListenerPort != 0 {
			cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(exposePath.ListenerPort))
		}
	}

	// Exclude any exposed health check ports when Proxy.Expose.Checks is true.
	if svc.Proxy.Expose.Checks {
		for _, check := range checks {
			if check.ExposedPort != 0 {
				cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, strconv.Itoa(check.ExposedPort))
			}
		}
	}
	return cfg, nil
}

// excludeInboundOutboundFromAnnotations gets the exclude inbound ports, exclude outbound ports, exclude CIDRs, exclude
// UIDs annotations from a pod and adds them to the iptables.Config.
func excludeInboundOutboundFromAnnotations(pod corev1.Pod, cfg *iptables.Config) {
	// Inbound ports
	excludeInboundPorts := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeInboundPorts, pod)
	cfg.ExcludeInboundPorts = append(cfg.ExcludeInboundPorts, excludeInboundPorts...)

	// Outbound ports
	excludeOutboundPorts := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundPorts, pod)
	cfg.ExcludeOutboundPorts = append(cfg.ExcludeOutboundPorts, excludeOutboundPorts...)

	// Outbound CIDRs
	excludeOutboundCIDRs := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeOutboundCIDRs, pod)
	cfg.ExcludeOutboundCIDRs = append(cfg.ExcludeOutboundCIDRs, excludeOutboundCIDRs...)

	// UIDs
	excludeUIDs := splitCommaSeparatedItemsFromAnnotation(annotationTProxyExcludeUIDs, pod)
	cfg.ExcludeUIDs = append(cfg.ExcludeUIDs, excludeUIDs...)
}
