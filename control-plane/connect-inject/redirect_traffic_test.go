package connectinject

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/sdk/iptables"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	defaultPodName   = "fakePod"
	defaultNamespace = "default"
	resourcePrefix   = "CONSUL"
	dnsEnvVariable   = "CONSUL_DNS_SERVICE_HOST"
	dnsIP            = "10.0.34.16"
)

func TestAddRedirectTrafficConfig(t *testing.T) {
	s := runtime.NewScheme()
	s.AddKnownTypes(schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}, &corev1.Pod{})
	decoder, err := admission.NewDecoder(s)
	require.NoError(t, err)
	cases := []struct {
		name       string
		webhook    MeshWebhook
		pod        *corev1.Pod
		namespace  corev1.Namespace
		dnsEnabled bool
		expCfg     iptables.Config
		expErr     error
	}{
		{
			name: "basic bare minimum pod",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
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
			expCfg: iptables.Config{
				ConsulDNSIP:       "",
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  proxyDefaultInboundPort,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{"5996"},
			},
		},
		{
			name: "metrics enabled",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationEnableMetrics:        "true",
						annotationPrometheusScrapePort: "13373",
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
			expCfg: iptables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    proxyDefaultInboundPort,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"13373"},
			},
		},
		{
			name: "metrics enabled with incorrect annotation",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationEnableMetrics:        "invalid",
						annotationPrometheusScrapePort: "13373",
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
			expCfg: iptables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    proxyDefaultInboundPort,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"13373"},
			},
			expErr: fmt.Errorf("%s annotation value of %s was invalid: %s", annotationEnableMetrics, "invalid", "strconv.ParseBool: parsing \"invalid\": invalid syntax"),
		},
		{
			name: "overwrite probes, transparent proxy annotation set",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTransparentProxyOverwriteProbes: "true",
						keyTransparentProxy:                       "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test",
							LivenessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									HTTPGet: &corev1.HTTPGetAction{
										Port: intstr.FromInt(exposedPathsLivenessPortsRangeStart),
									},
								},
							},
						},
					},
				},
			},
			expCfg: iptables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    proxyDefaultInboundPort,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{strconv.Itoa(exposedPathsLivenessPortsRangeStart)},
			},
		},
		{
			name: "exclude inbound ports",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTProxyExcludeInboundPorts: "1111,11111",
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
			expCfg: iptables.Config{
				ConsulDNSIP:         "",
				ProxyUserID:         strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:    proxyDefaultInboundPort,
				ProxyOutboundPort:   iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:         []string{"5996"},
				ExcludeInboundPorts: []string{"1111", "11111"},
			},
		},
		{
			name: "exclude outbound ports",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTProxyExcludeOutboundPorts: "2222,22222",
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
			expCfg: iptables.Config{
				ConsulDNSIP:          "",
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     proxyDefaultInboundPort,
				ProxyOutboundPort:    iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:          []string{"5996"},
				ExcludeOutboundPorts: []string{"2222", "22222"},
			},
		},
		{
			name: "exclude outbound CIDRs",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTProxyExcludeOutboundCIDRs: "3.3.3.3,3.3.3.3/24",
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
			expCfg: iptables.Config{
				ConsulDNSIP:          "",
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     proxyDefaultInboundPort,
				ProxyOutboundPort:    iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:          []string{strconv.Itoa(initContainersUserAndGroupID)},
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
			},
		},
		{
			name: "exclude UIDs",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTProxyExcludeUIDs: "4444,44444",
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
			expCfg: iptables.Config{
				ConsulDNSIP:       "",
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  proxyDefaultInboundPort,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{"4444", "44444", strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
		{
			name: "exclude inbound ports, outbound ports, outbound CIDRs, and UIDs",
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						annotationTProxyExcludeInboundPorts:  "1111,11111",
						annotationTProxyExcludeOutboundPorts: "2222,22222",
						annotationTProxyExcludeOutboundCIDRs: "3.3.3.3,3.3.3.3/24",
						annotationTProxyExcludeUIDs:          "4444,44444",
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
			expCfg: iptables.Config{
				ConsulDNSIP:          "",
				ProxyUserID:          strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:     proxyDefaultInboundPort,
				ProxyOutboundPort:    iptables.DefaultTProxyOutboundPort,
				ExcludeInboundPorts:  []string{"1111", "11111"},
				ExcludeOutboundPorts: []string{"2222", "22222"},
				ExcludeOutboundCIDRs: []string{"3.3.3.3", "3.3.3.3/24"},
				ExcludeUIDs:          []string{"4444", "44444", strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
		{
			name:       "dns enabled",
			dnsEnabled: true,
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				ResourcePrefix:        resourcePrefix,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						keyConsulDNS: "true",
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
			expCfg: iptables.Config{
				ConsulDNSIP:       dnsIP,
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  proxyDefaultInboundPort,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{strconv.Itoa(initContainersUserAndGroupID)},
			},
		},
		{
			name:       "dns enabled set but consul dns host environment variable missing",
			dnsEnabled: false,
			webhook: MeshWebhook{
				Log:                   logrtest.TestLogger{T: t},
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSet(),
				decoder:               decoder,
				ResourcePrefix:        resourcePrefix,
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: defaultNamespace,
					Name:      defaultPodName,
					Annotations: map[string]string{
						keyConsulDNS: "true",
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
			expCfg: iptables.Config{
				ConsulDNSIP:       dnsIP,
				ProxyUserID:       strconv.Itoa(sidecarUserAndGroupID),
				ProxyInboundPort:  proxyDefaultInboundPort,
				ProxyOutboundPort: iptables.DefaultTProxyOutboundPort,
				ExcludeUIDs:       []string{strconv.Itoa(initContainersUserAndGroupID)},
			},
			expErr: fmt.Errorf("environment variable %s not found", dnsEnvVariable),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.dnsEnabled {
				err = os.Setenv(dnsEnvVariable, dnsIP)
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = os.Unsetenv(dnsEnvVariable)
				})
			}
			err = c.webhook.addRedirectTrafficConfigAnnotation(c.pod, c.namespace)

			// Only compare annotation and iptables config on successful runs
			if c.expErr == nil {
				require.NoError(t, err)
				anno, ok := c.pod.Annotations[annotationRedirectTraffic]
				require.Equal(t, ok, true)

				actualConfig := iptables.Config{}
				err = json.Unmarshal([]byte(anno), &actualConfig)
				require.NoError(t, err)
				require.Equal(t, c.expCfg, actualConfig)
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
			annotations:           map[string]string{keyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
		"enabled globally, ns not set, annotation is true": {
			globalEnabled:         true,
			annotations:           map[string]string{keyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"disabled globally, ns not set, annotation not provided": {
			expectConsulDNSConfig: false,
		},
		"disabled globally, ns not set, annotation is false": {
			annotations:           map[string]string{keyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
		"disabled globally, ns not set, annotation is true": {
			annotations:           map[string]string{keyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"disabled globally, ns enabled, annotation not set": {
			namespaceLabel:        map[string]string{keyConsulDNS: "true"},
			expectConsulDNSConfig: true,
		},
		"enabled globally, ns disabled, annotation not set": {
			globalEnabled:         true,
			namespaceLabel:        map[string]string{keyConsulDNS: "false"},
			expectConsulDNSConfig: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := MeshWebhook{
				EnableConsulDNS:        c.globalEnabled,
				EnableTransparentProxy: true,
				ResourcePrefix:         "consul",
				ConsulConfig:           &consul.Config{HTTPPort: 8500},
			}
			err := os.Setenv(dnsEnvVariable, dnsIP)
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = os.Unsetenv(dnsEnvVariable)
			})

			pod := minimal()
			pod.Annotations = c.annotations

			ns := testNS
			ns.Labels = c.namespaceLabel
			iptablesConfig, err := w.iptablesConfigJSON(*pod, ns)
			require.NoError(t, err)

			actualConfig := iptables.Config{}
			err = json.Unmarshal([]byte(iptablesConfig), &actualConfig)
			require.NoError(t, err)
			if c.expectConsulDNSConfig {
				require.Equal(t, dnsIP, actualConfig.ConsulDNSIP)
			} else {
				require.Empty(t, actualConfig.ConsulDNSIP)
			}
		})
	}
}

func TestHandler_constructDNSServiceHostName(t *testing.T) {
	cases := []struct {
		prefix string
		result string
	}{
		{
			prefix: "consul-consul",
			result: "CONSUL_CONSUL_DNS_SERVICE_HOST",
		},
		{
			prefix: "release",
			result: "RELEASE_DNS_SERVICE_HOST",
		},
		{
			prefix: "consul-dc1",
			result: "CONSUL_DC1_DNS_SERVICE_HOST",
		},
	}

	for _, c := range cases {
		t.Run(c.prefix, func(t *testing.T) {
			w := MeshWebhook{ResourcePrefix: c.prefix}
			require.Equal(t, c.result, w.constructDNSServiceHostName())
		})
	}
}
