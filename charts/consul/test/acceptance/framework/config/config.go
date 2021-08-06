package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

// HelmChartPath is the path to the helm chart.
// Note: this will need to be changed if this file is moved.
const (
	HelmChartPath     = "../../../.."
	LicenseSecretName = "license"
	LicenseSecretKey  = "key"
)

// TestConfig holds configuration for the test suite.
type TestConfig struct {
	Kubeconfig    string
	KubeContext   string
	KubeNamespace string

	EnableMultiCluster     bool
	SecondaryKubeconfig    string
	SecondaryKubeContext   string
	SecondaryKubeNamespace string

	EnableEnterprise  bool
	EnterpriseLicense string

	EnableOpenshift bool

	EnablePodSecurityPolicies bool

	EnableTransparentProxy bool

	ConsulImage    string
	ConsulK8SImage string

	NoCleanupOnFailure bool
	DebugDirectory     string

	UseKind bool

	helmChartPath string
}

// HelmValuesFromConfig returns a map of Helm values
// that includes any non-empty values from the TestConfig.
func (t *TestConfig) HelmValuesFromConfig() (map[string]string, error) {
	helmValues := map[string]string{}

	// If Kind is being used they use a pod to provision the underlying PV which will hang if we
	// use "Fail" for the webhook failurePolicy.
	if t.UseKind {
		setIfNotEmpty(helmValues, "connectInject.failurePolicy", "Ignore")
	}
	// Set the enterprise image first if enterprise tests are enabled.
	// It can be overwritten by the -consul-image flag later.
	if t.EnableEnterprise {
		entImage, err := t.entImage()
		if err != nil {
			return nil, err
		}
		setIfNotEmpty(helmValues, "global.image", entImage)
	}

	if t.EnterpriseLicense != "" {
		setIfNotEmpty(helmValues, "server.enterpriseLicense.secretName", LicenseSecretName)
		setIfNotEmpty(helmValues, "server.enterpriseLicense.secretKey", LicenseSecretKey)
	}

	if t.EnableOpenshift {
		setIfNotEmpty(helmValues, "global.openshift.enabled", "true")
	}

	if t.EnablePodSecurityPolicies {
		setIfNotEmpty(helmValues, "global.enablePodSecurityPolicies", "true")
	}

	setIfNotEmpty(helmValues, "connectInject.transparentProxy.defaultEnabled", strconv.FormatBool(t.EnableTransparentProxy))

	setIfNotEmpty(helmValues, "global.image", t.ConsulImage)
	setIfNotEmpty(helmValues, "global.imageK8S", t.ConsulK8SImage)

	return helmValues, nil
}

// entImage parses out consul version from Chart.yaml
// and sets global.image to the consul enterprise image with that version.
func (t *TestConfig) entImage() (string, error) {
	if t.helmChartPath == "" {
		t.helmChartPath = HelmChartPath
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

	appVersion, ok := chartMap["appVersion"].(string)
	if !ok {
		return "", errors.New("unable to cast chartMap.appVersion to string")
	}
	var preRelease string
	// Handle versions like 1.9.0-rc1.
	if strings.Contains(appVersion, "-") {
		split := strings.Split(appVersion, "-")
		appVersion = split[0]
		preRelease = fmt.Sprintf("-%s", split[1])
	}

	return fmt.Sprintf("hashicorp/consul-enterprise:%s-ent%s", appVersion, preRelease), nil
}

// setIfNotEmpty sets key to val in map m if value is not empty.
func setIfNotEmpty(m map[string]string, key, val string) {
	if val != "" {
		m[key] = val
	}
}
