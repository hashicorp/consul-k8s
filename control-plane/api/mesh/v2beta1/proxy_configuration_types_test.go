// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v2beta1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	pbmesh "github.com/hashicorp/consul/proto-public/pbmesh/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestProxyConfiguration_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		OurConsulNamespace string
		OurConsulPartition string
		OurData            *ProxyConfiguration

		TheirName            string
		TheirConsulNamespace string
		TheirConsulPartition string
		TheirData            *pbmesh.ProxyConfiguration
		ResourceOverride     *pbresource.Resource // Used to test that an empty resource of another type will not match

		Matches bool
	}{
		"empty fields matches": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.ProxyConfiguration{},
			},
			TheirName:            "name",
			TheirConsulNamespace: constants.DefaultConsulNS,
			TheirConsulPartition: constants.DefaultConsulPartition,
			TheirData:            &pbmesh.ProxyConfiguration{},
			Matches:              true,
		},
		"all fields set matches": {
			OurConsulNamespace: "consul-ns",
			OurConsulPartition: "consul-partition",
			OurData: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"prefix-1", "prefix-2"},
						Names:    []string{"workload-name"},
						Filter:   "first-filter",
					},
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: 2,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 1234,
							DialedDirectly:       true,
						},
						MutualTlsMode: 1,
						LocalConnection: map[string]*pbmesh.ConnectionConfig{
							"local": {
								ConnectTimeout: &durationpb.Duration{
									Seconds: 5,
									Nanos:   10,
								},
								RequestTimeout: &durationpb.Duration{
									Seconds: 2,
									Nanos:   10,
								},
							},
						},
						InboundConnections: &pbmesh.InboundConnectionsConfig{
							MaxInboundConnections:     5,
							BalanceInboundConnections: 10,
						},
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_LOCAL,
						ExposeConfig: &pbmesh.ExposeConfig{
							ExposePaths: []*pbmesh.ExposePath{
								{
									ListenerPort:  19000,
									Path:          "/expose-path",
									LocalPathPort: 1901,
									Protocol:      2,
								},
							},
						},
						AccessLogs: &pbmesh.AccessLogsConfig{
							Enabled:             true,
							DisableListenerLogs: true,
							Type:                3,
							Path:                "/path",
							JsonFormat:          "jsonFormat",
							TextFormat:          "text format.",
						},
						PublicListenerJson:  "publicListenerJson{}",
						ListenerTracingJson: "listenerTracingJson{}",
						LocalClusterJson:    "localClusterJson{}",
					},
					BootstrapConfig: &pbmesh.BootstrapConfig{
						StatsdUrl:                       "statsdURL",
						DogstatsdUrl:                    "dogstatsdURL",
						StatsTags:                       []string{"statsTags"},
						PrometheusBindAddr:              "firstBindAddr",
						StatsBindAddr:                   "secondBindAddr",
						ReadyBindAddr:                   "thirdBindAddr",
						OverrideJsonTpl:                 "overrideJSON",
						StaticClustersJson:              "staticClusterJSON",
						StaticListenersJson:             "staticListenersJSON",
						StatsSinksJson:                  "statsSinksJSON",
						StatsConfigJson:                 "statsConfigJSON",
						StatsFlushInterval:              "45s",
						TracingConfigJson:               "tracingConfigJSON",
						TelemetryCollectorBindSocketDir: "/bindSocketDir",
					},
				},
			},
			TheirName:            "foo",
			TheirConsulNamespace: "consul-ns",
			TheirConsulPartition: "consul-partition",
			TheirData: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Prefixes: []string{"prefix-1", "prefix-2"},
					Names:    []string{"workload-name"},
					Filter:   "first-filter",
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: 2,
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 1234,
						DialedDirectly:       true,
					},
					MutualTlsMode: 1,
					LocalConnection: map[string]*pbmesh.ConnectionConfig{
						"local": {
							ConnectTimeout: &durationpb.Duration{
								Seconds: 5,
								Nanos:   10,
							},
							RequestTimeout: &durationpb.Duration{
								Seconds: 2,
								Nanos:   10,
							},
						},
					},
					InboundConnections: &pbmesh.InboundConnectionsConfig{
						MaxInboundConnections:     5,
						BalanceInboundConnections: 10,
					},
					MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_LOCAL,
					ExposeConfig: &pbmesh.ExposeConfig{
						ExposePaths: []*pbmesh.ExposePath{
							{
								ListenerPort:  19000,
								Path:          "/expose-path",
								LocalPathPort: 1901,
								Protocol:      2,
							},
						},
					},
					AccessLogs: &pbmesh.AccessLogsConfig{
						Enabled:             true,
						DisableListenerLogs: true,
						Type:                3,
						Path:                "/path",
						JsonFormat:          "jsonFormat",
						TextFormat:          "text format.",
					},
					PublicListenerJson:  "publicListenerJson{}",
					ListenerTracingJson: "listenerTracingJson{}",
					LocalClusterJson:    "localClusterJson{}",
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					StatsdUrl:                       "statsdURL",
					DogstatsdUrl:                    "dogstatsdURL",
					StatsTags:                       []string{"statsTags"},
					PrometheusBindAddr:              "firstBindAddr",
					StatsBindAddr:                   "secondBindAddr",
					ReadyBindAddr:                   "thirdBindAddr",
					OverrideJsonTpl:                 "overrideJSON",
					StaticClustersJson:              "staticClusterJSON",
					StaticListenersJson:             "staticListenersJSON",
					StatsSinksJson:                  "statsSinksJSON",
					StatsConfigJson:                 "statsConfigJSON",
					StatsFlushInterval:              "45s",
					TracingConfigJson:               "tracingConfigJSON",
					TelemetryCollectorBindSocketDir: "/bindSocketDir",
				},
			},
			Matches: true,
		},
		"different types does not match": {
			OurConsulNamespace: constants.DefaultConsulNS,
			OurConsulPartition: constants.DefaultConsulPartition,
			OurData: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: pbmesh.ProxyConfiguration{},
			},
			ResourceOverride: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "name",
					Type: pbmesh.TCPRouteType,
					Tenancy: &pbresource.Tenancy{
						Partition: constants.DefaultConsulNS,
						Namespace: constants.DefaultConsulPartition,

						// Because we are explicitly defining NS/partition, this will not default and must be explicit.
						// At a future point, this will move out of the Tenancy block.
						PeerName: constants.DefaultConsulPeer,
					},
				},
				Data:     inject.ToProtoAny(&pbmesh.ProxyConfiguration{}),
				Metadata: meshConfigMeta(),
			},
			Matches: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			consulResource := c.ResourceOverride
			if c.TheirName != "" {
				consulResource = constructProxyConfigurationResource(c.TheirData, c.TheirName, c.TheirConsulNamespace, c.TheirConsulPartition)
			}
			require.Equal(t, c.Matches, c.OurData.MatchesConsul(consulResource, c.OurConsulNamespace, c.OurConsulPartition))
		})
	}
}

