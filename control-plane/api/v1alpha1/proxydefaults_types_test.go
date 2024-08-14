// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Test MatchesConsul for cases that should return true.
func TestProxyDefaults_MatchesConsul(t *testing.T) {
	cases := map[string]struct {
		Ours    ProxyDefaults
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        common.Global,
				Kind:        capi.ProxyDefaults,
				Namespace:   "default",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
			Matches: true,
		},
		"all fields set matches": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
					MeshGateway: MeshGateway{
						Mode: "local",
					},
					Expose: Expose{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/test/path",
								LocalPathPort: 42,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/root/test/path",
								LocalPathPort: 4201,
								Protocol:      "https",
							},
						},
					},
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
						DialedDirectly:       true,
					},
					MutualTLSMode: MutualTLSModePermissive,
					AccessLogs: &AccessLogs{
						Enabled:             true,
						DisableListenerLogs: true,
						Type:                FileLogSinkType,
						Path:                "/var/log/envoy.logs",
						TextFormat:          "ITS WORKING %START_TIME%",
					},
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: json.RawMessage(`{"AWSServiceName": "s3", "Region": "us-west-2"}`),
							Required:  false,
						},
						EnvoyExtension{
							Name:      "zipkin",
							Arguments: json.RawMessage(`{"ClusterName": "zipkin_cluster", "Port": "9411", "CollectorEndpoint":"/api/v2/spans"}`),
							Required:  true,
						},
					},
					FailoverPolicy: &FailoverPolicy{
						Mode:    "sequential",
						Regions: []string{"us-west-1"},
					},
					PrioritizeByLocality: &PrioritizeByLocality{
						Mode: "failover",
					},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Kind: capi.ProxyDefaults,
				Name: common.Global,
				Config: map[string]interface{}{
					"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}",
				},
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeLocal,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/test/path",
							LocalPathPort: 42,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/root/test/path",
							LocalPathPort: 4201,
							Protocol:      "https",
						},
					},
				},
				TransparentProxy: &capi.TransparentProxyConfig{
					OutboundListenerPort: 1000,
					DialedDirectly:       true,
				},
				MutualTLSMode: capi.MutualTLSModePermissive,
				AccessLogs: &capi.AccessLogsConfig{
					Enabled:             true,
					DisableListenerLogs: true,
					Type:                capi.FileLogSinkType,
					Path:                "/var/log/envoy.logs",
					TextFormat:          "ITS WORKING %START_TIME%",
				},
				EnvoyExtensions: []capi.EnvoyExtension{
					{
						Name: "aws_request_signing",
						Arguments: map[string]interface{}{
							"AWSServiceName": "s3",
							"Region":         "us-west-2",
						},
						Required: false,
					},
					{
						Name: "zipkin",
						Arguments: map[string]interface{}{
							"ClusterName":       "zipkin_cluster",
							"Port":              "9411",
							"CollectorEndpoint": "/api/v2/spans",
						},
						Required: true,
					},
				},
				FailoverPolicy: &capi.ServiceResolverFailoverPolicy{
					Mode:    "sequential",
					Regions: []string{"us-west-1"},
				},
				PrioritizeByLocality: &capi.ServiceResolverPrioritizeByLocality{
					Mode: "failover",
				},
			},
			Matches: true,
		},
		"mismatched types does not match": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{},
			},
			Theirs: &capi.ServiceConfigEntry{
				Name: common.Global,
				Kind: capi.ProxyDefaults,
			},
			Matches: false,
		},
		// Consul's API returns the TransparentProxy object as empty
		// even when it was written as a nil pointer so test that we
		// treat the two as equal (https://github.com/hashicorp/consul/issues/10595).
		"empty transparentProxy object from Consul API matches nil pointer on CRD": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{
					// Passing a nil pointer here.
					TransparentProxy: nil,
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        common.Global,
				Kind:        capi.ProxyDefaults,
				Namespace:   "default",
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
			Matches: true,
		},
		// Since we needed to add a special case to handle the nil pointer on
		// the CRD (see above test case), also test that if the CRD and API
		// have empty TransparentProxy structs that they're still equal to ensure
		// we didn't break something when adding the special case.
		"empty transparentProxy object from Consul API matches empty object on CRD": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: common.Global,
				},
				Spec: ProxyDefaultsSpec{
					// Using the empty struct here.
					TransparentProxy: &TransparentProxy{},
				},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        common.Global,
				Kind:        capi.ProxyDefaults,
				Namespace:   "default",
				CreateIndex: 1,
				ModifyIndex: 2,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				TransparentProxy: &capi.TransparentProxyConfig{},
			},
			Matches: true,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestProxyDefaults_ToConsul(t *testing.T) {
	cases := map[string]struct {
		Ours ProxyDefaults
		Exp  *capi.ProxyConfigEntry
	}{
		"empty fields": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{},
			},
			Exp: &capi.ProxyConfigEntry{
				Name: "name",
				Kind: capi.ProxyDefaults,
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: ProxyDefaultsSpec{
					Config: json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
					MeshGateway: MeshGateway{
						Mode: "remote",
					},
					Expose: Expose{
						Checks: true,
						Paths: []ExposePath{
							{
								ListenerPort:  80,
								Path:          "/default",
								LocalPathPort: 9091,
								Protocol:      "tcp",
							},
							{
								ListenerPort:  8080,
								Path:          "/v2",
								LocalPathPort: 3001,
								Protocol:      "https",
							},
						},
					},
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
						DialedDirectly:       true,
					},
					MutualTLSMode: MutualTLSModeStrict,
					AccessLogs: &AccessLogs{
						Enabled:             true,
						DisableListenerLogs: true,
						Type:                FileLogSinkType,
						Path:                "/var/log/envoy.logs",
						TextFormat:          "ITS WORKING %START_TIME%",
					},
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: json.RawMessage(`{"AWSServiceName": "s3", "Region": "us-west-2"}`),
							Required:  false,
						},
						EnvoyExtension{
							Name:      "zipkin",
							Arguments: json.RawMessage(`{"ClusterName": "zipkin_cluster", "Port": "9411", "CollectorEndpoint":"/api/v2/spans"}`),
							Required:  true,
						},
					},
					FailoverPolicy: &FailoverPolicy{
						Mode:    "sequential",
						Regions: []string{"us-west-1"},
					},
					PrioritizeByLocality: &PrioritizeByLocality{
						Mode: "none",
					},
				},
			},
			Exp: &capi.ProxyConfigEntry{
				Kind:      capi.ProxyDefaults,
				Name:      "name",
				Namespace: "",
				Config: map[string]interface{}{
					"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}",
				},
				MeshGateway: capi.MeshGatewayConfig{
					Mode: capi.MeshGatewayModeRemote,
				},
				Expose: capi.ExposeConfig{
					Checks: true,
					Paths: []capi.ExposePath{
						{
							ListenerPort:  80,
							Path:          "/default",
							LocalPathPort: 9091,
							Protocol:      "tcp",
						},
						{
							ListenerPort:  8080,
							Path:          "/v2",
							LocalPathPort: 3001,
							Protocol:      "https",
						},
					},
				},
				TransparentProxy: &capi.TransparentProxyConfig{
					OutboundListenerPort: 1000,
					DialedDirectly:       true,
				},
				MutualTLSMode: capi.MutualTLSModeStrict,
				AccessLogs: &capi.AccessLogsConfig{
					Enabled:             true,
					DisableListenerLogs: true,
					Type:                capi.FileLogSinkType,
					Path:                "/var/log/envoy.logs",
					TextFormat:          "ITS WORKING %START_TIME%",
				},
				EnvoyExtensions: []capi.EnvoyExtension{
					{
						Name: "aws_request_signing",
						Arguments: map[string]interface{}{
							"AWSServiceName": "s3",
							"Region":         "us-west-2",
						},
						Required: false,
					},
					{
						Name: "zipkin",
						Arguments: map[string]interface{}{
							"ClusterName":       "zipkin_cluster",
							"Port":              "9411",
							"CollectorEndpoint": "/api/v2/spans",
						},
						Required: true,
					},
				},
				FailoverPolicy: &capi.ServiceResolverFailoverPolicy{
					Mode:    "sequential",
					Regions: []string{"us-west-1"},
				},
				PrioritizeByLocality: &capi.ServiceResolverPrioritizeByLocality{
					Mode: "none",
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul("datacenter")
			proxyDefaults, ok := act.(*capi.ProxyConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, proxyDefaults)
		})
	}
}

