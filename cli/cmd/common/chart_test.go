package common

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart"
)

func TestLoadChart(t *testing.T) {
	// TODO: Make this work
	chartFs := embed.FS{}
	chartDirName := "testdata/testchart"

	expected := chart.Chart{}

	actual, err := LoadChart(chartFs, chartDirName)
	require.NoError(t, err)
	require.Equal(t, expected, *actual)
}

//go:embed fixtures/consul/* fixtures/consul/templates/_helpers.tpl
var testChart embed.FS

func TestReadChartFiles(t *testing.T) {
	files, err := ReadChartFiles(testChart, "fixtures/consul")
	require.NoError(t, err)
	var foundChart, foundValues, foundTemplate, foundHelper bool
	for _, f := range files {
		if f.Name == "Chart.yaml" {
			require.Equal(t, "chart", string(f.Data))
			foundChart = true
		}
		if f.Name == "values.yaml" {
			require.Equal(t, "values", string(f.Data))
			foundValues = true
		}
		if f.Name == "templates/foo.yaml" {
			require.Equal(t, "foo", string(f.Data))
			foundTemplate = true
		}
		if f.Name == "templates/_helpers.tpl" {
			require.Equal(t, "helpers", string(f.Data))
			foundHelper = true
		}
	}
	require.True(t, foundChart)
	require.True(t, foundValues)
	require.True(t, foundTemplate)
	require.True(t, foundHelper)
}
