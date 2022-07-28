package connectinject

import (
	"sort"
	"strconv"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestCreateRedirectTrafficConfig(t *testing.T) {
	cases := []struct {
		name          string
		consulService api.AgentService
		checks        map[string]*api.AgentCheck
		expCfg        iptables.Config
		expError      string
	}{
		{
			name: "proxyID with service port provided",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:       strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:  20000,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
			},
		},
		{
			name: "proxyID with bind_port(int) provided",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"bind_port": 21000,
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:       strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:  21000,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
			},
		},
		{
			name: "proxyID with bind_port(string) provided",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"bind_port": "21000",
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:       strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:  21000,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
			},
		},
		{
			name: "proxyID with bind_port(invalid type) provided",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"bind_port": "invalid",
					},
				},
			},
			expError: "failed parsing Proxy.Config: 1 error(s) decoding:\n\n* cannot parse 'bind_port' as int:",
		},
		{
			name: "proxyID with proxy outbound listener port",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					TransparentProxy: &api.TransparentProxyConfig{
						OutboundListenerPort: 21000,
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:       strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:  20000,
				ProxyOutboundPort: 21000,
			},
		},
		{
			name: "proxy config has envoy_prometheus_bind_addr set",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"envoy_prometheus_bind_addr": "0.0.0.0:9000",
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:         strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:    20000,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts: []string{"9000"},
			},
		},
		{
			name: "proxy config has an invalid envoy_prometheus_bind_addr set",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"envoy_prometheus_bind_addr": "9000",
					},
				},
			},
			expError: "failed parsing host and port from envoy_prometheus_bind_addr: address 9000: missing port in address",
		},
		{
			name: "proxy config has envoy_stats_bind_addr set",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"envoy_stats_bind_addr": "0.0.0.0:8000",
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:         strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:    20000,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts: []string{"8000"},
			},
		},
		{
			name: "proxy config has an invalid envoy_stats_bind_addr set",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Config: map[string]interface{}{
						"envoy_stats_bind_addr": "8000",
					},
				},
			},
			expError: "failed parsing host and port from envoy_stats_bind_addr: address 8000: missing port in address",
		},
		{
			name: "proxy config has expose paths with listener port set",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					Expose: api.ExposeConfig{
						Paths: []api.ExposePath{
							{
								ListenerPort:  23000,
								LocalPathPort: 8080,
								Path:          "/health",
							},
						},
					},
				},
			},
			expCfg: iptables.Config{
				ProxyUserID:         strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:    20000,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts: []string{"23000"},
			},
		},
		{
			name: "proxy config has expose paths with checks set to true",
			consulService: api.AgentService{
				Kind:    api.ServiceKindConnectProxy,
				ID:      "test-proxy-id",
				Port:    20000,
				Address: "1.1.1.1",
				Proxy: &api.AgentServiceConnectProxyConfig{
					DestinationServiceName: "foo",
					DestinationServiceID:   "foo-id",
					Expose: api.ExposeConfig{
						Checks: true,
					},
				},
			},
			checks: map[string]*api.AgentCheck{
				"http": {
					ExposedPort: 21500,
				},
				"grpc": {
					ExposedPort: 21501,
				},
			},

			expCfg: iptables.Config{
				ProxyUserID:         strconv.Itoa(envoyUserAndGroupID),
				ProxyInboundPort:    20000,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts: []string{"21500", "21501"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := createRedirectTrafficConfig(&c.consulService, c.checks)

			if c.expError == "" {
				require.NoError(t, err)

				sort.Strings(c.expCfg.ExcludeInboundPorts)
				sort.Strings(cfg.ExcludeInboundPorts)
				require.Equal(t, c.expCfg, cfg)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), c.expError)
			}
		})
	}
}

func TestExcludeInboundOutboundFromAnnotations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		pod      func() corev1.Pod
		expected iptables.Config
	}{
		{
			name: "exclude inbound ports",
			pod: func() corev1.Pod {
				pod1 := createPod("pod1", "1.2.3.4", false, false)
				pod1.Annotations[annotationTProxyExcludeInboundPorts] = "1111,11111"
				return *pod1
			},
			expected: iptables.Config{
				ExcludeInboundPorts: []string{"1111", "11111"},
			},
		},
		{
			name: "exclude outbound ports",
			pod: func() corev1.Pod {
				pod2 := createPod("pod2", "1.2.3.4", false, false)
				pod2.Annotations[annotationTProxyExcludeOutboundPorts] = "2222,22222"
				return *pod2
			},
			expected: iptables.Config{
				ExcludeOutboundPorts: []string{"2222", "22222"},
			},
		},
		{
			name: "exclude outbound CIDRs",
			pod: func() corev1.Pod {
				pod3 := createPod("pod3", "1.2.3.4", false, false)
				pod3.Annotations[annotationTProxyExcludeOutboundCIDRs] = "3.3.3.3,3.3.3.3/24"
				return *pod3
			},
			expected: iptables.Config{
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
			},
		},
		{
			name: "exclude UIDs",
			pod: func() corev1.Pod {
				pod4 := createPod("pod4", "1.2.3.4", false, false)
				pod4.Annotations[annotationTProxyExcludeUIDs] = "4444,44444"
				return *pod4
			},
			expected: iptables.Config{
				ExcludeUIDs: []string{"4444", "44444"},
			},
		},
		{
			name: "do not exclude anything",
			pod: func() corev1.Pod {
				pod5 := createPod("pod5", "1.2.3.4", false, false)
				return *pod5
			},
			expected: iptables.Config{},
		},
		{
			name: "exclude inbound ports, outbound ports, outbound CIDRs, and UIDs",
			pod: func() corev1.Pod {
				pod6 := createPod("pod6", "1.2.3.4", false, false)
				pod6.Annotations[annotationTProxyExcludeInboundPorts] = "1111,11111"
				pod6.Annotations[annotationTProxyExcludeOutboundPorts] = "2222,22222"
				pod6.Annotations[annotationTProxyExcludeOutboundCIDRs] = "3.3.3.3,3.3.3.3/24"
				pod6.Annotations[annotationTProxyExcludeUIDs] = "4444,44444"
				return *pod6
			},
			expected: iptables.Config{
				ExcludeInboundPorts:  []string{"1111", "11111"},
				ExcludeOutboundPorts: []string{"2222", "22222"},
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
				ExcludeUIDs:          []string{"4444", "44444"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := iptables.Config{}
			excludeInboundOutboundFromAnnotations(c.pod(), &actual)
			require.Equal(t, c.expected, actual)
		})
	}
}
