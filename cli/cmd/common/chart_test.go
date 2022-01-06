package common

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"
)

// Embed a test chart to test against.
//go:embed fixtures/consul/* fixtures/consul/templates/_helpers.tpl
var testChartFiles embed.FS

func TestLoadChart(t *testing.T) {
	directory := "fixtures/consul"

	expectedApiVersion := "v2"
	expectedName := "Foo"
	expectedVersion := "0.1.0"
	expectedDescription := "Mock Helm Chart for testing."
	expectedValues := map[string]interface{}{
		"key": "value",
	}

	actual, err := LoadChart(testChartFiles, directory)
	require.NoError(t, err)
	require.Equal(t, expectedApiVersion, actual.Metadata.APIVersion)
	require.Equal(t, expectedName, actual.Metadata.Name)
	require.Equal(t, expectedVersion, actual.Metadata.Version)
	require.Equal(t, expectedDescription, actual.Metadata.Description)
	require.Equal(t, expectedValues, actual.Values)
}

func TestReadChartFiles(t *testing.T) {
	directory := "fixtures/consul"
	expectedFileNames := []string{"Chart.yaml", "values.yaml", "templates/_helpers.tpl", "templates/foo.yaml"}

	files, err := ReadChartFiles(testChartFiles, directory)
	require.NoError(t, err)

	actualFileNames := make([]string, len(files))
	for i, f := range files {
		actualFileNames[i] = f.Name
	}

	for _, expectedFileName := range expectedFileNames {
		require.Contains(t, actualFileNames, expectedFileName)
	}
}