// TestProxyConfiguration_Resource also includes test to verify ResourceID().
func TestProxyConfiguration_Resource(t *testing.T) {
	cases := map[string]struct {
		Ours            *ProxyConfiguration
		ConsulNamespace string
		ConsulPartition string
		ExpectedName    string
		ExpectedData    *pbmesh.ProxyConfiguration
	}{
		"empty fields": {
			Ours: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: pbmesh.ProxyConfiguration{},
			},
			ConsulNamespace: constants.DefaultConsulNS,
			ConsulPartition: constants.DefaultConsulPartition,
			ExpectedName:    "foo",
			ExpectedData:    &pbmesh.ProxyConfiguration{},
		},
		"every field set": {
			Ours: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "kube-ns",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{"prefix-1", "prefix-2"},
						Names:    []string{"workload-name"},
						Filter:   "first-filter",
					},
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: 2,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 1234,
							DialedDirectly:       true,
						},
						MutualTlsMode: 1,
						LocalConnection: map[string]*pbmesh.ConnectionConfig{
							"local": {
								ConnectTimeout: &durationpb.Duration{
									Seconds: 5,
									Nanos:   10,
								},
								RequestTimeout: &durationpb.Duration{
									Seconds: 2,
									Nanos:   10,
								},
							},
						},
						InboundConnections: &pbmesh.InboundConnectionsConfig{
							MaxInboundConnections:     5,
							BalanceInboundConnections: 10,
						},
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_LOCAL,
						ExposeConfig: &pbmesh.ExposeConfig{
							ExposePaths: []*pbmesh.ExposePath{
								{
									ListenerPort:  19000,
									Path:          "/expose-path",
									LocalPathPort: 1901,
									Protocol:      2,
								},
							},
						},
						AccessLogs: &pbmesh.AccessLogsConfig{
							Enabled:             true,
							DisableListenerLogs: true,
							Type:                3,
							Path:                "/path",
							JsonFormat:          "jsonFormat",
							TextFormat:          "text format.",
						},
						PublicListenerJson:  "publicListenerJson{}",
						ListenerTracingJson: "listenerTracingJson{}",
						LocalClusterJson:    "localClusterJson{}",
					},
					BootstrapConfig: &pbmesh.BootstrapConfig{
						StatsdUrl:                       "statsdURL",
						DogstatsdUrl:                    "dogstatsdURL",
						StatsTags:                       []string{"statsTags"},
						PrometheusBindAddr:              "firstBindAddr",
						StatsBindAddr:                   "secondBindAddr",
						ReadyBindAddr:                   "thirdBindAddr",
						OverrideJsonTpl:                 "overrideJSON",
						StaticClustersJson:              "staticClusterJSON",
						StaticListenersJson:             "staticListenersJSON",
						StatsSinksJson:                  "statsSinksJSON",
						StatsConfigJson:                 "statsConfigJSON",
						StatsFlushInterval:              "45s",
						TracingConfigJson:               "tracingConfigJSON",
						TelemetryCollectorBindSocketDir: "/bindSocketDir",
					},
				},
			},
			ConsulNamespace: "not-default-namespace",
			ConsulPartition: "not-default-partition",
			ExpectedName:    "foo",
			ExpectedData: &pbmesh.ProxyConfiguration{
				Workloads: &pbcatalog.WorkloadSelector{
					Prefixes: []string{"prefix-1", "prefix-2"},
					Names:    []string{"workload-name"},
					Filter:   "first-filter",
				},
				DynamicConfig: &pbmesh.DynamicConfig{
					Mode: 2,
					TransparentProxy: &pbmesh.TransparentProxy{
						OutboundListenerPort: 1234,
						DialedDirectly:       true,
					},
					MutualTlsMode: 1,
					LocalConnection: map[string]*pbmesh.ConnectionConfig{
						"local": {
							ConnectTimeout: &durationpb.Duration{
								Seconds: 5,
								Nanos:   10,
							},
							RequestTimeout: &durationpb.Duration{
								Seconds: 2,
								Nanos:   10,
							},
						},
					},
					InboundConnections: &pbmesh.InboundConnectionsConfig{
						MaxInboundConnections:     5,
						BalanceInboundConnections: 10,
					},
					MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_LOCAL,
					ExposeConfig: &pbmesh.ExposeConfig{
						ExposePaths: []*pbmesh.ExposePath{
							{
								ListenerPort:  19000,
								Path:          "/expose-path",
								LocalPathPort: 1901,
								Protocol:      2,
							},
						},
					},
					AccessLogs: &pbmesh.AccessLogsConfig{
						Enabled:             true,
						DisableListenerLogs: true,
						Type:                3,
						Path:                "/path",
						JsonFormat:          "jsonFormat",
						TextFormat:          "text format.",
					},
					PublicListenerJson:  "publicListenerJson{}",
					ListenerTracingJson: "listenerTracingJson{}",
					LocalClusterJson:    "localClusterJson{}",
				},
				BootstrapConfig: &pbmesh.BootstrapConfig{
					StatsdUrl:                       "statsdURL",
					DogstatsdUrl:                    "dogstatsdURL",
					StatsTags:                       []string{"statsTags"},
					PrometheusBindAddr:              "firstBindAddr",
					StatsBindAddr:                   "secondBindAddr",
					ReadyBindAddr:                   "thirdBindAddr",
					OverrideJsonTpl:                 "overrideJSON",
					StaticClustersJson:              "staticClusterJSON",
					StaticListenersJson:             "staticListenersJSON",
					StatsSinksJson:                  "statsSinksJSON",
					StatsConfigJson:                 "statsConfigJSON",
					StatsFlushInterval:              "45s",
					TracingConfigJson:               "tracingConfigJSON",
					TelemetryCollectorBindSocketDir: "/bindSocketDir",
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			actual := c.Ours.Resource(c.ConsulNamespace, c.ConsulPartition)
			expected := constructProxyConfigurationResource(c.ExpectedData, c.ExpectedName, c.ConsulNamespace, c.ConsulPartition)

			opts := append([]cmp.Option{
				protocmp.IgnoreFields(&pbresource.Resource{}, "status", "generation", "version"),
				protocmp.IgnoreFields(&pbresource.ID{}, "uid"),
			}, test.CmpProtoIgnoreOrder()...)
			diff := cmp.Diff(expected, actual, opts...)
			require.Equal(t, "", diff, "ProxyConfiguration do not match")
		})
	}
}

