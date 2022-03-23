package helm

// HACK we should have the Helm chart itself produce a struct.
// This temporary implementation gives us only the values we need.
type Values struct {
	// Values is a map of values to set in the helm chart.
	Global Global `yaml:"global"`
}

type Global struct {
	EnterpriseLicense EnterpriseLicense `yaml:"enterpriseLicense"`
	Image             string            `yaml:"image"`
}

type EnterpriseLicense struct {
	SecretName string `yaml:"secretName"`
}
