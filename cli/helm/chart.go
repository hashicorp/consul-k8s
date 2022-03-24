package helm

import (
	"embed"
	"path/filepath"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	helmCLI "helm.sh/helm/v3/pkg/cli"
)

const (
	chartFileName    = "Chart.yaml"
	valuesFileName   = "values.yaml"
	templatesDirName = "templates"
)

// LoadChart will attempt to load a chart from the embedded file system.
func LoadChart(chart embed.FS, chartDirName string) (*chart.Chart, error) {
	chartFiles, err := ReadChartFiles(chart, chartDirName)
	if err != nil {
		return nil, err
	}

	return loader.LoadFiles(chartFiles)
}

// ReadChartFiles reads the chart files from the embedded file system, and loads their contents into
// []*loader.BufferedFile. This is a format that the Helm Go SDK functions can read from to create a chart to install
// from. The names of these files are important, as there are case statements in the Helm Go SDK looking for files named
// "Chart.yaml" or "templates/<templatename>.yaml", which is why even though the embedded file system has them named
// "consul/Chart.yaml" we have to strip the "consul" prefix out, which is done by the call to the helper method readFile.
func ReadChartFiles(chart embed.FS, chartDirName string) ([]*loader.BufferedFile, error) {
	var chartFiles []*loader.BufferedFile

	// Load Chart.yaml and values.yaml first.
	for _, f := range []string{chartFileName, valuesFileName} {
		file, err := readFile(chart, filepath.Join(chartDirName, f), chartDirName)
		if err != nil {
			return nil, err
		}
		chartFiles = append(chartFiles, file)
	}

	// Now load everything under templates/.
	dirs, err := chart.ReadDir(filepath.Join(chartDirName, templatesDirName))
	if err != nil {
		return nil, err
	}
	for _, f := range dirs {
		if f.IsDir() {
			// We only need to include files in the templates directory.
			continue
		}

		file, err := readFile(chart, filepath.Join(chartDirName, templatesDirName, f.Name()), chartDirName)
		if err != nil {
			return nil, err
		}
		chartFiles = append(chartFiles, file)
	}

	return chartFiles, nil
}

// FetchChartValues will attempt to fetch the values from the currently installed Helm chart.
func FetchChartValues(namespace, name string, settings *helmCLI.EnvSettings, uiLogger action.DebugLog) (map[string]interface{}, error) {
	cfg := new(action.Configuration)
	cfg, err := InitActionConfig(cfg, namespace, settings, uiLogger)
	if err != nil {
		return nil, err
	}

	status := action.NewStatus(cfg)
	release, err := status.Run(name)
	if err != nil {
		return nil, err
	}

	return release.Config, nil
}

func readFile(chart embed.FS, f string, pathPrefix string) (*loader.BufferedFile, error) {
	bytes, err := chart.ReadFile(f)
	if err != nil {
		return nil, err
	}
	// Remove the path prefix.
	rel, err := filepath.Rel(pathPrefix, f)
	if err != nil {
		return nil, err
	}
	return &loader.BufferedFile{
		Name: rel,
		Data: bytes,
	}, nil
}
