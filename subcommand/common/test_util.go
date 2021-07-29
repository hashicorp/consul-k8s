package common

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// WriteTempFile writes contents to a temporary file and returns the file
// name. It will remove the file once the test completes.
func WriteTempFile(t *testing.T, contents string) string {
	t.Helper()
	file, err := ioutil.TempFile("", "testName")
	require.NoError(t, err)
	_, err = file.WriteString(contents)
	require.NoError(t, err)

	t.Cleanup(func() {
		os.Remove(file.Name())
	})
	return file.Name()
}
