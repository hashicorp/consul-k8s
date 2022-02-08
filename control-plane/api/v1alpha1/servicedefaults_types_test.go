package v1alpha1

import (
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceDefaults_ToConsul(t *testing.T) {
	cases := map[string]struct {
		input    *ServiceDefaults
		expected *capi.ServiceConfigEntry
	}{
		"empty fields": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Name: "foo",
				Kind: capi.ServiceDefaults,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "https",
					MeshGateway: MeshGateway{
						Mode: "local",
					},
					Expose: Expose{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/path",
								LocalPathPort: 9000,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/another-path",
								LocalPathPort: 9091,
								Protocol:      "http2",
							},
						},
					},
					ExternalSNI: "external-sni",
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
						DialedDirectly:       true,
					},
					UpstreamConfig: &Upstreams{
						Defaults: &Upstream{
							Name:              "upstream-default",
							Namespace:         "ns",
							Partition:         "part",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  10,
							Limits: &UpstreamLimits{
								MaxConnections:        intPointer(10),
								MaxPendingRequests:    intPointer(10),
								MaxConcurrentRequests: intPointer(10),
							},
							PassiveHealthCheck: &PassiveHealthCheck{
								Interval: metav1.Duration{
									Duration: 2 * time.Second,
								},
								MaxFailures: uint32(20),
							},
							MeshGateway: MeshGateway{
								Mode: "local",
							},
						},
						Overrides: []*Upstream{
							{
								Name:              "upstream-override-1",
								Namespace:         "ns",
								Partition:         "part",
								EnvoyListenerJSON: `{"key": "value"}`,
								EnvoyClusterJSON:  `{"key": "value"}`,
								Protocol:          "http2",
								ConnectTimeoutMs:  15,
								Limits: &UpstreamLimits{
									MaxConnections:        intPointer(5),
									MaxPendingRequests:    intPointer(5),
									MaxConcurrentRequests: intPointer(5),
								},
								PassiveHealthCheck: &PassiveHealthCheck{
									Interval: metav1.Duration{
										Duration: 2 * time.Second,
									},
									MaxFailures: uint32(10),
								},
								MeshGateway: MeshGateway{
									Mode: "remote",
								},
							},
							{
								Name:              "upstream-default",
								Namespace:         "ns",
								Partition:         "part",
								EnvoyListenerJSON: `{"key": "value"}`,
								EnvoyClusterJSON:  `{"key": "value"}`,
								Protocol:          "http2",
								ConnectTimeoutMs:  10,
								Limits: &UpstreamLimits{
									MaxConnections:        intPointer(2),
									MaxPendingRequests:    intPointer(2),
									MaxConcurrentRequests: intPointer(2),
								},
								PassiveHealthCheck: &PassiveHealthCheck{
									Interval: metav1.Duration{
										Duration: 2 * time.Second,
									},
									MaxFailures: uint32(10),
								},
								MeshGateway: MeshGateway{
									Mode: "remote",
								},
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "foo",
				Protocol: "https",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/path",
							LocalPathPort: 9000,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/another-path",
							LocalPathPort: 9091,
							Protocol:      "http2",
						},
					},
				},
				ExternalSNI: "external-sni",
				TransparentProxy: &capi.TransparentProxyConfig{
					OutboundListenerPort: 1000,
					DialedDirectly:       true,
				},
				UpstreamConfig: &capi.UpstreamConfiguration{
					Defaults: &capi.UpstreamConfig{
						Name:              "upstream-default",
						Namespace:         "ns",
						Partition:         "part",
						EnvoyListenerJSON: `{"key": "value"}`,
						EnvoyClusterJSON:  `{"key": "value"}`,
						Protocol:          "http2",
						ConnectTimeoutMs:  10,
						Limits: &capi.UpstreamLimits{
							MaxConnections:        intPointer(10),
							MaxPendingRequests:    intPointer(10),
							MaxConcurrentRequests: intPointer(10),
						},
						PassiveHealthCheck: &capi.PassiveHealthCheck{
							Interval:    2 * time.Second,
							MaxFailures: uint32(20),
						},
						MeshGateway: capi.MeshGatewayConfig{
							Mode: "local",
						},
					},
					Overrides: []*capi.UpstreamConfig{
						{
							Name:              "upstream-override-1",
							Namespace:         "ns",
							Partition:         "part",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  15,
							Limits: &capi.UpstreamLimits{
								MaxConnections:        intPointer(5),
								MaxPendingRequests:    intPointer(5),
								MaxConcurrentRequests: intPointer(5),
							},
							PassiveHealthCheck: &capi.PassiveHealthCheck{
								Interval:    2 * time.Second,
								MaxFailures: uint32(10),
							},
							MeshGateway: capi.MeshGatewayConfig{
								Mode: "remote",
							},
						},
						{
							Name:              "upstream-default",
							Namespace:         "ns",
							Partition:         "part",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  10,
							Limits: &capi.UpstreamLimits{
								MaxConnections:        intPointer(2),
								MaxPendingRequests:    intPointer(2),
								MaxConcurrentRequests: intPointer(2),
							},
							PassiveHealthCheck: &capi.PassiveHealthCheck{
								Interval:    2 * time.Second,
								MaxFailures: uint32(10),
							},
							MeshGateway: capi.MeshGatewayConfig{
								Mode: "remote",
							},
						},
					},
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			output := testCase.input.ToConsul("datacenter")
			require.Equal(t, testCase.expected, output)
		})
	}
}

