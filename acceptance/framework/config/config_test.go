// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfig_HelmValuesFromConfig(t *testing.T) {
	tests := []struct {
		name       string
		testConfig TestConfig
		want       map[string]string
	}{
		{
			"returns empty map by default",
			TestConfig{},
			map[string]string{
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets consul image",
			TestConfig{
				ConsulImage: "consul:test-version",
			},
			map[string]string{
				"global.image": "consul:test-version",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets consul-k8s image",
			TestConfig{
				ConsulK8SImage: "consul-k8s:test-version",
			},
			map[string]string{
				"global.imageK8S": "consul-k8s:test-version",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets both images",
			TestConfig{
				ConsulImage:    "consul:test-version",
				ConsulK8SImage: "consul-k8s:test-version",
			},
			map[string]string{
				"global.image":    "consul:test-version",
				"global.imageK8S": "consul-k8s:test-version",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets ent license secret",
			TestConfig{
				EnableEnterprise:  true,
				EnterpriseLicense: "ent-license",
				ConsulImage:       "consul:test-version",
			},
			map[string]string{
				"global.enterpriseLicense.secretName":           "license",
				"global.enterpriseLicense.secretKey":            "key",
				"connectInject.transparentProxy.defaultEnabled": "false",
				"global.image": "consul:test-version",
			},
		},
		{
			"doesn't set ent license if license is empty",
			TestConfig{
				EnterpriseLicense: "",
			},
			map[string]string{
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets openshift value when EnableOpenshift is set",
			TestConfig{
				EnableOpenshift: true,
			},
			map[string]string{
				"global.openshift.enabled":                      "true",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets enablePodSecurityPolicies helm value when -enable-pod-security-policies is set",
			TestConfig{
				EnablePodSecurityPolicies: true,
			},
			map[string]string{
				"global.enablePodSecurityPolicies":              "true",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets transparentProxy.defaultEnabled helm value to true when -enable-transparent-proxy is set",
			TestConfig{
				EnableTransparentProxy: true,
			},
			map[string]string{
				"connectInject.transparentProxy.defaultEnabled": "true",
			},
		},
		{
			"sets connectInject.cni.enabled helm value to true when -enable-cni is set",
			TestConfig{
				EnableCNI: true,
			},
			map[string]string{
				"connectInject.cni.enabled":                     "true",
				"connectInject.cni.logLevel":                    "debug",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
		{
			"sets dualstack helm value",
			TestConfig{
				DualStack: true,
			},
			map[string]string{
				"global.dualStack.defaultEnabled":               "true",
				"connectInject.transparentProxy.defaultEnabled": "false",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := tt.testConfig.HelmValuesFromConfig()
			require.NoError(t, err)
			require.Equal(t, tt.want, values)
		})
	}
}

func TestConfig_HelmValuesFromConfig_EntImage(t *testing.T) {
	tests := []struct {
		consulImage string
		expImage    string
		expErr      string
	}{
		{
			consulImage: "hashicorp/consul:1.15.3",
			expImage:    "hashicorp/consul-enterprise:1.15.3-ent",
		},
		{
			consulImage: "hashicorp/consul:1.16.0-rc1",
			expImage:    "hashicorp/consul-enterprise:1.16.0-rc1-ent",
		},
		{
			consulImage: "hashicorp/consul:1.14.0-beta1",
			expImage:    "hashicorp/consul-enterprise:1.14.0-beta1-ent",
		},
		{
			consulImage: "hashicorp/consul@sha256:oioi2452345kjhlkh",
			expImage:    "hashicorp/consul@sha256:oioi2452345kjhlkh",
		},
		// Nightly tags differ from release tags ('-ent' suffix is omitted)
		{
			consulImage: "docker.mirror.hashicorp.services/hashicorppreview/consul:1.17-dev",
			expImage:    "docker.mirror.hashicorp.services/hashicorppreview/consul-enterprise:1.17-dev",
		},
		{
			consulImage: "docker.mirror.hashicorp.services/hashicorppreview/consul:1.17-dev-ubi",
			expImage:    "docker.mirror.hashicorp.services/hashicorppreview/consul-enterprise:1.17-dev-ubi",
		},
		{
			consulImage: "docker.mirror.hashicorp.services/hashicorppreview/consul@sha256:oioi2452345kjhlkh",
			expImage:    "docker.mirror.hashicorp.services/hashicorppreview/consul@sha256:oioi2452345kjhlkh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.consulImage, func(t *testing.T) {
			// Write values.yaml to a temp dir which will then get parsed.
			valuesYAML := fmt.Sprintf(`global:
  image: %s
`, tt.consulImage)
			tmp, err := os.MkdirTemp("", "")
			require.NoError(t, err)
			defer os.RemoveAll(tmp)
			require.NoError(t, os.WriteFile(filepath.Join(tmp, "values.yaml"), []byte(valuesYAML), 0644))

			cfg := TestConfig{
				EnableEnterprise: true,
				helmChartPath:    tmp,
			}
			values, err := cfg.HelmValuesFromConfig()
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expImage, values["global.image"])
			}
		})
	}
}

func Test_KubeEnvListFromStringList(t *testing.T) {
	tests := []struct {
		name           string
		kubeContexts   []string
		KubeConfigs    []string
		kubeNamespaces []string
		expKubeEnvList []KubeTestConfig
	}{
		{
			name:           "empty-lists",
			kubeContexts:   []string{},
			KubeConfigs:    []string{},
			kubeNamespaces: []string{},
			expKubeEnvList: []KubeTestConfig{{}},
		},
		{
			name:           "kubeContext set",
			kubeContexts:   []string{"ctx1", "ctx2"},
			KubeConfigs:    []string{},
			kubeNamespaces: []string{},
			expKubeEnvList: []KubeTestConfig{{KubeContext: "ctx1"}, {KubeContext: "ctx2"}},
		},
		{
			name:           "kubeNamespace set",
			kubeContexts:   []string{},
			KubeConfigs:    []string{"/path/config1", "/path/config2"},
			kubeNamespaces: []string{},
			expKubeEnvList: []KubeTestConfig{{KubeConfig: "/path/config1"}, {KubeConfig: "/path/config2"}},
		},
		{
			name:           "kubeConfigs set",
			kubeContexts:   []string{},
			KubeConfigs:    []string{},
			kubeNamespaces: []string{"ns1", "ns2"},
			expKubeEnvList: []KubeTestConfig{{KubeNamespace: "ns1"}, {KubeNamespace: "ns2"}},
		},
		{
			name:           "multiple everything",
			kubeContexts:   []string{"ctx1", "ctx2"},
			KubeConfigs:    []string{"/path/config1", "/path/config2"},
			kubeNamespaces: []string{"ns1", "ns2"},
			expKubeEnvList: []KubeTestConfig{{KubeContext: "ctx1", KubeNamespace: "ns1", KubeConfig: "/path/config1"}, {KubeContext: "ctx2", KubeNamespace: "ns2", KubeConfig: "/path/config2"}},
		},
		{
			name:           "multiple context and configs",
			kubeContexts:   []string{"ctx1", "ctx2"},
			KubeConfigs:    []string{"/path/config1", "/path/config2"},
			kubeNamespaces: []string{},
			expKubeEnvList: []KubeTestConfig{{KubeContext: "ctx1", KubeConfig: "/path/config1"}, {KubeContext: "ctx2", KubeConfig: "/path/config2"}},
		},
		{
			name:           "multiple namespace and configs",
			kubeContexts:   []string{},
			KubeConfigs:    []string{"/path/config1", "/path/config2"},
			kubeNamespaces: []string{"ns1", "ns2"},
			expKubeEnvList: []KubeTestConfig{{KubeNamespace: "ns1", KubeConfig: "/path/config1"}, {KubeNamespace: "ns2", KubeConfig: "/path/config2"}},
		},
		{
			name:           "multiple context and namespace",
			kubeContexts:   []string{"ctx1", "ctx2"},
			KubeConfigs:    []string{},
			kubeNamespaces: []string{"ns1", "ns2"},
			expKubeEnvList: []KubeTestConfig{{KubeContext: "ctx1", KubeNamespace: "ns1"}, {KubeContext: "ctx2", KubeNamespace: "ns2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := NewKubeTestConfigList(tt.KubeConfigs, tt.kubeContexts, tt.kubeNamespaces)
			require.Equal(t, tt.expKubeEnvList, actual)
		})
	}
}

func Test_GetPrimaryKubeEnv(t *testing.T) {
	tests := []struct {
		name              string
		kubeEnvList       []KubeTestConfig
		expPrimaryKubeEnv KubeTestConfig
	}{
		{
			name:              "context config multiple namespace single",
			kubeEnvList:       []KubeTestConfig{{KubeContext: "ctx1", KubeNamespace: "ns1", KubeConfig: "/path/config1"}, {KubeContext: "ctx2", KubeConfig: "/path/config2"}},
			expPrimaryKubeEnv: KubeTestConfig{KubeContext: "ctx1", KubeNamespace: "ns1", KubeConfig: "/path/config1"},
		},
		{
			name:              "context config multiple namespace single",
			kubeEnvList:       []KubeTestConfig{},
			expPrimaryKubeEnv: KubeTestConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := TestConfig{
				KubeEnvs: tt.kubeEnvList,
			}
			actual := cfg.GetPrimaryKubeEnv()
			require.Equal(t, tt.expPrimaryKubeEnv, actual)
		})
	}
}