// Test validation for fields other than Config. Config is tested
// in separate tests below.
func TestProxyDefaults_Validate(t *testing.T) {
	cases := map[string]struct {
		input          *ProxyDefaults
		expectedErrMsg string
	}{
		"valid envoyExtension": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: json.RawMessage(`{"AWSServiceName": "s3", "Region": "us-west-2"}`),
							Required:  false,
						},
						EnvoyExtension{
							Name:      "zipkin",
							Arguments: json.RawMessage(`{"ClusterName": "zipkin_cluster", "Port": "9411", "CollectorEndpoint":"/api/v2/spans"}`),
							Required:  true,
						},
					},
				},
			},
			expectedErrMsg: "",
		},
		"meshgateway.mode": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					MeshGateway: MeshGateway{
						Mode: "foobar",
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.meshGateway.mode: Invalid value: "foobar": must be one of "remote", "local", "none", ""`,
		},
		"expose.paths[].protocol": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
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
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.expose.paths[0].protocol: Invalid value: "invalid-protocol": must be one of "http", "http2"`,
		},
		"expose.paths[].path": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
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
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.expose.paths[0].path: Invalid value: "invalid-path": must begin with a '/'`,
		},
		"transparentProxy.outboundListenerPort": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					TransparentProxy: &TransparentProxy{
						OutboundListenerPort: 1000,
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.transparentProxy.outboundListenerPort: Invalid value: 1000: use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port",
		},
		"mode": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					Mode: proxyModeRef("transparent"),
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.mode: Invalid value: \"transparent\": use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode",
		},
		"mutualTLSMode": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					MutualTLSMode: MutualTLSMode("asdf"),
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.mutualTLSMode: Invalid value: "asdf": Must be one of "", "strict", or "permissive".`,
		},
		"accessLogs.type": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					AccessLogs: &AccessLogs{
						Type: "foo",
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.accessLogs.type: Invalid value: \"foo\": invalid access log type (must be one of \"stdout\", \"stderr\", \"file\"",
		},
		"accessLogs.path missing": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					AccessLogs: &AccessLogs{
						Type: "file",
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.accessLogs.path: Invalid value: \"\": path must be specified when using file type access logs",
		},
		"accessLogs.path for wrong type": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					AccessLogs: &AccessLogs{
						Path: "/var/log/envoy.logs",
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.accessLogs.path: Invalid value: \"/var/log/envoy.logs\": path is only valid for file type access logs",
		},
		"accessLogs.jsonFormat": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					AccessLogs: &AccessLogs{
						JSONFormat: "{ \"start_time\": \"%START_TIME\"", // intentionally missing the closing brace
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.accessLogs.jsonFormat: Invalid value: \"{ \\\"start_time\\\": \\\"%START_TIME\\\"\": invalid access log json",
		},
		"accessLogs.textFormat": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					AccessLogs: &AccessLogs{
						JSONFormat: "{ \"start_time\": \"%START_TIME\" }",
						TextFormat: "MY START TIME %START_TIME",
					},
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: spec.accessLogs.textFormat: Invalid value: \"MY START TIME %START_TIME\": cannot specify both access log jsonFormat and textFormat",
		},
		"envoyExtension.arguments single empty": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: json.RawMessage(`{"AWSServiceName": "s3", "Region": "us-west-2"}`),
							Required:  false,
						},
						EnvoyExtension{
							Name:      "zipkin",
							Arguments: nil,
							Required:  true,
						},
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.envoyExtensions.envoyExtension[1].arguments: Required value: arguments must be defined`,
		},
		"envoyExtension.arguments multi empty": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: nil,
							Required:  false,
						},
						EnvoyExtension{
							Name:      "zipkin",
							Arguments: nil,
							Required:  true,
						},
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: [spec.envoyExtensions.envoyExtension[0].arguments: Required value: arguments must be defined, spec.envoyExtensions.envoyExtension[1].arguments: Required value: arguments must be defined]`,
		},
		"envoyExtension.arguments invalid json": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					EnvoyExtensions: EnvoyExtensions{
						EnvoyExtension{
							Name:      "aws_request_signing",
							Arguments: json.RawMessage(`{"SOME_INVALID_JSON"}`),
							Required:  false,
						},
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.envoyExtensions.envoyExtension[0].arguments: Invalid value: "{\"SOME_INVALID_JSON\"}": must be valid map value: invalid character '}' after object key`,
		},
		"failoverPolicy.mode invalid": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					FailoverPolicy: &FailoverPolicy{
						Mode: "wrong-mode",
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.failoverPolicy.mode: Invalid value: "wrong-mode": must be one of "", "sequential", "order-by-locality"`,
		},
		"prioritize by locality invalid": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
					PrioritizeByLocality: &PrioritizeByLocality{
						Mode: "wrong-mode",
					},
				},
			},
			expectedErrMsg: `proxydefaults.consul.hashicorp.com "global" is invalid: spec.prioritizeByLocality.mode: Invalid value: "wrong-mode": must be one of "", "none", "failover"`,
		},
		"multi-error": {
			input: &ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name: "global",
				},
				Spec: ProxyDefaultsSpec{
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
					AccessLogs: &AccessLogs{
						JSONFormat: "{ \"start_time\": \"%START_TIME\" }",
						TextFormat: "MY START TIME %START_TIME",
					},
					Mode: proxyModeRef("transparent"),
				},
			},
			expectedErrMsg: "proxydefaults.consul.hashicorp.com \"global\" is invalid: [spec.meshGateway.mode: Invalid value: \"invalid-mode\": must be one of \"remote\", \"local\", \"none\", \"\", spec.transparentProxy.outboundListenerPort: Invalid value: 1000: use the annotation `consul.hashicorp.com/transparent-proxy-outbound-listener-port` to configure the Outbound Listener Port, spec.mode: Invalid value: \"transparent\": use the annotation `consul.hashicorp.com/transparent-proxy` to configure the Transparent Proxy Mode, spec.accessLogs.textFormat: Invalid value: \"MY START TIME %START_TIME\": cannot specify both access log jsonFormat and textFormat, spec.expose.paths[0].path: Invalid value: \"invalid-path\": must begin with a '/', spec.expose.paths[0].protocol: Invalid value: \"invalid-protocol\": must be one of \"http\", \"http2\"]",
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

