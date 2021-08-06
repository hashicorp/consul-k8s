package logger

import (
	"fmt"
	"testing"
	"time"

	terratestTesting "github.com/gruntwork-io/terratest/modules/testing"
)

// TestLogger implements terratest's TestLogger interface
// so that we can pass it to terratest objects to have consistent logging
// across all tests.
type TestLogger struct{}

// Logf takes a format string and args and calls Logf function.
func (tl TestLogger) Logf(t terratestTesting.TestingT, format string, args ...interface{}) {
	tt, ok := t.(*testing.T)
	if !ok {
		t.Error("failed to cast")
	}
	tt.Helper()

	Logf(tt, format, args...)
}

// Logf takes a format string and args and logs
// formatted string with a timestamp.
func Logf(t *testing.T, format string, args ...interface{}) {
	t.Helper()

	log := fmt.Sprintf(format, args...)
	Log(t, log)
}

// Log calls t.Log, adding an RFC3339 timestamp to the beginning of the log line.
func Log(t *testing.T, args ...interface{}) {
	t.Helper()

	allArgs := []interface{}{time.Now().Format(time.RFC3339)}
	allArgs = append(allArgs, args...)
	t.Log(allArgs...)
}
