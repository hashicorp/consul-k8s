package common

import (
	"os"
	"strings"
)

const (
	DefaultReleaseName       = "consul"
	DefaultReleaseNamespace  = "consul"
	ConsulDemoAppReleaseName = "consul-demo"
	TopLevelChartDirName     = "consul"
	ReleaseTypeConsul        = "Consul"
	ReleaseTypeConsulDemo    = "Consul demo application"

	// CLILabelKey and CLILabelValue are added to each secret on creation so the CLI knows
	// which key to delete on an uninstall.
	CLILabelKey   = "managed-by"
	CLILabelValue = "consul-k8s"
)

// Abort returns true if the raw input string is not equal to "y" or "yes".
func Abort(raw string) bool {
	confirmation := strings.TrimSuffix(raw, "\n")
	return !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes")
}

// MergeMaps merges two maps giving b precedent.
// @source: https://github.com/helm/helm/blob/main/pkg/cli/values/options.go
func MergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = MergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// CloseWithError terminates a command and cleans up its resources.
// If termination fails, the error is logged and the process exits with an
// exit code of 1.
func CloseWithError(c *BaseCommand) {
	if err := c.Close(); err != nil {
		c.Log.Error(err.Error())
		os.Exit(1)
	}
}

// IsValidLabel checks if a given label conforms to RFC 1123 https://datatracker.ietf.org/doc/html/rfc1123.
// This standard requires that the label begins and ends with an alphanumeric character, does not exceed 63 characters,
// and contains only alphanumeric characters and '-'.
func IsValidLabel(label string) bool {
	if len(label) > 63 || len(label) == 0 {
		return false
	}

	for i, c := range label {
		isAlphaNumeric := c >= '0' && c <= '9' || c >= 'a' && c <= 'z'
		isTerminal := i == 0 || i == len(label)-1

		// First and last character must be alphanumeric.
		if isTerminal && !isAlphaNumeric {
			return false
		}

		// All other characters must be alphanumeric or '-'.
		if !isAlphaNumeric && c != '-' {
			return false
		}
	}

	return true
}