func TestProxyDefaults_ValidateConfigValid(t *testing.T) {
	cases := map[string]json.RawMessage{
		"envoy_tracing_json":                     json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"}`),
		"protocol":                               json.RawMessage(`{"protocol":  "http"}`),
		"members":                                json.RawMessage(`{"members":  3}`),
		"envoy_tracing_json & protocol":          json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","protocol":  "http"}`),
		"envoy_tracing_json & members":           json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","members":  3}`),
		"protocol & members":                     json.RawMessage(`{"protocol": "https","members":  3}`),
		"envoy_tracing_json, protocol & members": json.RawMessage(`{"envoy_tracing_json": "{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}","protocol":  "http", "members": 3}`),
	}
	for name, c := range cases {
		proxyDefaults := ProxyDefaults{
			ObjectMeta: metav1.ObjectMeta{
				Name: common.Global,
			},
			Spec: ProxyDefaultsSpec{
				Config: c,
			},
		}
		t.Run(name, func(t *testing.T) {
			require.Nil(t, proxyDefaults.validateConfig(nil))
		})
	}
}

func TestProxyDefaults_ValidateConfigInvalid(t *testing.T) {
	cases := map[string]json.RawMessage{
		"non_map json": json.RawMessage(`"{\"http\":{\"name\":\"envoy.zipkin\",\"config\":{\"collector_cluster\":\"zipkin\",\"collector_endpoint\":\"/api/v1/spans\",\"shared_span_context\":false}}}"`),
		"yaml":         json.RawMessage(`protocol: http`),
		"json array":   json.RawMessage(`[1,2,3,4]`),
		"json literal": json.RawMessage(`1`),
	}
	for name, c := range cases {
		proxyDefaults := ProxyDefaults{
			ObjectMeta: metav1.ObjectMeta{
				Name: common.Global,
			},
			Spec: ProxyDefaultsSpec{
				Config: c,
			},
		}
		t.Run(name, func(t *testing.T) {
			require.Contains(t, proxyDefaults.validateConfig(field.NewPath("spec")).Detail, "must be valid map value")
		})
	}
}

