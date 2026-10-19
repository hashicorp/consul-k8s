// Copyright IBM Corp. 2018, 2026
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/sdk/nftables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
)

const (
	defaultPodName   = "fakePod"
	defaultNamespace = "default"
)

func TestAIAgentRedirectTrafficOverrides(t *testing.T) {
	for _, source := range []string{"selected CRD", "missing CRD", "no client"} {
		t.Run(source, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, v1alpha1.AddToScheme(s))
			builder := fake.NewClientBuilder().WithScheme(s)
			if source == "selected CRD" {
				builder.WithObjects(&v1alpha1.AgentConfig{
					ObjectMeta: metav1.ObjectMeta{Name: "custom"},
					Spec: v1alpha1.AgentConfigSpec{Defaults: v1alpha1.AgentDefaults{
						InterceptorPort: 22001, McpPort: 22002, HITL: v1alpha1.AgentHITL{Port: 22003},
					}},
				})
			}
			w := MeshWebhook{Log: logrtest.New(t)}
			if source != "no client" {
				w.Client = builder.Build()
			}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
				constants.AnnotationAIRole:                 constants.AIAgentRole,
				constants.AnnotationAIAgentConfig:          "custom",
				constants.AnnotationAIAgentInterceptorPort: "23001",
				constants.AnnotationAIAgentMCPPort:         "23002",
				constants.AnnotationAIAgentHITLPort:        "23003",
			}}}
			raw, err := w.nftablesConfigJSON(pod, corev1.Namespace{})
			require.NoError(t, err)
			var cfg nftables.Config
			require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
			require.Equal(t, []string{"23002", "23003", "23001",
				strconv.Itoa(constants.DefaultOBOInboundPort), strconv.Itoa(constants.DefaultOBOOutboundPort)}, cfg.ExcludeInboundPorts)
			require.Equal(t, []string{"23002",
				strconv.Itoa(constants.DefaultOBOInboundPort), strconv.Itoa(constants.DefaultOBOOutboundPort)}, cfg.ExcludeOutboundPorts)

			pod.Annotations[constants.AnnotationAIAgentMCPPort] = "invalid"
			_, err = w.nftablesConfigJSON(pod, corev1.Namespace{})
			require.ErrorContains(t, err, constants.AnnotationAIAgentMCPPort)
		})
	}
}

