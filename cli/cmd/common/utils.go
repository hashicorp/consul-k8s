package common

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	helmCLI "helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

const (
	DefaultReleaseName      = "consul"
	DefaultReleaseNamespace = "consul"
	chartFileName           = "Chart.yaml"
	valuesFileName          = "values.yaml"
	templatesDirName        = "templates"
	TopLevelChartDirName    = "consul"

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

// InitActionConfig initializes a Helm Go SDK action configuration. This function currently uses a hack to override the
// namespace field that gets set in the K8s client set up by the SDK.
func InitActionConfig(actionConfig *action.Configuration, namespace string, settings *helmCLI.EnvSettings, logger action.DebugLog) (*action.Configuration, error) {
	getter := settings.RESTClientGetter()
	configFlags := getter.(*genericclioptions.ConfigFlags)
	configFlags.Namespace = &namespace
	err := actionConfig.Init(settings.RESTClientGetter(), namespace,
		os.Getenv("HELM_DRIVER"), logger)
	if err != nil {
		return nil, fmt.Errorf("error setting up helm action configuration to find existing installations: %s", err)
	}
	return actionConfig, nil
}

// CheckForInstallations uses the helm Go SDK to find helm releases in all namespaces where the chart name is
// "consul", and returns the release name and namespace if found, or an error if not found.
func CheckForInstallations(settings *helmCLI.EnvSettings, uiLogger action.DebugLog) (string, string, error) {
	// Need a specific action config to call helm list, where namespace is NOT specified.
	listConfig := new(action.Configuration)
	if err := listConfig.Init(settings.RESTClientGetter(), "",
		os.Getenv("HELM_DRIVER"), uiLogger); err != nil {
		return "", "", fmt.Errorf("couldn't initialize helm config: %s", err)
	}

	lister := action.NewList(listConfig)
	lister.AllNamespaces = true
	lister.StateMask = action.ListAll
	res, err := lister.Run()
	if err != nil {
		return "", "", fmt.Errorf("couldn't check for installations: %s", err)
	}

	for _, rel := range res {
		if rel.Chart.Metadata.Name == "consul" {
			return rel.Name, rel.Namespace, nil
		}
	}
	return "", "", errors.New("couldn't find consul installation")
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
