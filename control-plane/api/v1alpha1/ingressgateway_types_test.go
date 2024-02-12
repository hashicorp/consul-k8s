// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package v1alpha1

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
)

func TestIngressGateway_MatchesConsul(t *testing.T) {

	defaultMaxConnections := uint32(100)
	defaultMaxPendingRequests := uint32(101)
	defaultMaxConcurrentRequests := uint32(102)

	maxConnections := uint32(200)
	maxPendingRequests := uint32(201)
	maxConcurrentRequests := uint32(202)

	cases := map[string]struct {
		Ours    IngressGateway
		Theirs  capi.ConfigEntry
		Matches bool
	}{
		"empty fields matches": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Theirs: &capi.IngressGatewayConfigEntry{
				Kind:      capi.IngressGateway,
				Name:      "name",
				Namespace: "foobar",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"all fields set matches": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						Enabled: true,
						SDS: &GatewayTLSSDSConfig{
							ClusterName:  "cluster1",
							CertResource: "cert1",
						},
						TLSMinVersion: "TLSv1_0",
						TLSMaxVersion: "TLSv1_1",
						CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
					},
					Defaults: &IngressServiceConfig{
						MaxConnections:        &defaultMaxConnections,
						MaxPendingRequests:    &defaultMaxPendingRequests,
						MaxConcurrentRequests: &defaultMaxConcurrentRequests,
						PassiveHealthCheck: &PassiveHealthCheck{
							Interval: metav1.Duration{
								Duration: 2 * time.Second,
							},
							MaxFailures:             uint32(20),
							EnforcingConsecutive5xx: pointer.Uint32(100),
							MaxEjectionPercent:      pointer.Uint32(10),
							BaseEjectionTime: &metav1.Duration{
								Duration: 10 * time.Second,
							},
						},
					},
					Listeners: []IngressListener{
						{
							Port:     8888,
							Protocol: "tcp",
							TLS: &GatewayTLSConfig{
								Enabled: true,
								SDS: &GatewayTLSSDSConfig{
									ClusterName:  "cluster1",
									CertResource: "cert1",
								},
								TLSMinVersion: "TLSv1_0",
								TLSMaxVersion: "TLSv1_1",
								CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
							},
							Services: []IngressService{
								{
									Name:      "name1",
									Hosts:     []string{"host1_1", "host1_2"},
									Namespace: "ns1",
									IngressServiceConfig: IngressServiceConfig{
										MaxConnections:        &maxConnections,
										MaxPendingRequests:    &maxPendingRequests,
										MaxConcurrentRequests: &maxConcurrentRequests,
									},
									TLS: &GatewayServiceTLSConfig{
										SDS: &GatewayTLSSDSConfig{
											ClusterName:  "cluster1",
											CertResource: "cert1",
										},
									},
									RequestHeaders: &HTTPHeaderModifiers{
										Add: map[string]string{
											"foo":    "bar",
											"source": "dest",
										},
										Set: map[string]string{
											"bar": "baz",
											"key": "car",
										},
										Remove: []string{
											"foo",
											"bar",
											"baz",
										},
									},
									ResponseHeaders: &HTTPHeaderModifiers{
										Add: map[string]string{
											"doo":    "var",
											"aource": "sest",
										},
										Set: map[string]string{
											"var": "vaz",
											"jey": "xar",
										},
										Remove: []string{
											"doo",
											"var",
											"vaz",
										},
									},
								},
								{
									Name:      "name2",
									Hosts:     []string{"host2_1", "host2_2"},
									Namespace: "ns2",
								},
							},
						},
						{
							Port:     9999,
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			Theirs: &capi.IngressGatewayConfigEntry{
				Kind:      capi.IngressGateway,
				Name:      "name",
				Namespace: "foobar",
				TLS: capi.GatewayTLSConfig{
					Enabled: true,
					SDS: &capi.GatewayTLSSDSConfig{
						ClusterName:  "cluster1",
						CertResource: "cert1",
					},
					TLSMinVersion: "TLSv1_0",
					TLSMaxVersion: "TLSv1_1",
					CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
				},
				Defaults: &capi.IngressServiceConfig{
					MaxConnections:        &defaultMaxConnections,
					MaxPendingRequests:    &defaultMaxPendingRequests,
					MaxConcurrentRequests: &defaultMaxConcurrentRequests,
					PassiveHealthCheck: &capi.PassiveHealthCheck{
						Interval:                2 * time.Second,
						MaxFailures:             uint32(20),
						EnforcingConsecutive5xx: pointer.Uint32(100),
						MaxEjectionPercent:      pointer.Uint32(10),
						BaseEjectionTime:        pointer.Duration(10 * time.Second),
					},
				},
				Listeners: []capi.IngressListener{
					{
						Port:     8888,
						Protocol: "tcp",
						TLS: &capi.GatewayTLSConfig{
							Enabled: true,
							SDS: &capi.GatewayTLSSDSConfig{
								ClusterName:  "cluster1",
								CertResource: "cert1",
							},
							TLSMinVersion: "TLSv1_0",
							TLSMaxVersion: "TLSv1_1",
							CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
						},
						Services: []capi.IngressService{
							{
								Name:                  "name1",
								Hosts:                 []string{"host1_1", "host1_2"},
								Namespace:             "ns1",
								Partition:             "default",
								MaxConnections:        &maxConnections,
								MaxPendingRequests:    &maxPendingRequests,
								MaxConcurrentRequests: &maxConcurrentRequests,
								TLS: &capi.GatewayServiceTLSConfig{
									SDS: &capi.GatewayTLSSDSConfig{
										ClusterName:  "cluster1",
										CertResource: "cert1",
									},
								},
								RequestHeaders: &capi.HTTPHeaderModifiers{
									Add: map[string]string{
										"foo":    "bar",
										"source": "dest",
									},
									Set: map[string]string{
										"bar": "baz",
										"key": "car",
									},
									Remove: []string{
										"foo",
										"bar",
										"baz",
									},
								},
								ResponseHeaders: &capi.HTTPHeaderModifiers{
									Add: map[string]string{
										"doo":    "var",
										"aource": "sest",
									},
									Set: map[string]string{
										"var": "vaz",
										"jey": "xar",
									},
									Remove: []string{
										"doo",
										"var",
										"vaz",
									},
								},
							},
							{
								Name:      "name2",
								Hosts:     []string{"host2_1", "host2_2"},
								Namespace: "ns2",
							},
						},
					},
					{
						Port:     9999,
						Protocol: "http",
						Services: []capi.IngressService{
							{
								Name: "*",
							},
						},
					},
				},
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: true,
		},
		"different types does not match": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Theirs: &capi.ProxyConfigEntry{
				Name:        "name",
				Kind:        capi.IngressGateway,
				Namespace:   "foobar",
				CreateIndex: 1,
				ModifyIndex: 2,
			},
			Matches: false,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, c.Matches, c.Ours.MatchesConsul(c.Theirs))
		})
	}
}