func TestServiceDefaults_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		internal *ServiceDefaults
		consul   capi.ConfigEntry
		matches  bool
	}{
		"empty fields matches": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			true,
		},
		"all fields populated matches": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "http",
					MeshGateway: MeshGateway{
						Mode: "remote",
					},
					Expose: Expose{
						Paths: []ExposePath{
							{
								ListenerPort:  8080,
								Path:          "/second/test/path",
								LocalPathPort: 11,
								Protocol:      "https",
							},
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
						},
					},
					ExternalSNI: "sni-value",
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
						DialedDirectly:       true,
					},
					UpstreamConfig: &Upstreams{
						Defaults: &Upstream{
							Name:              "upstream-default",
							Namespace:         "ns",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  10,
							Limits: &UpstreamLimits{
								MaxConnections:        intPointer(10),
								MaxPendingRequests:    intPointer(10),
								MaxConcurrentRequests: intPointer(10),
							},
							PassiveHealthCheck: &PassiveHealthCheck{
								Interval: metav1.Duration{
									Duration: 2 * time.Second,
								},
								MaxFailures: uint32(20),
							},
							MeshGateway: MeshGateway{
								Mode: "local",
							},
						},
						Overrides: []*Upstream{
							{
								Name:              "upstream-override-1",
								Namespace:         "ns",
								EnvoyListenerJSON: `{"key": "value"}`,
								EnvoyClusterJSON:  `{"key": "value"}`,
								Protocol:          "http2",
								ConnectTimeoutMs:  15,
								Limits: &UpstreamLimits{
									MaxConnections:        intPointer(5),
									MaxPendingRequests:    intPointer(5),
									MaxConcurrentRequests: intPointer(5),
								},
								PassiveHealthCheck: &PassiveHealthCheck{
									Interval: metav1.Duration{
										Duration: 2 * time.Second,
									},
									MaxFailures: uint32(10),
								},
								MeshGateway: MeshGateway{
									Mode: "remote",
								},
							},
							{
								Name:              "upstream-default",
								Namespace:         "ns",
								EnvoyListenerJSON: `{"key": "value"}`,
								EnvoyClusterJSON:  `{"key": "value"}`,
								Protocol:          "http2",
								ConnectTimeoutMs:  10,
								Limits: &UpstreamLimits{
									MaxConnections:        intPointer(2),
									MaxPendingRequests:    intPointer(2),
									MaxConcurrentRequests: intPointer(2),
								},
								PassiveHealthCheck: &PassiveHealthCheck{
									Interval: metav1.Duration{
										Duration: 2 * time.Second,
									},
									MaxFailures: uint32(10),
								},
								MeshGateway: MeshGateway{
									Mode: "remote",
								},
							},
						},
					},
				},
			},
			&capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "my-test-service",
				Protocol: "http",
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
				Expose: capi.ExposeConfig{
					Paths: []capi.ExposePath{
						{
							ListenerPort:  8080,
							Path:          "/second/test/path",
							LocalPathPort: 11,
							Protocol:      "https",
						},
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
					},
				},
				ExternalSNI: "sni-value",
				TransparentProxy: &capi.TransparentProxyConfig{
					OutboundListenerPort: 1000,
					DialedDirectly:       true,
				},
				UpstreamConfig: &capi.UpstreamConfiguration{
					Defaults: &capi.UpstreamConfig{
						Name:              "upstream-default",
						Namespace:         "ns",
						EnvoyListenerJSON: `{"key": "value"}`,
						EnvoyClusterJSON:  `{"key": "value"}`,
						Protocol:          "http2",
						ConnectTimeoutMs:  10,
						Limits: &capi.UpstreamLimits{
							MaxConnections:        intPointer(10),
							MaxPendingRequests:    intPointer(10),
							MaxConcurrentRequests: intPointer(10),
						},
						PassiveHealthCheck: &capi.PassiveHealthCheck{
							Interval:    2 * time.Second,
							MaxFailures: uint32(20),
						},
						MeshGateway: capi.MeshGatewayConfig{
							Mode: "local",
						},
					},
					Overrides: []*capi.UpstreamConfig{
						{
							Name:              "upstream-override-1",
							Namespace:         "ns",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  15,
							Limits: &capi.UpstreamLimits{
								MaxConnections:        intPointer(5),
								MaxPendingRequests:    intPointer(5),
								MaxConcurrentRequests: intPointer(5),
							},
							PassiveHealthCheck: &capi.PassiveHealthCheck{
								Interval:    2 * time.Second,
								MaxFailures: uint32(10),
							},
							MeshGateway: capi.MeshGatewayConfig{
								Mode: "remote",
							},
						},
						{
							Name:              "upstream-default",
							Namespace:         "ns",
							EnvoyListenerJSON: `{"key": "value"}`,
							EnvoyClusterJSON:  `{"key": "value"}`,
							Protocol:          "http2",
							ConnectTimeoutMs:  10,
							Limits: &capi.UpstreamLimits{
								MaxConnections:        intPointer(2),
								MaxPendingRequests:    intPointer(2),
								MaxConcurrentRequests: intPointer(2),
							},
							PassiveHealthCheck: &capi.PassiveHealthCheck{
								Interval:    2 * time.Second,
								MaxFailures: uint32(10),
							},
							MeshGateway: capi.MeshGatewayConfig{
								Mode: "remote",
							},
						},
					},
				},
			},
			true,
		},
		"mismatched types does not match": {
			&ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{},
			},
			&capi.ProxyConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			false,
		},
		// Consul's API returns the TransparentProxy object as empty
		// even when it was written as a nil pointer so test that we
		// treat the two as equal (https://github.com/hashicorp/consul/issues/10595).
		"empty transparentProxy object from Consul API matches nil pointer on CRD": {
			internal: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{
					// Passing a nil pointer here.
					TransparentProxy: nil,
				},
			},
			consul: &capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				// Consul will always return this even if it was written
				// as a nil pointer.
				TransparentProxy: &capi.TransparentProxyConfig{},
			},
			matches: true,
		},
		// Since we needed to add a special case to handle the nil pointer on
		// the CRD (see above test case), also test that if the CRD and API
		// have empty TransparentProxy structs that they're still equal to ensure
		// we didn't break something when adding the special case.
		"empty transparentProxy object from Consul API matches empty object on CRD": {
			internal: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-test-service",
				},
				Spec: ServiceDefaultsSpec{
					// Using the empty struct here.
					TransparentProxy: &TransparentProxy{},
				},
			},
			consul: &capi.ServiceConfigEntry{
				Kind:        capi.ServiceDefaults,
				Name:        "my-test-service",
				Namespace:   "namespace",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				// Consul will always return this even if it was written
				// as a nil pointer.
				TransparentProxy: &capi.TransparentProxyConfig{},
			},
			matches: true,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, testCase.matches, testCase.internal.MatchesConsul(testCase.consul))
		})
	}
}

