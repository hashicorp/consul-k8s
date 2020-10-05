package framework

import (
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
	cfg := TestConfig{
		EnableEnterprise: true,
		// We need to set a different path because these tests are run from a different directory.
		helmChartPath: "../../..",
	}
	values, err := cfg.HelmValuesFromConfig()
	require.NoError(t, err)
	require.Contains(t, values["global.image"], "hashicorp/consul-enterprise")
}