func TestIngressGateway_ToConsul(t *testing.T) {

	defaultMaxConnections := uint32(100)
	defaultMaxPendingRequests := uint32(101)
	defaultMaxConcurrentRequests := uint32(102)

	maxConnections := uint32(200)
	maxPendingRequests := uint32(201)
	maxConcurrentRequests := uint32(202)

	cases := map[string]struct {
		Ours IngressGateway
		Exp  *capi.IngressGatewayConfigEntry
	}{
		"empty fields": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{},
			},
			Exp: &capi.IngressGatewayConfigEntry{
				Kind: capi.IngressGateway,
				Name: "name",
				Meta: map[string]string{
					common.SourceKey:     common.SourceValue,
					common.DatacenterKey: "datacenter",
				},
			},
		},
		"every field set": {
			Ours: IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "name",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						Enabled: true,
						SDS: &GatewayTLSSDSConfig{
							ClusterName:  "cluster1",
							CertResource: "cert1",
						},
						TLSMinVersion: "TLSv1_0",
						TLSMaxVersion: "TLSv1_1",
						CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
					},
					Defaults: &IngressServiceConfig{
						MaxConnections:        &defaultMaxConnections,
						MaxPendingRequests:    &defaultMaxPendingRequests,
						MaxConcurrentRequests: &defaultMaxConcurrentRequests,
						PassiveHealthCheck: &PassiveHealthCheck{
							Interval: metav1.Duration{
								Duration: 2 * time.Second,
							},
							MaxFailures:             uint32(20),
							EnforcingConsecutive5xx: pointer.Uint32(100),
							MaxEjectionPercent:      pointer.Uint32(10),
							BaseEjectionTime: &metav1.Duration{
								Duration: 10 * time.Second,
							},
						},
					},
					Listeners: []IngressListener{
						{
							Port:     8888,
							Protocol: "tcp",
							TLS: &GatewayTLSConfig{
								Enabled: true,
								SDS: &GatewayTLSSDSConfig{
									ClusterName:  "cluster1",
									CertResource: "cert1",
								},
								TLSMinVersion: "TLSv1_0",
								TLSMaxVersion: "TLSv1_1",
								CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
							},
							Services: []IngressService{
								{
									Name:      "name1",
									Hosts:     []string{"host1_1", "host1_2"},
									Namespace: "ns1",
									Partition: "default",
									IngressServiceConfig: IngressServiceConfig{
										MaxConnections:        &maxConnections,
										MaxPendingRequests:    &maxPendingRequests,
										MaxConcurrentRequests: &maxConcurrentRequests,
									},
									TLS: &GatewayServiceTLSConfig{
										SDS: &GatewayTLSSDSConfig{
											ClusterName:  "cluster1",
											CertResource: "cert1",
										},
									},
									RequestHeaders: &HTTPHeaderModifiers{
										Add: map[string]string{
											"foo":    "bar",
											"source": "dest",
										},
										Set: map[string]string{
											"bar": "baz",
											"key": "car",
										},
										Remove: []string{
											"foo",
											"bar",
											"baz",
										},
									},
									ResponseHeaders: &HTTPHeaderModifiers{
										Add: map[string]string{
											"doo":    "var",
											"aource": "sest",
										},
										Set: map[string]string{
											"var": "vaz",
											"jey": "xar",
										},
										Remove: []string{
											"doo",
											"var",
											"vaz",
										},
									},
								},
								{
									Name:      "name2",
									Hosts:     []string{"host2_1", "host2_2"},
									Namespace: "ns2",
								},
							},
						},
						{
							Port:     9999,
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			Exp: &capi.IngressGatewayConfigEntry{
				Kind: capi.IngressGateway,
				Name: "name",
				TLS: capi.GatewayTLSConfig{
					Enabled: true,
					SDS: &capi.GatewayTLSSDSConfig{
						ClusterName:  "cluster1",
						CertResource: "cert1",
					},
					TLSMinVersion: "TLSv1_0",
					TLSMaxVersion: "TLSv1_1",
					CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
				},
				Defaults: &capi.IngressServiceConfig{
					MaxConnections:        &defaultMaxConnections,
					MaxPendingRequests:    &defaultMaxPendingRequests,
					MaxConcurrentRequests: &defaultMaxConcurrentRequests,
					PassiveHealthCheck: &capi.PassiveHealthCheck{
						Interval:                2 * time.Second,
						MaxFailures:             uint32(20),
						EnforcingConsecutive5xx: pointer.Uint32(100),
						MaxEjectionPercent:      pointer.Uint32(10),
						BaseEjectionTime:        pointer.Duration(10 * time.Second),
					},
				},
				Listeners: []capi.IngressListener{
					{
						Port:     8888,
						Protocol: "tcp",
						TLS: &capi.GatewayTLSConfig{
							Enabled: true,
							SDS: &capi.GatewayTLSSDSConfig{
								ClusterName:  "cluster1",
								CertResource: "cert1",
							},
							TLSMinVersion: "TLSv1_0",
							TLSMaxVersion: "TLSv1_1",
							CipherSuites:  []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "AES128-SHA"},
						},
						Services: []capi.IngressService{
							{
								Name:                  "name1",
								Hosts:                 []string{"host1_1", "host1_2"},
								Namespace:             "ns1",
								Partition:             "default",
								MaxConnections:        &maxConnections,
								MaxPendingRequests:    &maxPendingRequests,
								MaxConcurrentRequests: &maxConcurrentRequests,
								TLS: &capi.GatewayServiceTLSConfig{
									SDS: &capi.GatewayTLSSDSConfig{
										ClusterName:  "cluster1",
										CertResource: "cert1",
									},
								},
								RequestHeaders: &capi.HTTPHeaderModifiers{
									Add: map[string]string{
										"foo":    "bar",
										"source": "dest",
									},
									Set: map[string]string{
										"bar": "baz",
										"key": "car",
									},
									Remove: []string{
										"foo",
										"bar",
										"baz",
									},
								},
								ResponseHeaders: &capi.HTTPHeaderModifiers{
									Add: map[string]string{
										"doo":    "var",
										"aource": "sest",
									},
									Set: map[string]string{
										"var": "vaz",
										"jey": "xar",
									},
									Remove: []string{
										"doo",
										"var",
										"vaz",
									},
								},
							},
							{
								Name:      "name2",
								Hosts:     []string{"host2_1", "host2_2"},
								Namespace: "ns2",
							},
						},
					},
					{
						Port:     9999,
						Protocol: "http",
						Services: []capi.IngressService{
							{
								Name: "*",
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

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			act := c.Ours.ToConsul("datacenter")
			ingressGateway, ok := act.(*capi.IngressGatewayConfigEntry)
			require.True(t, ok, "could not cast")
			require.Equal(t, c.Exp, ingressGateway)
		})
	}
}

func TestIngressGateway_Validate(t *testing.T) {
	zero := uint32(0)

	cases := map[string]struct {
		input             *IngressGateway
		namespacesEnabled bool
		partitionEnabled  bool
		expectedErrMsgs   []string
	}{
		"tls.minTLSVersion invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						TLSMinVersion: "foo",
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.tls.tlsMinVersion: Invalid value: "foo": must be one of "TLS_AUTO", "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3", ""`,
			},
		},
		"tls.maxTLSVersion invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						TLSMaxVersion: "foo",
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.tls.tlsMaxVersion: Invalid value: "foo": must be one of "TLS_AUTO", "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3", ""`,
			},
		},
		"listener.protocol invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "invalid",
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].protocol: Invalid value: "invalid": must be one of "tcp", "http", "http2", "grpc"`,
			},
		},
		"len(services) > 0 when protocol==tcp": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "svc1",
								},
								{
									Name: "svc2",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services: Invalid value: "[{\"name\":\"svc1\"},{\"name\":\"svc2\"}]": if protocol is "tcp", only a single service is allowed, found 2`,
			},
		},
		"protocol != http when service.name==*": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].name: Invalid value: "*": if name is "*", protocol must be "http" but was "tcp"`,
			},
		},
		"len(hosts) > 0 when service.name==*": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "http",
							Services: []IngressService{
								{
									Name:  "*",
									Hosts: []string{"host1", "host2"},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].hosts: Invalid value: "[\"host1\",\"host2\"]": hosts must be empty if name is "*"`,
			},
		},
		"len(hosts) > 0 when protocol==tcp": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:  "name",
									Hosts: []string{"host1", "host2"},
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].hosts: Invalid value: "[\"host1\",\"host2\"]": hosts must be empty if protocol is "tcp"`,
			},
		},
		"listeners.tls.minTLSVersion invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							TLS: &GatewayTLSConfig{
								TLSMinVersion: "foo",
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.listeners[0].tls.tlsMinVersion: Invalid value: "foo": must be one of "TLS_AUTO", "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3", ""`,
			},
		},
		"listeners.tls.maxTLSVersion invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							TLS: &GatewayTLSConfig{
								TLSMaxVersion: "foo",
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.listeners[0].tls.tlsMaxVersion: Invalid value: "foo": must be one of "TLS_AUTO", "TLSv1_0", "TLSv1_1", "TLSv1_2", "TLSv1_3", ""`,
			},
		},
		"service.namespace set when namespaces disabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: "foo",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].namespace: Invalid value: "foo": Consul Enterprise namespaces must be enabled to set service.namespace`,
			},
		},
		"service.namespace set when namespaces enabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: "foo",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: true,
		},
		"tls valid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					TLS: GatewayTLSConfig{
						TLSMinVersion: "TLS_AUTO",
						TLSMaxVersion: "TLS_AUTO",
					},
				},
			},
		},
		"listeners.tls valid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							TLS: &GatewayTLSConfig{
								TLSMinVersion: "TLS_AUTO",
								TLSMaxVersion: "TLS_AUTO",
							},
						},
					},
				},
			},
		},
		"service.partition set when partitions disabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Partition: "foo",
								},
							},
						},
					},
				},
			},
			partitionEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].services[0].partition: Invalid value: "foo": Consul Enterprise admin-partitions must be enabled to set service.partition`,
			},
		},
		"service.partition set when partitions enabled": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Partition: "foo",
								},
							},
						},
					},
				},
			},
			partitionEnabled: true,
		},
		"defaults.maxConnections invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Defaults: &IngressServiceConfig{
						MaxConnections: &zero,
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.defaults.maxconnections: Invalid`,
			},
		},
		"defaults.maxPendingRequests invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Defaults: &IngressServiceConfig{
						MaxPendingRequests: &zero,
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.defaults.maxpendingrequests: Invalid`,
			},
		},
		"defaults.maxConcurrentRequests invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Defaults: &IngressServiceConfig{
						MaxConcurrentRequests: &zero,
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.defaults.maxconcurrentrequests: Invalid`,
			},
		},
		"service.maxConnections invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "svc1",
									IngressServiceConfig: IngressServiceConfig{
										MaxConnections: &zero,
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.listeners[0].maxconnections: Invalid`,
			},
		},
		"service.maxConcurrentRequests invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "svc1",
									IngressServiceConfig: IngressServiceConfig{
										MaxConcurrentRequests: &zero,
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.listeners[0].maxconcurrentrequests: Invalid`,
			},
		},
		"service.maxPendingRequests invalid": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "http",
							Services: []IngressService{
								{
									Name: "svc1",
									IngressServiceConfig: IngressServiceConfig{
										MaxPendingRequests: &zero,
									},
								},
							},
						},
					},
				},
			},
			expectedErrMsgs: []string{
				`spec.listeners[0].maxpendingrequests: Invalid`,
			},
		},

		"multiple errors": {
			input: &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "invalid",
							Services: []IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			namespacesEnabled: false,
			expectedErrMsgs: []string{
				`spec.listeners[0].protocol: Invalid value: "invalid": must be one of "tcp", "http", "http2", "grpc"`,
				`spec.listeners[0].services[0].name: Invalid value: "*": if name is "*", protocol must be "http" but was "invalid"`,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.input.Validate(common.ConsulMeta{NamespacesEnabled: testCase.namespacesEnabled, PartitionsEnabled: testCase.partitionEnabled})
			if len(testCase.expectedErrMsgs) != 0 {
				require.Error(t, err)
				for _, s := range testCase.expectedErrMsgs {
					require.Contains(t, err.Error(), s)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Test defaulting behavior when namespaces are enabled as well as disabled.
func TestIngressGateway_DefaultNamespaceFields(t *testing.T) {
	namespaceConfig := map[string]struct {
		consulMeta          common.ConsulMeta
		expectedDestination string
	}{
		"disabled": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    false,
				DestinationNamespace: "",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "",
		},
		"destinationNS": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "foo",
				Mirroring:            false,
				Prefix:               "",
			},
			expectedDestination: "foo",
		},
		"mirroringEnabledWithoutPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "",
			},
			expectedDestination: "bar",
		},
		"mirroringWithPrefix": {
			consulMeta: common.ConsulMeta{
				NamespacesEnabled:    true,
				DestinationNamespace: "",
				Mirroring:            true,
				Prefix:               "ns-",
			},
			expectedDestination: "ns-bar",
		},
	}

	for name, s := range namespaceConfig {
		t.Run(name, func(t *testing.T) {
			input := &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name: "name",
								},
								{
									Name:      "other-name",
									Namespace: "other",
								},
							},
						},
					},
				},
			}
			output := &IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "bar",
				},
				Spec: IngressGatewaySpec{
					Listeners: []IngressListener{
						{
							Protocol: "tcp",
							Services: []IngressService{
								{
									Name:      "name",
									Namespace: s.expectedDestination,
								},
								{
									Name:      "other-name",
									Namespace: "other",
								},
							},
						},
					},
				},
			}
			input.DefaultNamespaceFields(s.consulMeta)
			require.True(t, cmp.Equal(input, output))
		})
	}
}

