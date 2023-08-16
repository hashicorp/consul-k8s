package helm

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"
)

// Embed a test chart to test against.
//
//go:embed test_fixtures/consul/* test_fixtures/consul/templates/_helpers.tpl
var testChartFiles embed.FS

func TestLoadChart(t *testing.T) {
	directory := "test_fixtures/consul"

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
	directory := "test_fixtures/consul"
	expectedFiles := map[string]string{
		"Chart.yaml":             "# This is a mock Helm Chart.yaml file used for testing.\napiVersion: v2\nname: Foo\nversion: 0.1.0\ndescription: Mock Helm Chart for testing.",
		"values.yaml":            "# This is a mock Helm values.yaml file used for testing.\nkey: value",
		"templates/_helpers.tpl": "helpers",
		"templates/foo.yaml":     "foo: bar\n",
	}

	files, err := readChartFiles(testChartFiles, directory)
	require.NoError(t, err)

	actualFiles := make(map[string]string, len(files))
	for _, f := range files {
		actualFiles[f.Name] = string(f.Data)
	}

	for expectedName, expectedContents := range expectedFiles {
		actualContents, ok := actualFiles[expectedName]
		require.True(t, ok, "Expected file %s not found", expectedName)
		require.Equal(t, expectedContents, actualContents)
	}
}