func TestServiceDefaults_Validate(t *testing.T) {
	cases := map[string]struct {
		input             *ServiceDefaults
		partitionsEnabled bool
		expectedErrMsg    string
	}{
		"valid": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "remote",
					},
					Expose: Expose{
						Checks: false,
						Paths: []ExposePath{
							{
								ListenerPort:  100,
								Path:          "/bar",
								LocalPathPort: 1000,
								Protocol:      "",
							},
						},
					},
				},
			},
			expectedErrMsg: "",
		},
		"protocol": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "foo",
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.protocol: Invalid value: "foo": must be one of "tcp", "http", "http2", "grpc"`,
		},
		"meshgateway.mode": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "foobar",
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.meshGateway.mode: Invalid value: "foobar": must be one of "remote", "local", "none", ""`,
		},
		"expose.paths[].protocol": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "invalid-protocol",
								Path:     "/valid-path",
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.expose.paths[0].protocol: Invalid value: "invalid-protocol": must be one of "http", "http2"`,
		},
		"expose.paths[].path": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "http",
								Path:     "invalid-path",
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.expose.paths[0].path: Invalid value: "invalid-path": must begin with a '/'`,
		},
		"transparentProxy.outboundListenerPort": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
					},
				},
			},
			expectedErrMsg: "servicedefaults.consul.hashicorp.com \"my-service\" is invalid: spec.transparentProxy.outboundListenerPort: Invalid value: 1000: use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port",
		},
		"mode": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Mode: proxyModeRef("transparent"),
				},
			},
			expectedErrMsg: "servicedefaults.consul.hashicorp.com \"my-service\" is invalid: spec.mode: Invalid value: \"transparent\": use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode",
		},
		"upstreamConfig.defaults.meshGateway": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Defaults: &Upstream{
							MeshGateway: MeshGateway{
								Mode: "foo",
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.defaults.meshGateway.mode: Invalid value: "foo": must be one of "remote", "local", "none", ""`,
		},
		"upstreamConfig.defaults.name": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Defaults: &Upstream{
							Name: "foobar",
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.defaults.name: Invalid value: "foobar": upstream.name for a default upstream must be ""`,
		},
		"upstreamConfig.defaults.partition": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Defaults: &Upstream{
							Partition: "upstream",
						},
					},
				},
			},
			partitionsEnabled: false,
			expectedErrMsg:    `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.defaults.partition: Invalid value: "upstream": Consul Enterprise Admin Partitions must be enabled to set upstream.partition`,
		},
		"upstreamConfig.overrides.meshGateway": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Overrides: []*Upstream{
							{
								Name: "override",
								MeshGateway: MeshGateway{
									Mode: "foo",
								},
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.overrides[0].meshGateway.mode: Invalid value: "foo": must be one of "remote", "local", "none", ""`,
		},
		"upstreamConfig.overrides.name": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Overrides: []*Upstream{
							{
								Name: "",
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.overrides[0].name: Invalid value: "": upstream.name for an override upstream cannot be ""`,
		},
		"upstreamConfig.overrides.partition": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					UpstreamConfig: &Upstreams{
						Overrides: []*Upstream{
							{
								Name:      "service",
								Partition: "upstream",
							},
						},
					},
				},
			},
			expectedErrMsg: `servicedefaults.consul.hashicorp.com "my-service" is invalid: spec.upstreamConfig.overrides[0].partition: Invalid value: "upstream": Consul Enterprise Admin Partitions must be enabled to set upstream.partition`,
		},
		"multi-error": {
			input: &ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-service",
				},
				Spec: ServiceDefaultsSpec{
					Protocol: "invalid",
					MeshGateway: MeshGateway{
						Mode: "invalid-mode",
					},
					Expose: Expose{
						Paths: []ExposePath{
							{
								Protocol: "invalid-protocol",
								Path:     "invalid-path",
							},
						},
					},
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
					},
					Mode: proxyModeRef("transparent"),
				},
			},
			expectedErrMsg: "servicedefaults.consul.hashicorp.com \"my-service\" is invalid: [spec.protocol: Invalid value: \"invalid\": must be one of \"tcp\", \"http\", \"http2\", \"grpc\", spec.meshGateway.mode: Invalid value: \"invalid-mode\": must be one of \"remote\", \"local\", \"none\", \"\", spec.transparentProxy.outboundListenerPort: Invalid value: 1000: use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port, spec.mode: Invalid value: \"transparent\": use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode, spec.expose.paths[0].path: Invalid value: \"invalid-path\": must begin with a '/', spec.expose.paths[0].protocol: Invalid value: \"invalid-protocol\": must be one of \"http\", \"http2\"]",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{})
			if testCase.expectedErrMsg != "" {
				require.EqualError(t, err, testCase.expectedErrMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func proxyModeRef(mode string) *ProxyMode {
	proxyMode := ProxyMode(mode)
	return &proxyMode
}

func TestServiceDefaults_AddFinalizer(t *testing.T) {
	serviceDefaults := &ServiceDefaults{}
	serviceDefaults.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, serviceDefaults.ObjectMeta.Finalizers)
}

func TestServiceDefaults_RemoveFinalizer(t *testing.T) {
	serviceDefaults := &ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	serviceDefaults.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, serviceDefaults.ObjectMeta.Finalizers)
}