func TestIngressGateway_AddFinalizer(t *testing.T) {
	ingressGateway := &IngressGateway{}
	ingressGateway.AddFinalizer("finalizer")
	require.Equal(t, []string{"finalizer"}, ingressGateway.ObjectMeta.Finalizers)
}

func TestIngressGateway_RemoveFinalizer(t *testing.T) {
	ingressGateway := &IngressGateway{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"f1", "f2"},
		},
	}
	ingressGateway.RemoveFinalizer("f1")
	require.Equal(t, []string{"f2"}, ingressGateway.ObjectMeta.Finalizers)
}

func TestIngressGateway_SetSyncedCondition(t *testing.T) {
	ingressGateway := &IngressGateway{}
	ingressGateway.SetSyncedCondition(corev1.ConditionTrue, "reason", "message")

	require.Equal(t, corev1.ConditionTrue, ingressGateway.Status.Conditions[0].Status)
	require.Equal(t, "reason", ingressGateway.Status.Conditions[0].Reason)
	require.Equal(t, "message", ingressGateway.Status.Conditions[0].Message)
	now := metav1.Now()
	require.True(t, ingressGateway.Status.Conditions[0].LastTransitionTime.Before(&now))
}

func TestIngressGateway_SetLastSyncedTime(t *testing.T) {
	ingressGateway := &IngressGateway{}
	syncedTime := metav1.NewTime(time.Now())
	ingressGateway.SetLastSyncedTime(&syncedTime)

	require.Equal(t, &syncedTime, ingressGateway.Status.LastSyncedTime)
}

