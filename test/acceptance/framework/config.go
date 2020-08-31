package framework

// TestConfig holds configuration for the test suite
type TestConfig struct {
	Kubeconfig    string
	KubeContext   string
	KubeNamespace string

	EnableMultiCluster     bool
	SecondaryKubeconfig    string
	SecondaryKubeContext   string
	SecondaryKubeNamespace string

	ConsulImage    string
	ConsulK8SImage string

	NoCleanupOnFailure bool
	DebugDirectory     string
}

// HelmValuesFromConfig returns a map of Helm values
// that includes any non-empty values from the TestConfig
func (t *TestConfig) HelmValuesFromConfig() map[string]string {
	helmValues := map[string]string{}

	setIfNotEmpty(helmValues, "global.image", t.ConsulImage)
	setIfNotEmpty(helmValues, "global.imageK8S", t.ConsulK8SImage)

	return helmValues
}

// setIfNotEmpty sets key to val in map m if value is not empty
func setIfNotEmpty(m map[string]string, key, val string) {
	if val != "" {
		m[key] = val
	}
}