func TestAddRedirectTrafficConfig(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}, &corev1.Pod{})
	decoder := admission.NewDecoder(s)
	cases := []struct {
		name       string
		webhook    MeshWebhook
		pod        *corev1.Pod
		namespace  corev1.Namespace
		dnsEnabled bool
		expCfg     nftables.Config
		expErr     error
	}{
		{
			name: "basic bare minimum pod",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   defaultNamespace,
					Name:        defaultPodName,
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:       "",
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  constants.ProxyDefaultInboundPort,
				ProxyOutboundPort: nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{"5996"},
			},
		},
		{
			name: "proxy health checks enabled",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationUseProxyHealthCheck: "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:   nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"21000"},
			},
		},
		{
			name: "metrics enabled",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationEnableMetrics:        "true",
						constants.AnnotationPrometheusScrapePort: "13373",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:   nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"13373"},
			},
		},
		{
			name: "metrics enabled with incorrect annotation",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationEnableMetrics:        "invalid",
						constants.AnnotationPrometheusScrapePort: "13373",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:   nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"13373"},
			},
			expErr: fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableMetrics, "invalid", "strconv.ParseBool: parsing \"invalid\": invalid syntax"),
		},
		{
			name: "overwrite probes, transparent proxy annotation set",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTransparentProxyOverwriteProbes: "true",
						constants.KeyTransparentProxy:                       "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
									},
								},
							},
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:   nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{strconv.Itoa(exposedPathsLivenessPortsRangeStart)},
			},
		},
		{
			name: "exclude inbound ports",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTProxyExcludeInboundPorts: "1111,11111",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:   nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"1111", "11111"},
			},
		},
		{
			name: "exclude outbound ports",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTProxyExcludeOutboundPorts: "2222,22222",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:          "",
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:    nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:          []string{"5996"},
				ExcludeOutboundPorts: []string{"2222", "22222"},
			},
		},
		{
			name: "exclude outbound CIDRs",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTProxyExcludeOutboundCIDRs: "3.3.3.3,3.3.3.3/24",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:          "",
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:    nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:          []string{strconv.Itoa(initContainersUserAndGroupID)},
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
			},
		},
		{
			name: "exclude UIDs",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTProxyExcludeUIDs: "4444,44444",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ConsulDNSIP:       "",
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  constants.ProxyDefaultInboundPort,
				ProxyOutboundPort: nftables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{"4444", "44444", strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
		{
			name: "exclude inbound ports, outbound ports, outbound CIDRs, and UIDs",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationTProxyExcludeInboundPorts:  "1111,11111",
						constants.AnnotationTProxyExcludeOutboundPorts: "2222,22222",
						constants.AnnotationTProxyExcludeOutboundCIDRs: "3.3.3.3,3.3.3.3/24",
						constants.AnnotationTProxyExcludeUIDs:          "4444,44444",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:    nftables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts:  []string{"1111", "11111"},
				ExcludeOutboundPorts: []string{"2222", "22222"},
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
				ExcludeUIDs:          []string{"4444", "44444", strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
		{
			name: "AI agent pod excludes MCP, HITL, interceptor, and OBO ports",
			webhook: MeshWebhook{
				Log:                   logrtest.New(t),
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						constants.AnnotationAIRole: constants.AIAgentRole,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
						},
					},
				},
			},
			expCfg: nftables.Config{
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     constants.ProxyDefaultInboundPort,
				ProxyOutboundPort:    nftables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts:  []string{"21003", "21004", "21005", "21102", "21103"},
				ExcludeOutboundPorts: []string{"21003", "21102", "21103"},
				ExcludeUIDs:          []string{strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.webhook.addRedirectTrafficConfigAnnotation(c.pod, c.namespace)

			// Only compare annotation and nft config on successful runs
			if c.expErr == nil {
				require.NoError(t, err)
				anno, ok := c.pod.Annotations[constants.AnnotationRedirectTraffic]
				require.Equal(t, ok, true)

				actualConfig := nftables.Config{}
				err = json.Unmarshal([]byte(anno), &actualConfig)
				require.NoError(t, err)
				assert.ObjectsAreEqual(c.expCfg, actualConfig)
			} else {
				require.EqualError(t, err, c.expErr.Error())
			}
		})
	}
}

func TestRedirectTraffic_consulDNS(t *testing.T) {
	cases := map[string]struct {
		globalEnabled         bool
		annotations           map[string]string
		namespaceLabel        map[string]string
		expectConsulDNSConfig bool
	}{
		"enabled globally, ns not set, annotation not provided": {
			globalEnabled:         true,
			expectConsulDNSConfig: true,
		},
		"enabled globally, ns not set, annotation is false": {
			globalEnabled:         true,
			annotations:           map[string]string{constants.KeyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
		"enabled globally, ns not set, annotation is true": {
			globalEnabled:         true,
			annotations:           map[string]string{constants.KeyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"disabled globally, ns not set, annotation not provided": {
			expectConsulDNSConfig: false,
		},
		"disabled globally, ns not set, annotation is false": {
			annotations:           map[string]string{constants.KeyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
		"disabled globally, ns not set, annotation is true": {
			annotations:           map[string]string{constants.KeyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"disabled globally, ns enabled, annotation not set": {
			namespaceLabel:        map[string]string{constants.KeyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"enabled globally, ns disabled, annotation not set": {
			globalEnabled:         true,
			namespaceLabel:        map[string]string{constants.KeyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableConsulDNS:        c.globalEnabled,
				EnableTransparentProxy: true,
				ConsulConfig:           &consul.Config{HTTPPort: 8500},
			}

			pod := minimal()
			pod.Annotations = c.annotations

			ns := testNS
			ns.Labels = c.namespaceLabel
			nftCfg, err := w.nftablesConfigJSON(*pod, ns)
			require.NoError(t, err)

			actualConfig := nftables.Config{}
			err = json.Unmarshal([]byte(nftCfg), &actualConfig)
			require.NoError(t, err)
			if c.expectConsulDNSConfig {
				require.Equal(t, "127.0.0.1", actualConfig.ConsulDNSIP)
				require.Equal(t, 8600, actualConfig.ConsulDNSPort)
			} else {
				require.Empty(t, actualConfig.ConsulDNSIP)
			}
		})
	}
}