func TestProxyConfiguration_Validate(t *testing.T) {
	cases := []struct {
		name            string
		input           *ProxyConfiguration
		expectedErrMsgs []string
	}{
		{
			name: "kitchen sink OK",
			input: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"test-prefix",
						},
						Names: []string{
							"test-name",
							"test-other-name",
						},
						Filter: "test-filter",
					},
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 1500,
							DialedDirectly:       false,
						},
						MutualTlsMode: pbmesh.MutualTLSMode_MUTUAL_TLS_MODE_DEFAULT,
						LocalConnection: map[string]*pbmesh.ConnectionConfig{
							"test-connection": {
								ConnectTimeout: &durationpb.Duration{Seconds: 5},
								RequestTimeout: &durationpb.Duration{Seconds: 10},
							},
						},
						InboundConnections: &pbmesh.InboundConnectionsConfig{
							MaxInboundConnections:     1,
							BalanceInboundConnections: 5,
						},
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_UNSPECIFIED,
						ExposeConfig: &pbmesh.ExposeConfig{
							ExposePaths: []*pbmesh.ExposePath{
								{
									ListenerPort:  80,
									Path:          "/test-path",
									LocalPathPort: 8080,
									Protocol:      pbmesh.ExposePathProtocol_EXPOSE_PATH_PROTOCOL_HTTP2,
								},
							},
						},
					},
					BootstrapConfig: &pbmesh.BootstrapConfig{
						StatsdUrl:                       "statsdUrl",
						DogstatsdUrl:                    "dogstatsUrl",
						StatsTags:                       []string{"test-tags", "another-test-tag"},
						PrometheusBindAddr:              "promBindAddr",
						StatsBindAddr:                   "statsBindAddr",
						ReadyBindAddr:                   "readyBindAddr",
						OverrideJsonTpl:                 "overrideJsonTpl",
						StaticClustersJson:              "staticClustersJson",
						StaticListenersJson:             "staticListenersJson",
						StatsSinksJson:                  "statsSinkJson",
						StatsConfigJson:                 "statsConfigJson",
						StatsFlushInterval:              "statsFlushInterval",
						TracingConfigJson:               "tracingConfigJson",
						TelemetryCollectorBindSocketDir: "bindSocketDir/",
					},
				},
			},
			expectedErrMsgs: nil,
		},
		{
			name: "missing workloads",
			input: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: pbmesh.ProxyConfiguration{
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 1500,
							DialedDirectly:       false,
						},
						MutualTlsMode: pbmesh.MutualTLSMode_MUTUAL_TLS_MODE_DEFAULT,
						LocalConnection: map[string]*pbmesh.ConnectionConfig{
							"test-connection": {
								ConnectTimeout: &durationpb.Duration{Seconds: 5},
								RequestTimeout: &durationpb.Duration{Seconds: 10},
							},
						},
						InboundConnections: &pbmesh.InboundConnectionsConfig{
							MaxInboundConnections:     1,
							BalanceInboundConnections: 5,
						},
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_UNSPECIFIED,
						ExposeConfig: &pbmesh.ExposeConfig{
							ExposePaths: []*pbmesh.ExposePath{
								{
									ListenerPort:  80,
									Path:          "/test-path",
									LocalPathPort: 8080,
									Protocol:      pbmesh.ExposePathProtocol_EXPOSE_PATH_PROTOCOL_HTTP2,
								},
							},
						},
					},
					BootstrapConfig: &pbmesh.BootstrapConfig{
						StatsdUrl:                       "statsdUrl",
						DogstatsdUrl:                    "dogstatsUrl",
						StatsTags:                       []string{"test-tags", "another-test-tag"},
						PrometheusBindAddr:              "promBindAddr",
						StatsBindAddr:                   "statsBindAddr",
						ReadyBindAddr:                   "readyBindAddr",
						OverrideJsonTpl:                 "overrideJsonTpl",
						StaticClustersJson:              "staticClustersJson",
						StaticListenersJson:             "staticListenersJson",
						StatsSinksJson:                  "statsSinkJson",
						StatsConfigJson:                 "statsConfigJson",
						StatsFlushInterval:              "statsFlushInterval",
						TracingConfigJson:               "tracingConfigJson",
						TelemetryCollectorBindSocketDir: "bindSocketDir/",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.workloads: Required value: cannot be empty`,
			},
		},
		{
			name: "missing dynamic config and bootstrap config",
			input: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"test-prefix",
						},
						Names: []string{
							"test-name",
							"test-other-name",
						},
						Filter: "test-filter",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec: Required value: at least one of "bootstrap_config" or "dynamic_config" fields must be set`,
			},
		},
		{
			name: "unsupported dynamic config fields",
			input: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"test-prefix",
						},
						Names: []string{
							"test-name",
							"test-other-name",
						},
						Filter: "test-filter",
					},
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 8080,
							DialedDirectly:       true, // unsupported.
						},
						MutualTlsMode:   pbmesh.MutualTLSMode_MUTUAL_TLS_MODE_PERMISSIVE, // unsupported.
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_REMOTE, // unsupported.
						AccessLogs: &pbmesh.AccessLogsConfig{ // unsupported.
							Enabled:             true,
							DisableListenerLogs: true,
						},
						PublicListenerJson:      "unsupported",
						ListenerTracingJson:     "unsupported",
						LocalClusterJson:        "unsupported",
						LocalWorkloadAddress:    "unsupported",
						LocalWorkloadPort:       8080,
						LocalWorkloadSocketPath: "unsupported",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.dynamicConfig.mutualTlsMode: Invalid value: MUTUAL_TLS_MODE_PERMISSIVE: field is currently not supported`,
				`spec.dynamicConfig.meshGatewayMode: Invalid value: MESH_GATEWAY_MODE_REMOTE: field is currently not supported`,
				`spec.dynamicConfig.accessLogs: Invalid value`,
				`spec.dynamicConfig.publicListenerJson: Invalid value: "unsupported": field is currently not supported`,
				`spec.dynamicConfig.listenerTracingJson: Invalid value: "unsupported": field is currently not supported`,
				`spec.dynamicConfig.localClusterJson: Invalid value: "unsupported": field is currently not supported`,
				`spec.dynamicConfig.localWorkloadAddress: Invalid value: "unsupported": field is currently not supported`,
				`spec.dynamicConfig.localWorkloadPort: Invalid value: 0x1f90: field is currently not supported`,
				`spec.dynamicConfig.localWorkloadSocketPath: Invalid value: "unsupported": field is currently not supported`,
				`spec.dynamicConfig.transparentProxy.dialedDirectely: Invalid value: true: field is currently not supported`,
			},
		},
		{
			name: "invalid ports",
			input: &ProxyConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-config",
				},
				Spec: pbmesh.ProxyConfiguration{
					Workloads: &pbcatalog.WorkloadSelector{
						Prefixes: []string{
							"test-prefix",
						},
						Names: []string{
							"test-name",
							"test-other-name",
						},
						Filter: "test-filter",
					},
					DynamicConfig: &pbmesh.DynamicConfig{
						Mode: pbmesh.ProxyMode_PROXY_MODE_TRANSPARENT,
						TransparentProxy: &pbmesh.TransparentProxy{
							OutboundListenerPort: 0,
							DialedDirectly:       false,
						},
						MutualTlsMode: pbmesh.MutualTLSMode_MUTUAL_TLS_MODE_DEFAULT,
						LocalConnection: map[string]*pbmesh.ConnectionConfig{
							"test-connection": {
								ConnectTimeout: &durationpb.Duration{Seconds: 5},
								RequestTimeout: &durationpb.Duration{Seconds: 10},
							},
						},
						InboundConnections: &pbmesh.InboundConnectionsConfig{
							MaxInboundConnections:     1,
							BalanceInboundConnections: 5,
						},
						MeshGatewayMode: pbmesh.MeshGatewayMode_MESH_GATEWAY_MODE_UNSPECIFIED,
						ExposeConfig: &pbmesh.ExposeConfig{
							ExposePaths: []*pbmesh.ExposePath{
								{
									ListenerPort:  0,
									Path:          "/test-path",
									LocalPathPort: 0,
									Protocol:      pbmesh.ExposePathProtocol_EXPOSE_PATH_PROTOCOL_HTTP2,
								},
							},
						},
					},
					BootstrapConfig: &pbmesh.BootstrapConfig{
						StatsdUrl:                       "statsdUrl",
						DogstatsdUrl:                    "dogstatsUrl",
						StatsTags:                       []string{"test-tags", "another-test-tag"},
						PrometheusBindAddr:              "promBindAddr",
						StatsBindAddr:                   "statsBindAddr",
						ReadyBindAddr:                   "readyBindAddr",
						OverrideJsonTpl:                 "overrideJsonTpl",
						StaticClustersJson:              "staticClustersJson",
						StaticListenersJson:             "staticListenersJson",
						StatsSinksJson:                  "statsSinkJson",
						StatsConfigJson:                 "statsConfigJson",
						StatsFlushInterval:              "statsFlushInterval",
						TracingConfigJson:               "tracingConfigJson",
						TelemetryCollectorBindSocketDir: "bindSocketDir/",
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.dynamicConfig.transparentProxy.outboundListenerPort: Invalid value: 0x0: port number is outside the range 1 to 65535`,
				`spec.dynamicConfig.exposeConfig.exposePaths[0].listenerPort: Invalid value: 0x0: port number is outside the range 1 to 65535`,
				`spec.dynamicConfig.exposeConfig.exposePaths[0].localPathPort: Invalid value: 0x0: port number is outside the range 1 to 65535`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate(common.ConsulTenancyConfig{})
			if len(tc.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range tc.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestProxyConfiguration_SetSyncedCondition(t *testing.T) {
	trafficPermissions := &ProxyConfiguration{}
	trafficPermissions.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, trafficPermissions.Status.Conditions[0].Status)
	require.Equal(t, "reason", trafficPermissions.Status.Conditions[0].Reason)
	require.Equal(t, "message", trafficPermissions.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
	require.True(t, trafficPermissions.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestProxyConfiguration_SetLastSyncedTime(t *testing.T) {
	trafficPermissions := &ProxyConfiguration{}
	syncedTime := metav1.NewTime(time.Now())
	trafficPermissions.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, trafficPermissions.Status.LastSyncedTime)
}

func TestProxyConfiguration_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			trafficPermissions := &ProxyConfiguration{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, trafficPermissions.SyncedConditionStatus())
		})
	}
}

