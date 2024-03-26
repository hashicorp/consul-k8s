// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package logger

import (
	"fmt"
	"github.com/hashicorp/consul/sdk/testutil"
	"time"

	terratesting "github.com/gruntwork-io/terratest/modules/testing"
)

// TestLogger implements Terratest's TestLogger interface so that we can pass it to Terratest objects to have consistent
// logging across all tests.
type TestLogger struct{}

// Logf takes a format string and args and calls Logf function.
func (tl TestLogger) Logf(t terratesting.TestingT, format string, args ...any) {
	tt, ok := t.(testutil.TestingTB)
	if !ok {
		t.Error("failed to cast")
	}
	tt.Helper()

	Logf(tt, format, args...)
}

// Logf takes a format string and args and logs formatted string with a timestamp.
func Logf(t testutil.TestingTB, format string, args ...any) {
	t.Helper()

	log := fmt.Sprintf(format, args...)
	Log(t, log)
}

// Log calls t.Log or r.Log, adding an RFC3339 timestamp to the beginning of the log line.
func Log(t testutil.TestingTB, args ...any) {
	t.Helper()
	allArgs := []any{time.Now().Format(time.RFC3339)}
	allArgs = append(allArgs, args...)
	t.Log(allArgs...)
}
