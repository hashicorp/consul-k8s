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
			map[string]string{},
		},
		{
			"sets consul image",
			TestConfig{
				ConsulImage: "consul:test-version",
			},
			map[string]string{"global.image": "consul:test-version"},
		},
		{
			"sets consul-k8s image",
			TestConfig{
				ConsulK8SImage: "consul-k8s:test-version",
			},
			map[string]string{"global.imageK8S": "consul-k8s:test-version"},
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
			},
		},
		{
			"sets ent license secret",
			TestConfig{
				EnterpriseLicenseSecretName: "ent-license",
				EnterpriseLicenseSecretKey:  "key",
			},
			map[string]string{
				"server.enterpriseLicense.secretName": "ent-license",
				"server.enterpriseLicense.secretKey":  "key",
			},
		},
		{
			"doesn't set ent license secret when only secret name is set",
			TestConfig{
				EnterpriseLicenseSecretName: "ent-license",
			},
			map[string]string{},
		},
		{
			"doesn't set ent license secret when only secret key is set",
			TestConfig{
				EnterpriseLicenseSecretKey: "key",
			},
			map[string]string{},
		},
		{
			"sets openshift value when EnableOpenshift is set",
			TestConfig{
				EnableOpenshift: true,
			},
			map[string]string{
				"global.openshift.enabled": "true",
			},
		},
		{
			"sets enablePodSecurityPolicies helm value when -enable-pod-security-policies is set",
			TestConfig{
				EnablePodSecurityPolicies: true,
			},
			map[string]string{
				"global.enablePodSecurityPolicies": "true",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := tt.testConfig.HelmValuesFromConfig()
			require.NoError(t, err)
			require.Equal(t, values, tt.want)
		})
	}
}

func TestConfig_HelmValuesFromConfig_EntImage(t *testing.T) {
	tests := []struct {
		appVersion string
		expImage   string
		expErr     string
	}{
		{
			appVersion: "1.9.0",
			expImage:   "hashicorp/consul-enterprise:1.9.0-ent",
		},
		{
			appVersion: "1.8.5-rc1",
			expImage:   "hashicorp/consul-enterprise:1.8.5-ent-rc1",
		},
		{
			appVersion: "1.7.0-beta3",
			expImage:   "hashicorp/consul-enterprise:1.7.0-ent-beta3",
		},
		{
			appVersion: "1",
			expErr:     "unable to cast chartMap.appVersion to string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.appVersion, func(t *testing.T) {

			// Write Chart.yaml to a temp dir which will then get parsed.
			chartYAML := fmt.Sprintf(`apiVersion: v1
name: consul
appVersion: %s
`, tt.appVersion)
			tmp, err := ioutil.TempDir("", "")
			require.NoError(t, err)
			defer os.RemoveAll(tmp)
			require.NoError(t, ioutil.WriteFile(filepath.Join(tmp, "Chart.yaml"), []byte(chartYAML), 0644))

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
