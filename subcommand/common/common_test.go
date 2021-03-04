package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogger_InvalidLogLevel(t *testing.T) {
	_, err := Logger("invalid")
	require.EqualError(t, err, "unknown log level: invalid")
}

func TestLogger(t *testing.T) {
	lgr, err := Logger("debug")
	require.NoError(t, err)
	require.NotNil(t, lgr)
	require.True(t, lgr.IsDebug())
}

func TestValidateUnprivilegedPort(t *testing.T) {
	err := ValidateUnprivilegedPort("-test-flag-name", "1234")
	require.NoError(t, err)
	err = ValidateUnprivilegedPort("-test-flag-name", "invalid-port")
	require.EqualError(t, err, "-test-flag-name value of invalid-port is not a valid integer")
	err = ValidateUnprivilegedPort("-test-flag-name", "22")
	require.EqualError(t, err, "-test-flag-name value of 22 is not in the unprivileged port range 1024-65535")
}