func TestIngressGateway_GetSyncedConditionStatus(t *testing.T) {
	cases := []corev1.ConditionStatus{
		corev1.ConditionUnknown,
		corev1.ConditionFalse,
		corev1.ConditionTrue,
	}
	for _, status := range cases {
		t.Run(string(status), func(t *testing.T) {
			ingressGateway := &IngressGateway{
				Status: Status{
					Conditions: []Condition{{
						Type:   ConditionSynced,
						Status: status,
					}},
				},
			}

			require.Equal(t, status, ingressGateway.SyncedConditionStatus())
		})
	}
}

func TestIngressGateway_GetConditionWhenStatusNil(t *testing.T) {
	require.Nil(t, (&IngressGateway{}).GetCondition(ConditionSynced))
}

func TestIngressGateway_SyncedConditionStatusWhenStatusNil(t *testing.T) {
	require.Equal(t, corev1.ConditionUnknown, (&IngressGateway{}).SyncedConditionStatus())
}

func TestIngressGateway_SyncedConditionWhenStatusNil(t *testing.T) {
	status, reason, message := (&IngressGateway{}).SyncedCondition()
	require.Equal(t, corev1.ConditionUnknown, status)
	require.Equal(t, "", reason)
	require.Equal(t, "", message)
}

func TestIngressGateway_ConsulKind(t *testing.T) {
	require.Equal(t, capi.IngressGateway, (&IngressGateway{}).ConsulKind())
}

func TestIngressGateway_KubeKind(t *testing.T) {
	require.Equal(t, "ingressgateway", (&IngressGateway{}).KubeKind())
}

func TestIngressGateway_ConsulName(t *testing.T) {
	require.Equal(t, "foo", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).ConsulName())
}

func TestIngressGateway_KubernetesName(t *testing.T) {
	require.Equal(t, "foo", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}).KubernetesName())
}

func TestIngressGateway_ConsulNamespace(t *testing.T) {
	require.Equal(t, "bar", (&IngressGateway{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "bar"}}).ConsulMirroringNS())
}

func TestIngressGateway_ConsulGlobalResource(t *testing.T) {
	require.False(t, (&IngressGateway{}).ConsulGlobalResource())
}

func TestIngressGateway_ObjectMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
		Name:      "name",
		Namespace: "namespace",
	}
	ingressGateway := &IngressGateway{
		ObjectMeta: meta,
	}
	require.Equal(t, meta, ingressGateway.GetObjectMeta())
}
