package framework

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"gopkg.in/yaml.v2"
)

// TestConfig holds configuration for the test suite
type TestConfig struct {
	Kubeconfig    string
	KubeContext   string
	KubeNamespace string

	EnableMultiCluster     bool
	SecondaryKubeconfig    string
	SecondaryKubeContext   string
	SecondaryKubeNamespace string

	EnableEnterprise            bool
	EnterpriseLicenseSecretName string
	EnterpriseLicenseSecretKey  string

	EnableOpenshift bool

	ConsulImage    string
	ConsulK8SImage string

	NoCleanupOnFailure bool
	DebugDirectory     string

	helmChartPath string
}

// HelmValuesFromConfig returns a map of Helm values
// that includes any non-empty values from the TestConfig
func (t *TestConfig) HelmValuesFromConfig() (map[string]string, error) {
	helmValues := map[string]string{}

	// Set the enterprise image first if enterprise tests are enabled.
	// It can be overwritten by the -consul-image flag later.
	if t.EnableEnterprise {
		entImage, err := t.entImage()
		if err != nil {
			return nil, err
		}
		setIfNotEmpty(helmValues, "global.image", entImage)
	}

	if t.EnterpriseLicenseSecretName != "" && t.EnterpriseLicenseSecretKey != "" {
		setIfNotEmpty(helmValues, "server.enterpriseLicense.secretName", t.EnterpriseLicenseSecretName)
		setIfNotEmpty(helmValues, "server.enterpriseLicense.secretKey", t.EnterpriseLicenseSecretKey)
	}

	if t.EnableOpenshift {
		setIfNotEmpty(helmValues, "global.openshift.enabled", "true")
	}

	setIfNotEmpty(helmValues, "global.image", t.ConsulImage)
	setIfNotEmpty(helmValues, "global.imageK8S", t.ConsulK8SImage)

	return helmValues, nil
}

// entImage parses out consul version from Chart.yaml
// and sets global.image to the consul enterprise image with that version.
func (t *TestConfig) entImage() (string, error) {
	if t.helmChartPath == "" {
		t.helmChartPath = helmChartPath
	}

	// Unmarshal Chart.yaml to get appVersion (i.e. Consul version)
	chart, err := ioutil.ReadFile(filepath.Join(t.helmChartPath, "Chart.yaml"))
	if err != nil {
		return "", err
	}

	var chartMap map[string]interface{}
	err = yaml.Unmarshal(chart, &chartMap)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("hashicorp/consul-enterprise:%s-ent", chartMap["appVersion"]), nil
}

// setIfNotEmpty sets key to val in map m if value is not empty
func setIfNotEmpty(m map[string]string, key, val string) {
	if val != "" {
		m[key] = val
	}
}