func TestProxyConfiguration_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ProxyConfiguration{}).GetCondition(ConditionSynced))
}

func TestProxyConfiguration_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ProxyConfiguration{}).SyncedConditionStatus())
}

func TestProxyConfiguration_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ProxyConfiguration{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestProxyConfiguration_KubeKind(t *testing.T) {
	require.Equal(t, "proxyconfiguration", (&ProxyConfiguration{}).KubeKind())
}

func TestProxyConfiguration_KubernetesName(t *testing.T) {
	require.Equal(t, "test", (&ProxyConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "bar",
		},
		Spec: pbmesh.ProxyConfiguration{},
	}).KubernetesName())
}

func TestProxyConfiguration_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	trafficPermissions := &ProxyConfiguration{
		ObjectMeta: meta,
	}
	require.Equal(t, &meta, trafficPermissions.GetObjectMeta())
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
// TODO: add when implemented
//func TestProxyConfiguration_DefaultNamespaceFields(t *testing.T)

func constructProxyConfigurationResource(tp *pbmesh.ProxyConfiguration, name, namespace, partition string) *pbresource.Resource {
	data := inject.ToProtoAny(tp)

	id := &pbresource.ID{
		Name: name,
		Type: pbmesh.ProxyConfigurationType,
		Tenancy: &pbresource.Tenancy{
			Partition: partition,
			Namespace: namespace,

			// Because we are explicitly defining NS/partition, this will not default and must be explicit.
			// At a future point, this will move out of the Tenancy block.
			PeerName: constants.DefaultConsulPeer,
		},
		Uid: "ABCD", // We add this to show it does not factor into the comparison
	}

	return &pbresource.Resource{
		Id:       id,
		Data:     data,
		Metadata: meshConfigMeta(),

		// We add the fields below to prove that they are not used in the Match when comparing the CRD to Consul.
		Version:    "123456",
		Generation: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Status: map[string]*pbresource.Status{
			"knock": {
				ObservedGeneration: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
				Conditions:         make([]*pbresource.Condition, 0),
				UpdatedAt:          timestamppb.Now(),
			},
		},
	}
}
