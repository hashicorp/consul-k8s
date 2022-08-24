package config

import (
	"fmt"
	"io/ioutil"
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
				"connectInject.enabled":                         "true",
				"connectInject.cni.enabled":                     "true",
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
			consulImage: "hashicorp/consul:1.9.0",
			expImage:    "hashicorp/consul-enterprise:1.9.0-ent",
		},
		{
			consulImage: "hashicorp/consul:1.8.5-rc1",
			expImage:    "hashicorp/consul-enterprise:1.8.5-ent-rc1",
		},
		{
			consulImage: "hashicorp/consul:1.7.0-beta3",
			expImage:    "hashicorp/consul-enterprise:1.7.0-ent-beta3",
		},
		{
			consulImage: "invalid",
			expErr:      "could not determine consul version from global.image: invalid",
		},
		{
			consulImage: "hashicorp/consul@sha256:oioi2452345kjhlkh",
			expImage:    "hashicorp/consul@sha256:oioi2452345kjhlkh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.consulImage, func(t *testing.T) {
			// Write values.yaml to a temp dir which will then get parsed.
			valuesYAML := fmt.Sprintf(`global:
  image: %s
`, tt.consulImage)
			tmp, err := ioutil.TempDir("", "")
			require.NoError(t, err)
			defer os.RemoveAll(tmp)
			require.NoError(t, ioutil.WriteFile(filepath.Join(tmp, "values.yaml"), []byte(valuesYAML), 0644))

			cfg := TestConfig{
				EnableEnterprise: true,
				helmChartPath:    tmp,
			}
			values, err := cfg.HelmValuesFromConfig()
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.Contains(t, values["global.image"], tt.expImage)
			}
		})
	}
}