func TestProxyDefaults_AddFinalizer(t *testing.T) {
	proxyDefaults := &ProxyDefaults{}
	proxyDefaults.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, proxyDefaults.ObjectMeta.Finalizers)
}

func TestProxyDefaults_RemoveFinalizer(t *testing.T) {
	proxyDefaults := &ProxyDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	proxyDefaults.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, proxyDefaults.ObjectMeta.Finalizers)
}

func TestProxyDefaults_SetSyncedCondition(t *testing.T) {
	proxyDefaults := &ProxyDefaults{}
	proxyDefaults.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, proxyDefaults.Status.Conditions[0].Status)
	require.Equal(t, "reason", proxyDefaults.Status.Conditions[0].Reason)
	require.Equal(t, "message", proxyDefaults.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, proxyDefaults.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestProxyDefaults_SetLastSyncedTime(t *testing.T) {
	proxyDefaults := &ProxyDefaults{}
	syncedTime := metav1.NewTime(time.Now())
	proxyDefaults.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, proxyDefaults.Status.LastSyncedTime)
}

func TestProxyDefaults_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			proxyDefaults := &ProxyDefaults{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, proxyDefaults.SyncedConditionStatus())
		})
	}
}

func TestProxyDefaults_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&ProxyDefaults{}).GetCondition(ConditionSynced))
}

func TestProxyDefaults_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&ProxyDefaults{}).SyncedConditionStatus())
}

func TestProxyDefaults_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&ProxyDefaults{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestProxyDefaults_ConsulKind(t *testing.T) {
	require.Equal(t, capi.ProxyDefaults, (&ProxyDefaults{}).ConsulKind())
}

func TestProxyDefaults_KubeKind(t *testing.T) {
	require.Equal(t, "proxydefaults", (&ProxyDefaults{}).KubeKind())
}

func TestProxyDefaults_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&ProxyDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestProxyDefaults_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&ProxyDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestProxyDefaults_ConsulNamespace(t *testing.T) {
	require.Equal(t, common.DefaultConsulNamespace, (&ProxyDefaults{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestProxyDefaults_ConsulGlobalResource(t *testing.T) {
	require.True(t, (&ProxyDefaults{}).ConsulGlobalResource())
}

func TestProxyDefaults_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	proxyDefaults := &ProxyDefaults{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, proxyDefaults.GetObjectMeta())
}
