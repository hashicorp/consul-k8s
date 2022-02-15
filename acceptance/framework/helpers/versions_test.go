package helpers

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// versionRegEx is a regular expression that matches a valid version string (e.g. v0.40.0).
var versionRegEx = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)

func TestFetchLatestConsulVersion(t *testing.T) {
	version, err := FetchLatestConsulVersion()
	require.NoError(t, err, "FetchLatestConsulVersion should not error")
	require.NotEmpty(t, versionRegEx.Find([]byte(version)), "FetchLatestConsulVersion should return a valid version")
}

func TestFetchPreviousConsulVersion(t *testing.T) {
	version, err := FetchPreviousConsulVersion()
	require.NoError(t, err, "FetchPreviousConsulVersion should not error")
	require.NotEmpty(t, versionRegEx.Find([]byte(version)), "FetchPreviousConsulVersion should return a valid version")
}

func TestFetchLatestControlPlaneVersion(t *testing.T) {
	version, err := FetchLatestControlPlaneVersion()
	require.NoError(t, err, "FetchLatestControlPlaneVersion should not error")
	require.NotEmpty(t, versionRegEx.Find([]byte(version)), "FetchLatestControlPlaneVersion should return a valid version")
}

func TestFetchPreviousControlPlaneVersion(t *testing.T) {
	version, err := FetchPreviousControlPlaneVersion()
	require.NoError(t, err, "FetchPreviousControlPlaneVersion should not error")
	require.NotEmpty(t, versionRegEx.Find([]byte(version)), "FetchPreviousControlPlaneVersion should return a valid version")
}