func TestServiceDefaults_SetSyncedCondition(t *testing.T) {
	serviceDefaults := &ServiceDefaults{}
	serviceDefaults.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, serviceDefaults.Status.Conditions[0].Status)
	require.Equal(t, "reason", serviceDefaults.Status.Conditions[0].Reason)
	require.Equal(t, "message", serviceDefaults.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, serviceDefaults.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestServiceDefaults_SetLastSyncedTime(t *testing.T) {
	serviceDefaults := &ServiceDefaults{}
	syncedTime := metav1.NewTime(time.Now())
	serviceDefaults.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, serviceDefaults.Status.LastSyncedTime)
}

func TestServiceDefaults_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			serviceDefaults := &ServiceDefaults{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, serviceDefaults.SyncedConditionStatus())
		})
	}
}

func TestServiceDefaults_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ServiceDefaults{}).GetCondition(ConditionSynced))
}

func TestServiceDefaults_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ServiceDefaults{}).SyncedConditionStatus())
}

func TestServiceDefaults_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ServiceDefaults{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestServiceDefaults_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ServiceDefaults, (&ServiceDefaults{}).ConsulKind())
}

func TestServiceDefaults_KubeKind(t *testing.T) {
	require.Equal(t, "servicedefaults", (&ServiceDefaults{}).KubeKind())
}

func TestServiceDefaults_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceDefaults_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestServiceDefaults_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&ServiceDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestServiceDefaults_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&ServiceDefaults{}).ConsulGlobalResource())
}

func TestServiceDefaults_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	serviceDefaults := &ServiceDefaults{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, serviceDefaults.GetObjectMeta())
}

func intPointer(i int) *int {
	return &i
}
