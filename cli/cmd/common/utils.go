package common

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
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
)

// ReadChartFiles reads the chart files from the embedded FS, and loads their contents into []*loader.BufferedFile. This
// is a format that the Helm Go SDK functions can read from to create a chart to install from. The names of these files
// are important, as there are case statements in the Helm Go SDK looking for files named "Chart.yaml" or
// "templates/<templatename>.yaml", which is why even though the embedded FS has them named "consul/Chart.yaml" we have
// to strip the "consul" prefix out.
func ReadChartFiles(chart embed.FS, chartDirName string) ([]*loader.BufferedFile, error) {
	var chartFiles []*loader.BufferedFile

	bytes, err := chart.ReadFile(fmt.Sprintf("%s/%s", chartDirName, chartFileName))
	if err != nil {
		return []*loader.BufferedFile{}, err
	}
	chartFiles = append(chartFiles,
		&loader.BufferedFile{
			Name: chartFileName,
			Data: bytes,
		},
	)

	bytes, err = chart.ReadFile(fmt.Sprintf("%s/%s", chartDirName, valuesFileName))
	if err != nil {
		return []*loader.BufferedFile{}, err
	}
	chartFiles = append(chartFiles,
		&loader.BufferedFile{
			Name: valuesFileName,
			Data: bytes,
		},
	)

	dirs, err := chart.ReadDir(fmt.Sprintf("%s/%s", chartDirName, templatesDirName))
	if err != nil {
		return []*loader.BufferedFile{}, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			// Read each template file from the embedded file system, i.e consul/templates/client-configmap.yaml, and store
			// it in a buffered file with the name templates/client-configmap.yaml. The Helm file loader expects to find
			// templates under the name "templates/*".
			bytes, err = chart.ReadFile(fmt.Sprintf("%s/%s/%s", chartDirName, templatesDirName, dir.Name()))
			if err != nil {
				return []*loader.BufferedFile{}, err
			}
			chartFiles = append(chartFiles,
				&loader.BufferedFile{
					Name: fmt.Sprintf("%s/%s", templatesDirName, dir.Name()),
					Data: bytes,
				},
			)
		}
	}

	return chartFiles, nil
}

// Abort returns true if the raw input string is not equal to "y" or "yes".
func Abort(raw string) bool {
	confirmation := strings.TrimSuffix(raw, "\n")
	if !(strings.ToLower(confirmation) == "y" || strings.ToLower(confirmation) == "yes") {
		return true
	}
	return false
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
